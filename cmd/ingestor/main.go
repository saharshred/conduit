// Command ingestor consumes normalized transactions off RabbitMQ and
// writes them into Postgres, deduping via internal/ingest +
// internal/pgingest. It also deliberately injects two kinds of failure so
// the retry/dead-letter path in internal/queue is provably exercised, not
// just theoretical:
//
//   - a random ~8% transient failure on any transaction — these succeed on
//     retry (RabbitMQ's retry queue redelivers them a couple seconds later)
//   - a deterministic "poison" transaction (native ID ending in "9999")
//     that always fails — these exhaust all retries and land on the
//     dead-letter queue, where a human (or the dashboard, Day 5) has to
//     look at them
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os/signal"
	"strings"
	"syscall"

	"github.com/saharshred/conduit/internal/ingest"
	"github.com/saharshred/conduit/internal/pgingest"
	"github.com/saharshred/conduit/internal/queue"
	"github.com/saharshred/conduit/internal/schema"
)

func main() {
	amqpURL := flag.String("rabbitmq", "amqp://guest:guest@localhost:5673/", "RabbitMQ URL")
	dsn := flag.String("postgres", "postgres://conduit:conduit@localhost:5434/conduit?sslmode=disable", "Postgres DSN")
	injectFailures := flag.Bool("inject-failures", true, "deliberately fail some deliveries to exercise retry + dead-letter")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := pgingest.Open(ctx, *dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	pipeline := ingest.New(store)

	conn, err := queue.DialWithRetry(*amqpURL, 10)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		log.Fatal(err)
	}
	defer ch.Close()
	if err := queue.Topology(ch); err != nil {
		log.Fatal(err)
	}

	var accepted, duplicate, transientFail, poisonFail int

	handler := func(ctx context.Context, txn schema.Transaction) error {
		if *injectFailures {
			if strings.HasSuffix(txn.NativeID, "9999") {
				poisonFail++
				return fmt.Errorf("simulated poison transaction: %s", txn.IdempotencyKey())
			}
			if rand.Intn(100) < 8 {
				transientFail++
				return fmt.Errorf("simulated transient failure: %s", txn.IdempotencyKey())
			}
		}

		ok, err := pipeline.Accept(ctx, txn)
		if err != nil {
			return err
		}
		if !ok {
			duplicate++
			return nil
		}
		if err := store.Insert(ctx, txn); err != nil {
			return err
		}
		accepted++
		if accepted%200 == 0 {
			log.Printf("ingestor: accepted=%d duplicate=%d transient-retries=%d poisoned=%d", accepted, duplicate, transientFail, poisonFail)
		}
		return nil
	}

	log.Println("ingestor: consuming conduit.transactions (Ctrl-C to stop)")
	if err := queue.Consume(ctx, ch, handler); err != nil {
		log.Fatal(err)
	}

	log.Printf("ingestor: final tally — accepted=%d duplicate=%d transient-retries=%d poisoned-to-dlq=%d",
		accepted, duplicate, transientFail, poisonFail)
}

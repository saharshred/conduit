// Command normalizer fetches transaction history from all three mock banks
// (see cmd/mockbanks), normalizes each into the unified schema, dedupes the
// pagination-overlap quirk within a bank (internal/normalize already does
// this), and publishes every transaction to RabbitMQ (internal/queue) for
// cmd/ingestor to pick up.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/saharshred/conduit/internal/normalize"
	"github.com/saharshred/conduit/internal/queue"
)

func main() {
	bankA := flag.String("bank-a", "http://localhost:8081", "bank-a base URL")
	bankB := flag.String("bank-b", "http://localhost:8082", "bank-b base URL")
	bankC := flag.String("bank-c", "http://localhost:8083", "bank-c base URL")
	amqpURL := flag.String("rabbitmq", "amqp://guest:guest@localhost:5673/", "RabbitMQ URL")
	flag.Parse()

	sources := []*normalize.Client{
		normalize.New("bank-a", *bankA, normalize.MapperBankA),
		normalize.New("bank-b", *bankB, normalize.MapperBankB),
		normalize.New("bank-c", *bankC, normalize.MapperBankC),
	}

	ctx := context.Background()

	conn, err := amqp.Dial(*amqpURL)
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
	pub := queue.NewPublisher(ch)

	var totalFetched, totalPublished int
	for _, c := range sources {
		txns, err := c.FetchAll(ctx)
		if err != nil {
			log.Fatalf("%s: %v", c.BankName, err)
		}
		totalFetched += len(txns)

		for _, txn := range txns {
			if err := pub.Publish(ctx, txn); err != nil {
				log.Fatalf("%s: publishing %s: %v", c.BankName, txn.IdempotencyKey(), err)
			}
			totalPublished++
		}
		fmt.Printf("%-8s fetched=%-6d published=%d\n", c.BankName, len(txns), len(txns))
	}

	fmt.Printf("\ntotal: fetched=%d published=%d\n", totalFetched, totalPublished)
}

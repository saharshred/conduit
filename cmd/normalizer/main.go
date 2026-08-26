// Command normalizer fetches transaction history from all three mock banks
// (see cmd/mockbanks), normalizes each into the unified schema, dedupes
// (both within a bank's own pagination overlap and, via internal/ingest,
// across any redelivery), and prints a summary.
//
// Day 3 wires this into RabbitMQ instead of stdout; Day 4 adds the fraud
// scorer downstream of that queue.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/saharshred/conduit/internal/ingest"
	"github.com/saharshred/conduit/internal/normalize"
)

func main() {
	bankA := flag.String("bank-a", "http://localhost:8081", "bank-a base URL")
	bankB := flag.String("bank-b", "http://localhost:8082", "bank-b base URL")
	bankC := flag.String("bank-c", "http://localhost:8083", "bank-c base URL")
	flag.Parse()

	sources := []*normalize.Client{
		normalize.New("bank-a", *bankA, normalize.MapperBankA),
		normalize.New("bank-b", *bankB, normalize.MapperBankB),
		normalize.New("bank-c", *bankC, normalize.MapperBankC),
	}

	pipeline := ingest.New(ingest.NewMemStore())
	ctx := context.Background()

	var totalFetched, totalAccepted, totalDuplicate int
	for _, c := range sources {
		txns, err := c.FetchAll(ctx)
		if err != nil {
			log.Fatalf("%s: %v", c.BankName, err)
		}
		totalFetched += len(txns)

		var accepted, duplicate int
		for _, txn := range txns {
			ok, err := pipeline.Accept(ctx, txn)
			if err != nil {
				log.Fatalf("%s: ingest: %v", c.BankName, err)
			}
			if ok {
				accepted++
			} else {
				duplicate++
			}
		}
		totalAccepted += accepted
		totalDuplicate += duplicate
		fmt.Printf("%-8s fetched=%-6d accepted=%-6d duplicate=%d\n", c.BankName, len(txns), accepted, duplicate)
	}

	fmt.Printf("\ntotal: fetched=%d accepted=%d duplicate=%d\n", totalFetched, totalAccepted, totalDuplicate)
}

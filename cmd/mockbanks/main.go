// Command mockbanks serves three mock bank APIs over HTTP, each backed by a
// pre-generated dataset (see scripts/gen-dataset) and each tuned to make one
// failure quirk dominant:
//
//	:8081  bank-a  flaky auth    — tokens expire after a handful of requests
//	:8082  bank-b  rate limited  — hard cap on requests per second
//	:8083  bank-c  odd pagination — every 4th page overlaps the previous one
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/saharshred/conduit/internal/banks"
)

func main() {
	dataDir := "data/"
	if len(os.Args) > 1 {
		dataDir = os.Args[1]
	}

	start("bank-a", ":8081", dataDir+"bank-a.json", banks.Config{
		Name:             "bank-a",
		TokenTTLRequests: 8,
		PageSize:         25,
	})
	start("bank-b", ":8082", dataDir+"bank-b.json", banks.Config{
		Name:               "bank-b",
		RateLimitPerSecond: 5,
		PageSize:           25,
	})
	start("bank-c", ":8083", dataDir+"bank-c.json", banks.Config{
		Name:            "bank-c",
		PaginationQuirk: true,
		PageSize:        25,
	})

	select {} // servers run in goroutines below; block forever
}

func start(name, addr, dataFile string, cfg banks.Config) {
	records, err := loadRecords(dataFile)
	if err != nil {
		log.Fatalf("%s: loading %s: %v", name, dataFile, err)
	}
	srv := banks.NewServer(cfg, records)
	log.Printf("%s listening on %s (%d transactions loaded from %s)", name, addr, len(records), dataFile)
	go func() {
		log.Fatal(http.ListenAndServe(addr, srv.Handler()))
	}()
}

func loadRecords(path string) ([]banks.NativeTxn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var records []banks.NativeTxn
	if err := json.NewDecoder(f).Decode(&records); err != nil {
		return nil, err
	}
	return records, nil
}

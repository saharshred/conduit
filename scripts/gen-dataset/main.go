// Command gen-dataset writes N weeks of synthetic transactions per mock
// bank, in each bank's own native field format, to JSON files under data/.
// Same trick as SPREAD: generate history once, replay it fast, instead of
// waiting weeks for real data to accumulate.
//
// Usage:
//
//	go run ./scripts/gen-dataset -weeks 6 -out data/
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

var merchants = []struct {
	name string
	mcc  string
}{
	{"Corner Coffee", "5814"},
	{"Metro Transit", "4111"},
	{"Cloudline Hosting", "7372"},
	{"Greenfield Grocers", "5411"},
	{"Union Hardware", "5251"},
	{"Riverside Pharmacy", "5912"},
}

func main() {
	weeks := flag.Int("weeks", 6, "simulated weeks of transaction history per bank")
	perDay := flag.Int("per-day", 200, "transactions per simulated day, per bank")
	outDir := flag.String("out", "data/", "output directory")
	seed := flag.Int64("seed", 7, "RNG seed")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	rng := rand.New(rand.NewSource(*seed))
	days := *weeks * 7
	start := time.Now().AddDate(0, 0, -days)

	writeBankA(*outDir, days, *perDay, start, rng)
	writeBankB(*outDir, days, *perDay, start, rng)
	writeBankC(*outDir, days, *perDay, start, rng)
}

func randomTxnFields(rng *rand.Rand) (amountCents int64, merchant string, mcc string) {
	m := merchants[rng.Intn(len(merchants))]
	return int64(500 + rng.Intn(20000)), m.name, m.mcc
}

func writeBankA(outDir string, days, perDay int, start time.Time, rng *rand.Rand) {
	var records []map[string]any
	n := 0
	for d := 0; d < days; d++ {
		for i := 0; i < perDay; i++ {
			amountCents, merchant, mcc := randomTxnFields(rng)
			ts := start.AddDate(0, 0, d).Add(time.Duration(rng.Intn(24*60)) * time.Minute)
			n++
			records = append(records, map[string]any{
				"tx_id":    fmt.Sprintf("a-%d", n),
				"acct":     fmt.Sprintf("acct-%d", 1+rng.Intn(25)),
				"amount":   float64(amountCents) / 100.0, // bank-a: dollars, not cents
				"merchant": merchant,
				"mcc":      mcc,
				"ts":       ts.Format(time.RFC3339),
			})
		}
	}
	writeJSON(filepath.Join(outDir, "bank-a.json"), records)
}

func writeBankB(outDir string, days, perDay int, start time.Time, rng *rand.Rand) {
	var records []map[string]any
	n := 0
	for d := 0; d < days; d++ {
		for i := 0; i < perDay; i++ {
			amountCents, merchant, mcc := randomTxnFields(rng)
			ts := start.AddDate(0, 0, d).Add(time.Duration(rng.Intn(24*60)) * time.Minute)
			n++
			records = append(records, map[string]any{
				"id":             fmt.Sprintf("b-%d", n),
				"account_number": fmt.Sprintf("acct-%d", 1+rng.Intn(25)),
				"amount_cents":   amountCents,
				"desc":           merchant,
				"category_code":  mcc,
				"posted_at":      ts.Format(time.RFC3339),
			})
		}
	}
	writeJSON(filepath.Join(outDir, "bank-b.json"), records)
}

func writeBankC(outDir string, days, perDay int, start time.Time, rng *rand.Rand) {
	var records []map[string]any
	n := 0
	for d := 0; d < days; d++ {
		for i := 0; i < perDay; i++ {
			amountCents, merchant, mcc := randomTxnFields(rng)
			ts := start.AddDate(0, 0, d).Add(time.Duration(rng.Intn(24*60)) * time.Minute)
			n++
			records = append(records, map[string]any{
				"uuid":     fmt.Sprintf("c-%d", n),
				"account":  fmt.Sprintf("acct-%d", 1+rng.Intn(25)),
				"cents":    amountCents,
				"payee":    merchant,
				"category": mcc,
				"when":     ts.Format(time.RFC3339),
			})
		}
	}
	writeJSON(filepath.Join(outDir, "bank-c.json"), records)
}

func writeJSON(path string, records []map[string]any) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(records); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %d records to %s\n", len(records), path)
}

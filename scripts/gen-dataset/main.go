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

// Must match services/scorer/lib/scorer/geo.ex's coordinate table exactly
// — the scorer can't flag impossible travel between cities it doesn't
// have coordinates for.
var cities = []string{
	"New York", "Chicago", "Los Angeles", "Miami", "Seattle",
	"Austin", "Denver", "Boston", "London", "Tokyo",
}

// homeCity is a pure function of account number so every bank's dataset
// agrees on which city a given account "lives" in, even though each bank
// is generated independently.
func homeCity(acctNum int) string {
	return cities[acctNum%len(cities)]
}

// dayCity decides where an account "is" for a given simulated day —
// deterministically, from (account, day) alone, not from the shared rng
// stream. That matters: bank-a, bank-b, and bank-c are generated as three
// independent passes, but they're supposed to be describing the same
// person. An earlier version rolled a fresh random city per *transaction*
// independently in each bank's loop, so the same account could "be" in
// Chicago for a bank-a transaction and Tokyo for a bank-b transaction
// four minutes later purely because the three RNG streams disagreed —
// which made geo_impossible fire on ~7% of all transactions instead of
// catching genuine anomalies. Keying off (account, day) with a local seed
// means all three banks agree on where the account was that day.
func dayCity(acctNum, day int) string {
	local := rand.New(rand.NewSource(int64(acctNum)*100_000 + int64(day)))
	home := homeCity(acctNum)
	if local.Intn(100) >= 5 {
		return home
	}
	for {
		c := cities[local.Intn(len(cities))]
		if c != home {
			return c
		}
	}
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
			acctNum := 1 + rng.Intn(25)
			ts := start.AddDate(0, 0, d).Add(time.Duration(rng.Intn(24*60)) * time.Minute)
			n++
			records = append(records, map[string]any{
				"tx_id":    fmt.Sprintf("a-%d", n),
				"acct":     fmt.Sprintf("acct-%d", acctNum),
				"amount":   float64(amountCents) / 100.0, // bank-a: dollars, not cents
				"merchant": merchant,
				"mcc":      mcc,
				"city":     dayCity(acctNum, d),
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
			acctNum := 1 + rng.Intn(25)
			ts := start.AddDate(0, 0, d).Add(time.Duration(rng.Intn(24*60)) * time.Minute)
			n++
			records = append(records, map[string]any{
				"id":             fmt.Sprintf("b-%d", n),
				"account_number": fmt.Sprintf("acct-%d", acctNum),
				"amount_cents":   amountCents,
				"desc":           merchant,
				"category_code":  mcc,
				"city_name":      dayCity(acctNum, d),
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
			acctNum := 1 + rng.Intn(25)
			ts := start.AddDate(0, 0, d).Add(time.Duration(rng.Intn(24*60)) * time.Minute)
			n++
			records = append(records, map[string]any{
				"uuid":     fmt.Sprintf("c-%d", n),
				"account":  fmt.Sprintf("acct-%d", acctNum),
				"cents":    amountCents,
				"payee":    merchant,
				"category": mcc,
				"loc":      dayCity(acctNum, d),
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

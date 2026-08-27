// Package schema is the unified transaction shape every bank's data gets
// normalized into. This is the actual product problem CONDUIT is modeling:
// three sources with three different native formats and failure quirks, one
// consistent shape coming out the other side.
package schema

import (
	"encoding/json"
	"fmt"
	"time"
)

// Transaction is the normalized shape. Every field here exists in some form
// in each bank's native payload — the normalizer's job (internal/normalize)
// is mapping quirky native fields onto this consistently.
type Transaction struct {
	Bank             string          `json:"bank"`
	NativeID         string          `json:"native_id"`
	AccountID        string          `json:"account_id"`
	AmountCents      int64           `json:"amount_cents"`
	Currency         string          `json:"currency"`
	MerchantName     string          `json:"merchant_name"`
	MerchantCategory string          `json:"merchant_category"` // MCC-style code
	City             string          `json:"city"`              // where the transaction occurred — feeds the scorer's geo_impossible rule
	Timestamp        time.Time       `json:"timestamp"`
	Raw              json.RawMessage `json:"raw,omitempty"` // original payload, kept for audit
}

// IdempotencyKey is stable across retries and duplicate deliveries: the
// same native transaction from the same bank always produces the same key,
// so ingesting it twice (a retried webhook, a re-fetched page) is a dedupe,
// not a double-post. See internal/ingest for where this gets enforced.
func (t Transaction) IdempotencyKey() string {
	return fmt.Sprintf("%s:%s", t.Bank, t.NativeID)
}

func (t Transaction) Validate() error {
	if t.Bank == "" || t.NativeID == "" {
		return fmt.Errorf("schema: transaction missing bank or native_id")
	}
	if t.AmountCents == 0 {
		return fmt.Errorf("schema: transaction %s has zero amount", t.IdempotencyKey())
	}
	return nil
}

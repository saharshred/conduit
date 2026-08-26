package normalize

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/saharshred/conduit/internal/banks"
)

func fixtureTxns(n int) []banks.NativeTxn {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	out := make([]banks.NativeTxn, n)
	for i := 0; i < n; i++ {
		out[i] = banks.NativeTxn{
			"tx_id":    fmt.Sprintf("txn-%05d", i),
			"acct":     "acct-1",
			"amount":   19.99,
			"merchant": "Coffee Shop",
			"mcc":      "5814",
			"ts":       base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
		}
	}
	return out
}

func TestFetchAllSurvivesRateLimitingAndPaginationOverlap(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises real backoff sleeps against a sliding rate-limit window")
	}
	native := fixtureTxns(40)
	srv := banks.NewServer(banks.Config{
		Name:               "bank-a",
		RateLimitPerSecond: 3, // aggressive — forces the client into 429 retries
		PaginationQuirk:    true,
		PageSize:           10,
	}, native)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	c := New("bank-a", ts.URL, MapperBankA)
	c.MaxRetries = 12 // needs to outlast the 1s sliding rate-limit window a few times over
	got, err := c.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(got) != len(native) {
		t.Fatalf("expected %d deduped transactions, got %d (pagination overlap not deduped, or records lost)", len(native), len(got))
	}
}

func TestFetchAllSurvivesTokenExpiryMidRun(t *testing.T) {
	native := fixtureTxns(60)
	srv := banks.NewServer(banks.Config{
		Name:             "bank-a",
		TokenTTLRequests: 2, // token dies almost immediately — forces repeated re-auth
		PageSize:         10,
	}, native)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	c := New("bank-a", ts.URL, MapperBankA)
	got, err := c.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(got) != len(native) {
		t.Fatalf("expected %d transactions despite token expiry, got %d", len(native), len(got))
	}
}

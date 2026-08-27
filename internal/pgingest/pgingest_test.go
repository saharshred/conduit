// Integration test against a real Postgres instance. Requires
// CONDUIT_TEST_DSN to be set. Skips cleanly otherwise so `go test ./...`
// still passes without Docker running.
package pgingest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/saharshred/conduit/internal/schema"
)

func testStore(t *testing.T) *Store {
	dsn := os.Getenv("CONDUIT_TEST_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_DSN not set — skipping Postgres integration test")
	}
	ctx := context.Background()

	schemaSQL, err := os.ReadFile("../../migrations/0001_ingest.sql")
	if err != nil {
		t.Fatalf("reading migration: %v", err)
	}
	if err := Migrate(ctx, dsn, string(schemaSQL)); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(s.Close)

	if _, err := s.pool.Exec(ctx, `TRUNCATE transactions RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return s
}

func sampleTxn(nativeID string) schema.Transaction {
	return schema.Transaction{
		Bank:             "bank-a",
		NativeID:         nativeID,
		AccountID:        "acct-1",
		AmountCents:      1999,
		Currency:         "USD",
		MerchantName:     "Coffee Shop",
		MerchantCategory: "5814",
		Timestamp:        time.Now(),
	}
}

func TestInsertAndSeenAndMark(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	txn := sampleTxn("txn-1")

	seen, err := s.SeenAndMark(ctx, txn.IdempotencyKey())
	if err != nil || seen {
		t.Fatalf("expected unseen before insert: seen=%v err=%v", seen, err)
	}

	if err := s.Insert(ctx, txn); err != nil {
		t.Fatalf("insert: %v", err)
	}

	seen, err = s.SeenAndMark(ctx, txn.IdempotencyKey())
	if err != nil || !seen {
		t.Fatalf("expected seen after insert: seen=%v err=%v", seen, err)
	}

	n, err := s.Count(ctx)
	if err != nil || n != 1 {
		t.Fatalf("expected count 1, got %d (err=%v)", n, err)
	}
}

func TestDuplicateInsertIsANoOp(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	txn := sampleTxn("txn-dup")

	if err := s.Insert(ctx, txn); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := s.Insert(ctx, txn); err != nil {
		t.Fatalf("second insert (should be a silent no-op, not an error): %v", err)
	}

	n, err := s.Count(ctx)
	if err != nil || n != 1 {
		t.Fatalf("expected exactly 1 row despite inserting twice, got %d (err=%v)", n, err)
	}
}

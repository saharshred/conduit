package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/saharshred/conduit/internal/schema"
)

func sampleTxn() schema.Transaction {
	return schema.Transaction{
		Bank:             "bank-a",
		NativeID:         "txn-1",
		AccountID:        "acct-1",
		AmountCents:      1999,
		Currency:         "USD",
		MerchantName:     "Coffee Shop",
		MerchantCategory: "5814",
		Timestamp:        time.Now(),
	}
}

func TestDuplicateDeliveryIsRejectedNotDoublePosted(t *testing.T) {
	p := New(NewMemStore())
	ctx := context.Background()
	txn := sampleTxn()

	accepted, err := p.Accept(ctx, txn)
	if err != nil || !accepted {
		t.Fatalf("first delivery should be accepted: accepted=%v err=%v", accepted, err)
	}

	// Same native transaction delivered again — a retried webhook, or the
	// pagination-overlap quirk re-serving a record.
	accepted, err = p.Accept(ctx, txn)
	if err != nil {
		t.Fatalf("second delivery: unexpected error: %v", err)
	}
	if accepted {
		t.Fatal("duplicate delivery should not be accepted a second time")
	}
}

func TestDifferentBanksWithSameNativeIDDoNotCollide(t *testing.T) {
	p := New(NewMemStore())
	ctx := context.Background()

	a := sampleTxn()
	a.Bank = "bank-a"
	b := sampleTxn()
	b.Bank = "bank-b" // same NativeID, different bank — must be a distinct key

	if accepted, err := p.Accept(ctx, a); err != nil || !accepted {
		t.Fatalf("bank-a txn: accepted=%v err=%v", accepted, err)
	}
	if accepted, err := p.Accept(ctx, b); err != nil || !accepted {
		t.Fatalf("bank-b txn with same native ID should still be accepted: accepted=%v err=%v", accepted, err)
	}
}

func TestRejectsInvalidTransaction(t *testing.T) {
	p := New(NewMemStore())
	bad := sampleTxn()
	bad.AmountCents = 0

	if _, err := p.Accept(context.Background(), bad); err == nil {
		t.Fatal("expected validation error for zero-amount transaction")
	}
}

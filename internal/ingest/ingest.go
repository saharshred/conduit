// Package ingest de-duplicates normalized transactions before they reach
// the fraud scorer. Every source in this project can and will redeliver the
// same transaction — a retried page fetch, a webhook fired twice after a
// timeout — so dedupe has to happen before scoring, not after, or the same
// transaction could get flagged (or miscounted for velocity checks) twice.
package ingest

import (
	"context"
	"sync"

	"github.com/saharshred/conduit/internal/schema"
)

// Store records which idempotency keys have already been ingested. A real
// deployment backs this with Postgres; Day 1 ships an in-memory
// implementation so the dedupe logic is unit-testable on its own.
type Store interface {
	SeenAndMark(ctx context.Context, key string) (alreadySeen bool, err error)
}

type MemStore struct {
	mu   sync.Mutex
	seen map[string]bool
}

func NewMemStore() *MemStore {
	return &MemStore{seen: map[string]bool{}}
}

func (m *MemStore) SeenAndMark(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen[key] {
		return true, nil
	}
	m.seen[key] = true
	return false, nil
}

// Pipeline is the ingestion boundary: normalize (upstream), validate,
// dedupe, and only then hand the transaction onward (to RabbitMQ, in the
// full build — Day 3).
type Pipeline struct {
	store Store
}

func New(store Store) *Pipeline {
	return &Pipeline{store: store}
}

// Accept returns true if txn is new and should be forwarded, false if it's
// a duplicate delivery that should be silently dropped.
func (p *Pipeline) Accept(ctx context.Context, txn schema.Transaction) (bool, error) {
	if err := txn.Validate(); err != nil {
		return false, err
	}
	seen, err := p.store.SeenAndMark(ctx, txn.IdempotencyKey())
	if err != nil {
		return false, err
	}
	return !seen, nil
}

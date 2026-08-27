// Package pgingest is the real, Postgres-backed implementation of
// ingest.Store (Day 1 shipped that as an interface for exactly this
// reason) and the durable home for every accepted transaction, which the
// fraud scorer (Day 4) reads from.
package pgingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saharshred/conduit/internal/schema"
)

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgingest: connecting: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pgingest: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// SeenAndMark satisfies ingest.Store. Unlike the in-memory version, this
// one is safe across process restarts and concurrent ingestors — the
// UNIQUE constraint on idempotency_key is what actually prevents a
// double-insert, the SELECT is just a fast path to avoid the round trip
// when possible.
func (s *Store) SeenAndMark(ctx context.Context, key string) (bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM transactions WHERE idempotency_key = $1`, key,
	).Scan(&id)
	if err == nil {
		return true, nil // already ingested
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("pgingest: checking idempotency key: %w", err)
	}
	return false, nil
}

// Insert writes a newly-accepted transaction. If two ingestors race past
// SeenAndMark for the same key, the UNIQUE constraint here is what
// actually stops the second one — that race is expected, not a bug.
func (s *Store) Insert(ctx context.Context, txn schema.Transaction) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO transactions
			(idempotency_key, bank, native_id, account_id, amount_cents, currency, merchant_name, merchant_category, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, txn.IdempotencyKey(), txn.Bank, txn.NativeID, txn.AccountID, txn.AmountCents,
		txn.Currency, txn.MerchantName, txn.MerchantCategory, txn.Timestamp)
	if err != nil {
		return fmt.Errorf("pgingest: inserting transaction: %w", err)
	}
	return nil
}

func (s *Store) Count(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM transactions`).Scan(&n)
	return n, err
}

func Migrate(ctx context.Context, dsn, sql string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}

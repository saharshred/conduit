# CONDUIT

An aggregator that survives flaky banks and catches its own attacker.

Multiple mock "bank" APIs, each with a different failure personality,
normalized into one schema — feeding a real-time fraud scorer you then try
to beat with your own adversarial traffic generator.

Signals for: **Plaid** (Go, multi-source normalization is literally their
product problem) and **Ramp** (Elixir fraud scoring, RabbitMQ retry
pattern) directly; **Ruby/Rails** for Coinbase and Stripe.

## Status

Day 1-2 of 6 — done.

- [x] Unified schema + idempotency keys (`internal/schema`)
- [x] Ingestion dedupe pipeline, storage-agnostic (`internal/ingest`)
- [x] Three mock bank APIs, each with a dominant quirk — flaky auth, rate
      limiting, overlapping pagination (`internal/banks`)
- [x] Normalizer client: re-auths on 401, backs off on 429, dedupes
      pagination overlap, maps three different native schemas onto one
      (`internal/normalize`)
- [x] Dataset generator: N weeks of synthetic transactions per bank
      (`scripts/gen-dataset`)
- [x] End-to-end CLI: `cmd/mockbanks` + `cmd/normalizer` run against each
      other and produce a clean, deduped transaction set
- [ ] Day 3 — RabbitMQ: normalizer publishes instead of printing; retry +
      dead-letter queue for ingestion failures
- [ ] Day 4 — Elixir fraud scorer: velocity / geo / MCC-drift rules
      (`services/scorer`)
- [ ] Day 5 — Rails admin dashboard, live flagged-transaction view
      (`services/dashboard`)
- [ ] Day 6 — adversarial traffic generator + measured false-positive rate
      (`attack/`)

## Why it's built this way

**Three banks, three genuinely different native schemas.** `bank-a` sends
dollars as a float under `amount`; `bank-b` sends cents as an int under
`amount_cents`; `bank-c` calls the same field `cents`. This isn't padding —
real bank aggregation APIs disagree exactly like this, and `internal/normalize`'s
three mapper functions are the actual normalization work Plaid's product
does at a much larger scale.

**Every quirk is real, not simulated for show.** `internal/banks.Config`
gives each mock bank one dominant failure mode:

| Bank | Quirk | What the client has to do about it |
|---|---|---|
| bank-a | Tokens expire after N requests | Catch 401, re-authenticate, retry the same page |
| bank-b | Hard rate limit per second | Catch 429, exponential backoff with jitter, retry |
| bank-c | Every 4th page re-serves the previous page's last record | Dedupe by native ID before it ever reaches ingest |

`internal/normalize/client_test.go` proves the client survives both the
rate limiting and the pagination overlap end to end, against a real
`httptest` server — not mocked-away versions of the failure.

**Idempotency at the boundary.** `internal/ingest` rejects a transaction
it's already seen (same bank + same native ID), whether the duplicate came
from the pagination quirk or a genuinely retried delivery. Same principle
as SPREAD's ledger, same reason: Stripe's idempotency-key pattern exists
because retries are guaranteed to happen, not a hypothetical edge case.

## Running it now

```sh
go run ./scripts/gen-dataset -weeks 6 -out data/
go run ./cmd/mockbanks data/ &
go run ./cmd/normalizer
go test ./...
```

Expect output like:

```
bank-a   fetched=420    accepted=420    duplicate=0
bank-b   fetched=420    accepted=420    duplicate=0
bank-c   fetched=420    accepted=420    duplicate=0

total: fetched=1260 accepted=1260 duplicate=0
```

## Architecture (target, end of Day 5)

```
Pre-generated dataset: 6 weeks of synthetic transactions per mock bank
        |
        v
Mock Bank A (flaky auth)  --\
Mock Bank B (hard rate limits) --> Go Normalization Service
Mock Bank C (odd pagination) --/    (unified schema, idempotency keys)
                                         |
                                         v
                                RabbitMQ (retry + dead-letter queue)
                                         |
                                         v
                          Elixir Scorer (velocity / geo / MCC-drift rules)
                                    |            |
                                    v            v
                                Postgres    Rails Admin Dashboard
                                             (live flagged transactions)

Adversarial Traffic Generator (simulated card-testing ring)
        -.attacks.-> Mock Bank A / B / C
```

## Demo moment (target)

The dashboard sitting quiet with normal traffic ticking by, then you kick
off the attack script. Within seconds, a cluster of flagged transactions
lights up on the live dashboard — and you can point to the exact rule that
caught it, plus the false-positive rate you measured against normal
traffic.

```
FRAUD RING DETECTED — 14 TXN / 4.2s — VELOCITY + MCC-DRIFT — FLAGGED
```

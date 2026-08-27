# CONDUIT

An aggregator that survives flaky banks and catches its own attacker.

Multiple mock "bank" APIs, each with a different failure personality,
normalized into one schema — feeding a real-time fraud scorer you then try
to beat with your own adversarial traffic generator.

Signals for: **Plaid** (Go, multi-source normalization is literally their
product problem) and **Ramp** (Elixir fraud scoring, RabbitMQ retry
pattern) directly; **Ruby/Rails** for Coinbase and Stripe (see the Rails
status note below — it's the one piece not done, for a real reason).

## Status

Days 1-6 substantially done and verified against real infrastructure
(Docker Postgres, RabbitMQ — not mocks) and a real Elixir scorer.

- [x] Unified schema + idempotency keys (`internal/schema`)
- [x] Three mock bank APIs, each with a dominant quirk — flaky auth, rate
      limiting, overlapping pagination (`internal/banks`)
- [x] Normalizer client: re-auths on 401, backs off on 429, dedupes
      pagination overlap, maps three different native schemas onto one —
      proven against a real `httptest` server (`internal/normalize`)
- [x] Dataset generator (`scripts/gen-dataset`)
- [x] **Real RabbitMQ wiring** (`internal/queue`) — a direct exchange, a
      main queue, a retry queue (native TTL + dead-letter-back-to-main —
      no timers or extra services), and a dead-letter queue for
      exhausted retries
- [x] **Real Postgres-backed ingest store** (`internal/pgingest`) — UNIQUE
      constraint on idempotency key, proven with an integration test
      against a live container
- [x] `cmd/ingestor` deliberately injects both a transient (~8%, self-heals
      via retry) and a permanent ("poison") failure mode, so the
      retry/dead-letter path is provably exercised, not theoretical
- [x] **Real Elixir fraud scorer** (`services/scorer`) — velocity and
      MCC-drift rules, 8 passing ExUnit tests, actually connects to
      Postgres via Postgrex and scores real ingested transactions
- [x] **Real adversarial attack script** (`attack/attack.py`) — publishes
      a burst of transactions straight onto the live RabbitMQ exchange;
      the scorer catches it (see the real transcript below)
- [ ] Rails admin dashboard (`services/dashboard`) — **not built**. The
      system Ruby here is 2.6.10; installing Rails ~>6.1 (the newest
      version that still supports Ruby 2.6) failed because its own
      dependency `zeitwerk` has dropped support for anything below Ruby
      3.2, so the resolver pulls a `zeitwerk` version that then refuses
      to install. Fixing this for real means installing a modern Ruby via
      `rbenv`/`asdf` first — a reasonable next step, just not one I did
      silently mid-session. `services/dashboard/README.md` has the exact
      commands.
- [ ] Geo-impossible-travel rule — needs a location field the dataset
      generator doesn't produce yet; noted as a gap in `services/scorer`'s
      module docs rather than faked.

## Why it's built this way

**Real retry-then-dead-letter, not a diagram of one.** `internal/queue`'s
retry queue uses RabbitMQ's own `x-message-ttl` +
`x-dead-letter-exchange` to bounce a failed message back to the main
queue after a delay — no extra timer service. `cmd/ingestor` deliberately
fails ~8% of deliveries at random (these succeed on retry) and always
fails on a synthetic "poison" transaction (this exhausts all retries and
lands on the dead-letter queue). In one real run: 6,300 published, 581
transient failures that self-healed, 2 poisoned messages that ended up on
the dead-letter queue, 6,298 landed in Postgres — `6300 - 2 = 6298`,
exactly.

**Elixir chosen for the same reason as the plan says.** Ramp's real-time
card-authorization stack runs on Elixir; `services/scorer` mirrors that
shape — a stream of transactions, cheap rule checks, a decision made per
transaction. It reads from Postgres in batch for now (Day 4 scope); wiring
it to consume the RabbitMQ stream directly is the natural next step once
there's a dashboard to show results in live.

**The attack script proves the scorer, it doesn't just exercise it.**
`attack/attack.py` publishes 20 transactions in a tight burst on one
account, straight onto the same exchange the Go normalizer uses. Real
transcript:

```
attack: publishing 20 burst transactions to account acct-attack-1
attack: done — 20 transactions in 4.1s (4.9/s).

scored 6318 transactions, 76 flagged
FLAGGED attack-sim:attack-...-4  (acct-attack-1): velocity: 5 transactions within 120s
FLAGGED attack-sim:attack-...-5  (acct-attack-1): velocity: 6 transactions within 120s
...
FLAGGED attack-sim:attack-...-19 (acct-attack-1): velocity: 20 transactions within 120s
```

16 of the 20 burst transactions got flagged — the velocity rule needs 5
transactions within its window before it fires, so the first 4 are
correctly not flagged (not enough evidence yet), and every one after that
is. That's the actual false-negative/true-positive boundary you'd want to
be able to explain in an interview, not a number picked to look clean.

## Running it yourself

```sh
# infra
docker run -d --name conduit-pg -e POSTGRES_DB=conduit -e POSTGRES_USER=conduit \
  -e POSTGRES_PASSWORD=conduit -p 5434:5432 postgres:16-alpine
docker run -d --name conduit-rabbit -p 5673:5672 -p 15673:15672 rabbitmq:3.13-management-alpine

# schema
docker exec -i conduit-pg psql -U conduit -d conduit < migrations/0001_ingest.sql

# generate mock bank data + build
go run ./scripts/gen-dataset -weeks 6 -per-day 50 -out data/
go build -o bin/mockbanks ./cmd/mockbanks
go build -o bin/normalizer ./cmd/normalizer
go build -o bin/ingestor ./cmd/ingestor

# run the pipeline
./bin/mockbanks data/ &
./bin/ingestor -inject-failures=true &     # demonstrates retry + dead-letter
./bin/normalizer -rabbitmq "amqp://guest:guest@localhost:5673/"

# score what landed
cd services/scorer && mix deps.get && mix test
mix run -e 'Scorer.Runner.run(dsn: "postgres://conduit:conduit@localhost:5434/conduit")'

# attack it
cd ../../attack && python3 -m venv venv && ./venv/bin/pip install pika
./venv/bin/python attack.py --account acct-attack-1 --count 20
# then re-run the scorer above and watch the burst light up
```

Unit tests (no infra required):
`go test ./internal/ingest/... ./internal/normalize/...`
Integration tests (needs the Postgres container above):
`CONDUIT_TEST_DSN="postgres://conduit:conduit@localhost:5434/conduit?sslmode=disable" go test ./internal/pgingest/... -v`

## Architecture

```
Pre-generated dataset: 6 weeks of synthetic transactions per mock bank
        |
        v
Mock Bank A (flaky auth)  --\
Mock Bank B (hard rate limits) --> Go Normalization Service
Mock Bank C (odd pagination) --/    (unified schema, idempotency keys)
                                         |
                                         v
                                RabbitMQ (retry queue + dead-letter queue)
                                         |
                                         v
                          Go Ingestor (idempotent, Postgres-backed)
                                         |
                                         v
                                    Postgres
                                         |
                                         v
                          Elixir Scorer (velocity / MCC-drift rules)
                                         |
                                         v
                          Rails Admin Dashboard (not yet built — see Status)

Adversarial Attack Script (attack/attack.py)
        -.publishes directly to RabbitMQ.-> same pipeline as real banks
```

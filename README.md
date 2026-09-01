# CONDUIT

An aggregator that survives flaky banks and catches its own attacker.

Multiple mock "bank" APIs, each with a different failure personality,
normalized into one schema — feeding a real-time fraud scorer you then try
to beat with your own adversarial traffic generator.

Signals for: **Plaid** (Go, multi-source normalization is literally their
product problem) and **Ramp** (Elixir fraud scoring, RabbitMQ retry
pattern) directly; **Ruby/Rails** for Coinbase and Stripe.

## Status

All of Days 1-6 done and verified against real infrastructure (Docker
Postgres, RabbitMQ — not mocks), a real Elixir scorer, and a real Rails
app.

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
- [x] **Real Elixir fraud scorer** (`services/scorer`) — velocity,
      mcc_drift, and geo_impossible rules, 16 passing ExUnit tests,
      actually connects to Postgres via Postgrex, scores real ingested
      transactions, and upserts flagged ones for the dashboard to read
- [x] **Real adversarial attack script** (`attack/attack.py`) — publishes
      a burst of transactions straight onto the live RabbitMQ exchange;
      the scorer catches it (see the real transcript below)
- [x] **Real Rails 8 admin dashboard** (`services/dashboard`) — reads
      `flagged_transactions` directly out of the same `conduit` Postgres
      database the scorer writes into; approve/deny actions actually
      persist. Getting here needed a newer Ruby than what ships on this
      machine (system Ruby 2.6.10 can't install a modern Rails —
      `zeitwerk` requires >=3.2); installed 3.2.4 via `rbenv` mid-session,
      documented in `services/dashboard/README.md`. No automated test
      suite yet — verified live against a real filled database instead
      (see the transcript in that README)
- [x] Geo-impossible-travel rule — added. Every mock bank transaction now
      carries a `city`; the rule flags two transactions on one account
      that are farther apart than physically possible to travel between
      in the elapsed time, using haversine distance over a small fixed
      city table (`services/scorer/lib/scorer/geo.ex`)
- [x] **`docker compose up --build` actually works** for the whole Go
      pipeline — Postgres, RabbitMQ, migrations, dataset generation,
      mock banks, ingestor, normalizer, all in one command. Verified
      locally and in CI, not just written and hoped for.
- [x] **CI** (`.github/workflows/ci.yml`) — separate Go and Elixir jobs
      (go vet, unit tests, race detector, Postgres integration tests,
      `mix test`), plus a full compose smoke test, all on every push

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

**A real bug the geo rule's own numbers caught.** First version of the
dataset generator rolled a random "away city" independently per
*transaction*, in each bank's own generation loop. Since bank-a, bank-b,
and bank-c are three independent passes describing the *same* underlying
person, that meant the merged, chronologically-sorted view of one
account's transactions could show it in Chicago for a bank-a transaction
and Tokyo for a bank-b transaction four minutes later — not because
anything fraudulent happened, but because the three RNG streams simply
disagreed with each other. Result: `geo_impossible` fired on 684 of 741
flags (92%) — obviously wrong for a rule that's supposed to be rare and
meaningful. Fixed by keying the "where is this account today" decision off
`(account, day)` with a local deterministic seed (`dayCity` in
`scripts/gen-dataset/main.go`) instead of the shared RNG stream, so all
three banks agree on the account's location for a given day. Re-run:
99 flagged out of 10,080 (37 geo, 62 mcc_drift) — a believable signal
instead of noise. Worth knowing this happened, because it's the same
class of problem a real aggregator hits: multiple sources describing one
person can manufacture apparent anomalies purely from disagreeing with
each other, independent of anything the person actually did.

**A real bug CI caught that local testing never would have.** Getting the
compose smoke test green in CI took two more pushes after it worked
locally. First failure looked like slowness (raised a timeout — wrong
fix, treated a symptom). Second failure showed the real error: `dial tcp
...5672: connection refused`, 150ms after `docker compose` reported
RabbitMQ *healthy*. `rabbitmq-diagnostics ping` can succeed — the Erlang
node is up — a moment before the AMQP listener on 5672 is actually
accepting connections; on a laptop that gap is usually too small to hit,
on a loaded shared CI runner it isn't. `cmd/ingestor` and `cmd/normalizer`
each dialed RabbitMQ exactly once and `log.Fatal`'d on any error, no
restart policy, so the container just died. Fixed with
`queue.DialWithRetry` (10 attempts, linear backoff) — the same "survive
the failure mode that's actually going to happen" instinct the rest of
this project is built on, just applied to itself.

## Running it yourself

**One command** for the whole Go pipeline — Postgres, RabbitMQ, schema
migration, mock bank dataset generation, the three mock banks, the
ingestor (retry/dead-letter active), and the normalizer feeding
everything through:

```sh
docker compose up --build
# watch queue depths at http://localhost:15673 (guest/guest)
```

This is verified working, start to finish, not just written — see CI.
`services/scorer` and `services/dashboard` run separately (different
language runtimes — see below) against this stack's exposed Postgres.

```sh
# score what landed (needs Elixir/mix — services/scorer/README.md)
cd services/scorer && mix deps.get && mix test
mix run -e 'Scorer.Runner.run(dsn: "postgres://conduit:conduit@localhost:5434/conduit")'

# dashboard — reads flagged_transactions from the same conduit database
# (needs Ruby 3.2+/Rails — services/dashboard/README.md)
cd ../dashboard && bin/rails db:migrate && bin/rails server -p 3001
# http://localhost:3001

# attack it
cd ../../attack && python3 -m venv venv && ./venv/bin/pip install pika
./venv/bin/python attack.py --account acct-attack-1 --count 20
# re-run the scorer, then refresh the dashboard (or wait 5s — it polls)
```

For local Go dev without rebuilding a container on every change, swap
`docker compose up --build` for `docker compose up postgres rabbitmq -d`
and run the Go binaries directly — see `cmd/*/main.go` flags.

Unit tests (no infra required):
`go test ./internal/ingest/... ./internal/normalize/...`
Integration tests (needs Postgres — `docker compose up postgres -d`):
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
                          Elixir Scorer (velocity / mcc_drift / geo_impossible)
                                         |
                                         v
                          Rails Admin Dashboard (approve/deny, 5s poll)

Adversarial Attack Script (attack/attack.py)
        -.publishes directly to RabbitMQ.-> same pipeline as real banks
```

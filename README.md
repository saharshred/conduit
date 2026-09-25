# CONDUIT

An aggregator that survives flaky banks and catches its own attacker —
live, in one command.

Multiple mock "bank" APIs, each with a different failure personality,
normalized into one schema — feeding a real-time fraud scorer, showing up
**instantly** on a live dashboard (Postgres `LISTEN/NOTIFY` → Turbo
Streams, not polling), that you then try to beat with your own
adversarial attack script and get a real precision/recall number back,
not just a flag count. `docker compose up --build` brings up the entire
system: Postgres, RabbitMQ, three mock banks, the ingestor, the
normalizer, a continuously-running Elixir scorer, and the Rails
dashboard. No manual steps, no "now separately start the Elixir thing."

![Live demo: an attack lands, the flagged count jumps, the new transactions show up at the top](docs/demo.gif)

*(That's a real recording: `docker compose run --rm attack` fires a
15-transaction burst, and within one scorer poll cycle the dashboard's
pending count goes from 115 → 126 with no manual refresh, no re-running
anything by hand.)*

![The metrics bar after a real attack: 70.4% recall, 10.7% precision, 1.57% false-positive rate on legitimate traffic — computed live, not hand-typed](docs/live-metrics.jpg)

*(Same attack, now pushed to the dashboard the instant it's flagged — no
polling delay — with a real precision/recall breakdown against the
attack script's own ground-truth labels.)*

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
      persist. 8 passing tests (see below), also verified live against a
      real filled database (see `services/dashboard/README.md`)
- [x] **Everything containerized, one command.** `services/scorer` now
      runs as a long-lived loop (`Scorer.Runner.loop/1`, re-scoring every
      5s) instead of a one-shot script you had to remember to re-run, and
      both it and the dashboard have their own Dockerfiles wired into the
      root `docker-compose.yml`. `docker compose run --rm attack` runs
      the Python attack script the same way, no local venv needed. The
      whole loop — attack lands, scorer picks it up on its next poll,
      dashboard updates itself instantly via the push described below —
      needs zero manual intervention, proven by the GIF and screenshot
      above.
- [x] Geo-impossible-travel rule — added. Every mock bank transaction now
      carries a `city`; the rule flags two transactions on one account
      that are farther apart than physically possible to travel between
      in the elapsed time, using haversine distance over a small fixed
      city table (`services/scorer/lib/scorer/geo.ex`)
- [x] **`docker compose up --build` actually works** for the whole Go
      pipeline — Postgres, RabbitMQ, migrations, dataset generation,
      mock banks, ingestor, normalizer, all in one command. Verified
      locally and in CI, not just written and hoped for.
- [x] **CI** (`.github/workflows/ci.yml`) — separate Go, Elixir, Rails, and
      ml_scorer jobs (go vet, unit tests, race detector, Postgres
      integration tests, `mix test`, `bin/rails test`, `pytest`), plus a
      full compose smoke test that now checks the scorer, ML classifier,
      and dashboard too, all on every push
- [x] **Real precision/recall, not a raw flag count.**
      `services/scorer/lib/scorer/evaluate.ex` and the Rails
      `DashboardMetrics` model both compute it independently, off the
      same free ground-truth label `attack/attack.py` already provides
      (every attack transaction is tagged `bank: "attack-sim"`) — 5
      passing Elixir tests plus 2 Rails tests on the arithmetic itself,
      including the "what if nothing's flagged yet" divide-by-zero cases
- [x] **Real-time push, not polling.** The Elixir scorer `NOTIFY`s
      Postgres the moment it flags something genuinely new (not every
      poll — only on an actual fresh insert, via Postgres's `xmax = 0`
      trick); a Rails background thread `LISTEN`s on the same channel and
      broadcasts a fresh render over Turbo Streams. An attack shows up on
      the dashboard instantly, no 5-second wait, verified by watching the
      page update itself with zero manual refresh (see the screenshot above)
- [x] **A real Rails test suite** (`services/dashboard/test/`) — 10
      passing tests: index rendering, status filtering, an unrecognized
      status param falling back safely, approve/deny actually persisting,
      ML score rendering, and the metrics arithmetic against seeded
      ground-truth data. Closes the one gap this README used to call out
      explicitly.
- [x] **A real second-stage ML classifier** (`services/ml_scorer`) — the
      rule engine is binary (fired or didn't); this layer learns from the
      same ground-truth labels to produce a continuous risk score instead.
      A `GradientBoostingClassifier` (scikit-learn) trains on real
      transaction history — amount, time-of-day, velocity, category drift,
      geo-implied speed, and how many rules fired — evaluated on a
      chronological (not random) held-out split so it's never trained on
      data from "the future." Runs continuously in its own container,
      writing a risk score back onto every rule-flagged transaction for
      the dashboard to show (`ML risk` column). 9 passing pytest tests.

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

**The hardest bug of all: Rails silently dropping a column it doesn't
own.** Containerizing the dashboard meant it had to boot against a truly
fresh database for the first time — and on every fresh boot, the `city`
column the Go migration adds would just... not be there, breaking the
scorer's geo rule. `git blame`-style debugging on my own migration script
wasn't enough this time; I isolated it by bringing up only `postgres` +
`migrate` (column persisted fine), then adding `dashboard` alone (column
vanished within about a second of it starting). Tight polling correlated
against timestamped container logs down to the millisecond pinned the
disappearance to `bin/rails db:migrate`'s own execution window — despite
`db:migrate` supposedly only applying this app's own pending migration
file. The actual mechanism: Rails 8's multi-database task runner, on a
database whose `schema_migrations` table is still empty, treats that as
"this database needs preparing" and loads `db/schema.rb` — which had been
committed *before* the Go side's city-column migration existed, so it
`force: :cascade`-recreated `transactions` from a stale snapshot and
silently destroyed the column a completely separate migration step had
just added, seconds earlier. Fixed at the actual root cause — regenerated
`schema.rb` from a database that has the column, so it's accurate — plus
made the Go-side migration step in `migrations/apply.sh` retry and
self-verify (`SELECT` the column back out of `information_schema` before
declaring success) rather than trusting a command's exit code alone.
Worth knowing this pattern exists: two systems that both think they own
schema for the same physical database is a real, repeatable failure mode,
not a one-off fluke — the fix isn't "don't let it happen," it's "verify,
don't assume."

## Running it yourself

**One command, the entire system:**

```sh
docker compose up --build
# → http://localhost:3001 for the live dashboard
# → http://localhost:15673 (guest/guest) to watch queue depths
```

That brings up Postgres, RabbitMQ, schema migrations, mock bank dataset
generation, the three mock banks, the ingestor (retry/dead-letter
active), the normalizer feeding everything through, a continuously
polling Elixir scorer, the ML classifier (trains once it has real
attack traffic, then scores continuously), and the Rails dashboard.
Verified working, start to finish, in CI on every push — not just
written and hoped for.

Attack it:

```sh
docker compose run --rm attack --account acct-demo --count 20
# pushed to the dashboard instantly (LISTEN/NOTIFY + Turbo Streams, no
# manual refresh) — the ml-scorer container picks up the new ground
# truth on its next pass and starts scoring the burst within seconds
```

For local dev without rebuilding a container on every change:

```sh
docker compose up postgres rabbitmq -d
go run ./cmd/mockbanks data/ &     # after go run ./scripts/gen-dataset ...
go run ./cmd/ingestor -postgres=postgres://conduit:conduit@localhost:5434/conduit?sslmode=disable &
go run ./cmd/normalizer

cd services/scorer && mix deps.get && mix test
mix run -e 'Scorer.Runner.loop(dsn: "postgres://conduit:conduit@localhost:5434/conduit")'

cd ../dashboard && bin/rails db:migrate && bin/rails server -p 3001

cd ../../attack && python3 -m venv venv && ./venv/bin/pip install pika
./venv/bin/python attack.py --account acct-attack-1 --count 20
```

Unit tests (no infra required):
`go test ./internal/ingest/... ./internal/normalize/...`
Integration tests (needs Postgres — `docker compose up postgres -d`):
`CONDUIT_TEST_DSN="postgres://conduit:conduit@localhost:5434/conduit?sslmode=disable" go test ./internal/pgingest/... -v`
Elixir: `cd services/scorer && mix test` (21 tests, no infra required —
`evaluate_test.exs` tests the precision/recall arithmetic as pure functions)
Rails (needs Postgres — `docker compose up postgres -d`):
`cd services/dashboard && RAILS_ENV=test bin/rails db:test:prepare && bin/rails test`
ML classifier (no infra required — feature engineering and the train/
eval split are pure functions of a DataFrame):
`cd services/ml_scorer && pip install -r requirements.txt && pytest -v`

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
              Elixir Scorer (velocity/mcc_drift/geo_impossible, polls every 5s)
                                         |
                                         v
              Python ML Scorer (GradientBoostingClassifier, re-ranks rule flags)
                                         |
                                         v
              Rails Dashboard (approve/deny, live via LISTEN/NOTIFY — localhost:3001)

Adversarial Attack Script (docker compose run --rm attack)
        -.publishes directly to RabbitMQ.-> same pipeline as real banks
        -.caught within one scorer poll cycle, no manual steps.->
```

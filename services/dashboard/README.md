# Dashboard

A real Rails 8 app — not a stub. Shows flagged transactions as
`services/scorer` (Elixir) writes them, backed directly by the same
`conduit` Postgres database the Go ingestor and Elixir scorer already use
(see `config/database.yml` — deliberately not a separate
`dashboard_development` database).

Runs in Docker now (`Dockerfile.dev`, wired into the root
`docker-compose.yml`) — `docker compose up --build` from the repo root is
the actual way to run this, no local Ruby needed. Getting a *local* Ruby
working (for `bin/rails console` etc.) needs 3.2+: system Ruby on the
machine this was built on was 2.6.10, and Rails' own `zeitwerk`
dependency dropped support below 3.2.

```sh
brew install rbenv ruby-build
rbenv install 3.2.4
rbenv global 3.2.4
gem install rails
```

## What's here

- `FlaggedTransaction` (`app/models`) — one row per transaction the
  scorer flagged, with the rule(s) that fired, an `ml_score` written by
  `services/ml_scorer` (nullable — null until that service scores it,
  shown as "scoring…"), and a `status` that starts at `pending` and moves
  to `approved`/`denied`
- `FlaggedTransactionsController` — index (filterable by status), show,
  and `approve`/`deny` member actions that actually persist
- `DashboardMetrics` (`app/models`) — precision/recall/false-positive-rate
  computed against the attack script's own ground-truth labels, shown as
  a stat bar on the pending view
- `LiveBroadcaster` (`app/models`, started from
  `config/initializers/live_broadcaster.rb`) — a background thread that
  `LISTEN`s on the same Postgres channel the scorer `NOTIFY`s, and
  broadcasts a fresh render over Turbo Streams the instant something new
  is flagged. Replaced the old 5-second `<meta http-equiv="refresh">`
  polling entirely — this is real push, not "polls fast enough to feel live"
- `db/migrate/..._create_flagged_transactions.rb` — the one table this
  app owns; `transactions` (read by the scorer) belongs to the Go
  ingestor's migration, not this app's

## Running it

From the repo root: `docker compose up --build` — brings this up along
with everything else. Standalone, against an already-running Postgres:

```sh
cd services/dashboard
bin/rails db:migrate     # only needs to run once against the shared conduit DB
bin/rails server -p 3001
```

The scorer (`../scorer`) runs continuously and populates
`flagged_transactions` on its own now; `../../attack/attack.py` (or
`docker compose run --rm attack`) shows new flagged rows up instantly —
pushed over Turbo Streams the moment the scorer's next poll finds them,
no manual refresh, no re-run of anything.

## A real bug: `db:migrate` silently dropping a column it doesn't own

Containerizing this surfaced something local testing never would have:
on a truly fresh database, `bin/rails db:migrate` — not just
`db:prepare`, which is documented to fall back to a full schema load —
also force-reloaded `db/schema.rb` because `schema_migrations` was still
empty. `schema.rb` had been committed *before* the Go side's `city`
column migration existed, so the reload `force: :cascade`-recreated
`transactions` from that stale snapshot and silently destroyed a column
a completely separate migration had added seconds earlier. Found by
isolating services one at a time in compose (`postgres` + `migrate`
alone: column persisted; adding `dashboard`: column vanished within ~1s
of it starting) and correlating timestamped container logs down to the
millisecond. Fixed by regenerating `schema.rb` from a database that
actually has the column — full writeup in the root README.

## Verified

8 passing tests (`test/`) — index rendering, status filtering, an
unrecognized status param falling back safely instead of erroring,
approve/deny actually persisting, and the `DashboardMetrics` arithmetic
against seeded ground-truth data including the "nothing's happened yet"
divide-by-zero cases. `rails new` was run with `--skip-test` while
getting the Ruby toolchain sorted; `test/test_helper.rb` was added back
by hand rather than regenerating the app.

Also verified live against a real filled database, repeated after full
teardown to confirm it's not a fluke:

```
GET  /                                   → 200, renders all pending flagged rows
POST /flagged_transactions/1/approve     → 302, status actually persisted to "approved"
```

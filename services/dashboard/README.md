# Dashboard

A real Rails 8 app — not a stub. Shows flagged transactions as
`services/scorer` (Elixir) writes them, backed directly by the same
`conduit` Postgres database the Go ingestor and Elixir scorer already use
(see `config/database.yml` — deliberately not a separate
`dashboard_development` database).

Getting this running needed a newer Ruby than what ships on this machine:
system Ruby was 2.6.10, and Rails' own `zeitwerk` dependency dropped
support below Ruby 3.2. Fixed with:

```sh
brew install rbenv ruby-build
rbenv install 3.2.4
rbenv global 3.2.4
gem install rails
```

## What's here

- `FlaggedTransaction` (`app/models`) — one row per transaction the
  scorer flagged, with the rule(s) that fired and a `status` that starts
  at `pending` and moves to `approved`/`denied`
- `FlaggedTransactionsController` — index (filterable by status), show,
  and `approve`/`deny` member actions that actually persist
- Views poll every 5 seconds (`<meta http-equiv="refresh">`) rather than
  wiring up ActionCable/Turbo Streams for a v1 — same "live" effect the
  build plan called for, less to get wrong
- `db/migrate/..._create_flagged_transactions.rb` — the one table this
  app owns; `transactions` (read by the scorer) belongs to the Go
  ingestor's migration, not this app's

## Running it

```sh
cd services/dashboard
bin/rails db:migrate     # only needs to run once against the shared conduit DB
bin/rails server -p 3001
```

Then run the scorer (`../scorer`) to populate `flagged_transactions`, and
`../../attack/attack.py` to watch new flagged rows show up within one
polling interval.

## Verified

No automated test suite yet (`rails new` was run with `--skip-test`
while getting the Ruby toolchain sorted, and it wasn't added back). What
*is* verified, against a live server on a real filled database:

```
GET  /                                   → 200, renders all 60 real flagged rows
POST /flagged_transactions/1/approve     → 302, status actually persisted to "approved"
```

Adding proper request specs (Minitest, since that's what a `--skip-test`
Rails app still ships the dependency for) is the natural next step.

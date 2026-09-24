# Scorer

The fraud rules engine — Elixir, chosen because it's what Ramp's actual
real-time card-authorization stack runs on. Not a batch job you have to
remember to re-run anymore: `Scorer.Runner.loop/1` polls every 5 seconds,
forever, matched to the dashboard's own 5s poll so a new attack shows up
on screen within about one cycle.

Runs in Docker now (`Dockerfile`, wired into the root
`docker-compose.yml`) — `docker compose up --build` from the repo root
brings this up along with everything else.

## What's here

- `lib/scorer/rules.ex` — the three rules: `velocity` (too many
  transactions on one account in too short a window), `mcc_drift` (a
  merchant category the account has never used before), `geo_impossible`
  (two transactions too far apart to both be genuine given the elapsed
  time — see `lib/scorer/geo.ex`, haversine distance over a small fixed
  city table)
- `lib/scorer/runner.ex` — connects to the same Postgres the Go ingestor
  writes into, scores everything, upserts flagged transactions for the
  dashboard to read. `run/1` does one pass (used by tests); `loop/1` is
  what actually runs in the container
- `test/` — 16 assertions: each rule fires when it should and, just as
  importantly, doesn't fire when it shouldn't (a normal same-city
  spending pattern never trips `geo_impossible`, a computation that
  genuinely depends on nothing invariant never gets flagged, etc.)

## Running it

```sh
mix deps.get
mix test

# one pass
mix run -e 'Scorer.Runner.run(dsn: "postgres://conduit:conduit@localhost:5434/conduit")'

# continuous, like the container does
mix run --no-halt -e 'Scorer.Runner.loop()'
```

## Real numbers, not claims

A live attack (`attack/attack.py`, 15-transaction burst) went from
publish to flagged-on-the-dashboard with zero manual steps — no re-running
the scorer, no manual refresh — the pending count moved from 115 to 126
within one poll cycle. See the GIF in the root README for the actual
recording, and that README for two real bugs found building this: a
dataset-generator bug that made `geo_impossible` fire on 92% of flags
before being fixed, and a Rails `db:migrate` schema-load gotcha that
silently dropped this service's own `city` dependency.

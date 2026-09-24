# Scorer

The fraud rules engine — Elixir, chosen because it's what Ramp's actual
real-time card-authorization stack runs on. Not a batch job you have to
remember to re-run anymore: `Scorer.Runner.loop/1` polls every 5 seconds,
forever. When it finds something genuinely new (not just re-confirming an
already-known flag — see the `xmax = 0` trick in `runner.ex`), it
`NOTIFY`s Postgres, and the dashboard picks that up instantly over Turbo
Streams — not "within one poll cycle," instantly.

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
- `lib/scorer/evaluate.ex` — precision/recall/false-positive-rate against
  real ground truth: `attack/attack.py` tags every transaction it
  publishes with `bank: "attack-sim"`, a free, honest label nothing else
  in the pipeline ever produces, so "was this actually the injected
  attack" doesn't need to be guessed at
- `test/` — 21 assertions: each rule fires when it should and, just as
  importantly, doesn't fire when it shouldn't (a normal same-city
  spending pattern never trips `geo_impossible`, a computation that
  genuinely depends on nothing invariant never gets flagged, etc.), plus
  the precision/recall arithmetic itself including the divide-by-zero
  edge cases (no attack yet, scorer hasn't flagged anything yet)

## Running it

```sh
mix deps.get
mix test

# one pass
mix run -e 'Scorer.Runner.run(dsn: "postgres://conduit:conduit@localhost:5434/conduit")'

# continuous, like the container does
mix run --no-halt -e 'Scorer.Runner.loop()'

# precision/recall against the real ground-truth attack labels
mix run -e 'Scorer.Evaluate.run(dsn: "postgres://conduit:conduit@localhost:5434/conduit")'
```

## Real numbers, not claims

A live attack (`attack/attack.py`) went from publish to
flagged-on-the-dashboard with zero manual steps — no re-running the
scorer, no manual refresh — pushed instantly via `NOTIFY`. One real run:
70.4% recall, 10.7% precision, 1.57% false-positive rate on legitimate
traffic, computed by `Scorer.Evaluate` off the attack script's own
ground-truth labels, not hand-picked to look clean. See the GIF and
screenshot in the root README for the actual recordings, and that README
for the real bugs found building this — including a dataset-generator
bug that made `geo_impossible` fire on 92% of flags before being fixed,
and a Rails `db:migrate` schema-load gotcha that silently dropped this
service's own `city` dependency.

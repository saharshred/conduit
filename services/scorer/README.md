# Fraud scorer (Day 4, Elixir)

Target: an Elixir service consuming normalized transactions off RabbitMQ
(Day 3) and applying three rule-based checks per transaction:

- **Velocity** — too many transactions on one account in too short a window.
- **Geo-impossible** — two transactions too far apart to both be genuine
  given the time between them (needs a location field added to the
  synthetic dataset generator).
- **MCC drift** — an account's spending suddenly jumps to merchant
  categories it's never used before.

Elixir was chosen deliberately: Ramp's real-time card-authorization stack
runs on it, and this scorer is meant to mirror that shape — a stream of
transactions, a set of cheap rule checks, a decision made in milliseconds.

Not yet implemented — needs `elixir`/`mix` installed locally. Scaffold:

```
mix new scorer --sup
```

then a `RulesEngine` module with one function per rule, each returning a
score contribution, summed and thresholded into a flag/no-flag decision
that gets written to Postgres and pushed to the dashboard (`services/dashboard`).

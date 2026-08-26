# Admin dashboard (Day 5, Ruby on Rails)

Target: a Rails admin view showing flagged transactions live as the scorer
(`services/scorer`) writes them, with an approve/deny action for a
reviewer. Ruby/Rails is the deliberate choice here — it's the shared
language between Coinbase's and Stripe's listed stacks.

Not yet implemented — needs a compatible Ruby/Rails/Bundler setup locally.
Scaffold:

```
rails new dashboard --database=postgresql --skip-test
```

then a single `FlaggedTransactions` resource: index (live via ActionCable
or short polling), show (the rule that triggered it + the raw payload for
audit), and an approve/deny action that writes back to Postgres.

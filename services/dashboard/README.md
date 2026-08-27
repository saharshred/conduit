# Admin dashboard (Day 5, Ruby on Rails)

Target: a Rails admin view showing flagged transactions live as the scorer
(`services/scorer`) writes them, with an approve/deny action for a
reviewer. Ruby/Rails is the deliberate choice here — it's the shared
language between Coinbase's and Stripe's listed stacks.

Not yet implemented. Attempted during the Day 5 session and blocked for a
concrete reason worth recording: this machine's system Ruby is 2.6.10, and
`gem install rails -v '~> 6.1.7'` (the newest Rails still compatible with
2.6) failed because Rails' own dependency `zeitwerk` has dropped support
for anything below Ruby 3.2 — the resolver picks a `zeitwerk` version that
then refuses to install on 2.6. Real fix, in order:

```
brew install rbenv
rbenv install 3.2.4
rbenv local 3.2.4
gem install rails
rails new dashboard --database=postgresql --skip-test
```

then a single `FlaggedTransactions` resource: index (live via ActionCable
or short polling), show (the rule that triggered it + the raw payload for
audit), and an approve/deny action that writes back to Postgres.

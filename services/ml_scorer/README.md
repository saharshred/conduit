# ML Scorer

A second-stage classifier on top of `services/scorer` (Elixir). The rule
engine is deliberately binary — a rule either fires or it doesn't — which
is the right first layer (cheap, explainable, auditable), but it means
two transactions that both trip `velocity` get treated as equally
suspicious even if one is barely over the threshold and the other is a
25-transaction burst. This layer learns a continuous risk score instead,
using the exact same ground truth `services/scorer/lib/scorer/evaluate.ex`
and Rails' `DashboardMetrics` already use: `attack/attack.py` tags every
transaction it publishes with `bank: "attack-sim"`, a free, honest label
nothing else in the pipeline ever produces.

## What's here

- `features.py` — pure feature engineering from a transactions DataFrame:
  amount, hour of day, weekday, whether the merchant category is the
  high-risk one, a velocity count (mirrors the Elixir rule's 120s
  window), whether the account has used this category before, geo-implied
  speed vs. the previous transaction (same haversine + fixed city table
  as `services/scorer/lib/scorer/geo.ex`), and how many rules fired. Kept
  separate from any database code so it's unit-testable without Postgres,
  and so training and scoring can never silently compute features two
  different ways.
- `train.py` — pulls real transaction history, trains a
  `GradientBoostingClassifier` (scikit-learn), and evaluates on a
  **chronological** held-out split — trained data always comes strictly
  before test data in time, so the reported numbers can't be inflated by
  the model having seen "the future." Split independently per class
  (legit vs. attack) so a short attack burst injected at one point in
  time can't land entirely in train or entirely in test.
- `loop.py` — once a model exists, polls for flagged transactions the
  rule engine caught but this layer hasn't scored yet, and writes a risk
  probability back onto `flagged_transactions.ml_score` for the dashboard
  to show.
- `entrypoint.sh` — retries training until there's real labeled data to
  train on (nothing publishes `attack-sim` traffic until you run the
  attack script), then runs the scoring loop forever — same "wait,
  don't fail" shape as the Elixir scorer's own poll loop.
- `test_features.py`, `test_train.py` — 9 tests, all pure functions, no
  database required.

## Running it

```sh
pip install -r requirements.txt
pytest -v

# needs a real database with both attack and legit transactions —
# run against the docker compose stack after firing the attack script:
CONDUIT_DSN="postgres://conduit:conduit@localhost:5434/conduit" python train.py
CONDUIT_DSN="postgres://conduit:conduit@localhost:5434/conduit" python loop.py
```

In `docker compose up`, this runs continuously as the `ml-scorer`
service, training once it has enough labeled data and then scoring
newly flagged transactions every 10 seconds.

## Real numbers, not claims

One real run against a 10K-transaction dataset plus a 25-transaction
attack burst: 100% precision/recall/AUC on the held-out split. That's a
genuinely separable pattern in this synthetic dataset (the attack always
uses a fixed high-risk merchant category in a tight burst), not a
guarantee this generalizes to subtler fraud — the point of this layer is
the pipeline (real ground truth in, chronological held-out evaluation,
no future leakage), which is the part that transfers to a harder
dataset, not the specific number.

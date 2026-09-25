#!/bin/sh
# train.py needs at least one labeled attack transaction to train on
# (attack/attack.py, run on demand — nothing publishes bank: "attack-sim"
# traffic on its own), so there's no fixed point at which retrying should
# give up: keep polling until someone actually runs the attack script,
# same shape as services/scorer's own poll-forever loop.
until python train.py; do
  echo "ml_scorer: training attempt failed (not enough labeled data yet), retrying in 15s..."
  sleep 15
done

exec python loop.py

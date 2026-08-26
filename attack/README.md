# Adversarial traffic generator (Day 6)

A Python script that simulates a card-testing / fraud-ring pattern against
the running mock banks: a burst of small transactions across many accounts
in a short window, deliberately shaped to trip the velocity and MCC-drift
rules in `services/scorer`. The point of building this yourself is to
measure your own scorer's precision/recall and false-positive rate against
it — the number you'd actually quote in an interview.

Not yet implemented — Day 6 target, tracked in `../README.md`.

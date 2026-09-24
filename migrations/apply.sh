#!/bin/sh
# Applies both migrations, then verifies the city column actually landed
# before exiting — belt and suspenders against a real gotcha: Postgres's
# official Docker image runs a brief temporary server to execute its own
# init scripts on a fresh volume, then restarts into the real long-running
# server. `pg_isready` (what the compose healthcheck polls) can report
# ready during that temporary phase, so `depends_on: condition:
# service_healthy` alone isn't a hard guarantee every later statement
# landed durably before something else reads the schema. Retrying the
# whole apply-and-verify sequence a few times is cheap and removes the
# race instead of hoping the timing works out.
set -eu

DSN="postgres://conduit:conduit@postgres:5432/conduit?sslmode=disable"

for attempt in 1 2 3 4 5; do
  psql -v ON_ERROR_STOP=1 "$DSN" -f /migrations/0001_ingest.sql
  psql -v ON_ERROR_STOP=1 "$DSN" -f /migrations/0002_add_city.sql

  HAS_CITY=$(psql -tA "$DSN" -c "SELECT count(*) FROM information_schema.columns WHERE table_name='transactions' AND column_name='city'")
  if [ "$HAS_CITY" = "1" ]; then
    echo "migrations applied and verified (attempt $attempt)"
    exit 0
  fi

  echo "verification failed on attempt $attempt (city column missing after apply) — retrying"
  sleep 2
done

echo "migrations did not verify after 5 attempts" >&2
exit 1

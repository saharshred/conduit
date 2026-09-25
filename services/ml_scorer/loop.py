"""
Runs continuously once a model exists (see train.py): every
ML_SCORE_INTERVAL_SECONDS, finds flagged transactions the rule engine
caught but this layer hasn't scored yet (ml_score IS NULL) and writes a
probability back onto flagged_transactions.ml_score for the dashboard to
show. Only scores what the rule engine already flagged — this is a
second-stage re-ranker on top of the rules, not a replacement for them.
"""

import os
import time

import joblib
import psycopg2

from features import build_feature_frame
from train import MODEL_PATH, dsn, load_transactions

SCORE_INTERVAL_SECONDS = int(os.environ.get("ML_SCORE_INTERVAL_SECONDS", "10"))


def score_once(conn, bundle) -> int:
    model, feature_cols = bundle["model"], bundle["feature_cols"]

    with conn.cursor() as cur:
        cur.execute("SELECT idempotency_key FROM flagged_transactions WHERE ml_score IS NULL")
        pending_keys = {row[0] for row in cur.fetchall()}

    if not pending_keys:
        return 0

    df = load_transactions(conn)
    features = build_feature_frame(df)
    rows = features[features["idempotency_key"].isin(pending_keys)]
    if rows.empty:
        return 0

    probs = model.predict_proba(rows[feature_cols])[:, 1]

    with conn.cursor() as cur:
        for key, prob in zip(rows["idempotency_key"], probs):
            cur.execute(
                "UPDATE flagged_transactions SET ml_score = %s WHERE idempotency_key = %s",
                (float(prob), key),
            )
    conn.commit()
    return len(rows)


def main():
    if not os.path.exists(MODEL_PATH):
        print("ml_scorer: no trained model at " + MODEL_PATH + " — run train.py first")
        return

    bundle = joblib.load(MODEL_PATH)
    conn = psycopg2.connect(dsn())
    print(f"ml_scorer: loaded model, scoring newly flagged transactions every {SCORE_INTERVAL_SECONDS}s")

    while True:
        try:
            n = score_once(conn, bundle)
            if n:
                print(f"ml_scorer: scored {n} newly flagged transaction(s)")
        except Exception as e:
            print(f"ml_scorer: error during scoring pass: {e}")
            conn.rollback()
        time.sleep(SCORE_INTERVAL_SECONDS)


if __name__ == "__main__":
    main()

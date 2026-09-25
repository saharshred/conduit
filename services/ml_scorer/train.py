"""
Trains a second-stage classifier on top of the rule engine
(services/scorer, Elixir): rules are cheap, explainable, and catch
obvious patterns, but they're binary — fired or didn't. This layer
learns from the same ground truth Scorer.Evaluate already uses
(attack/attack.py tags every transaction it publishes with
bank: "attack-sim") to produce a continuous risk score instead, so two
transactions that both trip `velocity` aren't treated as equally
suspicious.

Evaluated on a chronological (not random) held-out split — training on
a random shuffle would let the model see transactions from *after* the
ones it's being tested on, which is impossible in production and would
inflate the numbers.
"""

import os
import sys
import warnings

import joblib
import pandas as pd
import psycopg2
from sklearn.ensemble import GradientBoostingClassifier
from sklearn.metrics import precision_score, recall_score, roc_auc_score

from features import FEATURE_COLUMNS, build_feature_frame

# pandas.read_sql works fine against a raw psycopg2 connection; the
# warning is about connections it hasn't explicitly tested, not an actual
# problem here.
warnings.filterwarnings("ignore", message="pandas only supports SQLAlchemy")

MODEL_PATH = os.environ.get("ML_MODEL_PATH", "/model/model.joblib")
MIN_TRANSACTIONS = 200


def dsn():
    return os.environ.get(
        "CONDUIT_DSN", "postgres://conduit:conduit@localhost:5434/conduit"
    )


def load_transactions(conn) -> pd.DataFrame:
    # rule_flag_count: how many of the three rules fired for this
    # transaction (0 if the rule engine never flagged it at all) — lets
    # the model use the rule engine's own verdict as one input among
    # several, rather than duplicating that logic from scratch.
    query = """
        SELECT
            t.idempotency_key, t.account_id, t.amount_cents, t.merchant_category,
            t.occurred_at, t.city, t.bank,
            COALESCE(
                array_length(regexp_split_to_array(f.reasons, E'\\n'), 1), 0
            ) AS rule_flag_count
        FROM transactions t
        LEFT JOIN flagged_transactions f ON f.idempotency_key = t.idempotency_key
        ORDER BY t.account_id, t.occurred_at
    """
    df = pd.read_sql(query, conn)
    df["occurred_at"] = pd.to_datetime(df["occurred_at"], utc=True)
    df["label"] = (df["bank"] == "attack-sim").astype(int)
    return df


def train_and_evaluate(df: pd.DataFrame):
    features = build_feature_frame(df).sort_values("occurred_at").reset_index(drop=True)

    # Split chronologically *within each class*, not across the whole
    # dataset — attacks tend to be a short burst injected at one point in
    # time, so a single global 80/20 cut can land every attack example
    # entirely in train or entirely in test (and GradientBoostingClassifier
    # can't train on a single class). Splitting per class keeps the
    # "never look at the future" property for each class's own history
    # while guaranteeing both classes show up on both sides.
    train_parts, test_parts = [], []
    for _, group in features.groupby("label"):
        split_idx = max(1, int(len(group) * 0.8)) if len(group) > 1 else len(group)
        train_parts.append(group.iloc[:split_idx])
        test_parts.append(group.iloc[split_idx:])
    train_df = pd.concat(train_parts).sort_values("occurred_at")
    test_df = pd.concat(test_parts).sort_values("occurred_at")

    X_train, y_train = train_df[FEATURE_COLUMNS], train_df["label"]
    X_test, y_test = test_df[FEATURE_COLUMNS], test_df["label"]

    model = GradientBoostingClassifier(random_state=42)
    model.fit(X_train, y_train)

    probs = model.predict_proba(X_test)[:, 1]
    preds = (probs >= 0.5).astype(int)

    metrics = {
        "train_size": len(train_df),
        "test_size": len(test_df),
        "test_attack_count": int(y_test.sum()),
        "precision": precision_score(y_test, preds, zero_division=0),
        "recall": recall_score(y_test, preds, zero_division=0),
        "auc": roc_auc_score(y_test, probs) if y_test.nunique() > 1 else float("nan"),
    }
    return model, metrics


def main():
    conn = psycopg2.connect(dsn())
    df = load_transactions(conn)
    conn.close()

    if len(df) < MIN_TRANSACTIONS or df["label"].nunique() < 2:
        print(
            f"ml_scorer: not enough labeled data yet ({len(df)} transactions, "
            f"need >= {MIN_TRANSACTIONS} with both attack and legit present) "
            "— run the attack script and try again"
        )
        sys.exit(1)

    model, metrics = train_and_evaluate(df)

    print("=== ML classifier evaluation (held-out, chronological split) ===")
    print(
        f"Train size: {metrics['train_size']}  Test size: {metrics['test_size']}  "
        f"Test attack count: {metrics['test_attack_count']}"
    )
    print(
        f"Precision: {metrics['precision'] * 100:.1f}%  "
        f"Recall: {metrics['recall'] * 100:.1f}%  "
        f"AUC: {metrics['auc']:.3f}"
    )

    os.makedirs(os.path.dirname(MODEL_PATH), exist_ok=True)
    joblib.dump({"model": model, "feature_cols": FEATURE_COLUMNS}, MODEL_PATH)
    print(f"ml_scorer: model saved to {MODEL_PATH}")


if __name__ == "__main__":
    main()

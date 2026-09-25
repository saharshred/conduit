import pandas as pd

from train import train_and_evaluate


def _synthetic_dataset(n_legit=300, n_attack=60):
    """Legit transactions: small amounts, low velocity, never gambling
    category. Attack transactions: gambling category, tight bursts, large
    amounts — separable enough that a real model should score them apart,
    proving the pipeline (feature build -> train -> held-out eval) works
    end to end without needing a live database."""
    rows = []
    base = pd.Timestamp("2026-01-01T00:00:00", tz="UTC")

    for i in range(n_legit):
        rows.append(
            {
                "idempotency_key": f"legit-{i}",
                "account_id": f"acct-{i % 20}",
                "amount_cents": 500 + (i % 30) * 100,
                "merchant_category": "5411",
                "occurred_at": base + pd.Timedelta(hours=i),
                "city": "New York",
                "rule_flag_count": 0,
                "label": 0,
            }
        )

    for i in range(n_attack):
        rows.append(
            {
                "idempotency_key": f"attack-{i}",
                "account_id": "acct-attack",
                "amount_cents": 5000 + i * 10,
                "merchant_category": "7995",
                "occurred_at": base + pd.Timedelta(days=200, seconds=i * 5),
                "city": "New York",
                "rule_flag_count": 2,
                "label": 1,
            }
        )

    return pd.DataFrame(rows)


def test_train_and_evaluate_returns_metrics_with_expected_keys():
    df = _synthetic_dataset()
    model, metrics = train_and_evaluate(df)

    assert model is not None
    for key in ["train_size", "test_size", "test_attack_count", "precision", "recall", "auc"]:
        assert key in metrics

    assert metrics["train_size"] + metrics["test_size"] == len(df)
    assert 0.0 <= metrics["precision"] <= 1.0
    assert 0.0 <= metrics["recall"] <= 1.0


def test_train_and_evaluate_separates_obvious_attack_pattern():
    # The attack rows are trivially separable (different category,
    # amount range, and they're all clustered at the end chronologically,
    # which lands them entirely in the held-out test split) — a working
    # pipeline should recall most of them.
    df = _synthetic_dataset()
    _, metrics = train_and_evaluate(df)

    assert metrics["recall"] > 0.5

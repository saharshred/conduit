import pandas as pd
import pytest

from features import FEATURE_COLUMNS, build_feature_frame


def _txn(idem, account, amount, category, occurred_at, city):
    return {
        "idempotency_key": idem,
        "account_id": account,
        "amount_cents": amount,
        "merchant_category": category,
        "occurred_at": pd.Timestamp(occurred_at, tz="UTC"),
        "city": city,
        "rule_flag_count": 0,
    }


def test_all_feature_columns_present():
    df = pd.DataFrame(
        [_txn("a", "acct-1", 1000, "5411", "2026-01-01T00:00:00", "New York")]
    )
    out = build_feature_frame(df)
    for col in FEATURE_COLUMNS:
        assert col in out.columns


def test_velocity_counts_prior_transactions_in_window_only():
    # 3 transactions 30s apart — the 3rd should see 2 prior within 120s.
    df = pd.DataFrame(
        [
            _txn("a", "acct-1", 100, "5411", "2026-01-01T00:00:00", "New York"),
            _txn("b", "acct-1", 100, "5411", "2026-01-01T00:00:30", "New York"),
            _txn("c", "acct-1", 100, "5411", "2026-01-01T00:01:00", "New York"),
            # far outside the window — should see 0 prior in-window.
            _txn("d", "acct-1", 100, "5411", "2026-01-01T01:00:00", "New York"),
        ]
    )
    out = build_feature_frame(df).set_index("idempotency_key")
    assert out.loc["a", "velocity_count"] == 0
    assert out.loc["b", "velocity_count"] == 1
    assert out.loc["c", "velocity_count"] == 2
    assert out.loc["d", "velocity_count"] == 0


def test_category_seen_before_is_false_on_first_use():
    df = pd.DataFrame(
        [
            _txn("a", "acct-1", 100, "5411", "2026-01-01T00:00:00", "New York"),
            _txn("b", "acct-1", 100, "5411", "2026-01-01T00:05:00", "New York"),
            _txn("c", "acct-1", 100, "7995", "2026-01-01T00:10:00", "New York"),
        ]
    )
    out = build_feature_frame(df).set_index("idempotency_key")
    assert out.loc["a", "category_seen_before"] == 0
    assert out.loc["b", "category_seen_before"] == 1
    assert out.loc["c", "category_seen_before"] == 0


def test_geo_speed_zero_for_first_transaction_and_same_city():
    df = pd.DataFrame(
        [
            _txn("a", "acct-1", 100, "5411", "2026-01-01T00:00:00", "New York"),
            _txn("b", "acct-1", 100, "5411", "2026-01-01T00:05:00", "New York"),
        ]
    )
    out = build_feature_frame(df).set_index("idempotency_key")
    assert out.loc["a", "geo_speed_kmh"] == 0.0
    assert out.loc["b", "geo_speed_kmh"] == 0.0


def test_geo_speed_high_for_impossible_travel():
    # New York -> Tokyo in 4 minutes is not a real flight.
    df = pd.DataFrame(
        [
            _txn("a", "acct-1", 100, "5411", "2026-01-01T00:00:00", "New York"),
            _txn("b", "acct-1", 100, "5411", "2026-01-01T00:04:00", "Tokyo"),
        ]
    )
    out = build_feature_frame(df).set_index("idempotency_key")
    assert out.loc["b", "geo_speed_kmh"] > 1100.0


def test_high_risk_mcc_flag():
    df = pd.DataFrame(
        [
            _txn("a", "acct-1", 100, "7995", "2026-01-01T00:00:00", "New York"),
            _txn("b", "acct-1", 100, "5411", "2026-01-01T00:00:00", "New York"),
        ]
    )
    out = build_feature_frame(df).set_index("idempotency_key")
    assert out.loc["a", "is_high_risk_mcc"] == 1
    assert out.loc["b", "is_high_risk_mcc"] == 0


def test_accounts_do_not_interfere_with_each_other():
    df = pd.DataFrame(
        [
            _txn("a", "acct-1", 100, "5411", "2026-01-01T00:00:00", "New York"),
            _txn("b", "acct-2", 100, "7995", "2026-01-01T00:00:01", "Tokyo"),
        ]
    )
    out = build_feature_frame(df).set_index("idempotency_key")
    assert out.loc["a", "velocity_count"] == 0
    assert out.loc["b", "velocity_count"] == 0
    assert out.loc["b", "geo_speed_kmh"] == 0.0

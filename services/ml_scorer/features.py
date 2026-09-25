"""
Feature engineering shared by train.py and loop.py — kept as a pure
function of a transactions DataFrame so it's unit-testable without a
database and so training and scoring can never silently drift apart
(compute the features one way when training, a slightly different way
when scoring, and the model looks broken for reasons that have nothing
to do with the model).

Deliberately mirrors the signals services/scorer/lib/scorer/{rules,geo}.ex
already compute (velocity window, category drift, geo-implied speed) so
this is a genuine second-stage model reasoning about the same evidence
the rule engine does, not a different, disconnected classifier — plus
amount and time-of-day, which the rule engine doesn't use at all.
"""

import numpy as np
import pandas as pd

# Same fixed city table as Scorer.Geo (services/scorer/lib/scorer/geo.ex) —
# a small lookup, not a geocoding service, by the same design tradeoff.
COORDINATES = {
    "New York": (40.7128, -74.0060),
    "Chicago": (41.8781, -87.6298),
    "Los Angeles": (34.0522, -118.2437),
    "Miami": (25.7617, -80.1918),
    "Seattle": (47.6062, -122.3321),
    "Austin": (30.2672, -97.7431),
    "Denver": (39.7392, -104.9903),
    "Boston": (42.3601, -71.0589),
    "London": (51.5074, -0.1278),
    "Tokyo": (35.6762, 139.6503),
}

VELOCITY_WINDOW_SECONDS = 120
EARTH_RADIUS_KM = 6371.0

FEATURE_COLUMNS = [
    "amount_cents",
    "hour_of_day",
    "weekday",
    "is_high_risk_mcc",
    "velocity_count",
    "category_seen_before",
    "geo_speed_kmh",
    "rule_flag_count",
]


def _haversine_km(lat1, lon1, lat2, lon2):
    lat1, lon1, lat2, lon2 = map(np.radians, [lat1, lon1, lat2, lon2])
    dlat = lat2 - lat1
    dlon = lon2 - lon1
    a = np.sin(dlat / 2) ** 2 + np.cos(lat1) * np.cos(lat2) * np.sin(dlon / 2) ** 2
    return EARTH_RADIUS_KM * 2 * np.arcsin(np.sqrt(np.clip(a, 0, 1)))


def _velocity_counts(times_seconds: np.ndarray) -> np.ndarray:
    """For each index i, count of prior entries (j < i) within the last
    VELOCITY_WINDOW_SECONDS — vectorized via searchsorted since times_seconds
    is already sorted ascending within the account group."""
    thresholds = times_seconds - VELOCITY_WINDOW_SECONDS
    first_in_window = np.searchsorted(times_seconds, thresholds, side="left")
    return np.arange(len(times_seconds)) - first_in_window


def _category_seen_before(categories: pd.Series) -> np.ndarray:
    seen = set()
    out = np.empty(len(categories), dtype=int)
    for i, cat in enumerate(categories):
        out[i] = 1 if cat in seen else 0
        seen.add(cat)
    return out


def _geo_speeds(cities: pd.Series, occurred_at: pd.Series) -> np.ndarray:
    prev_city = cities.shift(1)
    prev_time = occurred_at.shift(1)
    out = np.zeros(len(cities))

    for i in range(len(cities)):
        pc, c = prev_city.iloc[i], cities.iloc[i]
        if pd.isna(pc) or pd.isna(c) or pc not in COORDINATES or c not in COORDINATES:
            continue
        lat1, lon1 = COORDINATES[pc]
        lat2, lon2 = COORDINATES[c]
        distance_km = _haversine_km(lat1, lon1, lat2, lon2)
        if distance_km < 1.0:
            continue
        hours = (occurred_at.iloc[i] - prev_time.iloc[i]).total_seconds() / 3600.0
        out[i] = (distance_km / hours) if hours > 0 else distance_km * 1e6
    return out


def build_feature_frame(df: pd.DataFrame) -> pd.DataFrame:
    """
    Input columns required: idempotency_key, account_id, amount_cents,
    merchant_category, occurred_at (tz-aware datetime64), city (nullable),
    rule_flag_count (int, 0 for transactions the rule engine never flagged).

    Returns a copy of df, sorted by (account_id, occurred_at), with the
    FEATURE_COLUMNS added.
    """
    out = df.sort_values(["account_id", "occurred_at"]).reset_index(drop=True)

    out["hour_of_day"] = out["occurred_at"].dt.hour
    out["weekday"] = out["occurred_at"].dt.weekday
    out["is_high_risk_mcc"] = (out["merchant_category"] == "7995").astype(int)

    velocity = np.empty(len(out), dtype=int)
    category_seen = np.empty(len(out), dtype=int)
    geo_speed = np.empty(len(out), dtype=float)

    for _, idx in out.groupby("account_id").groups.items():
        idx = list(idx)
        times = (
            out.loc[idx, "occurred_at"].values.astype("datetime64[s]").astype(np.int64)
        )
        velocity[idx] = _velocity_counts(times)
        category_seen[idx] = _category_seen_before(out.loc[idx, "merchant_category"])
        geo_speed[idx] = _geo_speeds(out.loc[idx, "city"], out.loc[idx, "occurred_at"])

    out["velocity_count"] = velocity
    out["category_seen_before"] = category_seen
    out["geo_speed_kmh"] = geo_speed

    return out

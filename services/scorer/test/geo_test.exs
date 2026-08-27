defmodule Scorer.GeoTest do
  use ExUnit.Case, async: true

  alias Scorer.Geo

  test "two transactions minutes apart on opposite sides of the world are impossible" do
    t1 = ~U[2026-01-01 09:00:00Z]
    t2 = DateTime.add(t1, 4 * 60, :second) # 4 minutes later

    assert {:impossible, kmh} = Geo.impossible_travel("Chicago", t1, "Tokyo", t2)
    assert kmh > 1100.0
  end

  test "same city, any time gap, is always plausible" do
    t1 = ~U[2026-01-01 09:00:00Z]
    t2 = DateTime.add(t1, 1, :second)
    assert Geo.impossible_travel("Chicago", t1, "Chicago", t2) == :ok
  end

  test "two nearby-ish cities a few hours apart are plausible (commercial flight range)" do
    t1 = ~U[2026-01-01 09:00:00Z]
    t2 = DateTime.add(t1, 5 * 3600, :second) # 5 hours later
    # New York -> Chicago is ~1150km; over 5 hours that's well within
    # plausible travel (even driving), let alone flying.
    assert Geo.impossible_travel("New York", t1, "Chicago", t2) == :ok
  end

  test "an unknown city fails safe rather than guessing" do
    t1 = ~U[2026-01-01 09:00:00Z]
    t2 = DateTime.add(t1, 1, :second)
    assert Geo.impossible_travel("Atlantis", t1, "Tokyo", t2) == :ok
  end

  test "zero time gap between distant cities is impossible, not a division error" do
    t1 = ~U[2026-01-01 09:00:00Z]
    assert {:impossible, :infinity} = Geo.impossible_travel("Chicago", t1, "Tokyo", t1)
  end
end

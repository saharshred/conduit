defmodule Scorer.EvaluateTest do
  use ExUnit.Case, async: true

  alias Scorer.Evaluate

  test "perfect scorer: catches every attack transaction, flags nothing legitimate" do
    r = Evaluate.compute(20, 10_000, 20, 0)
    assert r.precision == 1.0
    assert r.recall == 1.0
    assert r.false_positive_rate == 0.0
  end

  test "matches the real observed run: 16 of 20 attack transactions caught" do
    r = Evaluate.compute(20, 10_060, 16, 71)
    assert_in_delta r.recall, 16 / 20, 0.0001
    assert_in_delta r.precision, 16 / (16 + 71), 0.0001
    assert_in_delta r.false_positive_rate, 71 / 10_060, 0.0001
  end

  test "a scorer that flags nothing has undefined precision, not a crash or a fake zero" do
    r = Evaluate.compute(20, 10_000, 0, 0)
    assert r.precision == nil
    assert r.recall == 0.0
  end

  test "no attack transactions in this run yields undefined recall, not a divide-by-zero" do
    r = Evaluate.compute(0, 10_000, 0, 5)
    assert r.recall == nil
    assert r.precision == 0.0
  end

  test "no legitimate transactions in this run yields undefined false-positive rate" do
    r = Evaluate.compute(20, 0, 20, 0)
    assert r.false_positive_rate == nil
  end
end

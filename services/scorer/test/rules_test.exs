defmodule Scorer.RulesTest do
  use ExUnit.Case, async: true

  alias Scorer.{Rules, Transaction}

  defp txn(overrides) do
    defaults = %{
      idempotency_key: "k",
      account_id: "acct-1",
      amount_cents: 1000,
      merchant_category: "5814",
      occurred_at: ~U[2026-01-01 09:00:00Z]
    }

    struct!(Transaction, Map.merge(defaults, overrides))
  end

  test "an account with no unusual activity is never flagged" do
    txns =
      for i <- 0..3 do
        txn(%{
          idempotency_key: "k#{i}",
          occurred_at: DateTime.add(~U[2026-01-01 09:00:00Z], i * 3600, :second)
        })
      end

    assert Rules.score(txns) == []
  end

  test "five transactions on one account within two minutes trips velocity" do
    txns =
      for i <- 0..4 do
        txn(%{
          idempotency_key: "k#{i}",
          occurred_at: DateTime.add(~U[2026-01-01 09:00:00Z], i * 10, :second)
        })
      end

    flagged = Rules.score(txns)

    assert length(flagged) == 1
    assert [flagged_txn] = flagged
    assert flagged_txn.idempotency_key == "k4"
    assert Enum.any?(flagged_txn.flags, &String.starts_with?(&1, "velocity:"))
  end

  test "the same five transactions spread across a day do not trip velocity" do
    txns =
      for i <- 0..4 do
        txn(%{
          idempotency_key: "k#{i}",
          occurred_at: DateTime.add(~U[2026-01-01 09:00:00Z], i * 3600, :second)
        })
      end

    assert Rules.score(txns) == []
  end

  test "a brand new merchant category after enough history trips mcc_drift" do
    history =
      for i <- 0..4 do
        txn(%{
          idempotency_key: "k#{i}",
          merchant_category: "5814",
          occurred_at: DateTime.add(~U[2026-01-01 09:00:00Z], i * 86_400, :second)
        })
      end

    drift =
      txn(%{
        idempotency_key: "k-drift",
        merchant_category: "7995", # gambling — never seen on this account before
        occurred_at: DateTime.add(~U[2026-01-01 09:00:00Z], 5 * 86_400, :second)
      })

    flagged = Rules.score(history ++ [drift])

    assert length(flagged) == 1
    assert [flagged_txn] = flagged
    assert flagged_txn.idempotency_key == "k-drift"
    assert Enum.any?(flagged_txn.flags, &String.starts_with?(&1, "mcc_drift:"))
  end

  test "an unfamiliar category is not flagged before enough history exists" do
    txns = [
      txn(%{idempotency_key: "k0", merchant_category: "5814"}),
      txn(%{
        idempotency_key: "k1",
        merchant_category: "7995",
        occurred_at: DateTime.add(~U[2026-01-01 09:00:00Z], 3600, :second)
      })
    ]

    # only one prior transaction — below @min_history_for_drift, so no
    # drift flag should fire yet even though the category is new
    assert Rules.score(txns) == []
  end

  test "a transaction can trip both rules at once" do
    # 5 prior transactions, all within the velocity window and all the
    # same category — enough history for mcc_drift to be eligible *and*
    # enough recent volume for velocity to already be at the threshold.
    burst =
      for i <- 0..4 do
        txn(%{
          idempotency_key: "k#{i}",
          merchant_category: "5814",
          occurred_at: DateTime.add(~U[2026-01-01 09:00:00Z], i * 10, :second)
        })
      end

    both =
      txn(%{
        idempotency_key: "k-both",
        merchant_category: "7995",
        occurred_at: DateTime.add(~U[2026-01-01 09:00:00Z], 45, :second)
      })

    flagged = Rules.score(burst ++ [both])

    # the 5th burst transaction also trips velocity on its own — that's
    # correct behavior, not a bug — so pick out the one we actually care
    # about rather than assuming it's the only flagged transaction.
    both_flagged = Enum.find(flagged, &(&1.idempotency_key == "k-both"))
    assert both_flagged != nil
    assert length(both_flagged.flags) == 2
  end
end

defmodule Scorer.Rules do
  @moduledoc """
  Rule-based fraud scoring — the actual reason this service is written in
  Elixir: Ramp's real-time card-authorization stack runs a stream of
  transactions through a set of cheap rule checks and has to decide in
  milliseconds, and this mirrors that shape.

  Two rules are implemented for real:

    * velocity — too many transactions on one account in too short a window
    * mcc_drift — an account's spending suddenly hits a merchant category
      it has never used before

  A third (geo-impossible: two transactions too far apart to both be
  genuine given the time between them) needs a location field the
  synthetic dataset generator doesn't produce yet — see the module doc in
  ../README.md for what that would take.
  """

  alias Scorer.Transaction

  @velocity_window_seconds 120
  @velocity_threshold 5
  @min_history_for_drift 5

  @doc """
  Scores a batch of transactions (any order, any mix of accounts) and
  returns only the ones that tripped at least one rule, each annotated
  with which rule(s) fired and why.
  """
  @spec score([Transaction.t()]) :: [Transaction.t()]
  def score(transactions) when is_list(transactions) do
    transactions
    |> Enum.group_by(& &1.account_id)
    |> Enum.flat_map(fn {_account, txns} ->
      txns
      |> Enum.sort_by(& &1.occurred_at, DateTime)
      |> score_account_history()
    end)
  end

  # Walks one account's transactions in chronological order so each rule
  # only ever looks backward — never at "future" transactions, which
  # would be cheating relative to how this runs in production (scoring a
  # transaction as it arrives, not after the fact with full hindsight).
  defp score_account_history(sorted_txns) do
    sorted_txns
    |> Enum.with_index()
    |> Enum.map(fn {txn, idx} ->
      prior = Enum.take(sorted_txns, idx)
      flags = velocity_flags(txn, prior) ++ mcc_drift_flags(txn, prior)
      %{txn | flags: flags}
    end)
    |> Enum.filter(&(&1.flags != []))
  end

  defp velocity_flags(txn, prior) do
    window_start = DateTime.add(txn.occurred_at, -@velocity_window_seconds, :second)

    recent_count =
      prior
      |> Enum.filter(fn t -> DateTime.compare(t.occurred_at, window_start) != :lt end)
      |> length()
      |> Kernel.+(1) # the transaction being scored counts too

    if recent_count >= @velocity_threshold do
      ["velocity: #{recent_count} transactions within #{@velocity_window_seconds}s"]
    else
      []
    end
  end

  defp mcc_drift_flags(txn, prior) do
    known = prior |> Enum.map(& &1.merchant_category) |> MapSet.new()

    cond do
      length(prior) < @min_history_for_drift ->
        [] # not enough history yet to call anything a "drift"

      MapSet.member?(known, txn.merchant_category) ->
        []

      true ->
        ["mcc_drift: first time this account has used category #{txn.merchant_category}"]
    end
  end
end

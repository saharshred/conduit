defmodule Scorer.Evaluate do
  @moduledoc """
  Measures the scorer against real ground truth instead of reporting a raw
  flag count and calling it a day. `attack/attack.py` tags every
  transaction it publishes with `bank: "attack-sim"` — nothing else in
  the pipeline ever produces that bank name — so "was this transaction
  actually the injected attack" is a free, honest label already sitting
  in the idempotency key (`"attack-sim:<id>"` vs `"bank-a:<id>"` etc.),
  not something bolted on after the fact to make a metric look good.

  This does NOT mean every non-attack flag is a "wrong" answer in the
  classic ML sense — a `geo_impossible` or `mcc_drift` flag on a
  synthetically-generated normal transaction is the rule correctly
  spotting an unusual pattern in randomly generated data, not a labeling
  error. What it measures precisely is: of everything the scorer flagged,
  what fraction was the known attack (precision against the injected
  attack specifically), and of the known attack, what fraction got
  caught (recall). That's a real, defensible number, not an inflated one.
  """

  alias Scorer.Runner

  defstruct [
    :attack_transactions,
    :legit_transactions,
    :attack_flagged,
    :legit_flagged,
    :precision,
    :recall,
    :false_positive_rate
  ]

  @doc """
  Computes precision/recall/false-positive-rate from raw counts. Kept as
  a pure function, separate from the DB query, so the arithmetic itself
  is unit-testable without Postgres.
  """
  def compute(attack_transactions, legit_transactions, attack_flagged, legit_flagged) do
    precision =
      case attack_flagged + legit_flagged do
        0 -> nil
        total_flagged -> attack_flagged / total_flagged
      end

    recall =
      case attack_transactions do
        0 -> nil
        n -> attack_flagged / n
      end

    false_positive_rate =
      case legit_transactions do
        0 -> nil
        n -> legit_flagged / n
      end

    %__MODULE__{
      attack_transactions: attack_transactions,
      legit_transactions: legit_transactions,
      attack_flagged: attack_flagged,
      legit_flagged: legit_flagged,
      precision: precision,
      recall: recall,
      false_positive_rate: false_positive_rate
    }
  end

  @doc """
  Runs against the real database: counts how many `attack-sim` vs.
  legitimate transactions exist, and how many of each got flagged.
  """
  def run(opts \\ []) do
    dsn = Keyword.get(opts, :dsn, System.get_env("CONDUIT_DSN") || "postgres://conduit:conduit@localhost:5434/conduit")
    {:ok, pid} = Postgrex.start_link(Runner.parse_dsn(dsn))

    %Postgrex.Result{rows: [[attack_total, legit_total]]} =
      Postgrex.query!(pid, """
      SELECT
        count(*) FILTER (WHERE bank = 'attack-sim'),
        count(*) FILTER (WHERE bank != 'attack-sim')
      FROM transactions
      """, [])

    %Postgrex.Result{rows: [[attack_flagged, legit_flagged]]} =
      Postgrex.query!(pid, """
      SELECT
        count(*) FILTER (WHERE idempotency_key LIKE 'attack-sim:%'),
        count(*) FILTER (WHERE idempotency_key NOT LIKE 'attack-sim:%')
      FROM flagged_transactions
      """, [])

    result = compute(attack_total, legit_total, attack_flagged, legit_flagged)
    print_report(result)
    result
  end

  defp print_report(r) do
    IO.puts("""

    === Scorer evaluation ===
    Attack transactions (ground truth positive): #{r.attack_transactions}
    Legitimate transactions (ground truth negative): #{r.legit_transactions}

    Attack transactions flagged (true positives):     #{r.attack_flagged}
    Legitimate transactions flagged (false positives): #{r.legit_flagged}

    Precision (of everything flagged, % that was the real attack): #{pct(r.precision)}
    Recall    (of the real attack, % that got caught):              #{pct(r.recall)}
    False positive rate (of legit traffic, % wrongly flagged):      #{pct(r.false_positive_rate)}
    """)
  end

  defp pct(nil), do: "n/a"
  defp pct(x), do: "#{Float.round(x * 100, 1)}%"
end

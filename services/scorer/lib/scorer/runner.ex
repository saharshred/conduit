defmodule Scorer.Runner do
  @moduledoc """
  Connects to the same Postgres database the Go ingestor writes into,
  loads every transaction, runs `Scorer.Rules` against it grouped by
  account, and upserts what got flagged into `flagged_transactions` — the
  table services/dashboard (Rails) reads from.

  `run/1` does one pass and returns (used by tests, and by a human running
  it once from a shell). `loop/1` is what actually runs in the container —
  polls on an interval so the dashboard reflects new transactions and new
  attacks without anyone manually re-invoking the scorer. Re-scoring the
  same data repeatedly is safe: `upsert_flagged` only touches `reasons`
  on conflict, never `status`, so a reviewer's approve/deny decision on
  an already-seen transaction is never clobbered by the next poll.
  """

  alias Scorer.{Rules, Transaction}

  def run(opts \\ []) do
    dsn = Keyword.get(opts, :dsn, default_dsn())
    {:ok, pid} = Postgrex.start_link(parse_dsn(dsn))
    scan_once(pid)
  end

  @doc """
  Runs forever, re-scoring all transactions every `interval_ms` (default
  5s — matched to the dashboard's own 5s poll, so a fresh flag is visible
  within one dashboard refresh of an attack landing).
  """
  def loop(opts \\ []) do
    interval_ms = Keyword.get(opts, :interval_ms, 5_000)
    dsn = Keyword.get(opts, :dsn, default_dsn())
    {:ok, pid} = Postgrex.start_link(parse_dsn(dsn))

    IO.puts("scorer: polling every #{interval_ms}ms\n")
    loop_forever(pid, interval_ms)
  end

  defp loop_forever(pid, interval_ms) do
    {transactions, flagged} = scan_once(pid)
    IO.puts("[#{DateTime.utc_now() |> DateTime.to_iso8601()}] scored #{length(transactions)}, #{length(flagged)} flagged")
    Process.sleep(interval_ms)
    loop_forever(pid, interval_ms)
  end

  defp scan_once(pid) do
    %Postgrex.Result{rows: rows} =
      Postgrex.query!(
        pid,
        "SELECT idempotency_key, account_id, amount_cents, merchant_category, city, occurred_at FROM transactions ORDER BY occurred_at",
        []
      )

    transactions =
      Enum.map(rows, fn [key, account, amount, category, city, occurred_at] ->
        %Transaction{
          idempotency_key: key,
          account_id: account,
          amount_cents: amount,
          merchant_category: category,
          city: city,
          occurred_at: naive_to_utc(occurred_at)
        }
      end)

    flagged = Rules.score(transactions)
    Enum.each(flagged, &upsert_flagged(pid, &1))

    {transactions, flagged}
  end

  # Upserts by idempotency_key — safe to re-run the scorer against the
  # same data (every poll, in loop mode) without duplicating rows or
  # clobbering a reviewer's approve/deny decision on a transaction
  # that's already been looked at.
  defp upsert_flagged(pid, txn) do
    Postgrex.query!(
      pid,
      """
      INSERT INTO flagged_transactions
        (idempotency_key, account_id, amount_cents, merchant_category, reasons, occurred_at, status, created_at, updated_at)
      VALUES ($1, $2, $3, $4, $5, $6, 'pending', now(), now())
      ON CONFLICT (idempotency_key) DO UPDATE
        SET reasons = EXCLUDED.reasons, updated_at = now()
      """,
      [txn.idempotency_key, txn.account_id, txn.amount_cents, txn.merchant_category,
       Enum.join(txn.flags, "\n"), DateTime.to_naive(txn.occurred_at)]
    )
  end

  defp naive_to_utc(%NaiveDateTime{} = ndt), do: DateTime.from_naive!(ndt, "Etc/UTC")
  defp naive_to_utc(%DateTime{} = dt), do: dt

  defp default_dsn, do: System.get_env("CONDUIT_DSN") || "postgres://conduit:conduit@localhost:5434/conduit"

  # Minimal DSN parser — good enough for postgres://user:pass@host:port/db,
  # avoids pulling in a full URI-parsing dependency for one call site.
  defp parse_dsn(dsn) do
    uri = URI.parse(dsn)
    [user, pass] = String.split(uri.userinfo || "conduit:conduit", ":", parts: 2)

    [
      hostname: uri.host,
      port: uri.port || 5432,
      username: user,
      password: pass,
      database: String.trim_leading(uri.path || "/conduit", "/")
    ]
  end
end

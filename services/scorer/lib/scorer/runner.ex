defmodule Scorer.Runner do
  @moduledoc """
  Connects to the same Postgres database the Go ingestor writes into,
  loads every transaction, runs `Scorer.Rules` against it grouped by
  account, and upserts what got flagged into `flagged_transactions` — the
  table services/dashboard (Rails) reads from. Batch mode for now; wiring
  this to consume the RabbitMQ stream directly instead of reading what's
  already landed is the natural next step.
  """

  alias Scorer.{Rules, Transaction}

  def run(opts \\ []) do
    dsn = Keyword.get(opts, :dsn, default_dsn())
    config = parse_dsn(dsn)

    {:ok, pid} = Postgrex.start_link(config)

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

    IO.puts("scored #{length(transactions)} transactions, #{length(flagged)} flagged\n")

    Enum.each(flagged, fn txn ->
      IO.puts("FLAGGED #{txn.idempotency_key} (#{txn.account_id}): #{Enum.join(txn.flags, ", ")}")
      upsert_flagged(pid, txn)
    end)

    {transactions, flagged}
  end

  # Upserts by idempotency_key — safe to re-run the scorer against the
  # same data (e.g. on a schedule) without duplicating rows or clobbering
  # a reviewer's approve/deny decision on a transaction that's already
  # been looked at.
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

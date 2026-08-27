defmodule Scorer.Transaction do
  @moduledoc """
  The scorer's view of a transaction — just enough fields to run the rules
  in `Scorer.Rules`. Built from rows in Postgres' `transactions` table
  (populated by CONDUIT's Go ingestor), not from RabbitMQ directly, to keep
  Day 4 scoped to "read what's already landed and durable."
  """

  @enforce_keys [:idempotency_key, :account_id, :amount_cents, :merchant_category, :occurred_at]
  defstruct [:idempotency_key, :account_id, :amount_cents, :merchant_category, :occurred_at, :city, flags: []]

  @type t :: %__MODULE__{
          idempotency_key: String.t(),
          account_id: String.t(),
          amount_cents: integer(),
          merchant_category: String.t(),
          occurred_at: DateTime.t(),
          city: String.t() | nil,
          flags: [String.t()]
        }
end

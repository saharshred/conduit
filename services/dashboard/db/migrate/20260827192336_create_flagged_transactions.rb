class CreateFlaggedTransactions < ActiveRecord::Migration[8.1]
  def change
    # Written by services/scorer (Elixir) whenever a transaction trips one
    # or more rules. `status` starts at "pending" and a reviewer moves it
    # to "approved" or "denied" from this dashboard — the actual point of
    # the table existing.
    create_table :flagged_transactions do |t|
      t.string :idempotency_key, null: false
      t.string :account_id, null: false
      t.bigint :amount_cents, null: false
      t.string :merchant_category, null: false
      t.text :reasons, null: false
      t.datetime :occurred_at, null: false
      t.string :status, null: false, default: "pending"

      t.timestamps
    end
    add_index :flagged_transactions, :idempotency_key, unique: true
    add_index :flagged_transactions, :status
  end
end

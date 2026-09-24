require "test_helper"

class DashboardMetricsTest < ActiveSupport::TestCase
  # `transactions` is owned by the Go ingestor, not this app — no
  # ActiveRecord model for it on purpose (see services/dashboard/README).
  # Seeding it directly with SQL here is the correct way to set up this
  # test, not a workaround.
  def insert_transaction(idempotency_key:, bank:)
    ActiveRecord::Base.connection.execute(<<~SQL)
      INSERT INTO transactions
        (idempotency_key, bank, native_id, account_id, amount_cents, currency, merchant_name, merchant_category, occurred_at)
      VALUES
        ('#{idempotency_key}', '#{bank}', '#{idempotency_key}', 'acct-1', 100, 'USD', 'Test Merchant', '5814', now())
    SQL
  end

  setup do
    ActiveRecord::Base.connection.execute("TRUNCATE transactions, flagged_transactions RESTART IDENTITY")
  end

  test "computes precision, recall, and false-positive rate from real ground truth" do
    # 2 attack transactions, both flagged (true positives)
    insert_transaction(idempotency_key: "attack-sim:1", bank: "attack-sim")
    insert_transaction(idempotency_key: "attack-sim:2", bank: "attack-sim")
    FlaggedTransaction.create!(idempotency_key: "attack-sim:1", account_id: "a", amount_cents: 1, merchant_category: "x", reasons: "velocity", occurred_at: Time.current)
    FlaggedTransaction.create!(idempotency_key: "attack-sim:2", account_id: "a", amount_cents: 1, merchant_category: "x", reasons: "velocity", occurred_at: Time.current)

    # 8 legitimate transactions, 1 flagged (a false positive)
    8.times { |i| insert_transaction(idempotency_key: "bank-a:#{i}", bank: "bank-a") }
    FlaggedTransaction.create!(idempotency_key: "bank-a:0", account_id: "a", amount_cents: 1, merchant_category: "x", reasons: "mcc_drift", occurred_at: Time.current)

    metrics = DashboardMetrics.compute

    assert_equal 2, metrics[:attack_total]
    assert_equal 2, metrics[:attack_flagged]
    assert_in_delta 2.0 / 3, metrics[:precision], 0.001   # 2 true positives, 1 false positive
    assert_in_delta 1.0, metrics[:recall], 0.001           # caught both attack transactions
    assert_in_delta 1.0 / 8, metrics[:false_positive_rate], 0.001
  end

  test "returns nil rather than dividing by zero when there is no data yet" do
    metrics = DashboardMetrics.compute
    assert_equal 0, metrics[:attack_total]
    assert_nil metrics[:precision]
    assert_nil metrics[:recall]
    assert_nil metrics[:false_positive_rate]
  end
end

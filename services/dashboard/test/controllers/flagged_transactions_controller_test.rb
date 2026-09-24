require "test_helper"

class FlaggedTransactionsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @pending = FlaggedTransaction.create!(
      idempotency_key: "bank-a:txn-1",
      account_id: "acct-1",
      amount_cents: 1999,
      merchant_category: "5814",
      reasons: "velocity: 5 transactions within 120s",
      occurred_at: Time.current,
      status: "pending"
    )
  end

  test "index renders the pending list by default" do
    get flagged_transactions_url
    assert_response :success
    assert_select "h1", "Flagged transactions"
    assert_select "a[href=?]", flagged_transaction_path(@pending), text: @pending.account_id
  end

  test "index filters by status" do
    approved = FlaggedTransaction.create!(
      idempotency_key: "bank-b:txn-2", account_id: "acct-2", amount_cents: 500,
      merchant_category: "5411", reasons: "mcc_drift: first time category 5411",
      occurred_at: Time.current, status: "approved"
    )

    get flagged_transactions_url(status: "approved")
    assert_response :success
    assert_select "a[href=?]", flagged_transaction_path(approved), text: approved.account_id
    assert_select "a[href=?]", flagged_transaction_path(@pending), false
  end

  test "an unrecognized status param falls back to pending rather than erroring" do
    get flagged_transactions_url(status: "not-a-real-status")
    assert_response :success
    assert_select "a[href=?]", flagged_transaction_path(@pending), text: @pending.account_id
  end

  test "show renders a single flagged transaction with its reasons" do
    get flagged_transaction_url(@pending)
    assert_response :success
    assert_match @pending.idempotency_key, response.body
    assert_match "velocity: 5 transactions within 120s", response.body
  end

  test "approve actually persists the status change" do
    post approve_flagged_transaction_url(@pending)
    assert_redirected_to flagged_transactions_url
    assert_equal "approved", @pending.reload.status
  end

  test "deny actually persists the status change" do
    post deny_flagged_transaction_url(@pending)
    assert_redirected_to flagged_transactions_url
    assert_equal "denied", @pending.reload.status
  end
end

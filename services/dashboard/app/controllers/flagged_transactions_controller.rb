class FlaggedTransactionsController < ApplicationController
  def index
    @status_filter = FlaggedTransaction::STATUSES.include?(params[:status]) ? params[:status] : "pending"
    @flagged_transactions = FlaggedTransaction.where(status: @status_filter).recent_first
    @counts = FlaggedTransaction.group(:status).count
    @metrics = DashboardMetrics.compute
  end

  def show
    @flagged_transaction = FlaggedTransaction.find(params[:id])
  end

  def approve
    transaction = FlaggedTransaction.find(params[:id])
    transaction.update!(status: "approved")
    redirect_to flagged_transactions_path(status: params[:status]), notice: "Approved #{transaction.idempotency_key}."
  end

  def deny
    transaction = FlaggedTransaction.find(params[:id])
    transaction.update!(status: "denied")
    redirect_to flagged_transactions_path(status: params[:status]), notice: "Denied #{transaction.idempotency_key}."
  end
end

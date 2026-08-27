class FlaggedTransaction < ApplicationRecord
  STATUSES = %w[pending approved denied].freeze

  validates :idempotency_key, presence: true, uniqueness: true
  validates :account_id, :amount_cents, :merchant_category, :occurred_at, presence: true
  validates :status, inclusion: { in: STATUSES }

  scope :pending, -> { where(status: "pending") }
  scope :recent_first, -> { order(occurred_at: :desc) }

  def reason_list
    (reasons || "").split("\n").reject(&:blank?)
  end

  def amount_dollars
    amount_cents / 100.0
  end
end

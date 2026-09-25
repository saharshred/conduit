class AddMlScoreToFlaggedTransactions < ActiveRecord::Migration[8.1]
  def change
    # Written by services/ml_scorer (Python) — a second-stage classifier
    # scored against every rule-flagged transaction, nullable because the
    # ML container trains once at startup and hasn't necessarily scored a
    # given row yet when it first appears.
    add_column :flagged_transactions, :ml_score, :float
  end
end

# Precision/recall/false-positive-rate against real ground truth —
# attack/attack.py tags every transaction it publishes with
# bank: "attack-sim", nothing else in the pipeline ever produces that
# bank name, so "was this actually the injected attack" is a free,
# honest label already sitting in idempotency_key ("attack-sim:<id>" vs
# "bank-a:<id>" etc.), not something bolted on to make a metric look
# good. Mirrors services/scorer/lib/scorer/evaluate.ex — same math, same
# ground-truth trick, computed independently on the Rails side so the
# dashboard doesn't need to shell out to Elixir to show it.
class DashboardMetrics
  def self.compute
    totals = ActiveRecord::Base.connection.select_one(<<~SQL)
      SELECT
        count(*) FILTER (WHERE bank = 'attack-sim') AS attack_total,
        count(*) FILTER (WHERE bank != 'attack-sim') AS legit_total
      FROM transactions
    SQL

    flagged = ActiveRecord::Base.connection.select_one(<<~SQL)
      SELECT
        count(*) FILTER (WHERE idempotency_key LIKE 'attack-sim:%') AS attack_flagged,
        count(*) FILTER (WHERE idempotency_key NOT LIKE 'attack-sim:%') AS legit_flagged
      FROM flagged_transactions
    SQL

    attack_total = totals["attack_total"].to_i
    legit_total = totals["legit_total"].to_i
    attack_flagged = flagged["attack_flagged"].to_i
    legit_flagged = flagged["legit_flagged"].to_i
    total_flagged = attack_flagged + legit_flagged

    {
      attack_total: attack_total,
      attack_flagged: attack_flagged,
      precision: total_flagged.positive? ? attack_flagged.to_f / total_flagged : nil,
      recall: attack_total.positive? ? attack_flagged.to_f / attack_total : nil,
      false_positive_rate: legit_total.positive? ? legit_flagged.to_f / legit_total : nil
    }
  end
end

# Replaces the old 5s meta-refresh with a real push: LISTEN on the same
# Postgres connection the Elixir scorer NOTIFYs on
# (services/scorer/lib/scorer/runner.ex), and broadcast a fresh render of
# the pending list over Turbo Streams the instant something new lands —
# not "within one poll cycle," instantly.
#
# Runs on a plain Ruby Thread rather than a job/worker: this app has no
# background job runtime worth spinning up for one long-lived listener,
# and `PG::Connection#wait_for_notify` is precisely the blocking-wait
# primitive this needs — it sleeps the thread until Postgres has
# something to say, not a polling loop with a sleep in it.
class LiveBroadcaster
  CHANNEL = "conduit_flagged"

  def self.start!
    Thread.new { new.listen_forever }
  end

  def listen_forever
    conn = connect
    conn.exec("LISTEN #{CHANNEL}")
    Rails.logger.info("LiveBroadcaster: listening on #{CHANNEL}")

    loop do
      # Timeout matters here for its own sake, not just to avoid blocking
      # forever: it lets this loop notice a dead/reset connection (via
      # the rescue below) instead of hanging on a socket that will never
      # receive anything again after Postgres restarts.
      conn.wait_for_notify(30)
      broadcast!
    end
  rescue PG::Error => e
    Rails.logger.error("LiveBroadcaster: connection error (#{e.message}), reconnecting in 2s")
    sleep 2
    retry
  end

  def broadcast!
    ActiveRecord::Base.connection_pool.with_connection do
      html = ApplicationController.renderer.render(
        partial: "flagged_transactions/live_content",
        assigns: {
          status_filter: "pending",
          flagged_transactions: FlaggedTransaction.where(status: "pending").recent_first,
          counts: FlaggedTransaction.group(:status).count,
          metrics: DashboardMetrics.compute
        }
      )

      Turbo::StreamsChannel.broadcast_replace_to(
        "flagged_transactions_pending",
        target: "flagged-live",
        html: html
      )
    end
  rescue => e
    # A broadcast failing should never take the listener thread down —
    # log it and keep listening for the next notification.
    Rails.logger.error("LiveBroadcaster: broadcast failed: #{e.class}: #{e.message}")
  end

  private

  def connect
    PG.connect(
      host: ENV.fetch("CONDUIT_DB_HOST", "postgres"),
      port: ENV.fetch("CONDUIT_DB_PORT", "5432"),
      user: ENV.fetch("CONDUIT_DB_USER", "conduit"),
      password: ENV.fetch("CONDUIT_DB_PASSWORD", "conduit"),
      dbname: ENV.fetch("CONDUIT_DB_NAME", "conduit")
    )
  end
end

# Only start the LISTEN/NOTIFY thread when actually running the app
# server — not during db:migrate, tailwindcss:build, rails console, rake
# tasks, etc. `Rails::Server` is only defined in the process started by
# `bin/rails server`.
Rails.application.config.after_initialize do
  if defined?(Rails::Server)
    LiveBroadcaster.start!
  end
end

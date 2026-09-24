ENV["RAILS_ENV"] ||= "test"
require_relative "../config/environment"
require "rails/test_help"

module ActiveSupport
  class TestCase
    # Runs each test in its own transaction, rolled back afterward — the
    # standard Rails isolation pattern. Fine here even though
    # `transactions` (the Go-owned table) is shared infrastructure: tests
    # only ever write to `flagged_transactions`, and only ever read
    # `transactions` for counts, never mutate it.
    parallelize(workers: :number_of_processors)
  end
end

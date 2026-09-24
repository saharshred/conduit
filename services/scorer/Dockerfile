FROM elixir:1.17-alpine

WORKDIR /app
RUN mix local.hex --force && mix local.rebar --force

COPY mix.exs mix.lock ./
RUN mix deps.get

COPY . .
RUN mix compile

# Runs forever, polling every 5s — see Scorer.Runner.loop for why that
# interval matches the dashboard's own polling.
ENTRYPOINT ["mix", "run", "--no-halt", "-e", "Scorer.Runner.loop()"]

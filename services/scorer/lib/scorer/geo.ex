defmodule Scorer.Geo do
  @moduledoc """
  Geo-impossible-travel: two transactions on the same account, far enough
  apart and close enough in time that the account holder would have had
  to travel faster than a commercial flight to genuinely make both.

  Needed a location on each transaction, which the original dataset didn't
  carry — added `city` end to end (dataset generator → each mock bank's
  native format → the unified schema → this table) rather than fake it
  with placeholder coordinates.

  Coordinates are a small fixed table, not a geocoding service — plenty
  for flagging "Chicago at 9:00, Tokyo at 9:04" as impossible without a
  network dependency.
  """

  # {lat, lon} in decimal degrees.
  @coordinates %{
    "New York" => {40.7128, -74.0060},
    "Chicago" => {41.8781, -87.6298},
    "Los Angeles" => {34.0522, -118.2437},
    "Miami" => {25.7617, -80.1918},
    "Seattle" => {47.6062, -122.3321},
    "Austin" => {30.2672, -97.7431},
    "Denver" => {39.7392, -104.9903},
    "Boston" => {42.3601, -71.0589},
    "London" => {51.5074, -0.1278},
    "Tokyo" => {35.6762, 139.6503}
  }

  @earth_radius_km 6371.0
  # A generous ceiling for "a human could plausibly have traveled this
  # fast" — commercial flight is ~900 km/h; 1100 km/h leaves room for time
  # zone / clock-skew slop without missing genuine impossibilities like
  # two different continents four minutes apart.
  @max_plausible_kmh 1100.0

  @doc "All city names this module knows coordinates for — used by the dataset generator."
  def known_cities, do: Map.keys(@coordinates)

  @doc """
  Given two {city, datetime} pairs for the same account, returns
  {:ok, implied_kmh} if travel between them would require exceeding
  @max_plausible_kmh, or :ok if it's physically plausible (or either city
  is unknown — fails safe by not flagging what it can't measure).
  """
  def impossible_travel(city_a, time_a, city_b, time_b) do
    with {lat_a, lon_a} <- Map.get(@coordinates, city_a),
         {lat_b, lon_b} <- Map.get(@coordinates, city_b) do
      distance_km = haversine_km(lat_a, lon_a, lat_b, lon_b)
      hours = abs(DateTime.diff(time_b, time_a, :second)) / 3600.0

      cond do
        distance_km < 1.0 -> :ok
        hours <= 0.0 -> {:impossible, :infinity}
        distance_km / hours > @max_plausible_kmh -> {:impossible, distance_km / hours}
        true -> :ok
      end
    else
      nil -> :ok # unknown city — can't measure, don't guess
    end
  end

  defp haversine_km(lat1, lon1, lat2, lon2) do
    dlat = deg2rad(lat2 - lat1)
    dlon = deg2rad(lon2 - lon1)

    a =
      :math.sin(dlat / 2) * :math.sin(dlat / 2) +
        :math.cos(deg2rad(lat1)) * :math.cos(deg2rad(lat2)) *
          :math.sin(dlon / 2) * :math.sin(dlon / 2)

    c = 2 * :math.atan2(:math.sqrt(a), :math.sqrt(1 - a))
    @earth_radius_km * c
  end

  defp deg2rad(deg), do: deg * :math.pi() / 180
end

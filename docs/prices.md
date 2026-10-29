# Prices

Aiptimize ingests Nord Pool day-ahead prices from the public Data Portal API,
stores the raw 15-minute points, and derives 30-minute blocks with the
configured import/export tariff.

## Provider

```http
GET https://dataportal-api.nordpoolgroup.com/api/DayAheadPrices
    ?date=YYYY-MM-DD&market=DayAhead&deliveryArea=SE4&currency=SEK
User-Agent: aiptimize/<version> (+https://github.com/AhmetBarac/aiptimize)
```

No key or special header is required. The client self-limits to roughly one
request per second and applies a 20-second timeout.

| Response | Handling |
|---|---|
| `200` | Parse `multiAreaEntries[].deliveryStart/deliveryEnd/entryPerArea[area]`; values are currency/MWh. `areaStates[].state` becomes `Final` or `Preliminary`. |
| `204` | Tomorrow is not published yet: `ErrNotPublished`, retried on the poll schedule. |
| `400`, `401`, `403` | Configuration or authority error: surfaced as `HTTPError`, cached data is kept. |
| `5xx` | Provider error, cached data is kept and the poller retries with backoff. |

Delivery dates on the portal are CET-based. The client filters points to the
site's local day (`Europe/Stockholm`), so a `date=` request for a Stockholm day
yields 92, 96 or 100 points depending on DST.

## Poll schedule

The `PricePoller` evaluates once a minute:

1. **Today** — fetch on startup and whenever the stored day is older than six
   hours or missing.
2. **Tomorrow** — from 13:02 local, retry every 15 minutes until it is published
   or 23:00. Stop as soon as it has data.
3. **Preliminary** — retry a preliminary tomorrow every six hours until it is
   final, so a re-publication replaces the stored day.
4. Failed attempts are retried at the same 15-minute interval; failures never
   delete the last valid data.

A successful fetch is logged and published as an SSE `prices` event. Manual
refresh (`POST /api/v1/prices/refresh`) follows the same path.

## Storage

`prices` is keyed by `(area, currency, delivery_start)`. Each day is replaced
transactionally (`DELETE` the local day range, then upsert), so a preliminary
re-publication cannot leave stale points behind. `fetched_at` records the fetch
time; `state` records the Nord Pool state per point.

## 15 → 30 minute aggregation

Each point is assigned to the half-hour block containing its UTC start
(`deliveryStart.Truncate(30m)`), and the block price is the mean of its points.
Gaps are emitted as `missing` blocks with zero points rather than silently
shifting later blocks into place. A block with fewer than two points is marked
missing and excluded from planning and cheapest-window selection.

DST is handled from UTC instants, not point counts:

| Day | Points | Blocks |
|---|---|---|
| Normal | 96 | 48 |
| Spring forward | 92 | 46 |
| Autumn fall-back | 100 | 50 |

On the autumn day, the repeated local 02:00 hour remains two distinct blocks
because their UTC starts differ by one hour.

## Tariff

```text
import = (spot_sek_kwh + grid_fee + energy_tax + markup) × (1 + vat/100)
export = spot_sek_kwh − export_fee
```

Negative spot prices are valid and are not clamped. The tariff is read from the
`price` settings section, is shown in the UI, and is stored with each computed
plan so a plan can be reproduced later. No FX conversion is invented.

## API

`GET /api/v1/prices?date=YYYY-MM-DD` or `?from=RFC3339&to=RFC3339` (maximum 7
days, default today + tomorrow) returns:

- `points` — 15-minute spot points;
- `blocks` — 30-minute blocks with `price_mwh`, `price_kwh`, `import_price`,
  `export_price`, `state`, `missing`, `points`;
- `tariff` — the tariff used;
- `summary` — min/max/average import and the cheapest contiguous 2-hour windows;
- `state`, `fetched_at`, `source`, `attribution`, `stale`.

`POST /api/v1/prices/refresh` accepts an optional `{"date":"YYYY-MM-DD"}` and
returns one result per day (`ok`, `points`, `state`, or a code such as
`price_not_published`). It requires a session and a CSRF token, but no hardware
arm gate because it only touches the local cache.

## UI

The `/prices` screen shows a 15-minute bar chart with a "now" marker, day tabs,
min/max/average import, the cheapest 2-hour windows for the selected day, the
30-minute block table (spot/import/export/state), the tariff breakdown, the
fetch time and the required attribution. It is usable at 390 × 844 without
horizontal scrolling.

## Attribution and terms

> Spot prices: Nord Pool Data Portal

The endpoint is the website backend and is intended for personal, low-volume
use. Aiptimize does not redistribute Nord Pool data. Users are responsible for
their agreement with Nord Pool.

## Simulator

With `AIPTIMIZE_SIMULATOR=true` the poller uses a deterministic local generator
instead of the network. It produces the correct number of points for each local
day (including DST) and is used by the Playwright smoke test and demos.

## Tests

`internal/prices` covers provider-schema fixtures for a 96-point day and the
100-point autumn DST day, a synthetic 92-point spring day, gap/missing blocks,
negative prices, tariff math, cheapest windows, provider errors, local-day
trimming, storage replacement, snapshots and the poll schedule. `internal/api`
covers the HTTP endpoints and status integration. Fixtures are synthesised with
`testdata/nordpool/generate.py`; the JSON shape was verified against a live Data
Portal response so the repository does not redistribute Nord Pool data.

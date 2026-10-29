# User interface

The visual target is the EcoFlow app family (dark, card-based energy
dashboards): near-black background, charcoal cards, large tabular values with
small muted units, one accent colour per energy source, pill buttons and a
minimal bottom navigation. Reference images supplied by the project owner live
in `GUI inspiration/` and are intentionally not committed.

## Design tokens

| Token | Dark (default) | Use |
|---|---|---|
| Background | `#000000` | Page, phone chrome |
| Surface | `#141416` | Cards |
| Surface 2 | `#1c1c1f` | Inputs, tiles, inactive pills |
| Text | `#f5f5f7` | Values and headings |
| Muted | `#9a9aa2` | Labels and secondary text |
| Border | `rgba(255,255,255,.08)` | Card and input outlines |
| Green | `#32d74b` | Solar, charging, success |
| Orange | `#ff9f0a` | Grid import, discharging, warnings |
| Blue | `#0a84ff` | Battery, links, chart bars |
| Yellow | `#ffd60a` | Prices/price marker |
| Red | `#ff453a` | Errors |

Cards use a 20 px radius, a subtle dark gradient and a soft shadow. Values use
`font-variant-numeric: tabular-nums` and tight letter spacing; units are small
and muted. Buttons are pills; the primary action is white with black text.

## Layout

- **Mobile:** sticky translucent header (brand, theme, language, logout), fixed
  bottom tab bar (Home, Plan, Prices, Car, Settings), safe-area padding,
  44 px targets, single column, charts full width.
- **Desktop:** left sidebar with the same destinations, content max-width
  1180 px, three-column card rows where useful.
- **Theme:** dark by default, stored in `localStorage`; a sun/moon button
  switches to a light palette that keeps the same component geometry.
- **Accessibility:** labels on every control, `aria-label` on icon buttons,
  `aria-live` for results, colour never the only signal (icons/text accompany
  it), reduced motion respected, no horizontal page scrolling at 390 px.

## Dashboard

1. Energy-flow card with five source tiles (solar, grid, battery, house, car);
   each has a coloured icon, a large value, a unit and a state hint. The solar
   tile is a button with a small today sparkline (measured over expected) and
   opens **Solar production**.
2. Solar & load forecast card: 24/48-hour tabs, PV and load totals, confidence,
   a green PV area with a house-load line, source, timestamp and warnings.
3. State-of-charge ring with the battery state and telemetry freshness.
4. Prices card with current import price, next price and the cheapest 2-hour
   window.
5. Safety gates and inverter check.
6. Data-source status and build information.

## Solar production

Full-page chart with today's measured production (green area), expected curve
(yellow), minimum and maximum estimates (dashed) and the house-load forecast
(blue), plus current output, produced/expected/load kWh and the estimate range.
A back button returns to the dashboard.

## Battery plan

Summary cards (savings, plan cost, no-battery cost, slots, cycles), one chart
with import price, PV, house load, battery bars and SOC, shaded slot windows,
and a schedule table. The screen is explicitly a dry run. The dashboard shows
savings, plan cost and the next slot, linking to the plan.

## Prices

Segmented day tabs, a 15-minute bar chart with a "now" marker, min/max/average
cards, per-day cheapest windows, a scrollable 30-minute block table, tariff
breakdown and attribution.

## Settings

Segmented section pills (Site, Inverter, Solar, Load, Plan, Safety, System) and
generous forms. Inverter includes a read-only connection test whose result is
shown as a green summary card. Solar edits 1–3 MPPT strings and forecast
parameters; Load edits daily energy, measured-profile blending and the 24-hour
profile; Plan edits the slot budget, horizon, saving threshold, setpoints and
end-SOC policy.

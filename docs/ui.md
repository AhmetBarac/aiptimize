# User interface

The visual target is the EcoFlow app family: a dark, device-like energy home
screen, large tabular values, one accent colour per energy source, generous
cards, a live topology and a compact primary navigation. The interface is
mobile-first and remains usable at 360 px.

Reference material reviewed during the v1.1 refresh:

- [EcoFlow App](https://www.ecoflow.com/us/app) — system overview, TOU planning,
  backup reserve, energy statistics and device management.
- [EcoFlow on the App Store](https://apps.apple.com/us/app/ecoflow-power-a-new-world/id1506693140)
  — Space / Automation / Profile tabs and scheduled tasks.
- [ComputerBase EcoFlow Stream review](https://www.computerbase.de/artikel/smart-home/balkonkraftwerk-ecoflow-stream-ultra-ac-pro-test.92117/)
  — hands-on description of the energy-flow screen, tappable house demand,
  widget statistics and day/week/month/year history.
- Local owner-supplied screenshots in `GUI inspiration/` (intentionally not
  committed).

We borrow the information hierarchy and interaction patterns, not EcoFlow
branding, product renders or account features. Aiptimize has no weather,
multi-home or device-pairing data, so those widgets are deliberately absent.

## Design tokens

| Token | Dark (default) | Use |
|---|---|---|
| Background | `#000000` | Page, phone chrome |
| Surface | `#141416` | Cards |
| Surface 2 | `#1c1c1f` | Inputs, chips, tiles |
| Surface 3 | `#26262a` | Inactive controls, skeletons |
| Text | `#f5f5f7` | Values and headings |
| Muted | `#9a9aa2` | Labels, units and secondary text |
| Border | `rgba(255,255,255,.08)` | Card and input outlines |
| Green | `#32d74b` | Solar, charging, success |
| Orange | `#ff9f0a` | Grid import, discharging, warnings |
| Blue | `#0a84ff` | Battery and links |
| Cyan | `#64d2ff` | Grid export and surplus |
| Purple | `#bf5af2` | EV / car |
| Yellow | `#ffd60a` | Prices |
| Red | `#ff453a` | Errors |

Cards use 22–28 px radii, a restrained gradient and a single-pixel border.
Values use `font-variant-numeric: tabular-nums`; units are small and muted.
Primary actions are white pills on dark surfaces. Light theme preserves the
same geometry and uses darker source colours for contrast.

## Navigation

**Mobile** — five slots: Home · Prices · **Plan** (raised centre action) · Car ·
More.

- More opens a bottom sheet with History, Solar, Settings, theme, language,
  sign out and the version.
- The header contains the brand, live status and an avatar/menu button.
- History and Solar remain one tap away from the home metric cards.

**Desktop** — a 232 px left sidebar with Home, Plan, Prices, History, Car and
Settings, plus theme, language and sign out at the bottom. The mobile header
and More sheet are not shown.

## Home

The home page is a site overview rather than a generic dashboard.

1. **Hero** — site name, local date, live badge and a self-powered ring for
   today. Solar, consumption and savings so far are shown beside the ring.
   Missing history shows `—` and "Collecting today's data", never a fake zero.
2. **Energy flow** — solar, grid, house, battery and (when configured) car
   nodes connected by animated, directional lines. The house is a button that
   opens the live supply-mix sheet. Each other node links to its detail page.
   Stale telemetry stops the animation and is labelled.
3. **Today's plan** — automatic/draft/applied status, a 24 h timeline with
   charge and discharge lanes, the actual slot list, expected savings, the next
   slot, the write-gate state and the saved inverter sync state. Actions remain
   gated by the safety switches.
4. **Today widgets** — solar, consumption, grid import and savings for today,
   with a seven-day sparkline when enough measured data exists. Each widget
   opens the relevant detail page.
5. **Forecast** — 24/48 h PV and load totals, confidence, source, warnings and
   the measured/forecast chart.
6. **Prices** — current and next import price, the cheapest window and a
   compact 24 h price strip.
7. **System status** — inverter, telemetry, forecast, writes and EV gates in a
   compact card. Build and database diagnostics live in **Settings → System**.

### Energy-flow semantics

- Solar and grid values are absolute; the hint says producing/importing/
  exporting.
- Battery power is shown absolute with a charging/discharging hint; positive
  `battery_w` means charging.
- House supply is an instantaneous estimate: solar direct first, then battery
  discharge, then grid. Percentages are normalised to the house load.
- The supply sheet notes that surplus solar can charge the battery or export.
- The self-powered percentage is
  `(1 - grid_import_kwh / (load_kwh + ev_kwh)) * 100`, clamped to 0–100. It is
  calculated from today's measured history, not from the plan.

## Detail pages

### History

Range presets `7 days`, `30 days` and `12 weeks` use the existing history API
(`resolution=day|week`, 92-day limit). Summary cards show PV, consumption, grid
import and savings, plus the self-powered share. The chart keeps PV and load as
separate series, draws grid import/export as bars and cost/savings on the
secondary axis; the breakdown table is collapsed by default.

### Solar production

Current output, produced/expected kWh, the estimate range, actual-vs-expected
accuracy and house-load estimate, followed by the full production chart.

### Battery plan

Savings hero, plan cost/baseline, slots/cycles/charge, a day timeline above the
price/PV/load/battery/SOC chart, the schedule table, editor, an inverter
synchronisation card and warnings. The card shows the saved sync state (in
sync / out of sync / not checked), when the comparison was made, which plan and
revision it compared, the differences and the last write, with a "Check now"
action that re-reads the schedule. Existing validation, revisions, locks and
write gates are unchanged.

### Car

State, power, current, session and mode cards; three descriptive mode cards
(Off / Now / Smart); the current/pause controls; the active charging plan; and
the existing static plan form. EV commands remain gated by the EV switch.

### Prices

Now / next / cheapest summary, day tabs, the 15-minute spot chart, min/max/
average, cheapest windows, 30-minute blocks and tariff breakdown.

### Settings

Site, inverter, solar, load, plan, EV, safety and system sections. System adds
build commit/date alongside the existing version and hardware details. Safety
continues to show both the environment and in-app gates.

## Component behaviour

- `Sheet` — teleported dialog with focus trap, Escape/backdrop close, body
  scroll lock, focus restoration and a bottom-sheet layout on mobile.
- `SegmentedControl` — labelled group of pill buttons using `aria-pressed`.
- `MetricCard` — label/value/unit, optional sparkline, link and skeleton.
- `EnergyFlow` — semantic node links/button, hidden text summary and reduced
  motion fallback.
- `PlanTimeline` — clips overnight slots to the selected local day, supports
  23/25-hour DST days, shows a now marker and exposes a hidden text list.
- `PriceStrip` — 24 h 30-minute price bars with a cheapest-window band and a
  now marker; decorative bars have a text summary.
- `LiveBadge` — live / stale / waiting / offline state with freshness age.

## Data sources and refresh

| Surface | Source | Refresh |
|---|---|---|
| Live flow and hero status | `/status` and `/stream` (SSE) | 15 s stream, 10 s polling fallback |
| Today's metrics | `/history?from=&to=&resolution=day` | 5 min |
| Home plan | `/plan?date=` | 60 s |
| Forecast | `/forecast` | 5 min |
| Prices | `/prices` | 15 min |

All date keys and labels use the site timezone. Requests load independently so
one failed source does not blank the page. Writes and EV commands are never
triggered by this UI refresh.

## Accessibility and responsive rules

- Every control has a label; icon-only buttons have `aria-label`.
- Sheets are dialogs with `aria-modal`, labelled titles, focus trapping and
  focus restoration.
- Colour is never the only signal; icons, text and direction hints accompany it.
- The flow has a visually hidden text equivalent and each node an accessible
  name.
- Targets are at least 44 px; the primary nav action is 56 px.
- `prefers-reduced-motion` disables flow, pulse and sheet animation.
- No horizontal page scrolling at 360, 390, 768 or 1280 px.
- Charts react to the data-bs theme and use muted axes, dashed grids and source
  colours from CSS variables.

## Non-goals

- No 3D house scene, weather tile, multi-home selector, EcoCredits, device
  pairing or cloud account surfaces.
- No service worker; control data must not be stale.
- No new chart or UI dependency; the interface uses Vue, Bootstrap icons,
  ECharts and CSS/SVG.

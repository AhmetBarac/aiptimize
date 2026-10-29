"""Generate synthetic Nord Pool Data Portal fixtures.

The JSON shape mirrors a live 200 response captured from the Data Portal on
2026-10-29. Prices are synthetic so the repository does not redistribute Nord
Pool data; dates cover a normal 96-point day and the 100-point autumn DST day.
"""

import json
import math
from datetime import datetime, timedelta
from pathlib import Path
from zoneinfo import ZoneInfo

STOCKHOLM = ZoneInfo("Europe/Stockholm")
OUT = Path(__file__).resolve().parent


def generate(date: str) -> dict:
    start_local = datetime.fromisoformat(date).replace(tzinfo=STOCKHOLM)
    end_local = start_local + timedelta(days=1)
    # Iterate in UTC so a fall-back day really contains 25 hours (100 points).
    t = start_local.astimezone(ZoneInfo("UTC"))
    end = end_local.astimezone(ZoneInfo("UTC"))
    entries = []
    index = 0
    while t < end:
        price = round(100 + 40 * math.sin(index / 9) + 8 * math.sin(index / 3) + index % 7, 2)
        entries.append(
            {
                "deliveryStart": t.isoformat().replace("+00:00", "Z"),
                "deliveryEnd": (t + timedelta(minutes=15))
                .isoformat()
                .replace("+00:00", "Z"),
                "entryPerArea": {"SE4": price, "SE3": round(price - 5, 2)},
            }
        )
        t += timedelta(minutes=15)
        index += 1
    return {
        "deliveryDateCET": date,
        "version": 1,
        "updatedAt": "2026-10-28T12:18:18Z",
        "deliveryAreas": ["SE4", "SE3"],
        "market": "DayAhead",
        "multiAreaEntries": entries,
        "currency": "SEK",
        "areaStates": [{"state": "Final", "areas": ["SE4", "SE3"]}],
        "areaAverages": [{"area": "SE4", "average": 100.0}],
    }


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    for date, name in (
        ("2026-10-29", "se4-2026-10-29.json"),
        ("2026-10-25", "se4-2026-10-25.json"),
    ):
        payload = generate(date)
        path = OUT / name
        path.write_text(json.dumps(payload, separators=(",", ":")), encoding="utf-8")
        print(f"{name}: {len(payload['multiAreaEntries'])} entries")


if __name__ == "__main__":
    main()

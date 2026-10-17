#!/usr/bin/env python3
"""Regenerate internal/api/devicedata/samsung-device-names.json.

Source: Google Play "supported devices" list, which maps Android model codes
(Build.MODEL, e.g. SM-S918B) to marketing names (e.g. Galaxy S23 Ultra). Run it
whenever new Samsung models should be recognised:

    python3 scripts/update-device-catalog.py
"""
import csv
import io
import json
import pathlib
import subprocess
import sys
import urllib.error
import urllib.request

URL = "https://storage.googleapis.com/play_public/supported_devices.csv"
OUT = pathlib.Path(__file__).resolve().parent.parent / "internal/api/devicedata/samsung-device-names.json"


def pick(names):
    # A few legacy codes map to several names; prefer "Galaxy ..." and break ties alphabetically.
    return sorted(names, key=lambda name: ("galaxy" not in name.lower(), name))[0]


def main():
    try:
        with urllib.request.urlopen(URL, timeout=60) as response:
            raw = response.read()
    except urllib.error.URLError:
        # Some Python installs ship without root certificates; curl uses the system store.
        raw = subprocess.run(["curl", "-fsSL", "--max-time", "60", URL], check=True, capture_output=True).stdout
    text = raw.decode("utf-16")
    rows = list(csv.reader(io.StringIO(text)))[1:]

    names_by_code = {}
    for brand, marketing, _device, model in rows:
        code = model.strip().upper()
        name = marketing.strip()
        if brand.strip().lower() != "samsung" or not code.startswith("SM-"):
            continue
        if not name or name.upper() == code or not name.isascii():
            continue
        names_by_code.setdefault(code, set()).add(name)

    catalog = {code: pick(names) for code, names in sorted(names_by_code.items())}
    if len(catalog) < 1000:
        sys.exit(f"refusing to write a suspiciously small catalog ({len(catalog)} entries)")
    OUT.write_text(json.dumps(catalog, indent=1, ensure_ascii=True) + "\n")
    print(f"wrote {len(catalog)} Samsung models to {OUT}")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Fails when the README or plugin.json disagree with the plugin's code defaults,
or when the README repeats claims the code does not back."""
import json
import pathlib
import re
import sys

root = pathlib.Path(__file__).resolve().parent.parent
config = (root / "plugin/src/config.ts").read_text()
readme = (root / "README.md").read_text()
manifest = json.loads((root / "plugin/.claude-plugin/plugin.json").read_text())
errors = []

block = re.search(r"DEFAULT_LIMITS[^=]*=\s*\{(.*?)\n\}", config, re.S).group(1)
limits = {m[0]: (int(m[1]), int(m[2])) for m in re.findall(r"(\w+): \{ warn: (\d+), trip: (\d+) \}", block)}
if len(limits) != 10:
    errors.append(f"expected 10 rules in DEFAULT_LIMITS, parsed {len(limits)}")

rows = dict(re.findall(r"^\| `(\w+)` \| [^|]+ \| ([^|]+ \| [^|]+) \|$", readme, re.M))
for rule, (warn, trip) in limits.items():
    want = " | ".join("off" if v == 0 else str(v) for v in (warn, trip))
    got = re.sub(r"\s+", " ", rows.get(rule, "missing")).strip()
    if got != want:
        errors.append(f"README rules table: {rule} says '{got}', code says '{want}'")

for key, field in manifest.get("userConfig", {}).items():
    m = re.match(r"(\w+)_(warn|trip)$", key)
    if m and m.group(1) in limits:
        want = limits[m.group(1)][0 if m.group(2) == "warn" else 1]
        if field.get("default") != want:
            errors.append(f"plugin.json {key} default {field.get('default')} != code {want}")
for key, want in (("quota_window_min", 5), ("stop_running", True), ("abort_turn", False)):
    if manifest["userConfig"][key]["default"] != want:
        errors.append(f"plugin.json {key} default is not {want}")

for bad in ("never spawned", "fanoutfuse watch"):
    if bad in readme.lower():
        errors.append(f"README contains '{bad}', which the code does not back")
if "claude plugin install fanoutfuse --marketplace anujjjjjj/fanoutfuse" not in readme:
    errors.append("README lacks the one-command install line")

for e in errors:
    print("FAIL", e)
print("plugin claims:", "FAIL" if errors else "ok")
sys.exit(1 if errors else 0)

#!/bin/sh
# Waybar module for E7 Refresh.
#
# waybar config:
#   "custom/e7": {
#     "exec": "/path/to/e7-refresh.sh",
#     "interval": 2,
#     "on-click": "/path/to/e7-refresh.sh toggle",
#     "on-click-middle": "/path/to/e7-refresh.sh open",
#     "format": "{}",
#     "tooltip": true
#   }
set -eu

BIN="${E7_REFRESH_BIN:-e7-refresh}"

case "${1:-status}" in
  start) exec "$BIN" start ;;
  stop) exec "$BIN" stop ;;
  toggle)
    if "$BIN" status --json | grep -q '"running": true'; then
      exec "$BIN" stop
    fi
    exec "$BIN" start
    ;;
  open) exec "$BIN" open ;;
esac

status="$("$BIN" status --json 2>/dev/null || printf '{}')"
STATUS_JSON="$status" python3 - <<'PY'
import json, os
try:
    s = json.loads(os.environ["STATUS_JSON"])
except Exception:
    s = {}
running = bool(s.get("running"))
count = s.get("refreshCount", 0)
spent = s.get("spent", 0)
text = "E7 %s" % (count if running else "")
cls = "running" if running else "idle"
tip = ("Refreshing: %s refreshes, %s skystones" % (count, spent)) if running else (s.get("lastResult") or "Idle")
print(json.dumps({"text": text, "class": cls, "tooltip": tip}, ensure_ascii=False))
PY

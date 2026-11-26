#!/bin/sh
# Polybar module for E7 Refresh. Add to ~/.config/polybar/config.ini:
#
#   [module/e7]
#   type = custom/script
#   exec = /path/to/e7-refresh.sh
#   interval = 2
#   click-left = /path/to/e7-refresh.sh toggle
#   format-foreground = ${colors.foreground}
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
if s.get("running"):
    print("E7 %s" % s.get("refreshCount", 0))
else:
    print("E7")
PY

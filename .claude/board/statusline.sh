#!/usr/bin/env bash
raw="$(cat)"
dir="$HOME/.local/share/buddymusic-board"
mkdir -p "$dir" 2>/dev/null && printf '%s' "$raw" > "$dir/statusline.json" 2>/dev/null
printf '%s' "$raw" | python3 -I -c '
import json, sys
try:
    s = json.loads(sys.stdin.read())
except Exception:
    s = None
parts = []
if isinstance(s, dict):
    name = (s.get("model") or {}).get("display_name")
    if name:
        parts.append(name)
    for k, v in (s.get("rate_limits") or {}).items():
        if isinstance(v, dict) and v.get("used_percentage") is not None:
            parts.append("%s %.0f%%" % (k, v["used_percentage"]))
    ctx = (s.get("context_window") or {}).get("used_percentage")
    if ctx is not None:
        parts.append("ctx %.0f%%" % ctx)
print(" | ".join(parts) if parts else "buddymusic")
' 2>/dev/null || echo buddymusic

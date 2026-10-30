import argparse
import json
import os
import re
import time
from datetime import datetime, date

KEYS = ["input", "cacheWrite", "cacheRead", "output"]
ID_RX = re.compile(r'"message":\{"model":"[^"]*","id":"([^"]+)"')
TS_RX = re.compile(r'"timestamp":"([^"]+)"')
NUM_RX = [
    re.compile(r'"usage":\{"input_tokens":(\d+)'),
    re.compile(r'"cache_creation_input_tokens":(\d+)'),
    re.compile(r'"cache_read_input_tokens":(\d+)'),
    re.compile(r'"output_tokens":(\d+)'),
]


def local_date(ts):
    try:
        return datetime.fromisoformat(ts.replace("Z", "+00:00")).astimezone().date()
    except ValueError:
        return None


def read_messages(path, messages, today):
    with open(path, encoding="utf-8", errors="replace") as f:
        for line in f:
            if '"usage":{' not in line:
                continue
            m = ID_RX.search(line)
            if not m:
                continue
            ts = TS_RX.search(line)
            if ts and local_date(ts.group(1)) != today:
                continue
            u = messages.setdefault(m.group(1), [0, 0, 0, 0])
            for i, rx in enumerate(NUM_RX):
                n = rx.search(line)
                if n and int(n.group(1)) > u[i]:
                    u[i] = int(n.group(1))


def sum_messages(messages):
    usage = {k: 0 for k in KEYS}
    for u in messages.values():
        for i, k in enumerate(KEYS):
            usage[k] += u[i]
    usage["total"] = sum(usage[k] for k in KEYS)
    return usage


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--project-dir", default=os.path.expanduser("~/.claude/projects/-home-ollulf-buddymusic"))
    p.add_argument("--out-file")
    a = p.parse_args()

    today = date.today()
    start = datetime.combine(today, datetime.min.time()).timestamp()

    manager_messages = {}
    agents = {}
    if os.path.isdir(a.project_dir):
        for name in sorted(os.listdir(a.project_dir)):
            path = os.path.join(a.project_dir, name)
            if name.endswith(".jsonl") and os.path.isfile(path) and os.path.getmtime(path) >= start:
                read_messages(path, manager_messages, today)
        for root, _, files in os.walk(a.project_dir):
            if os.path.basename(root) != "subagents":
                continue
            for name in sorted(files):
                path = os.path.join(root, name)
                if name.startswith("agent-") and name.endswith(".jsonl") and os.path.getmtime(path) >= start:
                    messages = {}
                    read_messages(path, messages, today)
                    agents[name[6:-6]] = sum_messages(messages)
    manager = sum_messages(manager_messages)

    total = {k: 0 for k in KEYS + ["total"]}
    for u in [manager] + list(agents.values()):
        for k in total:
            total[k] += u[k]

    limits = {}
    limits_at = None
    status_file = os.path.expanduser("~/.local/share/buddymusic-board/statusline.json")
    if os.path.isfile(status_file):
        try:
            with open(status_file, encoding="utf-8") as f:
                status = json.load(f)
            for k, v in (status.get("rate_limits") or {}).items():
                if not isinstance(v, dict) or v.get("used_percentage") is None:
                    continue
                limits[k] = {
                    "used": float(v["used_percentage"]),
                    "resetsAt": int(v["resets_at"]) * 1000 if v.get("resets_at") else None,
                }
            limits_at = int(os.path.getmtime(status_file) * 1000)
        except (OSError, ValueError, TypeError, AttributeError):
            pass

    result = {
        "date": today.isoformat(),
        "updatedAt": int(time.time() * 1000),
        "total": total,
        "manager": manager,
        "agents": agents,
        "limits": limits,
        "limitsAt": limits_at,
    }
    out = json.dumps(result, separators=(",", ":"))
    if a.out_file:
        with open(a.out_file, "w", encoding="utf-8") as f:
            f.write(out)
    else:
        print(out)


if __name__ == "__main__":
    main()

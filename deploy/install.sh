#!/bin/bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "Run with: sudo bash $0"
    exit 1
fi

REPO="$(cd "$(dirname "$0")/.." && pwd)"
OWNER="${SUDO_USER:-$(stat -c %U "$REPO")}"
OWNER_HOME="$(getent passwd "$OWNER" | cut -d: -f6)"
ENV_FILE=/etc/buddymusic/api.env
MUSIC=/srv/music

echo "== Build as $OWNER"
GO="$(runuser -u "$OWNER" -- bash -lc 'command -v go' 2>/dev/null || true)"
[ -n "$GO" ] || GO="$OWNER_HOME/.local/go/bin/go"
[ -x "$GO" ] || { echo "Go not found for $OWNER"; exit 1; }
SUBJECT="$(runuser -u "$OWNER" -- git -C "$REPO" log -1 --format=%s)"
if [[ "$SUBJECT" =~ ^Release\ ([0-9][0-9.]*)$ ]]; then
    VERSION="${BASH_REMATCH[1]}"
else
    VERSION="1.0.$(runuser -u "$OWNER" -- git -C "$REPO" log -1 --format=%cd --date=format:%d%m)"
fi
echo "== Version $VERSION"
runuser -u "$OWNER" -- bash -c "cd '$REPO/server' && '$GO' test ./... && '$GO' build -trimpath -ldflags='-s -w -X main.version=$VERSION' -o .build/buddymusic-api ."

echo
echo "== System user buddymusic"
getent group music >/dev/null || groupadd --system music
id buddymusic >/dev/null 2>&1 || useradd --system --user-group --no-create-home --home-dir /var/lib/buddymusic --shell /usr/sbin/nologin buddymusic
usermod -aG music buddymusic

echo
echo "== Folders"
for d in "$MUSIC/_incoming" "$MUSIC/library" "$MUSIC/_feedback"; do
    install -d -g music -m 2775 "$d"
done

echo
echo "== Binary"
install -m 0755 "$REPO/server/.build/buddymusic-api" /usr/local/bin/buddymusic-api

echo
echo "== Config $ENV_FILE"
install -d -m 0750 -g buddymusic /etc/buddymusic
if [ ! -f "$ENV_FILE" ]; then
    cp "$REPO/deploy/api.env.example" "$ENV_FILE"
    sed -i "s|^SESSION_KEY=.*|SESSION_KEY=$(openssl rand -base64 32)|" "$ENV_FILE"
    echo "Created with a new SESSION_KEY."
fi
if grep -q '^SCAN_PASSWORD=$' "$ENV_FILE" && [ -t 0 ]; then
    read -r -s -p "Password of the Navidrome admin account buddymusic-scan (Enter to skip): " SCAN_PW
    echo
    if [ -n "$SCAN_PW" ]; then
        ESCAPED="${SCAN_PW//\\/\\\\}"
        ESCAPED="${ESCAPED//\"/\\\"}"
        grep -v '^SCAN_PASSWORD=' "$ENV_FILE" > "$ENV_FILE.new"
        printf 'SCAN_PASSWORD="%s"\n' "$ESCAPED" >> "$ENV_FILE.new"
        mv "$ENV_FILE.new" "$ENV_FILE"
    fi
    unset SCAN_PW ESCAPED
fi
chown root:buddymusic "$ENV_FILE"
chmod 0640 "$ENV_FILE"

echo
echo "== Firewall"
if command -v ufw >/dev/null && ufw status | grep -q "Status: active"; then
    ufw allow in on tailscale0 to any port 4534 proto tcp comment buddymusic
else
    echo "ufw is not active; the service itself only answers Tailscale and localhost addresses."
fi

echo
echo "== Service"
install -m 0644 "$REPO/deploy/buddymusic-api.service" /etc/systemd/system/buddymusic-api.service
systemctl daemon-reload
systemctl enable buddymusic-api >/dev/null
systemctl restart buddymusic-api

echo
echo "== Check"
for _ in $(seq 1 20); do
    curl -fsS http://127.0.0.1:4534/api/health >/dev/null 2>&1 && break
    sleep 0.5
done
systemctl is-active buddymusic-api
curl -fsS http://127.0.0.1:4534/api/health && echo
grep -q '^SCAN_PASSWORD=$' "$ENV_FILE" && echo "Note: SCAN_PASSWORD is empty, Navidrome finds new uploads on its own watcher or schedule."
echo "Open: http://$(tailscale status --self --json 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin)["Self"]["DNSName"].split(".")[0])' 2>/dev/null || echo musicbox):4534"

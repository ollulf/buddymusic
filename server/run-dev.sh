#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p .dev
[ -f .dev/session.key ] || openssl rand -base64 32 > .dev/session.key
export SESSION_KEY="$(cat .dev/session.key)"
export DATA_DIR="${DATA_DIR:-$PWD/.dev}"
export MUSIC_ROOT="${MUSIC_ROOT:-/srv/music}"
export LISTEN="${LISTEN:-:4535}"
exec go run .

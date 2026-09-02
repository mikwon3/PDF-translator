#!/usr/bin/env bash
# Runs ON your Mac. Syncs the web app + engine to a server and runs the (sudo-free)
# remote setup. Re-run any time to push code changes.
#
# Defaults target the Spark (vLLM host). Override via env:
#   HOST=mikwon@203.255.40.88 SSH_PORT=30 SSH_KEY=~/spark-key \
#   APP_DIR=/home/mikwon/paperko WEB_PORT=8700 \
#   PAPERKO_LLM_URL=http://localhost:8000/v1 \
#   bash webapp/deploy/deploy.sh
set -euo pipefail

HOST="${HOST:-mikwon@203.255.40.88}"
SSH_PORT="${SSH_PORT:-30}"
SSH_KEY="${SSH_KEY:-$HOME/spark-key}"
APP_DIR="${APP_DIR:-/home/mikwon/paperko}"
WEB_PORT="${WEB_PORT:-8700}"
LLM_URL="${PAPERKO_LLM_URL:-http://localhost:8000/v1}"
LLM_MODEL="${PAPERKO_LLM_MODEL:-unsloth/Qwen3.6-35B-A3B-NVFP4-Fast}"
REPO="$(cd "$(dirname "$0")/../.." && pwd)"

SSH_OPTS=(-p "$SSH_PORT" -o StrictHostKeyChecking=accept-new)
[ -f "$SSH_KEY" ] && SSH_OPTS+=(-i "$SSH_KEY" -o IdentitiesOnly=yes)

echo "→ repo:   $REPO"
echo "→ host:   $HOST  (ssh port $SSH_PORT)"
echo "→ target: $APP_DIR   web:$WEB_PORT   LLM:$LLM_URL"

ssh "${SSH_OPTS[@]}" "$HOST" "mkdir -p '$APP_DIR'"

echo "→ rsync (engine-py + webapp; data/ and caches preserved)"
rsync -az --delete \
  -e "ssh ${SSH_OPTS[*]}" \
  --exclude '__pycache__' \
  --exclude '.venv' \
  --exclude 'webapp/data' \
  --exclude '*.egg-info' \
  --exclude 'engine-py/build' \
  --exclude '.git' \
  "$REPO/engine-py" "$REPO/webapp" \
  "$HOST:$APP_DIR/"

echo "→ remote setup (sudo-free, user systemd)"
ssh "${SSH_OPTS[@]}" "$HOST" \
  "APP_DIR='$APP_DIR' PORT='$WEB_PORT' PAPERKO_LLM_URL='$LLM_URL' PAPERKO_LLM_MODEL='$LLM_MODEL' bash '$APP_DIR/webapp/deploy/remote-setup.sh'"

echo
echo "✓ deployed. Web app runs on 127.0.0.1:$WEB_PORT on the server (cloudflared will front it)."
echo "  Next: cloudflared tunnel →  webapp/deploy/cloudflared-setup.md"

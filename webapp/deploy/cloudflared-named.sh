#!/usr/bin/env bash
# Runs ON the Spark. Promotes the quick tunnel to a permanent NAMED tunnel and
# installs it as a user systemd service (no sudo). Requires a Cloudflare-managed
# domain and a one-time interactive login done first:
#
#   ~/.local/bin/cloudflared tunnel login        # opens a browser; pick your domain
#   TUNNEL=paperko HOSTNAME=paperko.example.com WEB_PORT=8700 \
#     bash ~/paperko/webapp/deploy/cloudflared-named.sh
set -euo pipefail

TUNNEL="${TUNNEL:-paperko}"
: "${HOSTNAME:?set HOSTNAME=paperko.yourdomain.com (a host under your Cloudflare domain)}"
WEB_PORT="${WEB_PORT:-8700}"
CF="$HOME/.local/bin/cloudflared"
export PATH="$HOME/.local/bin:$PATH"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"

[ -f "$HOME/.cloudflared/cert.pem" ] || { echo "ERROR: run '$CF tunnel login' first (browser auth)."; exit 1; }

echo "[1/5] create tunnel '$TUNNEL' (if missing)"
$CF tunnel list 2>/dev/null | awk '{print $2}' | grep -qx "$TUNNEL" || $CF tunnel create "$TUNNEL"
UUID=$($CF tunnel list 2>/dev/null | awk -v n="$TUNNEL" '$2==n{print $1}' | head -1)
CRED="$HOME/.cloudflared/${UUID}.json"
echo "  tunnel UUID: $UUID"

echo "[2/5] route DNS $HOSTNAME → $TUNNEL"
$CF tunnel route dns "$TUNNEL" "$HOSTNAME" || echo "  (route may already exist — continuing)"

echo "[3/5] write ~/.cloudflared/config.yml"
cat > "$HOME/.cloudflared/config.yml" <<CFG
tunnel: $UUID
credentials-file: $CRED
ingress:
  - hostname: $HOSTNAME
    service: http://localhost:$WEB_PORT
  - service: http_status:404
CFG

echo "[4/5] user systemd unit (paperko-tunnel)"
mkdir -p "$HOME/.config/systemd/user"
cat > "$HOME/.config/systemd/user/paperko-tunnel.service" <<UNIT
[Unit]
Description=PaperKo Cloudflare Tunnel
After=network.target paperko-web.service

[Service]
Type=simple
ExecStart=$CF tunnel run $TUNNEL
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
UNIT

echo "[5/5] stop quick tunnel + start named tunnel service"
loginctl enable-linger "$USER" 2>/dev/null || true
pkill -f "cloudflared tunnel --url" 2>/dev/null || true
systemctl --user daemon-reload
systemctl --user enable --now paperko-tunnel
sleep 3
systemctl --user --no-pager --lines=8 status paperko-tunnel || true
echo
echo "✓ permanent URL:  https://$HOSTNAME"

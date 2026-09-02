#!/usr/bin/env bash
# Expose the local PaperKo web server to the internet with a Cloudflare quick
# tunnel — no account, no domain, no firewall changes. Prints a public
# https://<random>.trycloudflare.com URL. Ctrl-C to stop (URL becomes invalid).
#
#   bash webapp/run.sh &         # start the server first (port 8000)
#   bash webapp/tunnel.sh        # then open the tunnel
#
# For a STABLE URL / custom domain, use a named tunnel instead (see README).
set -euo pipefail
PORT="${PORT:-8000}"

if ! command -v cloudflared >/dev/null 2>&1; then
  echo "cloudflared not found. Install it first:"
  echo "  macOS:  brew install cloudflared"
  echo "  Linux:  see https://pkg.cloudflare.com/  (or download the binary)"
  exit 1
fi

echo "opening Cloudflare quick tunnel to http://localhost:$PORT ..."
echo "→ watch for the https://<...>.trycloudflare.com URL below; share that."
exec cloudflared tunnel --url "http://localhost:$PORT"

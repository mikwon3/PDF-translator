#!/usr/bin/env bash
# Runs ON the target server (Ubuntu). Installs PaperKo web into a venv and
# registers it as a **user** systemd service — no sudo/root required.
# Persists across logout/reboot via `loginctl enable-linger`.
#
#   APP_DIR=$HOME/paperko PORT=8700 \
#   PAPERKO_LLM_URL=http://localhost:8000/v1 \
#   bash webapp/deploy/remote-setup.sh
set -euo pipefail

APP_DIR="${APP_DIR:-$HOME/paperko}"
PORT="${PORT:-8700}"
LLM_URL="${PAPERKO_LLM_URL:-http://localhost:8000/v1}"
LLM_MODEL="${PAPERKO_LLM_MODEL:-unsloth/Qwen3.6-35B-A3B-NVFP4-Fast}"

export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"

echo "[1/5] python venv + packages"
cd "$APP_DIR"
[ -d .venv ] || python3 -m venv .venv
.venv/bin/pip install -q --upgrade pip
.venv/bin/pip install -q -r webapp/requirements.txt
.venv/bin/pip install -q ./engine-py

echo "[2/5] import check"
PYTHONPATH="$APP_DIR/engine-py" .venv/bin/python - <<'PY'
import translate_engine, fastapi, uvicorn, pymupdf
try:
    import ahocorasick; fast = "yes"
except Exception:
    fast = "no (pure-python fallback)"
print(f"deps OK  engine={translate_engine.__version__}  pymupdf={pymupdf.__doc__.split()[1] if pymupdf.__doc__ else '?'}  fast-glossary={fast}")
PY

echo "[3/5] user systemd unit (paperko-web)"
mkdir -p "$HOME/.config/systemd/user"
cat > "$HOME/.config/systemd/user/paperko-web.service" <<UNIT
[Unit]
Description=PaperKo Web (FastAPI)
After=network.target

[Service]
Type=simple
WorkingDirectory=$APP_DIR
Environment=PYTHONPATH=$APP_DIR/engine-py
Environment=PAPERKO_LLM_URL=$LLM_URL
Environment=PAPERKO_LLM_MODEL=$LLM_MODEL
ExecStart=$APP_DIR/.venv/bin/python -m uvicorn webapp.server:app --host 127.0.0.1 --port $PORT
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
UNIT

echo "[4/5] enable linger + start service (survives logout/reboot)"
loginctl enable-linger "$USER" 2>/dev/null || true
systemctl --user daemon-reload
systemctl --user enable --now paperko-web
sleep 2
systemctl --user --no-pager --lines=6 status paperko-web || true

echo "[5/5] local health"
curl -fsS "http://127.0.0.1:$PORT/api/info" >/dev/null \
  && echo "web OK on 127.0.0.1:$PORT  (LLM=$LLM_URL)" \
  || echo "WARN: web not responding (journalctl --user -u paperko-web -n 50)"

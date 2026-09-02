#!/usr/bin/env bash
# Run the PaperKo web server locally.
#   bash webapp/run.sh            # http://localhost:8000
#   PORT=9000 bash webapp/run.sh
set -euo pipefail
cd "$(dirname "$0")/.."          # repo root

PORT="${PORT:-8000}"
export PAPERKO_LLM_URL="${PAPERKO_LLM_URL:-http://203.255.40.88:8567/v1}"
export PAPERKO_LLM_MODEL="${PAPERKO_LLM_MODEL:-unsloth/Qwen3.6-35B-A3B-NVFP4-Fast}"
export PYTHONPATH="$PWD/engine-py:${PYTHONPATH:-}"

# prefer the project venv if present
PY="python3"
[ -x ".venv/bin/python" ] && PY=".venv/bin/python"

echo "PaperKo Web → http://localhost:$PORT   (LLM: $PAPERKO_LLM_URL)"
exec "$PY" -m uvicorn webapp.server:app --host 0.0.0.0 --port "$PORT"

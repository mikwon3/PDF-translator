"""Smoke-test the stdio JSON-RPC server the way the Go app would drive it."""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PDF = ROOT / "tests" / "fixtures" / "sample.pdf"


def frame(obj: dict) -> bytes:
    data = json.dumps(obj).encode()
    return f"Content-Length: {len(data)}\r\n\r\n".encode() + data


def read_message(stream) -> dict | None:
    headers = {}
    while True:
        line = stream.readline()
        if not line:
            return None
        line = line.decode("ascii", "replace").strip()
        if line == "":
            break
        k, _, v = line.partition(":")
        headers[k.strip().lower()] = v.strip()
    n = int(headers.get("content-length", "0"))
    return json.loads(stream.read(n).decode())


def main() -> int:
    job_dir = Path(tempfile.mkdtemp(prefix="paperko_rpc_"))
    env = dict(os.environ)
    env["PAPERKO_LLM_URL"] = env.get("PAPERKO_LLM_URL", "http://127.0.0.1:8123/v1")
    env["PYTHONPATH"] = str(ROOT)

    proc = subprocess.Popen(
        [sys.executable, "-m", "translate_engine"],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=sys.stderr.buffer, env=env,
    )
    assert proc.stdin and proc.stdout

    def call(_id, method, params):
        proc.stdin.write(frame({"jsonrpc": "2.0", "id": _id, "method": method, "params": params}))
        proc.stdin.flush()
        # drain until we get a response with our id (skip notifications)
        while True:
            msg = read_message(proc.stdout)
            if msg is None:
                raise RuntimeError("server closed")
            if msg.get("id") == _id:
                return msg

    r = call(1, "initialize", {"data_dir": str(job_dir),
                               "llm": {"base_url": env["PAPERKO_LLM_URL"],
                                       "model": "qwen3.6-35B-A3B-NVFP4-fast"}})
    print("initialize:", json.dumps(r["result"], ensure_ascii=False))

    r = call(2, "document.open", {"path": str(PDF)})
    doc_id = r["result"]["doc_id"]
    print("document.open:", json.dumps(r["result"], ensure_ascii=False))

    r = call(3, "document.analyze", {"doc_id": doc_id, "pdf_path": str(PDF),
                                     "job_dir": str(job_dir), "job_id": "j_test", "ocr": "auto"})
    print("document.analyze:", json.dumps(r["result"], ensure_ascii=False))
    ir_path = r["result"]["ir_path"]

    r = call(4, "document.translate", {"job_id": "j_test", "ir_path": ir_path,
                                       "options": {"style": "formal", "max_concurrency": 4}})
    if "error" in r:
        print("document.translate ERROR:", r["error"])
    else:
        print("document.translate:", json.dumps(r["result"]["stats"], ensure_ascii=False))
        tpath = r["result"]["translation_path"]
        out_pdf = str(job_dir / "output.pdf")
        r = call(5, "document.render", {"job_id": "j_test", "pdf_path": str(PDF),
                                        "ir_path": ir_path, "translation_path": tpath,
                                        "out_path": out_pdf, "options": {"mode": "replace"}})
        print("document.render:", json.dumps(r["result"]["report"], ensure_ascii=False))

    call_shutdown = frame({"jsonrpc": "2.0", "id": 99, "method": "shutdown", "params": {}})
    proc.stdin.write(call_shutdown)
    proc.stdin.flush()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
    print("OK: RPC lifecycle complete")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

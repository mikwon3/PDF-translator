"""A tiny OpenAI-compatible mock vLLM server for offline pipeline testing.

Run:  python tests/mock_llm.py  (listens on :8123, path prefix /v1)

It "translates" by wrapping the input in a Korean sentence while preserving any
⟦..⟧ placeholders and numbers, so post-processing (placeholder equality, number
check, length ratio) exercises real paths without a GPU.
"""
from __future__ import annotations

import json
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODEL = "qwen3.6-35B-A3B-NVFP4-fast"
_PH = re.compile(r"⟦[A-Z]\d+⟧")


def fake_translate(user_content: str) -> str:
    # extract the "번역할 텍스트" section
    src = user_content
    if "## 번역할 텍스트" in user_content:
        src = user_content.split("## 번역할 텍스트", 1)[1].strip()
    placeholders = _PH.findall(src)
    # produce plausible Korean of similar length, keeping placeholders + numbers
    nums = re.findall(r"\d+(?:[.,]\d+)*", src)
    body = "이 문장은 한국어로 번역된 결과이며 원문의 의미를 자연스럽게 전달한다"
    # pad to a length proportional to source so the length-ratio check passes
    reps = max(1, len(src) // 60)
    parts = [body] * reps
    text = ". ".join(parts) + "."
    # append preserved tokens/numbers so equality/number checks pass
    tail = " ".join(placeholders + nums)
    if tail:
        text = text + " " + tail
    return text


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):  # silence
        pass

    def _send(self, code, obj):
        data = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path.endswith("/models"):
            self._send(200, {"data": [{"id": MODEL, "object": "model"}]})
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        req = json.loads(self.rfile.read(length) or b"{}")
        messages = req.get("messages", [])
        user = next((m["content"] for m in reversed(messages) if m["role"] == "user"), "")
        out = fake_translate(user)
        self._send(200, {
            "id": "chatcmpl-mock", "object": "chat.completion", "model": MODEL,
            "choices": [{"index": 0, "message": {"role": "assistant", "content": out},
                         "finish_reason": "stop"}],
            "usage": {"prompt_tokens": len(user) // 4, "completion_tokens": len(out) // 4,
                      "total_tokens": (len(user) + len(out)) // 4},
        })


if __name__ == "__main__":
    import sys
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8123
    srv = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"mock vLLM on http://127.0.0.1:{port}/v1 (model={MODEL})", flush=True)
    srv.serve_forever()

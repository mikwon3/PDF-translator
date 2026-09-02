"""qwen.py — vLLM (OpenAI-compatible) async client (03 §4, 04 §3).

Handles chat completions with concurrency limiting, exponential backoff retries,
adaptive backpressure and thinking-mode suppression for Qwen models.
"""

from __future__ import annotations

import asyncio
import logging
import random
import re
from dataclasses import dataclass, field

import httpx

from .errors import EngineError, LLM_MODEL_MISSING, LLM_UNREACHABLE

log = logging.getLogger("translate_engine.qwen")

_THINK_RE = re.compile(r"<think>.*?</think>", re.S | re.I)
_RETRYABLE_STATUS = {408, 409, 429, 500, 502, 503, 504}
# Hard ceiling for output tokens; reasoning models can burn a lot before answering.
_MAX_OUTPUT_CEILING = 8192


@dataclass
class HealthInfo:
    ok: bool
    latency_ms: float = 0.0
    model_found: bool = False
    error: str | None = None


@dataclass
class ChatResult:
    text: str
    finish_reason: str = "stop"
    prompt_tokens: int = 0
    completion_tokens: int = 0


@dataclass
class _Backpressure:
    """Halves concurrency on sustained 429s, restores it once things settle."""

    base: int
    current: int
    consecutive_429: int = 0
    successes_since_cut: int = 0

    def on_429(self) -> None:
        self.consecutive_429 += 1
        if self.consecutive_429 >= 3 and self.current > 1:
            self.current = max(1, self.current // 2)
            self.consecutive_429 = 0
            self.successes_since_cut = 0
            log.warning("backpressure: reducing concurrency to %d", self.current)

    def on_success(self) -> None:
        self.consecutive_429 = 0
        if self.current < self.base:
            self.successes_since_cut += 1
            if self.successes_since_cut >= 20:
                self.current = min(self.base, self.current * 2)
                self.successes_since_cut = 0
                log.info("backpressure: restoring concurrency to %d", self.current)


class QwenClient:
    def __init__(
        self,
        base_url: str,
        model: str,
        api_key: str | None = None,
        *,
        max_concurrency: int = 4,
        timeout_s: float = 120.0,
        max_retries: int = 3,
        seed: int | None = 42,
        reasoning_effort: str | None = "none",
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.model = model
        self.api_key = api_key
        self.timeout_s = timeout_s
        self.max_retries = max_retries
        self.seed = seed
        # Disable "thinking" for low-latency deterministic translation.  Different
        # servers honour different switches, so we send them all — they are ignored
        # where unsupported: OpenAI-standard `reasoning_effort` (LM Studio),
        # Qwen/vLLM `chat_template_kwargs.enable_thinking`, and `reasoning.enabled`.
        self.reasoning_effort = reasoning_effort
        self._bp = _Backpressure(base=max_concurrency, current=max_concurrency)
        self._sem = asyncio.Semaphore(max_concurrency)
        self._client: httpx.AsyncClient | None = None
        # usage accumulation
        self.total_prompt_tokens = 0
        self.total_completion_tokens = 0
        self.total_retries = 0

    # ------------------------------------------------------------------ #
    def _headers(self) -> dict[str, str]:
        h = {"Content-Type": "application/json"}
        if self.api_key:
            h["Authorization"] = f"Bearer {self.api_key}"
        return h

    async def _http(self) -> httpx.AsyncClient:
        if self._client is None:
            self._client = httpx.AsyncClient(timeout=self.timeout_s, headers=self._headers())
        return self._client

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()
            self._client = None

    async def __aenter__(self) -> "QwenClient":
        return self

    async def __aexit__(self, *exc) -> None:
        await self.aclose()

    # ------------------------------------------------------------------ #
    async def health(self) -> HealthInfo:
        client = await self._http()
        loop = asyncio.get_event_loop()
        start = loop.time()
        try:
            resp = await client.get(f"{self.base_url}/models")
            latency = (loop.time() - start) * 1000
            if resp.status_code != 200:
                return HealthInfo(ok=False, latency_ms=latency, error=f"HTTP {resp.status_code}")
            ids = [m.get("id") for m in resp.json().get("data", [])]
            return HealthInfo(
                ok=True, latency_ms=round(latency, 1), model_found=self.model in ids
            )
        except (httpx.ConnectError, httpx.TimeoutException, httpx.HTTPError) as exc:
            return HealthInfo(ok=False, error=str(exc))

    async def chat(
        self,
        messages: list[dict],
        *,
        temperature: float = 0.2,
        top_p: float = 0.9,
        max_tokens: int = 2048,
        extra_body: dict | None = None,
    ) -> ChatResult:
        payload = {
            "model": self.model,
            "messages": messages,
            "temperature": temperature,
            "top_p": top_p,
            "max_tokens": max_tokens,
            "stream": False,
            # suppress "thinking" for low-latency deterministic output (03 §4.3);
            # multiple switches so it works across vLLM / LM Studio / others
            "chat_template_kwargs": {"enable_thinking": False},
        }
        if self.reasoning_effort is not None:
            payload["reasoning_effort"] = self.reasoning_effort          # OpenAI/LM Studio
            payload["reasoning"] = {"enabled": self.reasoning_effort != "none"}
        if self.seed is not None:
            payload["seed"] = self.seed
        if extra_body:
            payload.update(extra_body)

        async with self._sem:
            result = await self._chat_with_retry(payload)

        # Retry with a larger budget if the model was cut off (03 §4.3).  Reasoning
        # ("thinking") models can spend the whole budget before emitting an answer,
        # so an empty length-capped result gets an aggressive bump, up to a ceiling.
        length_retries = 0
        while result.finish_reason == "length" and length_retries < 3 \
                and payload["max_tokens"] < _MAX_OUTPUT_CEILING:
            grow = 2.0 if not result.text.strip() else 1.5
            payload["max_tokens"] = min(_MAX_OUTPUT_CEILING, int(payload["max_tokens"] * grow) + 512)
            length_retries += 1
            async with self._sem:
                result = await self._chat_with_retry(payload)

        self.total_prompt_tokens += result.prompt_tokens
        self.total_completion_tokens += result.completion_tokens
        return result

    async def _chat_with_retry(self, payload: dict) -> ChatResult:
        client = await self._http()
        url = f"{self.base_url}/chat/completions"
        last_err: Exception | None = None

        for attempt in range(self.max_retries + 1):
            try:
                resp = await client.post(url, json=payload)
                if resp.status_code == 200:
                    self._bp.on_success()
                    return _parse_completion(resp.json())
                if resp.status_code == 404:
                    raise EngineError(LLM_MODEL_MISSING, f"model or endpoint not found: {url}")
                if resp.status_code in _RETRYABLE_STATUS:
                    if resp.status_code == 429:
                        self._bp.on_429()
                    delay = _retry_after(resp) or _backoff(attempt)
                    last_err = EngineError(LLM_UNREACHABLE, f"HTTP {resp.status_code}")
                    if attempt < self.max_retries:
                        self.total_retries += 1
                        await asyncio.sleep(delay)
                        continue
                    raise last_err
                # non-retryable HTTP error
                raise EngineError(LLM_UNREACHABLE, f"HTTP {resp.status_code}: {resp.text[:200]}")
            except (httpx.ConnectError, httpx.TimeoutException, httpx.ReadError) as exc:
                last_err = exc
                if attempt < self.max_retries:
                    self.total_retries += 1
                    await asyncio.sleep(_backoff(attempt))
                    continue
                raise EngineError(LLM_UNREACHABLE, f"connection failed: {exc}") from exc

        raise EngineError(LLM_UNREACHABLE, f"exhausted retries: {last_err}")


def _parse_completion(data: dict) -> ChatResult:
    choices = data.get("choices", [])
    if not choices:
        return ChatResult(text="", finish_reason="stop")
    choice = choices[0]
    content = (choice.get("message") or {}).get("content", "") or ""
    content = _THINK_RE.sub("", content)  # defensive: strip stray thinking blocks
    usage = data.get("usage", {}) or {}
    return ChatResult(
        text=content,
        finish_reason=choice.get("finish_reason", "stop"),
        prompt_tokens=int(usage.get("prompt_tokens", 0)),
        completion_tokens=int(usage.get("completion_tokens", 0)),
    )


def _backoff(attempt: int) -> float:
    # 1s → 2s → 4s with ±25% jitter (03 §4.2)
    base = 2.0 ** attempt
    return base * (0.75 + random.random() * 0.5)


def _retry_after(resp: httpx.Response) -> float | None:
    val = resp.headers.get("Retry-After")
    if not val:
        return None
    try:
        return float(val)
    except ValueError:
        return None

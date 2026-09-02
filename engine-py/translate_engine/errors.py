"""Structured engine errors.

Every recoverable failure surfaces as an :class:`EngineError` carrying one of the
codes defined in ``04-api-spec.md`` §2.4.  The JSON-RPC layer maps ``code`` into
``error.data.code`` so the Go side can branch on it.
"""

from __future__ import annotations

from typing import Any

# Canonical error codes (04-api-spec.md §2.4).
PDF_ENCRYPTED = "pdf_encrypted"
PDF_CORRUPT = "pdf_corrupt"
FILE_NOT_FOUND = "file_not_found"
LLM_UNREACHABLE = "llm_unreachable"
LLM_MODEL_MISSING = "llm_model_missing"
CANCELLED = "cancelled"
RESOURCE_MISSING = "resource_missing"
INTERNAL = "internal"


class EngineError(Exception):
    """An error with a machine-readable ``code`` and an optional detail payload."""

    def __init__(self, code: str, message: str, detail: Any | None = None) -> None:
        super().__init__(message)
        self.code = code
        self.message = message
        self.detail = detail

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"code": self.code, "message": self.message}
        if self.detail is not None:
            out["detail"] = self.detail
        return out

    def __repr__(self) -> str:  # pragma: no cover - debugging aid
        return f"EngineError(code={self.code!r}, message={self.message!r})"

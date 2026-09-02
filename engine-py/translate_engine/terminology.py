"""terminology.py — glossary loading, matching and verification (03 §3).

Matches occurrences of glossary terms inside a translation unit's source text so
that only the *relevant* terms are injected into the prompt (FR-32).  Uses
Aho-Corasick when ``pyahocorasick`` is installed, falling back to a compiled
regex alternation otherwise (identical results, slower on huge glossaries).
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from pathlib import Path

from .ir import TermHit

try:  # optional acceleration (03 §3.2)
    import ahocorasick  # type: ignore

    _HAVE_AC = True
except ImportError:  # pragma: no cover
    _HAVE_AC = False


@dataclass
class TermEntry:
    src: str
    dst: str
    domain: str = ""
    priority: int = 0
    case_sensitive: bool = False
    match_inflections: bool = True
    note: str = ""


@dataclass
class TermViolation:
    src: str
    expected: str
    reason: str  # "missing"


class Glossary:
    """A loaded, indexed glossary snapshot."""

    def __init__(self, entries: list[TermEntry]) -> None:
        self.entries = entries
        self._by_key: dict[str, TermEntry] = {}
        self._build_index()

    # ------------------------------------------------------------------ #
    @classmethod
    def load(cls, path: Path) -> "Glossary":
        data = json.loads(Path(path).read_text(encoding="utf-8"))
        raw_terms = data.get("terms", data) if isinstance(data, dict) else data
        entries = [
            TermEntry(
                src=t["src"],
                dst=t["dst"],
                domain=t.get("domain", ""),
                priority=int(t.get("priority", 0)),
                case_sensitive=bool(t.get("case_sensitive", False)),
                match_inflections=bool(t.get("match_inflections", True)),
                note=t.get("note", ""),
            )
            for t in raw_terms
            if t.get("src") and t.get("dst")
        ]
        return cls(entries)

    @classmethod
    def empty(cls) -> "Glossary":
        return cls([])

    # ------------------------------------------------------------------ #
    def _variants(self, entry: TermEntry) -> list[str]:
        forms = [entry.src]
        if entry.match_inflections and entry.src and entry.src[-1].isalpha():
            base = entry.src
            forms.append(base + "s")
            if base.endswith(("s", "x", "z", "ch", "sh")):
                forms.append(base + "es")
            if base.endswith("y") and len(base) > 1 and base[-2] not in "aeiou":
                forms.append(base[:-1] + "ies")
        return forms

    def _build_index(self) -> None:
        self._by_key.clear()
        # lowercased key -> original surface form (for case-sensitive re-check)
        self._orig_form: dict[str, str] = {}
        pairs: list[tuple[str, TermEntry]] = []
        for e in self.entries:
            for form in self._variants(e):
                # always index/search lowercased; case_sensitive is enforced in match()
                key = form.lower()
                prev = self._by_key.get(key)
                if prev is None or e.priority > prev.priority:
                    self._by_key[key] = e
                    self._orig_form[key] = form
                pairs.append((key, e))

        if _HAVE_AC and pairs:
            aut = ahocorasick.Automaton()
            for key, _e in pairs:
                aut.add_word(key, key)
            aut.make_automaton()
            self._automaton = aut
        else:
            self._automaton = None
            if self._by_key:
                self._regex = re.compile(
                    "|".join(sorted((re.escape(k) for k in self._by_key), key=len, reverse=True))
                )
            else:
                self._regex = None

    # ------------------------------------------------------------------ #
    def match(self, text: str, *, max_terms: int = 20) -> list[TermHit]:
        """Return up to ``max_terms`` glossary hits, longest-match preferred,
        ranked by priority then occurrence count (03 §3.3)."""
        if not self.entries or not text:
            return []
        lowered = text.lower()

        # collect (start, end, key) spans
        spans: list[tuple[int, int, str]] = []
        if self._automaton is not None:
            for end_idx, key in self._automaton.iter(lowered if True else text):
                start = end_idx - len(key) + 1
                spans.append((start, end_idx + 1, key))
        elif self._regex is not None:
            for m in self._regex.finditer(lowered):
                spans.append((m.start(), m.end(), m.group(0)))
        else:
            return []

        # word-boundary filter + case_sensitive re-check on the original text
        valid: list[tuple[int, int, str]] = []
        for start, end, key in spans:
            entry = self._by_key.get(key)
            if entry is None:
                continue
            if not _word_boundary(text, start, end):
                continue
            if entry.case_sensitive and text[start:end] != self._orig_form.get(key, key):
                continue
            valid.append((start, end, key))

        # longest-match-only: drop spans fully contained in a longer one
        valid.sort(key=lambda s: (s[0], -(s[1] - s[0])))
        chosen: list[tuple[int, int, str]] = []
        occupied_end = -1
        for start, end, key in sorted(valid, key=lambda s: (s[0], -(s[1]))):
            if start >= occupied_end:
                chosen.append((start, end, key))
                occupied_end = end

        # aggregate by entry
        counts: dict[int, int] = {}
        rep: dict[int, TermEntry] = {}
        for _s, _e, key in chosen:
            entry = self._by_key[key]
            eid = id(entry)
            counts[eid] = counts.get(eid, 0) + 1
            rep[eid] = entry

        hits = [
            TermHit(src=e.src, dst=e.dst, priority=e.priority, count=counts[eid])
            for eid, e in rep.items()
        ]
        hits.sort(key=lambda h: (-h.priority, -h.count, h.src.lower()))
        return hits[:max_terms]

    # ------------------------------------------------------------------ #
    def verify(self, source: str, target: str) -> list[TermViolation]:
        """Enforced-glossary check: every term present in ``source`` must have
        its ``dst`` appear in ``target`` (03 §3.2)."""
        violations: list[TermViolation] = []
        for hit in self.match(source, max_terms=1000):
            if hit.dst and hit.dst not in target:
                violations.append(
                    TermViolation(src=hit.src, expected=hit.dst, reason="missing")
                )
        return violations


def _word_boundary(text: str, start: int, end: int) -> bool:
    before = text[start - 1] if start > 0 else " "
    after = text[end] if end < len(text) else " "
    # only enforce boundaries for alphanumeric term edges
    left_ok = not (text[start].isalnum() and before.isalnum())
    right_ok = not (text[end - 1].isalnum() and after.isalnum())
    return left_ok and right_ok

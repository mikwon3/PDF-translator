"""Resume a translate job from its checkpoint, then render.

Reuses completed TUs from translation.json and re-runs only the rest (e.g. TUs
that failed while the LLM server was restarting).  Usage:
    python tests/resume_run.py <src.pdf> <out.pdf> <job_dir> [pages]
"""
import asyncio
import os
import sys
from pathlib import Path

from translate_engine.ir import Document
from translate_engine.layout import LayoutEngine
from translate_engine.qwen import QwenClient
from translate_engine.renderer import RenderOptions, Renderer
from translate_engine.terminology import Glossary
from translate_engine.translator import TranslateOptions, Translator


def parse_pages(spec):
    if not spec:
        return None
    out = set()
    for part in spec.split(","):
        if "-" in part:
            a, b = part.split("-")
            out.update(range(int(a) - 1, int(b)))
        else:
            out.add(int(part) - 1)
    return sorted(out)


async def main():
    src, out, job_dir = sys.argv[1], sys.argv[2], Path(sys.argv[3])
    pages = parse_pages(sys.argv[4]) if len(sys.argv) > 4 else None
    doc_path = job_dir / "document.json"
    doc = Document.load(doc_path) if doc_path.exists() else LayoutEngine().analyze(src, pages=pages)

    client = QwenClient(os.environ["PAPERKO_LLM_URL"], os.environ["PAPERKO_LLM_MODEL"],
                        max_concurrency=int(os.environ.get("PAPERKO_LLM_CONCURRENCY", "4")))
    translator = Translator(client=client, glossary=Glossary.empty(), opts=TranslateOptions())
    cp = job_dir / "translation.json"

    def prog(p):
        print(f"\rresume {p.done}/{p.total}", end="", file=sys.stderr)

    try:
        tr = await translator.translate_document(doc, checkpoint_path=cp, resume_from=cp,
                                                 on_progress=prog)
    finally:
        await client.aclose()
    print(f"\nstats: {tr.stats}", file=sys.stderr)

    Renderer(opts=RenderOptions(mode="replace")).render(src, doc, tr, Path(out))
    print(f"rendered -> {out}", file=sys.stderr)


if __name__ == "__main__":
    asyncio.run(main())

"""PaperKo Web — a small FastAPI server that runs the translate_engine locally.

Structure A: the heavy work (PDF parse / layout / render via PyMuPDF) runs on
this machine; the LLM is remote (vLLM). Expose it publicly with a Cloudflare
Tunnel (see tunnel.sh) so a tiny cloud box isn't needed.

Run:
    pip install -r webapp/requirements.txt   # + the engine (pip install ./engine-py)
    python -m uvicorn webapp.server:app --host 0.0.0.0 --port 8000
"""
from __future__ import annotations

import asyncio
import dataclasses
import json
import os
import re
import shutil
import uuid
from pathlib import Path

import fitz  # PyMuPDF
import httpx
from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from fastapi.responses import FileResponse, Response, StreamingResponse
from fastapi.staticfiles import StaticFiles

from translate_engine import __version__
from translate_engine.layout import LayoutEngine
from translate_engine.qwen import QwenClient
from translate_engine.terminology import Glossary
from translate_engine.renderer import RenderOptions, Renderer
from translate_engine.translator import Translator, TranslateOptions

BASE = Path(__file__).resolve().parent
DATA = Path(os.environ.get("PAPERKO_WEB_DATA", BASE / "data"))
JOBS = DATA / "jobs"
JOBS.mkdir(parents=True, exist_ok=True)

DEFAULT_LLM_URL = os.environ.get("PAPERKO_LLM_URL", "http://203.255.40.88:8567/v1")
DEFAULT_LLM_MODEL = os.environ.get("PAPERKO_LLM_MODEL", "unsloth/Qwen3.6-35B-A3B-NVFP4-Fast")
MAX_CONCURRENT_JOBS = int(os.environ.get("PAPERKO_WEB_JOBS", "2"))
MAX_UPLOAD_MB = int(os.environ.get("PAPERKO_WEB_MAX_MB", "60"))

app = FastAPI(title="PaperKo Web", version=__version__)
layout = LayoutEngine()
jobs: dict[str, dict] = {}
job_sem = asyncio.Semaphore(MAX_CONCURRENT_JOBS)


@app.middleware("http")
async def _no_cache_ui(request, call_next):
    """Always revalidate the HTML/JS/CSS so browsers never run a stale UI build."""
    resp = await call_next(request)
    if request.url.path in ("/", "/index.html", "/app.js", "/style.css"):
        resp.headers["Cache-Control"] = "no-cache, no-store, must-revalidate"
    return resp


def job_dir(jid: str) -> Path:
    return JOBS / jid


def _parse_pages(spec: str) -> list[int] | None:
    spec = (spec or "").strip()
    if not spec:
        return None
    out: set[int] = set()
    for part in spec.split(","):
        part = part.strip()
        if not part:
            continue
        if "-" in part:
            a, b = part.split("-", 1)
            for i in range(int(a), int(b) + 1):
                if i >= 1:
                    out.add(i - 1)
        elif part.isdigit():
            out.add(int(part) - 1)
    return sorted(out)


def _write_glossary(text: str, path: Path) -> str:
    """Parse a plain-text glossary ('src, dst[, priority]' per line, # comments)
    into the engine's glossary.json. Returns the path, or '' if empty."""
    terms = []
    for line in (text or "").splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = [p.strip() for p in re.split(r"[,\t]", line)]
        if len(parts) < 2 or not parts[0] or not parts[1]:
            continue
        t = {"src": parts[0], "dst": parts[1], "domain": "", "priority": 0,
             "case_sensitive": False, "match_inflections": True, "note": ""}
        if len(parts) >= 3 and parts[2].lstrip("-").isdigit():
            t["priority"] = int(parts[2])
        terms.append(t)
    if not terms:
        return ""
    path.write_text(json.dumps({"terms": terms}, ensure_ascii=False), encoding="utf-8")
    return str(path)


def _job_glossary(job) -> Glossary:
    gp = job.get("glossary")
    return Glossary.load(Path(gp)) if gp and Path(gp).exists() else Glossary.empty()


# --------------------------------------------------------------------------- #
# API
# --------------------------------------------------------------------------- #

# Program/author credits shown in the "정보(About)" dialog. Mirrors the desktop
# app's services/about.go — edit here to change what the web UI displays.
ABOUT = {
    "name": "PaperKo Web",
    "version": __version__,
    "description": "학술 논문 PDF 한국어 번역 · 웹",
    "author": "Dasan5",
    "department": "토목공학과 (Dept. of Civil Engineering)",
    "organization": "경상국립대학교 (Gyeongsang National University)",
    "year": "2026",
    "contact": "kwonm@gnu.ac.kr",
    "license": "사내 사용",
}


@app.get("/api/_sample")
async def _sample():
    """Tiny bundled PDF for the /diag.html self-test page."""
    p = BASE.parent / "engine-py" / "tests" / "fixtures" / "sample.pdf"
    if not p.exists():
        raise HTTPException(404, "sample not found")
    return FileResponse(str(p), media_type="application/pdf")


@app.get("/api/info")
async def info():
    return {"name": "PaperKo Web", "version": __version__,
            "default_llm": {"url": DEFAULT_LLM_URL, "model": DEFAULT_LLM_MODEL},
            "about": ABOUT}


@app.get("/api/about")
async def about():
    return ABOUT


@app.get("/api/models")
async def list_models(url: str = "", key: str = ""):
    base = (url or DEFAULT_LLM_URL).rstrip("/")
    headers = {"Authorization": f"Bearer {key}"} if key else {}
    try:
        async with httpx.AsyncClient(timeout=15) as c:
            r = await c.get(base + "/models", headers=headers)
            r.raise_for_status()
            return {"models": [m["id"] for m in r.json().get("data", []) if m.get("id")]}
    except Exception as e:  # noqa: BLE001
        raise HTTPException(502, f"cannot reach server: {e}")


@app.post("/api/upload")
async def upload(file: UploadFile = File(...)):
    jid = uuid.uuid4().hex[:12]
    d = job_dir(jid)
    d.mkdir(parents=True, exist_ok=True)
    pdf_path = d / "input.pdf"
    size = 0
    limit = MAX_UPLOAD_MB * 1024 * 1024
    with open(pdf_path, "wb") as f:
        while chunk := await file.read(1 << 20):
            size += len(chunk)
            if size > limit:
                f.close()
                shutil.rmtree(d, ignore_errors=True)
                raise HTTPException(413, f"file too large (> {MAX_UPLOAD_MB} MB)")
            f.write(chunk)
    try:
        meta = await asyncio.to_thread(layout.open_document, pdf_path)
    except Exception as e:  # noqa: BLE001
        shutil.rmtree(d, ignore_errors=True)
        raise HTTPException(400, f"cannot open PDF: {e}")
    jobs[jid] = {"id": jid, "pdf": str(pdf_path), "state": "opened",
                 "meta": dataclasses.asdict(meta), "queue": None,
                 "ir": None, "tr": None, "out": None}
    return {"job_id": jid, "filename": file.filename, "meta": jobs[jid]["meta"]}


@app.post("/api/translate")
async def translate(job_id: str = Form(...), pages: str = Form(""),
                    style: str = Form("formal"), mode: str = Form("replace"),
                    llm_url: str = Form(""), llm_model: str = Form(""),
                    concurrency: int = Form(6), glossary: str = Form(""),
                    enforce_glossary: bool = Form(False)):
    job = jobs.get(job_id)
    if not job:
        raise HTTPException(404, "job not found")
    job["glossary"] = _write_glossary(glossary, job_dir(job_id) / "glossary.json")
    job["enforce"] = enforce_glossary
    q: asyncio.Queue = asyncio.Queue()
    job["queue"] = q
    job["state"] = "queued"
    job["progress"] = None
    loop = asyncio.get_running_loop()
    asyncio.create_task(_run_job(job, q, loop, _parse_pages(pages), style, mode,
                                 llm_url or DEFAULT_LLM_URL, llm_model or DEFAULT_LLM_MODEL,
                                 max(1, min(16, concurrency)), enforce_glossary))
    return {"ok": True}


async def _run_job(job, q, loop, pages, style, mode, llm_url, llm_model, concurrency, enforce):
    def emit(ev: dict) -> None:
        et = ev.get("type")
        # Mirror live progress into the job so /status polling can drive the UI even
        # when Cloudflare buffers the SSE stream (proxy holds it until close).
        if et == "state":
            job["state"] = ev["state"]
        elif et == "progress":
            job["progress"] = {"stage": ev.get("stage"), "done": ev.get("done"), "total": ev.get("total")}
        loop.call_soon_threadsafe(q.put_nowait, ev)

    async with job_sem:
        try:
            d = job_dir(job["id"])
            emit({"type": "state", "state": "analyzing"})
            doc = await asyncio.to_thread(
                layout.analyze, Path(job["pdf"]), pages=pages,
                on_progress=lambda dn, tt: emit({"type": "progress", "stage": "analyze",
                                                 "done": dn, "total": tt}))
            ir_path = d / "document.json"
            doc.save(ir_path)
            job["ir"] = str(ir_path)

            emit({"type": "state", "state": "translating"})
            client = QwenClient(llm_url, llm_model, max_concurrency=concurrency)
            translator = Translator(client=client, glossary=_job_glossary(job),
                                    opts=TranslateOptions(style=style, max_concurrency=concurrency,
                                                          enforce_glossary=enforce))
            tr_path = d / "translation.json"
            try:
                tr = await translator.translate_document(
                    doc, checkpoint_path=tr_path,
                    on_progress=lambda p: emit({"type": "progress", "stage": "translate",
                                                "done": p.done, "total": p.total, "page": p.page}),
                    on_page_done=lambda pg: emit({"type": "page_done", "page": pg}),
                    on_tu_failed=lambda tu: emit({"type": "tu_failed", "tu_id": tu.id,
                                                  "page": tu.context.get("page")}))
            finally:
                await client.aclose()
            job["tr"] = str(tr_path)

            emit({"type": "state", "state": "rendering"})
            out = d / "output.pdf"
            renderer = Renderer(opts=RenderOptions(mode=mode))
            report = await asyncio.to_thread(
                renderer.render, Path(job["pdf"]), doc, tr, out,
                on_progress=lambda dn, tt: emit({"type": "progress", "stage": "render",
                                                 "done": dn, "total": tt}))
            job["out"] = str(out)
            job["state"] = "done"
            emit({"type": "done", "stats": tr.stats, "report": report.to_dict()})
        except Exception as e:  # noqa: BLE001
            job["state"] = "failed"
            emit({"type": "error", "message": str(e)})
        finally:
            emit({"type": "_end"})


@app.get("/api/jobs/{job_id}/events")
async def events(job_id: str):
    job = jobs.get(job_id)
    if not job or job.get("queue") is None:
        raise HTTPException(404, "no active job")
    q = job["queue"]

    async def gen():
        # 2KB padding comment: nudges buffering proxies (Cloudflare) to start
        # flushing the stream instead of holding it until the connection closes.
        yield ":" + (" " * 2048) + "\n\n"
        yield "retry: 3000\n\n"
        while True:
            ev = await q.get()
            if ev.get("type") == "_end":
                yield f"data: {json.dumps({'type': 'end'})}\n\n"
                break
            yield f"data: {json.dumps(ev, ensure_ascii=False)}\n\n"

    return StreamingResponse(gen(), media_type="text/event-stream",
                             headers={
                                 # `no-transform` stops Cloudflare from gzip/br-compressing the
                                 # stream (compression buffers SSE → browser EventSource stalls).
                                 "Cache-Control": "no-cache, no-transform",
                                 "X-Accel-Buffering": "no",
                                 "Content-Encoding": "identity",
                             })


@app.get("/api/jobs/{job_id}/status")
async def job_status(job_id: str):
    """Poll fallback for the UI when the SSE stream is interrupted."""
    job = jobs.get(job_id)
    if not job:
        raise HTTPException(404, "job not found")
    return {"state": job.get("state"), "has_out": bool(job.get("out")),
            "progress": job.get("progress")}


@app.get("/api/jobs/{job_id}/preview/{page}")
async def preview(job_id: str, page: int, kind: str = "source", zoom: float = 2.0):
    job = jobs.get(job_id)
    if not job:
        raise HTTPException(404, "job not found")
    path = job["out"] if (kind == "translated" and job.get("out")) else job["pdf"]
    zoom = max(0.5, min(4.0, zoom))

    def render() -> bytes:
        doc = fitz.open(path)
        try:
            if page < 0 or page >= doc.page_count:
                raise HTTPException(404, "page out of range")
            pix = doc.load_page(page).get_pixmap(matrix=fitz.Matrix(zoom, zoom))
            return pix.tobytes("png")
        finally:
            doc.close()

    png = await asyncio.to_thread(render)
    return Response(content=png, media_type="image/png",
                    headers={"Cache-Control": "no-store"})


@app.post("/api/jobs/{job_id}/retranslate")
async def retranslate(job_id: str, tu_id: str = Form(...),
                      llm_url: str = Form(""), llm_model: str = Form(""),
                      mode: str = Form("replace"), concurrency: int = Form(4)):
    """Re-run one failed/edited translation unit (UC-03), then re-render the PDF."""
    from translate_engine.ir import Document, TranslationResult

    job = jobs.get(job_id)
    if not job or not job.get("ir") or not job.get("tr"):
        raise HTTPException(404, "job not translated yet")

    doc = await asyncio.to_thread(Document.load, Path(job["ir"]))
    tr = await asyncio.to_thread(TranslationResult.load, Path(job["tr"]))
    doc.translation_units = tr.translation_units  # same objects → in-place update

    client = QwenClient(llm_url or DEFAULT_LLM_URL, llm_model or DEFAULT_LLM_MODEL,
                        max_concurrency=max(1, min(16, concurrency)))
    translator = Translator(client=client, glossary=_job_glossary(job),
                            opts=TranslateOptions(enforce_glossary=job.get("enforce", False)))
    try:
        tu = await translator.translate_single(doc, tu_id)
    except KeyError:
        await client.aclose()
        raise HTTPException(404, f"unknown tu_id: {tu_id}")
    finally:
        await client.aclose()

    await asyncio.to_thread(tr.save, Path(job["tr"]))
    out = Path(job["out"]) if job.get("out") else job_dir(job_id) / "output.pdf"
    renderer = Renderer(opts=RenderOptions(mode=mode))
    await asyncio.to_thread(renderer.render, Path(job["pdf"]), doc, tr, out)
    job["out"] = str(out)
    return {"ok": True, "tu_id": tu.id, "status": tu.status,
            "target": tu.target_text, "page": tu.context.get("page")}


@app.post("/api/jobs/{job_id}/retranslate_page")
async def retranslate_page(job_id: str, page: int = Form(...),
                           llm_url: str = Form(""), llm_model: str = Form(""),
                           mode: str = Form("replace"), concurrency: int = Form(6)):
    """Re-translate every translatable unit on one page, then re-render."""
    from translate_engine.ir import Document, TranslationResult

    job = jobs.get(job_id)
    if not job or not job.get("ir") or not job.get("tr"):
        raise HTTPException(404, "job not translated yet")

    doc = await asyncio.to_thread(Document.load, Path(job["ir"]))
    tr = await asyncio.to_thread(TranslationResult.load, Path(job["tr"]))
    doc.translation_units = tr.translation_units
    tu_ids = [tu.id for tu in tr.translation_units if tu.context.get("page") == page]
    if not tu_ids:
        return {"ok": True, "count": 0}

    client = QwenClient(llm_url or DEFAULT_LLM_URL, llm_model or DEFAULT_LLM_MODEL,
                        max_concurrency=max(1, min(16, concurrency)))
    translator = Translator(client=client, glossary=_job_glossary(job),
                            opts=TranslateOptions(enforce_glossary=job.get("enforce", False)))
    try:
        await asyncio.gather(*[translator.translate_single(doc, tid) for tid in tu_ids])
    finally:
        await client.aclose()

    await asyncio.to_thread(tr.save, Path(job["tr"]))
    out = Path(job["out"]) if job.get("out") else job_dir(job_id) / "output.pdf"
    renderer = Renderer(opts=RenderOptions(mode=mode))
    await asyncio.to_thread(renderer.render, Path(job["pdf"]), doc, tr, out)
    job["out"] = str(out)
    return {"ok": True, "count": len(tu_ids)}


@app.get("/api/jobs/{job_id}/download")
async def download(job_id: str):
    job = jobs.get(job_id)
    if not job or not job.get("out"):
        raise HTTPException(404, "no output yet")
    title = (job["meta"].get("title") or "translated")
    safe = "".join(c for c in title if c.isalnum() or c in " _-가-힣")[:60].strip() or "translated"
    return FileResponse(job["out"], filename=f"{safe}_KO.pdf", media_type="application/pdf")


# static SPA (mounted last so /api/* wins)
app.mount("/", StaticFiles(directory=str(BASE / "static"), html=True), name="static")

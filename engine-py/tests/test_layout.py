from pathlib import Path

from translate_engine.ir import Document
from translate_engine.layout import LayoutEngine

PDF = Path(__file__).parent / "fixtures" / "sample.pdf"


def test_open_document_metadata():
    eng = LayoutEngine()
    meta = eng.open_document(PDF)
    assert meta.page_count == 2
    assert meta.has_text_layer is True
    assert meta.pages_without_text == []       # text pages must NOT be flagged as scans
    assert meta.doc_id.startswith("d_")


def test_analyze_produces_clean_blocks():
    eng = LayoutEngine()
    doc = eng.analyze(PDF)
    assert len(doc.pages) == 2
    blocks = doc.all_blocks()
    assert blocks, "expected some blocks"
    # text must survive extraction uncorrupted (no OCR on a text page)
    joined = " ".join(b.text() for b in blocks)
    assert "attention" in joined.lower()
    assert "translation" in joined.lower()
    # a heading and a caption should be classified
    types = {b.type for b in blocks}
    assert "heading" in types or "title" in types
    assert any(b.type == "caption" for b in blocks)


def test_inline_marks_detected():
    eng = LayoutEngine()
    doc = eng.analyze(PDF)
    raws = [m["raw"] for b in doc.all_blocks() for m in b.inline_marks]
    assert any("[12]" == r for r in raws), raws
    assert any("Vaswani" in r for r in raws), raws


def test_reading_order_monotonic_per_page():
    eng = LayoutEngine()
    doc = eng.analyze(PDF)
    for page in doc.pages:
        orders = [b.reading_order for b in page.blocks]
        assert orders == sorted(orders)


def test_split_block_lines_separates_headings():
    from translate_engine.ir import Line, Span
    from translate_engine.layout import _split_block_lines

    def L(text, y):
        return Line(bbox=[80, y, 500, y + 11], spans=[Span(text=text, bbox=[80, y, 500, y + 11])])

    lines = [
        L("Fig. 5 Details of the RC specimens (unit=mm)", 284),
        L("   ", 296),
        L("3. Experimental test", 323),
        L("   ", 334),
        L("3.1 RC specimen", 348),
        L("Three RC specimens were constructed for the program.", 372),
        L("were half-scale models of RC columns.", 385),
        L("3.2 Details of the test specimens", 538),
        L("The three RC specimens were used to test the Velcro.", 562),
        L("Fig. 7 presents the wrapping method of Velcro. It was", 575),
    ]
    segs = _split_block_lines(lines)
    heads = ["".join(sp.text for sp in s[0].spans).strip() for s in segs]
    assert "3. Experimental test" in heads
    assert "3.1 RC specimen" in heads
    assert "3.2 Details of the test specimens" in heads
    # the "3.1" body is one block that must NOT be split at the "Fig. 7 presents" line
    body = ["  ".join("".join(sp.text for sp in ln.spans) for ln in s) for s in segs]
    assert any("Fig. 7 presents" in b and "used to test" in b for b in body)


def test_clean_span_text_strips_spec_markers():
    from translate_engine.layout import _clean_span_text

    # control-char provision markers (ACI 440.11 uses U+0019 / U+001B) -> ◆ tofu
    assert _clean_span_text("\x1b7.2.2.1 Concrete") == "7.2.2.1 Concrete"
    assert _clean_span_text("\x19R7.2.1 Concentrated") == "R7.2.1 Concentrated"
    # a leading "=" change marker glued to a clause number or body word
    assert _clean_span_text("=4.9.2 The strength") == "4.9.2 The strength"
    assert _clean_span_text("=The Code provisions") == "The Code provisions"
    # soft hyphens (invisible line-break points) are removed
    assert _clean_span_text("require­ment") == "requirement"
    # legitimate text and real equations are left intact
    assert _clean_span_text("7.1.1 This chapter") == "7.1.1 This chapter"
    assert _clean_span_text("f = 0.85") == "f = 0.85"          # spaced '=' kept
    assert _clean_span_text("rho=4.9") == "rho=4.9"            # mid-token '=' kept


def test_split_block_lines_separates_merged_captions():
    from translate_engine.ir import Line, Span
    from translate_engine.layout import _split_block_lines

    def L(text, y):
        return Line(bbox=[80, y, 420, y + 11], spans=[Span(text=text, bbox=[80, y, 420, y + 11])])

    # a lumped "Fig. 2 … / Table 1 …" block must split into two caption blocks
    segs = _split_block_lines([
        L("Fig. 2 Mushroom-shaped head hook of improved Velcro", 253),
        L("Table 1 Physical properties of nylon", 277),
    ])
    firsts = ["".join(sp.text for sp in s[0].spans).strip() for s in segs]
    assert any(f.startswith("Fig. 2") for f in firsts)
    assert any(f.startswith("Table 1") for f in firsts)
    assert len(segs) == 2


def test_mark_mdpi_sidebar_keeps_masthead_and_metadata():
    from translate_engine.ir import Block, Document, Line, Page, Span
    from translate_engine.layout import _mark_mdpi_sidebar

    def blk(bid, typ, text, x0, y0, x1, y1):
        b = Block(id=bid, page=0, type=typ, bbox=[x0, y0, x1, y1])
        b.lines = [Line(bbox=[x0, y0, x1, y1], spans=[Span(text=text, bbox=[x0, y0, x1, y1])])]
        return b

    blocks = [
        blk("m", "title", "polymers", 76, 55, 164, 90),                 # masthead logo
        blk("t", "title", "Article Modelling of Web-Crippling", 35, 106, 523, 150),  # paper title
        blk("ab", "paragraph", "Abstract: The concentrated load …", 166, 261, 560, 420),
        blk("c1", "paragraph", "Citation: Zhang, L.; Li, Q. …", 36, 439, 146, 470),
        blk("c2", "paragraph", "Received: 10 November 2022", 36, 559, 125, 571),
        blk("c3", "paragraph", "Publisher’s Note: MDPI stays neutral", 36, 601, 152, 637),
        blk("c4", "paragraph", "Licensee MDPI, Basel, Switzerland.", 36, 696, 153, 708),
    ]
    doc = Document(doc_id="d", source_path="", meta={},
                   pages=[Page(index=0, width=595, height=842, blocks=blocks)])
    _mark_mdpi_sidebar(doc)
    kept = {b.id for b in blocks if not b.translate}
    assert kept == {"m", "c1", "c2", "c3", "c4"}          # masthead + sidebar kept
    assert next(b for b in blocks if b.id == "t").translate      # title translated
    assert next(b for b in blocks if b.id == "ab").translate     # abstract translated


def test_ir_roundtrip(tmp_path):
    eng = LayoutEngine()
    doc = eng.analyze(PDF)
    p = tmp_path / "doc.json"
    doc.save(p)
    loaded = Document.load(p)
    assert loaded.doc_id == doc.doc_id
    assert len(loaded.all_blocks()) == len(doc.all_blocks())

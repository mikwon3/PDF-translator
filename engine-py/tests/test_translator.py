from translate_engine.ir import Block, Document, Page, Placeholder, StyleSummary, TU
from translate_engine.terminology import Glossary
from translate_engine.translator import (
    Translator, TranslateOptions, _join_blocks, _is_translatable_prose,
)


def _translator():
    return Translator(client=None, glossary=Glossary.empty(), opts=TranslateOptions())


def _block(bid, text, marks=None):
    b = Block(id=bid, page=0, type="paragraph", bbox=[0, 0, 100, 20])
    from translate_engine.ir import Line, Span
    b.lines = [Line(bbox=[0, 0, 100, 10], spans=[Span(text=text, bbox=[0, 0, 100, 10])])]
    b.style_summary = StyleSummary()
    b.inline_marks = marks or []
    return b


def test_mask_and_restore_roundtrip():
    t = _translator()
    blk = _block("b1", "See [12] and http://x.io for x_i details",
                 marks=[{"kind": "citation", "raw": "[12]"},
                        {"kind": "url", "raw": "http://x.io"}])
    masked, phs = t._mask(blk.text(), [blk])
    assert "[12]" not in masked and "http://x.io" not in masked
    keys = {p.key for p in phs}
    assert len(keys) == 2
    # restore
    restored = masked
    for p in phs:
        restored = restored.replace(f"⟦{p.key}⟧", p.raw)
    assert "[12]" in restored and "http://x.io" in restored


def test_postprocess_strips_prefix_and_fences():
    t = _translator()
    tu = TU(id="tu1", block_ids=["b1"], source_text="Hello world this is a test sentence.")
    out, kind, _w = t._postprocess("```\n번역: 안녕하세요 이것은 테스트 문장입니다 그리고 더 길게.\n```", tu)
    assert kind == "ok"
    assert "번역:" not in out and "```" not in out


def test_postprocess_dropped_placeholder_is_restored_soft():
    # small models routinely drop math/citation tokens; rather than hard-failing
    # (which reverts the block to English), the dropped token is re-appended and the
    # result accepted as soft so the translation stays usable.
    t = _translator()
    tu = TU(id="tu1", block_ids=["b1"], source_text="foo ⟦M1⟧ bar",
            placeholders=[Placeholder(key="M1", kind="math", raw="$x$")])
    out, kind, warns = t._postprocess("한국어 번역 결과입니다", tu)  # missing ⟦M1⟧
    assert kind == "soft"
    assert "$x$" in out                      # dropped math restored, not lost
    assert any("restored" in w for w in warns)


def test_postprocess_source_repetition_is_soft():
    t = _translator()
    tu = TU(id="tu1", block_ids=["b1"], source_text="The quick brown fox jumps over.")
    _out, kind, _warns = t._postprocess("The quick brown fox jumps over.", tu)
    assert kind == "soft"   # suspicious but not discarded — retried, then acceptable


def test_postprocess_length_anomaly_is_soft():
    t = _translator()
    tu = TU(id="tu1", block_ids=["b1"],
            source_text="This is a reasonably long English source sentence used for testing ratios.")
    _out, kind, _w = t._postprocess("짧다", tu)  # far too short
    assert kind == "soft"


def _typed(bid, typ, text, cont=None, order=0):
    b = _block(bid, text)
    b.type = typ
    b.continues = cont
    b.reading_order = order
    return b


def test_build_tus_splits_list_items_but_flows_paragraph_lines():
    # A continues-chain: list item "1." with two wrapped paragraph lines, then
    # a second list item "2.". Each list item (with its wrapped lines) must be
    # ONE TU (own region → own line), not merged into a single blob nor split
    # per physical line.
    t = _translator()
    blocks = [
        _typed("b1", "list", "1. First item:", cont=None, order=0),
        _typed("b2", "paragraph", "wrapped line one", cont="b1", order=1),
        _typed("b3", "paragraph", "wrapped line two", cont="b2", order=2),
        _typed("b4", "list", "2. Second item:", cont="b3", order=3),
        _typed("b5", "paragraph", "second body", cont="b4", order=4),
    ]
    doc = Document(doc_id="d", source_path="", meta={},
                   pages=[Page(index=0, width=595, height=842, blocks=blocks)])
    tus = t.build_tus(doc)
    groups = [tu.block_ids for tu in tus]
    assert ["b1", "b2", "b3"] in groups   # list item 1 + its wrapped lines = one TU
    assert ["b4", "b5"] in groups          # list item 2 starts a new TU
    assert len(tus) == 2


def test_is_echo_detects_untranslated_heading():
    t = _translator()
    # echoed heading (target == source, no Hangul) → echo
    tu = TU(id="h1", block_ids=["b1"], source_text="2. Optimization method",
            status="done", target_text="2. Optimization method")
    assert t._is_echo(tu)
    # genuinely translated heading → not echo
    tu2 = TU(id="h2", block_ids=["b2"], source_text="3. Numerical modeling",
             status="done", target_text="3. 수치 모델링")
    assert not t._is_echo(tu2)


def test_is_translatable_prose_excludes_names_numbers_headers():
    # real prose → flagged as translatable (so an untranslated echo IS a failure)
    assert _is_translatable_prose(
        "The problem of beams on flexible foundation is considered a classic example.")
    assert _is_translatable_prose("본 논문은 빔과 기초 간의 비선형 상호작용 역학을 표현할 수 있는 요소를 제시한다.")
    # NOT prose → never marked failed (kept source is correct)
    assert not _is_translatable_prose(
        "Suchart Limkatanyu*, Kittisak Kuntiyawichai**, and Minho Kwon****")
    assert not _is_translatable_prose("KSCE Journal of Civil Engineering (2013) 17(1):192-201")
    assert not _is_translatable_prose("10.89 0.26 12.74 1.36 11.08 11.10 0.10 0.22 13.02")
    assert not _is_translatable_prose("dB x ( ) ∂Bu x ( ) =")


def test_join_blocks_no_space_between_cjk_linewrap():
    # A Korean line-wrap has no real space: "논리 자" + "체가" → "논리 자체가".
    a = _block("a", "상향 판정 논리 자")
    b = _block("b", "체가 성립하지 않는다.")
    assert _join_blocks([a, b]) == "상향 판정 논리 자체가 성립하지 않는다."
    # but a Latin boundary keeps the space
    c = _block("c", "value is")
    d = _block("d", "large")
    assert _join_blocks([c, d]) == "value is large"

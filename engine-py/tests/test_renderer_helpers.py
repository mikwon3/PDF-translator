from translate_engine.ir import Block, StyleSummary
from translate_engine.renderer import Renderer, RenderOptions, _strip_math_delims


def test_strip_math_delims_normalizes_leaked_latex():
    # inline-math off: $..$ / \(..\) delimiters dropped, LaTeX → Unicode
    assert _strip_math_delims("$f_y$") == "f_y"
    assert _strip_math_delims(r"2.5d = 1,031 \text{ mm} \leq a") == "2.5d = 1,031  mm ≤ a"


def test_strip_math_delims_subscript_braces_without_backslash():
    # models emit "f_{fu}" (no backslash) — braces must be removed, not left raw
    assert _strip_math_delims("f_{fu}") == "f_fu"
    assert _strip_math_delims("f_{yf}/f_{yus}") == "f_yf/f_yus"
    assert _strip_math_delims("x^{2}") == "x^2"


def _blk(typ, align):
    b = Block(id="b", page=0, type=typ, bbox=[0, 0, 100, 20])
    b.style_summary = StyleSummary(align=align)
    return b


def test_body_paragraph_never_centered():
    # a body paragraph mislabeled "center" by alignment detection must render left
    r = Renderer(opts=RenderOptions())
    html = r._html("some translated body text", _blk("paragraph", "center"), 1.3)
    assert "text-align:left" in html
    # a real heading keeps center
    html_h = r._html("A Centered Title", _blk("heading", "center"), 1.3)
    assert "text-align:center" in html_h

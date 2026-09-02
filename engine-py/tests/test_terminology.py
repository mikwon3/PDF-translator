from translate_engine.terminology import Glossary, TermEntry


def make(entries):
    return Glossary([TermEntry(**e) for e in entries])


def test_longest_match_preferred():
    g = make([
        {"src": "attention", "dst": "어텐션", "priority": 5},
        {"src": "self-attention", "dst": "셀프 어텐션", "priority": 5},
    ])
    hits = {h.src: h for h in g.match("We use self-attention layers.")}
    assert "self-attention" in hits
    assert "attention" not in hits  # subsumed by the longer match


def test_word_boundary():
    g = make([{"src": "net", "dst": "망"}])
    assert g.match("the network is deep") == []  # 'net' inside 'network' rejected
    assert g.match("a neural net here")          # standalone 'net' matches


def test_case_sensitive():
    g = make([{"src": "IT", "dst": "정보기술", "case_sensitive": True}])
    assert g.match("the IT department")
    assert g.match("it works") == []


def test_inflections():
    g = make([{"src": "model", "dst": "모델", "match_inflections": True}])
    assert g.match("several models were trained")


def test_max_terms_priority_order():
    entries = [{"src": f"term{i}", "dst": f"역어{i}", "priority": i} for i in range(30)]
    g = make(entries)
    text = " ".join(f"term{i}" for i in range(30))
    hits = g.match(text, max_terms=5)
    assert len(hits) == 5
    assert [h.priority for h in hits] == sorted([h.priority for h in hits], reverse=True)


def test_verify_missing():
    g = make([{"src": "attention", "dst": "어텐션"}])
    v = g.verify("attention mechanism", "메커니즘만 번역")
    assert v and v[0].src == "attention"
    assert g.verify("attention mechanism", "어텐션 메커니즘") == []

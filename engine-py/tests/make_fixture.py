"""Generate a small academic-looking PDF fixture for pipeline testing."""
from pathlib import Path

import fitz

OUT = Path(__file__).parent / "fixtures" / "sample.pdf"
OUT.parent.mkdir(parents=True, exist_ok=True)


def put(page, x, y, txt, size=10, bold=False):
    page.insert_text((x, y), txt, fontsize=size,
                     fontname="Times-Bold" if bold else "Times-Roman")


def box(page, rect, txt, size=10, align=3):
    page.insert_textbox(fitz.Rect(*rect), txt, fontsize=size,
                        fontname="Times-Roman", align=align)


doc = fitz.open()
p = doc.new_page(width=612, height=792)
put(p, 96, 90, "Attention Mechanisms for Neural Machine Translation", size=17, bold=True)
put(250, 115, "Jane Doe, John Smith", size=10) if False else put(p, 250, 115, "Jane Doe, John Smith", 10)
put(p, 72, 152, "Abstract", size=12, bold=True)
box(p, (72, 160, 540, 226),
    "We propose a novel attention mechanism that improves translation quality. "
    "Our model achieves a BLEU score of 34.2 on the WMT2014 dataset, outperforming "
    "prior work by 2.1 points. The approach is simple and efficient [12].")
put(p, 72, 252, "1. Introduction", size=12, bold=True)
box(p, (72, 262, 540, 372),
    "Neural machine translation has advanced rapidly in recent years. "
    "The transformer architecture (Vaswani et al., 2017) removed recurrence entirely. "
    "Given an input sequence, the model computes attention weights over all positions. "
    "This allows the network to capture long-range dependencies effectively and to "
    "parallelize computation across the sequence length during training.")
put(p, 72, 398, "Figure 1: The attention weight matrix visualized as a heatmap.", size=9)

p2 = doc.new_page(width=612, height=792)
put(p2, 72, 90, "2. Method", size=12, bold=True)
box(p2, (72, 102, 540, 200),
    "We define the attention score between query and key vectors. "
    "The softmax normalizes these scores into a probability distribution. "
    "Each output is a weighted sum of value vectors. We train with the Adam optimizer and a "
    "learning rate schedule that warms up over the first 4000 steps and then decays.")
put(p2, 72, 228, "3. Results", size=12, bold=True)
box(p2, (72, 240, 540, 300),
    "Our method outperforms all baselines on both datasets. "
    "Table 1 reports BLEU and chrF scores. Ablations confirm each component contributes.")

doc.save(str(OUT))
print(f"wrote {OUT} ({doc.page_count} pages)")

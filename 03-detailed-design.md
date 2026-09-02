# 03. 상세 설계서 — `translate_engine` 및 데이터 모델

## 1. 데이터 모델: 문서 IR

모든 모듈은 아래 IR을 통해서만 소통한다. 정본 스키마는 `schemas/document-ir.schema.json`이며, Python은 `ir.py`의 dataclass, Go는 코드 생성 타입으로 매핑한다.

### 1.1 최상위 구조

```jsonc
{
  "ir_version": "1.0",
  "doc_id": "d_9f2c…",              // 파일 해시 기반 (SHA-256 앞 12자)
  "source_path": "/path/paper.pdf",
  "meta": {
    "title": "…", "page_count": 12,
    "has_text_layer": true,          // 페이지별 상세는 pages[].ocr_used
    "language": "en"
  },
  "pages": [ Page ],
  "translation_units": [ TU ],       // analyze 후 translator가 생성
  "stats": { "block_count": 214, "tu_count": 168 }
}
```

### 1.2 Page / Block / Span

```jsonc
Page: {
  "index": 0,                        // 0-base
  "width": 612.0, "height": 792.0,   // pt 단위
  "rotation": 0,
  "ocr_used": false,
  "columns": [ {"x0":54,"x1":300}, … ],   // 감지된 단 경계
  "blocks": [ Block ]                // reading order 순으로 정렬 저장
}

Block: {
  "id": "b_0012",                    // 문서 내 유일
  "page": 0,
  "type": "paragraph",               // §01-srs FR-20의 12종
  "bbox": [54.0, 120.5, 300.2, 210.0],   // x0,y0,x1,y1 (좌상단 원점)
  "reading_order": 12,
  "translate": true,                 // figure/table/formula/header/footer는 false
  "continues": "b_0009",             // 페이지/단 경계로 잘린 문단의 앞 블록 id (없으면 null)
  "lines": [ { "bbox":[…], "spans":[ Span ] } ],
  "style_summary": {                 // 렌더러가 사용하는 대표 스타일
    "font_size": 9.6, "bold": false, "italic": false,
    "color": "#000000", "align": "justify", "line_height": 1.32
  },
  "confidence": 0.98                 // 레이아웃 모델 신뢰도
}

Span: {
  "text": "Attention is all you need",
  "bbox": […], "font": "NimbusRomNo9L", "size": 9.6,
  "flags": {"bold":false,"italic":false,"superscript":false},
  "color": "#000000"
}
```

### 1.3 TU (Translation Unit)

```jsonc
TU: {
  "id": "tu_0034",
  "block_ids": ["b_0009","b_0012"],     // continues 병합 결과
  "source_text": "… masked text with ⟦R1⟧ and ⟦M1⟧ …",  // 플레이스홀더 적용 텍스트
  "placeholders": [
    {"key":"M1","kind":"math","raw":"$x_i$","span_ref":["b_0012",3]},
    {"key":"R1","kind":"citation","raw":"[12]"},
    {"key":"U1","kind":"url","raw":"https://…"}
  ],
  "context": {"section":"3. Method","prev_tu":"tu_0033"},
                                        // 프롬프트 구성 시 prev_tu의 원문 꼬리·번역 꼬리와
                                        // 문서 수준 abstract_summary(초록 요약, analyze 후 1회 생성)를 사용
  "term_hits": [ {"src":"attention","dst":"어텐션","priority":10} ],
  "status": "pending",                  // pending|running|done|failed|skipped
  "target_text": null,                  // 번역·후처리 완료 텍스트 (플레이스홀더 복원 후)
  "attempts": 0, "error": null,
  "edited_by_user": false
}
```

플레이스홀더 토큰 형식은 `⟦K⟧`(U+27E6/27E7) — 일반 논문 텍스트에 등장하지 않는 문자를 사용해 충돌을 피하고, 후처리에서 정규식 `⟦[A-Z]\d+⟧`로 검증한다.

### 1.4 잡 작업 디렉터리 레이아웃

```
<data>/jobs/<job_id>/
├── document.json        # analyze 결과 IR
├── translation.json     # TU 상태·번역 결과 (체크포인트: TU 완료마다 원자적 갱신)
├── glossary.json        # 잡 시작 시 Go가 전달한 용어집 스냅샷
├── output.pdf           # render 결과
├── preview/page-000.png # (선택) 미리보기
└── job.log
```

---

## 2. `layout.py` — PDF Parser + Layout Analyzer + OCR

### 2.1 책임

- PDF 열기·검증(암호, 손상), 메타데이터 추출
- 텍스트/스타일/좌표 추출 (PDF Parser)
- 블록 감지·유형 분류·읽기 순서 산출 (Layout Analyzer)
- 텍스트 레이어 없는 페이지 OCR (OCR 서브컴포넌트)
- 결과를 IR(`document.json`)로 직렬화

### 2.2 공개 인터페이스

```python
class LayoutEngine:
    def __init__(self, model_dir: Path, ocr_enabled: bool = True): ...

    def open_document(self, pdf_path: Path, password: str | None = None) -> DocMeta:
        """페이지 수·크기·텍스트 레이어 유무 등 경량 메타만 반환 (분석 전 단계)."""

    def analyze(self, pdf_path: Path, *,
                pages: range | None = None,
                on_progress: Callable[[int, int], None] | None = None
                ) -> Document:
        """전체 파이프라인 실행 → IR 반환. on_progress(page_done, total)."""
```

### 2.3 내부 파이프라인

```
페이지 래스터화(150dpi) ─► ① 레이아웃 모델 추론 (DocLayout-YOLO/ONNX)
                                │  → 블록 후보 [type, bbox, conf]
PyMuPDF 텍스트 추출 ──────► ② 텍스트-블록 정합 (span↔block 할당, IoU 기반)
                                │
                           ③ 텍스트 레이어 판정: 블록 내 추출 텍스트 밀도
                                │  < 임계값 → OCR 경로 (RapidOCR, 해당 페이지만)
                           ④ 읽기 순서: 단(column) 감지 → XY-cut 정렬
                                │  (단 내 위→아래, 단 좌→우, header/footer 제외)
                           ⑤ 문단 연결: 단/페이지 경계에서
                                │  (끝문장 미완결 + 스타일 동일 + 다음 블록이 첫 순서)
                                │  → continues 링크 생성
                           ⑥ 번역 대상 판정: type 기반 기본값
                                │  figure/table/formula/header/footer → translate=false
                           ⑦ IR 조립·검증(스키마) → document.json
```

주요 규칙:

- **블록-스팬 정합**: 모델 bbox와 PyMuPDF 스팬 bbox의 IoU ≥ 0.5면 할당. 어느 블록에도 속하지 않는 스팬은 최근접 블록에 붙이고, 그래도 남으면 `paragraph` 블록을 합성한다(텍스트 유실 금지 원칙).
- **표 내부 텍스트**: v1에서는 표 블록 전체를 번역 제외(원형 유지)하고, 표 캡션만 번역한다. 표 셀 번역은 v1.x(레이아웃 난이도 높음).
- **인라인 수식 감지**: 수식 폰트(CMMI, CMSY, *Math*, *Symbol* 계열) 스팬 + `$…$` 패턴을 플레이스홀더 후보로 마킹해 IR에 남긴다(실제 마스킹은 translator).
- **신뢰도 낮은 블록**(conf < 0.6)은 `type` 유지하되 UI 오버레이에서 경고 표시 대상으로 플래그.

### 2.4 오류 처리

| 상황 | 처리 |
|---|---|
| 암호 PDF | `EngineError(code="pdf_encrypted")` → Go가 암호 입력 UI 후 재시도 |
| 손상 PDF | 페이지 단위 복구 시도, 실패 페이지는 `blocks:[]` + 오류 플래그 |
| 모델 로딩 실패 | 폴백: PyMuPDF 자체 블록 추출(품질 저하 모드) + 경고 |

---

## 3. `terminology.py` — Terminology

### 3.1 책임

- 용어집 스냅샷(JSON) 로드, 인덱스 구축
- TU 원문에서 등장 용어 매칭(대소문자·복수형 옵션)
- 프롬프트 주입용 용어 목록 산출 및 후처리 검증 지원

### 3.2 공개 인터페이스

```python
class Glossary:
    @classmethod
    def load(cls, path: Path) -> "Glossary": ...      # glossary.json

    def match(self, text: str, *, max_terms: int = 20) -> list[TermHit]:
        """Aho-Corasick 매칭 → 우선순위 내림차순 상위 max_terms.
        단어 경계 검사(\\b), case_sensitive 항목별 적용, 최장일치 우선."""

    def verify(self, source: str, target: str) -> list[TermViolation]:
        """강제 적용 모드용: source에 등장한 용어의 역어가 target에 없으면 위반 보고."""
```

```python
TermEntry:  src, dst, domain, priority(int, 높을수록 우선),
            case_sensitive(bool), match_inflections(bool), note
TermHit:    src, dst, priority, count
```

### 3.3 규칙

- 겹치는 매칭(`self-attention` ⊃ `attention`)은 **최장일치만** 채택.
- `match_inflections=True`면 단순 복수형(-s/-es)·소문자화 변형까지 매칭 (형태소 분석 수준은 범위 외).
- 매칭 결과가 20개를 넘으면 priority → count 순으로 절단 (프롬프트 길이 보호, FR-32).

---

## 4. `qwen.py` — vLLM 클라이언트

### 4.1 책임

- vLLM OpenAI 호환 API 호출 (chat completions), 비동기·동시성 제한·재시도
- Qwen 계열 모델 특성 제어(사고 모드 비활성화 등), 토큰 사용량 수집
- 헬스체크

### 4.2 공개 인터페이스

```python
class QwenClient:
    def __init__(self, base_url: str, model: str, api_key: str | None = None,
                 *, max_concurrency: int = 4, timeout_s: float = 120.0,
                 max_retries: int = 3): ...

    async def health(self) -> HealthInfo:
        """GET /v1/models — 모델 존재·응답 지연 확인."""

    async def chat(self, messages: list[dict], *,
                   temperature: float = 0.2, top_p: float = 0.9,
                   max_tokens: int = 2048,
                   extra_body: dict | None = None) -> ChatResult:
        """세마포어로 동시성 제한. 429/5xx/타임아웃 → 지수 백오프 재시도
        (1s→2s→4s, ±jitter). ChatResult: text, finish_reason, usage."""
```

### 4.3 설계 규칙

- **사고(thinking) 모드 차단**: 번역은 저지연·결정적 출력이 목표. `extra_body={"chat_template_kwargs": {"enable_thinking": False}}`를 기본 적용하고, 출력에 `<think>…</think>`가 섞여오면 방어적으로 제거한다(모델·서버 설정 변화 대비).
- **결정성**: `temperature=0.2, top_p=0.9, seed 고정(설정 가능)` — 재번역 시 동일 입력 → 유사 출력.
- **컨텍스트 예산**: 프롬프트+출력 ≤ 8K 토큰을 기본 상한으로 TU 크기를 제한(§5.3). 토큰 수 추정은 `문자수/3.2` 근사(영문) + 안전 마진 20%.
- **backpressure**: 연속 429 또는 평균 지연이 임계 초과 시 동시성을 절반으로 자동 감축, 안정화되면 복원 (NFR-13).
- finish_reason이 `length`면 max_tokens를 1.5배로 1회 재시도.

---

## 5. `translator.py` — Translator + Post Processor

### 5.1 책임

- IR에서 TU 생성(블록 병합·플레이스홀더 마스킹)
- 프롬프트 구성(문맥+용어) 및 `qwen.py` 호출 오케스트레이션
- 후처리: 플레이스홀더 복원, 출력 정제, 검증, 용어 강제 적용
- `translation.json` 체크포인트 관리, 진행률 콜백

### 5.2 공개 인터페이스

```python
class Translator:
    def __init__(self, client: QwenClient, glossary: Glossary,
                 opts: TranslateOptions): ...

    async def translate_document(self, doc: Document, *,
        resume_from: Path | None = None,           # translation.json 체크포인트
        on_progress: Callable[[TUProgress], None] | None = None,
        cancel: asyncio.Event | None = None) -> TranslationResult: ...

    async def translate_single(self, doc: Document, tu_id: str) -> TU:
        """UC-03 부분 재번역."""
```

```python
TranslateOptions: style("formal"|"concise"), enforce_glossary(bool),
                  max_concurrency, page_range, custom_system_prompt(str|None)
```

### 5.3 TU 구성 규칙

1. `translate=true` 블록을 reading order로 순회, `continues` 체인은 하나의 TU로 병합.
2. TU가 토큰 예산(기본 원문 1,200토큰)을 넘으면 문장 경계(pysbd)에서 분할.
3. 반대로 너무 짧은 인접 블록(제목 등)은 병합하지 않는다 — 제목·캡션은 항상 독립 TU (스타일이 다르고 렌더링 단위이므로).
4. 마스킹: layout이 마킹한 인라인 수식/인용/URL/코드 스팬을 `⟦M1⟧⟦R1⟧⟦U1⟧⟦C1⟧`로 치환하고 `placeholders[]`에 기록.

### 5.4 프롬프트 템플릿 (기본)

```
[system]
당신은 학술 논문 전문 번역가입니다. 영어 학술 텍스트를 정확하고 자연스러운
한국어 논문 문체(격식체, '-다' 종결)로 번역합니다.
규칙:
1. ⟦A1⟧ 형태의 토큰은 수식·인용·링크 자리표시자입니다. 절대 수정·삭제·번역하지
   말고 원문과 동일한 위치 관계로 유지하세요.
2. 용어집이 주어지면 반드시 지정된 역어를 사용하세요.
3. 번역문만 출력하세요. 설명, 주석, 원문 반복, 마크다운을 덧붙이지 마세요.
4. 숫자, 단위, 고유명사, 약어(예: CNN, BLEU)는 원문 그대로 유지하세요.

[user]
## 문맥
논문: {title}
초록 요약: {abstract_summary (번역 시작 시 초록 TU를 먼저 번역·요약해 2문장 이내로 고정)}
현재 섹션: {section}
직전 문단 원문 끝: {prev_source_tail (최대 200자)}
직전 문단 번역 끝: {prev_target_tail (최대 300자)}

## 용어집
{src} → {dst}   (term_hits 상위 20개, 없으면 섹션 생략)

## 번역할 텍스트
{source_text}
```

### 5.5 후처리 파이프라인 (Post Processor)

각 TU의 LLM 출력에 순서대로 적용:

| # | 단계 | 실패 시 |
|---|---|---|
| 1 | `<think>` 블록·앞뒤 공백·마크다운 코드펜스 제거 | — |
| 2 | 접두 정제: "번역:", "Translation:" 류 프리픽스 제거 | — |
| 3 | **플레이스홀더 검증**: 원문 placeholder 집합 == 출력 집합 | 불일치 → 재번역 큐(최대 2회), 최종 실패 시 원문 유지+`failed` |
| 4 | 플레이스홀더 → 원본 요소(raw) 복원 | — |
| 5 | 숫자·참조 검증: 원문 숫자 멀티셋 ⊆ 출력 (연도·소수점 허용 규칙 적용) | 경고 플래그(차단 안 함) |
| 6 | 원문 반복 감지: 출력이 원문과 동일/영문 비율 > 40% | 재번역 큐 |
| 7 | 용어 강제(옵션): `Glossary.verify` 위반 → 단순 치환 가능하면 치환, 아니면 재번역 | 경고 플래그 |
| 8 | 길이 이상 감지: 출력/원문 문자비 < 0.3 또는 > 3.0 | 재번역 큐 |

### 5.6 체크포인트·취소

- TU 완료마다 `translation.json`에 원자적 저장(temp 파일 → rename). 재개 시 `done` TU는 건너뛴다.
- `cancel` 이벤트 수신 시: 신규 요청 중단, 진행 중 요청은 완료 대기(타임아웃 10초) 후 체크포인트 저장하고 `cancelled` 반환.

---

## 6. `renderer.py` — PDF Renderer

### 6.1 책임

- 원본 PDF + IR + 번역 결과 → 레이아웃 유지 번역 PDF 생성
- 한글 폰트 서브셋 임베딩, 텍스트 맞춤(fitting), 출력 모드 2종

### 6.2 공개 인터페이스

```python
class Renderer:
    def __init__(self, font_dir: Path, opts: RenderOptions): ...

    def render(self, src_pdf: Path, doc: Document, tr: TranslationResult,
               out_path: Path, *,
               on_progress: Callable[[int, int], None] | None = None) -> RenderReport:
        """RenderReport: 페이지별 {fitted, shrunk, expanded, overflowed} 블록 수."""

RenderOptions: mode("replace"|"interleaved"), font_family("noto"|"nanum"),
               min_font_scale(0.72), mark_machine_translated(True)
```

### 6.3 렌더링 알고리즘 (mode="replace")

```
for page in pages:
  1. 번역 대상 블록의 bbox에 리댁션 적용 → 원본 텍스트 제거
     (그림·표·수식·배경 벡터는 그대로 남음)
  2. for block(translate=true, target 있음):
       html = 스타일 매핑(target_text, style_summary)
              # bold/italic → <b>/<i>, size → font-size, align 반영
       fit  = insert_htmlbox(block.bbox, html, scale_low=min_font_scale)
       if 넘침:
         a) 폰트 72%까지 축소  (insert_htmlbox의 scale 결과 확인)
         b) 행간 1.32 → 1.15 축소 후 재시도
         c) bbox 하단 확장: 아래 여백(다음 블록과의 간격)이 있으면 최대 그만큼
         d) 그래도 넘치면: 말줄임 없이 전량 삽입하되 블록에 ⚠ 마커 주석 추가,
            전문은 PDF 주석(popup annotation)으로 첨부 → RenderReport.overflowed
  3. 실패(failed) TU 블록: 원문 유지 + 옅은 주황 배경 하이라이트 주석
```

- 한글은 영문 대비 폭이 넓어 같은 내용이 대체로 **더 길어진다**. 학술 문단(justify, 9~10pt) 기준 축소 상한 72%면 실측상 대부분 수용된다는 가정으로 시작하고, 골든 셋으로 캘리브레이션한다(테스트 계획 §5).
- 폰트: Noto Sans KR(본문)·Noto Serif KR(세리프 원문일 때) 서브셋 임베딩. `mark_machine_translated`면 PDF 메타데이터와 1페이지 하단에 "기계번역본" 소형 라벨 삽입 (C-04).

### 6.4 mode="interleaved" (대역본)

원본 페이지를 그대로 복사한 뒤, 각 원본 페이지 다음에 replace 방식으로 만든 번역 페이지를 삽입한다. 구현은 replace 결과 문서와 원본 문서의 페이지 인터리브 병합.

---

## 7. `__main__.py` — 엔진 엔트리포인트 (인프라 모듈)

5개 핵심 모듈 외 인프라로서 다음을 담당한다.

- 실행 모드 2종: ① 인자 없이 실행 시 stdio JSON-RPC 서버(앱 사이드카 모드) ② 서브커맨드 실행 시 개발·테스트용 CLI (`python -m translate_engine analyze|translate|render …` — 파이프라인을 앱 없이 구동, `05-dev-plan.md`의 마일스톤 검증에 사용)
- stdio JSON-RPC 2.0 서버 (Content-Length 프레이밍, asyncio)
- 메서드 라우팅: `initialize / document.open / document.analyze / document.translate / document.render / tu.retranslate / job.cancel / shutdown` (명세는 `04-api-spec.md`)
- 잡 단위 asyncio Task 관리, 진행률 알림 발행
- 전역 예외 → 구조화 오류 응답 변환, stderr 구조화 로깅

## 8. Go 측 상세 설계 (요약)

### 8.1 Job Manager 상태머신

```
PENDING → ANALYZING → TRANSLATING → RENDERING → DONE
   │           │        │      ▲         │
   │           │        ▼      │         │
   │           │      PAUSED ──┘         │      # llm_unreachable 시 자동 전이,
   │           │        │                │      # 헬스체크 통과 후 사용자 재개
   └───────────┴────────┴────────────────┴──► FAILED / CANCELLED
재개(resume): PAUSED·FAILED·CANCELLED 잡 + translation.json 존재 → TRANSLATING부터
```

- 잡 레코드: `{job_id, doc_id, pdf_path, opts, state, progress, error, created_at, finished_at, metrics}` — SQLite `jobs` 테이블.
- 동시 실행 잡 수: v1은 1개(엔진 프로세스 1개). 배치(FR-74)는 큐로 직렬 처리.

### 8.2 Glossary Store (SQLite)

```sql
CREATE TABLE glossaries (id INTEGER PK, name TEXT, created_at, updated_at);
CREATE TABLE terms (
  id INTEGER PK, glossary_id INTEGER REFERENCES glossaries,
  src TEXT NOT NULL, dst TEXT NOT NULL,
  domain TEXT, priority INTEGER DEFAULT 0,
  case_sensitive INTEGER DEFAULT 0, match_inflections INTEGER DEFAULT 1,
  note TEXT, UNIQUE(glossary_id, src, case_sensitive)
);
```

잡 시작 시 선택된 용어집을 `glossary.json` 스냅샷으로 내보내 잡 디렉터리에 기록(재현성 보장 — 잡 도중 용어집 편집이 진행 중 잡에 영향 없음).

### 8.3 IR 타입 동기화

`schemas/*.schema.json`을 정본으로 두고, Go(`go-jsonschema`)·Python(`datamodel-code-generator` 또는 수동 dataclass+검증) 타입을 생성·검증한다. CI에서 스키마-코드 불일치를 검출한다.

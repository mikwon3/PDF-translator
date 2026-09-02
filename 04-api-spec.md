# 04. 인터페이스 명세서

세 개의 통신 경계를 정의한다.

1. Frontend ↔ Go: Wails v3 서비스 바인딩 + 이벤트
2. Go ↔ Python: stdio JSON-RPC 2.0
3. Python ↔ vLLM: OpenAI 호환 HTTP API

공통 규칙: 모든 식별자는 snake_case JSON. 시각은 RFC3339. 좌표는 PDF pt 단위.

---

## 1. Frontend ↔ Go (Wails v3)

### 1.1 서비스 메서드

#### DocumentService

| 메서드 | 시그니처 | 설명 |
|---|---|---|
| OpenDocument | `(path string, password string) → DocMeta` | PDF 열기 + 경량 메타. 오류코드: `pdf_encrypted`, `pdf_corrupt`, `file_not_found` |
| AnalyzeDocument | `(docId string, pages string) → jobId` | 레이아웃 분석 시작(비동기). `pages`: `""`(전체) 또는 `"1-10,15"` — 사용자 표기(1-base). Go가 0-base 정수 배열로 변환해 RPC(`document.analyze`)에 전달한다 |
| GetDocumentIR | `(docId string) → DocumentIR` | 분석 완료된 IR 조회 (오버레이 표시용) |
| UpdateBlock | `(docId, blockId string, patch BlockPatch) → ok` | 사용자 수동 수정: `{type?, translate?}` (FR-25) |
| GetPagePreview | `(docId string, page int, kind string) → pngBase64` | `kind: "source" 또는 "translated"` |

#### JobService

| 메서드 | 시그니처 | 설명 |
|---|---|---|
| StartTranslation | `(docId string, opts TranslateJobOptions) → jobId` | 번역 잡 시작 |
| CancelJob | `(jobId string) → ok` | 취소 (완료분 보존) |
| ResumeJob | `(jobId string) → newJobId` | 체크포인트 기반 재개 |
| RetranslateUnit | `(jobId, tuId string) → ok` | 블록(TU) 재번역 (UC-03) |
| EditTranslation | `(jobId, tuId, text string) → ok` | 사용자 직접 수정 (`edited_by_user=true`) |
| RenderOutput | `(jobId string, opts RenderOptions) → outputPath` | PDF 생성(비동기, 완료 이벤트) |
| GetJob / ListJobs | — | 잡 상태·이력 조회 |

```ts
interface TranslateJobOptions {
  glossary_ids: number[];
  page_range: string;            // "" = 전체
  style: "formal" | "concise";
  enforce_glossary: boolean;
  max_concurrency?: number;      // 기본 설정값 상속
  custom_system_prompt?: string;
}
```

#### GlossaryService

| 메서드 | 설명 |
|---|---|
| `ListGlossaries() → Glossary[]` / `CreateGlossary(name)` / `RenameGlossary` / `DeleteGlossary` | 용어집 CRUD |
| `ListTerms(glossaryId, query, offset, limit)` / `UpsertTerm(glossaryId, term)` / `DeleteTerm(termId)` | 용어 CRUD·검색 |
| `ImportCSV(glossaryId, path) → {added, updated, skipped}` / `ExportCSV(glossaryId, path)` | CSV 입출력 (FR-62) |

#### SettingsService

| 메서드 | 설명 |
|---|---|
| `GetSettings() / SaveSettings(s)` | 전체 설정 |
| `TestLLMConnection(url, model, apiKey) → HealthInfo` | vLLM 헬스체크 (FR-36). `HealthInfo: {ok, latency_ms, model_found, error?}` |

### 1.2 이벤트 (Go → Frontend)

| 이벤트 | 페이로드 | 발행 시점 |
|---|---|---|
| `job:state` | `{job_id, state, error?}` | 상태머신 전이마다 |
| `job:progress` | `{job_id, stage, page_done, page_total, tu_done, tu_total, pct, eta_s}` | 진행률 알림 스로틀 200ms |
| `job:page_done` | `{job_id, page}` | 페이지 번역 완료 → 미리보기 갱신 트리거 |
| `job:tu_failed` | `{job_id, tu_id, block_ids, error}` | TU 최종 실패 |
| `engine:status` | `{state}` — starting / ready / crashed / restarting | 사이드카 상태 변화 |
| `log:line` | `{level, source, msg, ts}` | 로그 뷰 (debug 설정 시) |

오류 응답 규격(모든 서비스 공통): `{code: string, message: string, detail?: object}` — Frontend는 `code`로 분기, `message`는 한국어 사용자 문구.

---

## 2. Go ↔ Python: stdio JSON-RPC 2.0

### 2.1 전송 규격

- 프레이밍: `Content-Length: <bytes>\r\n\r\n<utf-8 json>` (LSP 동일)
- Go→Python: 요청(request) 및 취소용 알림. Python→Go: 응답(response) + 알림(notification)
- id는 Go가 부여하는 단조 증가 정수. 알림은 id 없음
- 256 KB 초과 페이로드는 파일 경로로 전달(§2.4)

### 2.2 메서드 (Go → Python)

#### `initialize`

```jsonc
// params
{ "protocol_version": "1.0",
  "data_dir": "/…/PaperKo",
  "llm": {"base_url":"http://10.0.0.5:8000/v1","model":"qwen3.6-35B-A3B-NVFP4-fast",
           "api_key": null, "max_concurrency": 4, "timeout_s": 120},
  "log_level": "info" }
// result
{ "engine_version": "0.3.0", "protocol_version": "1.0",
  "capabilities": {"ocr": true, "gpu": false},
  "models_loaded": true }
```

버전 불일치 시 Go는 엔진을 종료하고 사용자에게 앱 재설치 안내(호환성 오류).

#### `document.open`

```jsonc
// params
{ "path": "/path/paper.pdf", "password": null }
// result (DocMeta)
{ "doc_id": "d_9f2c1a8b04e7", "page_count": 12,
  "title": "…", "has_text_layer": true, "encrypted": false,
  "pages_without_text": [7, 8] }
```

#### `document.analyze`

```jsonc
// params
{ "doc_id": "d_9f2c…", "pdf_path": "/path/paper.pdf",
  "pages": null,                     // null=전체, 또는 [0,1,2,…] (0-base — UI의 1-base 표기는 Go가 변환)
  "job_dir": "/…/jobs/j_0001",
  "ocr": "auto" }                    // "auto"|"off"|"force"
// result
{ "ir_path": "/…/jobs/j_0001/document.json",
  "stats": {"block_count": 214, "tu_estimate": 168, "ocr_pages": 2},
  "warnings": [{"page": 3, "code": "low_confidence_blocks", "count": 2}] }
```

#### `document.translate`

```jsonc
// params
{ "job_id": "j_0001",
  "ir_path": "/…/document.json",
  "glossary_path": "/…/glossary.json",
  "options": { "style": "formal", "enforce_glossary": true,
               "page_range": null, "max_concurrency": 4,
               "custom_system_prompt": null },
  "resume": false }                  // true면 기존 translation.json에서 재개
// result
{ "translation_path": "/…/translation.json",
  "stats": { "tu_total": 168, "tu_done": 165, "tu_failed": 3,
             "retries": 12, "tokens_prompt": 154200, "tokens_completion": 98100,
             "duration_s": 214.5 } }
```

#### `document.render`

```jsonc
// params
{ "job_id": "j_0001", "pdf_path": "/path/paper.pdf",
  "ir_path": "…", "translation_path": "…",
  "out_path": "/…/jobs/j_0001/output.pdf",
  "options": { "mode": "replace", "font_family": "noto",
               "min_font_scale": 0.72, "mark_machine_translated": true,
               "preview_png": true } }
// result
{ "out_path": "…/output.pdf",
  "report": { "pages": 12,
    "blocks": {"fitted": 180, "shrunk": 22, "expanded": 9, "overflowed": 1},
    "overflow_blocks": [{"page": 6, "block_id": "b_0141"}] } }
```

#### `tu.retranslate`

```jsonc
// params
{ "job_id": "j_0001", "tu_id": "tu_0034" }
// result: 갱신된 TU 객체 (translation.json에도 반영됨)
```

#### `job.cancel` (알림, id 없음)

```jsonc
{ "job_id": "j_0001" }   // 진행 중인 analyze/translate/render에 적용
```

#### `shutdown`

정리 후 프로세스 종료. 3초 내 미종료 시 Go가 강제 종료.

### 2.3 알림 (Python → Go)

| 메서드 | params | 비고 |
|---|---|---|
| `progress` | `{job_id, stage, done, total, page?, eta_s?}` — stage: analyze / translate / render | 스로틀 200ms |
| `partial` | `{job_id, kind:"page_translated", page}` | 페이지 완료 → 미리보기 갱신 |
| `tu_failed` | `{job_id, tu_id, block_ids, error:{code,message}}` | 최종 실패 TU |
| `log` | `{level, msg, ts}` | stderr와 별개의 구조화 로그(선택) |

### 2.4 오류 코드

JSON-RPC error object의 `error.data.code` 사용:

| code | 의미 | Go 측 처리 |
|---|---|---|
| `pdf_encrypted` | 암호 필요 | 암호 입력 UI |
| `pdf_corrupt` | 파싱 불가 | 사용자 오류 표시 |
| `llm_unreachable` | vLLM 연결 실패 | 설정 확인 유도 + 잡 `PAUSED` 전이(`03-detailed-design.md` §8.1) |
| `llm_model_missing` | 모델명 불일치 | 설정 확인 유도 |
| `cancelled` | 취소됨 | 잡 CANCELLED 전이 |
| `resource_missing` | 모델/폰트 파일 누락 | 재설치 안내 |
| `internal` | 기타 예외 (traceback은 data.detail) | 잡 FAILED + 로그 |

---

## 3. Python ↔ vLLM (OpenAI 호환)

### 3.1 요청

```
POST {base_url}/chat/completions
Authorization: Bearer {api_key}   # 설정된 경우만
```

```jsonc
{
  "model": "qwen3.6-35B-A3B-NVFP4-fast",
  "messages": [
    {"role": "system", "content": "<§03 5.4 시스템 프롬프트>"},
    {"role": "user",   "content": "<문맥+용어집+원문>"}
  ],
  "temperature": 0.2,
  "top_p": 0.9,
  "max_tokens": 2048,
  "seed": 42,
  "stream": false,                    // TU 단위 응답이 짧아 v1은 비스트리밍
  "chat_template_kwargs": {"enable_thinking": false}   // vLLM extra body
}
```

### 3.2 응답 처리

- `choices[0].message.content` → 후처리 파이프라인 입력
- `usage.{prompt_tokens, completion_tokens}` → 잡 메트릭 누적
- `finish_reason == "length"` → max_tokens 1.5배로 1회 재시도
- HTTP 429/500/502/503/타임아웃 → 지수 백오프 재시도(§03 4.3), `Retry-After` 헤더 존중

### 3.3 헬스체크

`GET {base_url}/models` → `data[].id`에 설정 모델명 존재 확인. 불일치 시 `llm_model_missing`.

---

## 4. 스키마 관리·호환성 규칙

- `schemas/`의 JSON Schema가 정본: `document-ir.schema.json`, `translation.schema.json`, `rpc.schema.json`
- 변경 규칙: 필드 추가는 마이너(하위호환), 필드 의미 변경·삭제는 `ir_version`/`protocol_version` 메이저 증가
- Go·Python 양쪽 CI에서 스키마 검증 테스트 필수 (`06-test-plan.md` §3.2)
- IR 파일에는 항상 `ir_version`을 기록하고, 엔진은 자신보다 높은 메이저 버전 파일을 거부한다

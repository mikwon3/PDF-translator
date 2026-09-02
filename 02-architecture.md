# 02. 시스템 아키텍처 설계서

## 1. 아키텍처 개요

### 1.1 설계 원칙

1. **프로세스 분리**: UI/앱 셸(Go·Wails)과 문서 처리 엔진(Python)을 별도 프로세스로 분리한다. Python 생태계(레이아웃 모델, PDF 라이브러리)를 활용하면서 Go 앱의 안정성을 지킨다 — 엔진이 죽어도 앱은 살아있다.
2. **단일 데이터 계약**: 모든 컴포넌트는 문서 IR(JSON 스키마)로만 데이터를 주고받는다. 모듈 교체(예: 레이아웃 모델 변경)가 IR 스키마를 깨지 않는 한 자유롭다.
3. **파이프라인 + 비동기**: 분석 → 번역 → 재조판은 페이지/블록 단위 스트리밍 파이프라인으로 처리하고, 진행 상황은 이벤트로 UI에 전달한다.
4. **외부 포트 금지**: 앱↔엔진은 stdio, 엔진↔vLLM은 사용자가 설정한 서버 주소만. 그 외 네트워크 통신 없음.

### 1.2 전체 구성도

```
┌─────────────────────────────────────────────────────────────┐
│                    Desktop App (단일 배포본)                  │
│                                                             │
│  ┌───────────────────────────┐   Wails v3 bindings/events   │
│  │  Frontend (WebView)       │◄────────────────────────────┐│
│  │  TypeScript + React       │                             ││
│  │  - PDF 뷰어(원문/번역 대조) │                             ││
│  │  - 블록 오버레이 편집       │                             ││
│  │  - 용어집/설정/진행률 UI    │                             ││
│  └───────────────────────────┘                             ││
│                                                            ▼│
│  ┌─────────────────────────────────────────────────────────┐│
│  │  Go Backend (Wails v3 host)                             ││
│  │  ┌──────────────┐ ┌──────────────┐ ┌──────────────────┐ ││
│  │  │ App Services │ │ Job Manager  │ │ Engine Supervisor│ ││
│  │  │ (바인딩 API)  │ │ (작업 상태머신)│ │ (사이드카 관리)   │ ││
│  │  └──────────────┘ └──────────────┘ └──────────────────┘ ││
│  │  ┌──────────────┐ ┌──────────────┐ ┌──────────────────┐ ││
│  │  │ Settings     │ │ Glossary Svc │ │ JSON-RPC Client  │ ││
│  │  │ (설정 관리)   │ │ (용어집 CRUD) │ │ (stdio framing)  │ ││
│  │  └──────────────┘ └──────────────┘ └──────────────────┘ ││
│  └───────────────────────┬─────────────────────────────────┘│
│                          │ stdin/stdout: JSON-RPC 2.0        │
│  ┌───────────────────────▼─────────────────────────────────┐│
│  │  Python Sidecar: translate_engine (PyInstaller 번들)     ││
│  │                                                         ││
│  │   __main__.py  ─ stdio JSON-RPC 서버·잡 디스패치          ││
│  │   layout.py    ─ PDF 파싱·레이아웃 분석·OCR → IR 생성      ││
│  │   terminology.py ─ 용어집 로드·용어 매칭(Aho-Corasick)     ││
│  │   translator.py ─ TU 구성·프롬프트·후처리·검증 파이프라인   ││
│  │   qwen.py      ─ vLLM OpenAI 호환 클라이언트(비동기)       ││
│  │   renderer.py  ─ IR + 번역 → 레이아웃 유지 PDF 생성        ││
│  └───────────────────────┬─────────────────────────────────┘│
└──────────────────────────┼──────────────────────────────────┘
                           │ HTTP  POST /v1/chat/completions
                           ▼
              ┌───────────────────────────┐
              │  vLLM Server (내부망/로컬)  │
              │  qwen3.6-35B-A3B-NVFP4-fast│
              └───────────────────────────┘
```

### 1.3 역할 분담 원칙

| 계층 | 담당 | 담당하지 않음 |
|---|---|---|
| Frontend (TS/React) | 표시·편집 UX, PDF 페이지 렌더링(미리보기), 진행률 표시 | 문서 처리 로직 일체 |
| Go Backend | 작업 오케스트레이션(잡 상태머신), 사이드카 수명주기, 설정·용어집 영속화, 파일 다이얼로그/OS 통합 | PDF·번역·조판 알고리즘 |
| Python `translate_engine` | 파싱·레이아웃·OCR·번역·후처리·재조판 전 파이프라인 | UI, 영속 설정, 프로세스 관리 |
| vLLM 서버 | LLM 추론 | 그 외 전부 (앱은 서버를 관리하지 않음 — 외부 전제) |

> 설계 결정 D-01: **번역 오케스트레이션(동시성·재시도·문맥 관리)은 Python(`translator.py`)에 둔다.** Go에 두는 대안도 있으나, 플레이스홀더 처리·프롬프트 구성·후처리가 레이아웃 IR과 강하게 결합되어 있어 언어 경계를 넘는 왕복(블록당 2회 IPC)을 없애는 쪽이 단순하고 빠르다. Go의 Job Manager는 잡 수준(시작/취소/진행률/체크포인트)만 관리한다.

## 2. 프로세스·통신 모델

### 2.1 사이드카 수명주기 (Engine Supervisor)

```
앱 시작 ──► 엔진 실행(자식 프로세스) ──► initialize 핸드셰이크(버전·기능 협상)
   │                                        │
   │            정상 ◄─────────────────────┘
   │
앱 종료 ──► shutdown 요청 → 3초 대기 → SIGTERM → 3초 → SIGKILL

비정상 종료 감지(waitpid/파이프 EOF)
   ──► 진행 중 잡 실패 처리 + UI 알림 ──► 지수 백오프 재시작(최대 3회/10분)
```

- 엔진 실행 파일 경로: 앱 리소스 디렉터리 내 번들 (`resources/engine/translate-engine[.exe]`). 개발 모드에서는 `python -m translate_engine`로 대체(설정 플래그).
- stderr는 로그 스트림으로 캡처하여 앱 로그에 병합한다. stdout은 JSON-RPC 전용이므로 엔진 코드에서 `print` 사용 금지(로거는 stderr로만).

### 2.2 IPC: stdio JSON-RPC 2.0

- 프레이밍: `Content-Length` 헤더 방식(LSP와 동일) — 대용량 IR 메시지에 안전.
- 방향: Go→Python 요청(메서드 호출), Python→Go 알림(진행률·로그·부분 결과). 예외적으로 취소(`job.cancel`)는 Go→Python 알림이다. 상세 명세는 `04-api-spec.md`.
- 대용량 데이터(IR JSON, 렌더링 PDF)는 메시지에 직접 싣지 않고 **작업 디렉터리의 파일 경로**로 교환한다(임계값 256 KB). 잡 작업 디렉터리는 Go가 생성·정리한다.

### 2.3 데이터 흐름 (UC-01 기준)

```
[open]      PDF 경로 ──layout.py──► DocMeta (페이지 수·암호·텍스트 레이어 유무)
[analyze]   PDF ──layout.py──► document.json (IR: 페이지·블록·스팬·읽기순서)
[translate] IR+용어집 스냅샷 ──translator.py──► TU 구성 → terminology.py(용어 매칭)
              → qwen.py(vLLM) → 후처리 → translation.json
[render]    IR+translation ──renderer.py──► output.pdf (+ 미리보기 PNG per page)
각 단계는 페이지 단위로 진행률 알림 발행 → Go Job Manager → Wails 이벤트 → UI
```

용어 매칭은 독립 RPC 단계가 아니라 `document.translate` 내부에서 TU별로 수행된다(용어집 스냅샷 경로를 translate 파라미터로 전달).

## 3. 기술 스택

### 3.1 앱 (Go / Wails)

| 항목 | 선택 | 비고 |
|---|---|---|
| Wails | v3.0 (alpha 채널) | 멀티윈도우·서비스 모델 사용. API 변경 리스크 → 버전 고정, 어댑터 계층으로 격리 |
| Go | 1.24+ | |
| Frontend | TypeScript + React + Vite | PDF 미리보기는 pdf.js 사용(표시 전용) |
| 상태관리 | Zustand(경량) | 잡 진행 상태는 Wails 이벤트 구독으로 갱신 |
| 설정 저장 | JSON 파일 (`settings.json`, §6의 OS 표준 데이터 경로 하위) | API 키는 OS keychain(`go-keyring`) |
| 용어집 저장 | SQLite (`modernc.org/sqlite`, CGO-free) | 엔진에는 잡 시작 시 스냅샷(JSON) 전달 |

### 3.2 엔진 (Python `translate_engine`)

| 항목 | 선택 | 비고 |
|---|---|---|
| Python | 3.11 (PyInstaller 번들) | 사용자 환경에 Python 설치 불요 |
| PDF 파싱·렌더링 | PyMuPDF (fitz) | 텍스트+스타일+bbox 추출, 리댁션·텍스트 삽입·폰트 임베딩까지 단일 라이브러리로 처리 |
| 레이아웃 분석 | DocLayout-YOLO (ONNX Runtime, CPU/GPU) | 후보: PP-DocLayout. ONNX로 변환해 PyTorch 의존 제거(번들 크기·기동 시간) |
| OCR | RapidOCR(ONNX) — 선택 기능 | 스캔 PDF에서만 로딩(지연 임포트) |
| 용어 매칭 | `ahocorasick` (pyahocorasick) | 수만 용어에서도 O(n) 매칭 |
| LLM 클라이언트 | `httpx` (async) 직접 구현 | openai SDK 대신 경량 직접 구현 — 의존 최소화, vLLM 확장 파라미터 제어 용이 |
| 문장 분할 | pysbd | TU 분할·문맥 창 구성에 사용 |

> 설계 결정 D-02: 레이아웃 모델은 **ONNX Runtime**으로 구동한다. PyTorch 번들은 배포본을 1 GB+ 키우므로 배제. 모델 파일(≈ 20~80 MB)은 앱 리소스에 포함한다.

> 설계 결정 D-03: PDF 재조판은 **PyMuPDF 리댁션+`insert_htmlbox`** 방식. 원본 페이지를 그대로 두고 텍스트 블록만 지운 뒤 같은 자리에 한국어 텍스트를 흘려 넣는다. 근거·대안 비교는 `03-detailed-design.md` §6.

### 3.3 LLM 서빙 (외부 전제)

- vLLM OpenAI 호환 서버, 모델 `qwen3.6-35B-A3B-NVFP4-fast`.
- 앱은 서버 기동·관리에 관여하지 않고 URL/모델명/키만 설정으로 받는다.
- 헬스체크: `GET /v1/models` 로 모델 존재 확인.

## 4. 디렉터리 구조

```
paperko/
├── main.go                      # Wails v3 부트스트랩
├── app/
│   ├── services/                # Wails 바인딩 서비스 (Frontend에 노출)
│   │   ├── document_service.go  #   문서 열기/분석/상태 조회
│   │   ├── job_service.go       #   번역 잡 시작/취소/재개
│   │   ├── glossary_service.go  #   용어집 CRUD/가져오기·내보내기
│   │   └── settings_service.go  #   설정, vLLM 헬스체크
│   ├── engine/
│   │   ├── supervisor.go        # 사이드카 실행·감시·재시작
│   │   ├── rpc.go               # JSON-RPC 클라이언트 (Content-Length framing)
│   │   └── types.go             # IR·RPC 메시지 Go 타입 (스키마에서 생성)
│   ├── jobs/
│   │   ├── manager.go           # 잡 상태머신·체크포인트
│   │   └── store.go             # 잡 이력 저장
│   └── glossary/store.go        # SQLite 접근
├── frontend/                    # React + TS
│   └── src/{views,components,stores,api}/
├── engine-py/                   # Python 엔진 (별도 venv/빌드)
│   ├── translate_engine/
│   │   ├── __init__.py
│   │   ├── __main__.py          # stdio JSON-RPC 서버, 잡 디스패처
│   │   ├── layout.py            # 파싱+레이아웃+OCR → IR
│   │   ├── terminology.py       # 용어집 매칭
│   │   ├── translator.py        # 번역 파이프라인·후처리
│   │   ├── qwen.py              # vLLM 클라이언트
│   │   ├── renderer.py          # PDF 재조판
│   │   ├── ir.py                # IR dataclass·스키마 (문서에는 5모듈 외 내부 모듈)
│   │   └── errors.py
│   ├── models/                  # ONNX 모델·폰트 리소스
│   ├── tests/
│   └── pyproject.toml
├── schemas/                     # IR·RPC JSON Schema (Go/Python 타입 생성의 원본)
└── build/                       # Wails 빌드 설정, PyInstaller spec, 패키징 스크립트
```

## 5. 핵심 시퀀스

### 5.1 번역 잡 전체 흐름

```
Frontend            Go(JobMgr)                Python(engine)            vLLM
   │ StartJob(opts)   │                            │                      │
   ├─────────────────►│ job: PENDING               │                      │
   │                  ├─ document.translate ──────►│                      │
   │                  │                            ├─ TU 구성·용어 매칭    │
   │                  │                            ├─ chat/completions ──►│ (동시 N)
   │                  │◄─ progress(page,pct) ──────┤◄─────────────────────┤
   │◄─ job:progress ──┤                            ├─ 후처리·검증          │
   │                  │◄─ partial(page_translated)─┤                      │
   │◄─ job:page_done ─┤ (미리보기 갱신)             │                      │
   │                  │◄─ result(translation) ─────┤                      │
   │                  ├─ document.render ─────────►│─ output.pdf 생성      │
   │◄─ job:state=DONE─┤◄─ result(pdf path) ────────┤                      │
```

### 5.2 오류·재시작 흐름

- vLLM 오류(개별 요청의 5xx/타임아웃): `qwen.py`가 재시도 → 계속 실패 시 해당 TU만 `failed`로 기록, 잡은 계속 진행. 완료 시 실패 블록 수를 결과에 포함.
- vLLM 접속 불능(`llm_unreachable` — 연속 실패로 서버 다운 판정): 잡을 `PAUSED`로 전이하고 UI에 안내. 서버 복구 확인(헬스체크) 후 사용자가 재개(체크포인트 기반).
- 엔진 크래시: Supervisor가 감지 → 잡 `FAILED(engine_crash)` → 재시작 → UI에 "이어하기" 제안(체크포인트 기반, `translation.json` 부분 결과 재사용).

## 6. 배포 아키텍처

| 항목 | 내용 |
|---|---|
| 패키징 | Wails 빌드 산출물 + PyInstaller onedir 엔진 + 모델·폰트 리소스를 하나의 설치본으로 (Windows: NSIS, macOS: .app+dmg, Linux: AppImage) |
| 크기 예산 | 총 ≤ 500 MB (엔진+모델이 대부분) |
| 코드서명 | macOS notarize / Windows 서명 — 사이드카 실행 차단 방지를 위해 엔진 바이너리도 서명 |
| 업데이트 | v1: 수동 다운로드. v1.x: 자동 업데이트 검토 |
| 설정/데이터 경로 | OS 표준 (Win `%AppData%/PaperKo`, macOS `~/Library/Application Support/PaperKo`, Linux `~/.local/share/paperko`) — 하위에 `jobs/`, `glossaries.db`, `logs/`, `settings.json` |

## 7. 관측성

- 구조화 로그(JSON lines): Go `slog`, Python `structlog`(stderr) → Supervisor가 병합, 파일 순환(7일).
- 잡별 메트릭 기록: 단계별 소요시간, TU 수, 재시도 수, 실패 블록 수, 토큰 사용량(vLLM usage 필드) → 잡 이력에 저장, UI "작업 상세"에서 표시.
- 크래시 리포트는 로컬 저장만 (외부 전송 없음, NFR-30).

# PaperKo — 학술 논문 PDF 한국어 번역 데스크탑 앱

**소프트웨어 개발 문서 세트 (v1.0)**

| 항목 | 내용 |
|---|---|
| 프로젝트명 | PaperKo (가칭) |
| 목적 | 학술지 논문 PDF를 **원본 레이아웃을 유지한 채** 한국어로 번역 |
| 플랫폼 | 데스크탑 (Windows / macOS / Linux) |
| 앱 프레임워크 | Go + Wails v3.0 |
| 번역 엔진 | Python 패키지 `translate_engine` (사이드카 프로세스) |
| LLM | 서버에서 vLLM으로 서빙 중인 `qwen3.6-35B-A3B-NVFP4-fast` (OpenAI 호환 API) |
| 문서 작성일 | 2026-08-06 |

## 문서 구성

| 문서 | 파일 | 내용 |
|---|---|---|
| 소프트웨어 요구사항 명세서 (SRS) | `01-srs.md` | 기능/비기능 요구사항, 사용자 시나리오, 제약사항 |
| 시스템 아키텍처 설계서 | `02-architecture.md` | 프로세스 모델, 컴포넌트 구조, 기술 스택, 배포 |
| 상세 설계서 | `03-detailed-design.md` | `translate_engine` 모듈별 상세 설계, 데이터 모델(IR), Go 측 설계 |
| 인터페이스 명세서 | `04-api-spec.md` | Wails 바인딩, Go↔Python JSON-RPC, vLLM API, 이벤트 |
| 개발 계획서 | `05-dev-plan.md` | 마일스톤, 일정, 리스크 관리 |
| 테스트 계획서 | `06-test-plan.md` | 테스트 전략, 골든 셋, 품질 지표 |

## 시스템 한 줄 요약

```
[Wails v3 데스크탑 앱 (Go + Web UI)]
        │  stdio JSON-RPC 2.0 (사이드카)
        ▼
[translate_engine (Python)]
  ├── __main__.py     # 엔트리포인트: stdio JSON-RPC 서버 / 개발용 CLI
  ├── layout.py       # PDF 파싱 + 레이아웃 분석 + OCR
  ├── terminology.py  # 전문용어 사전 관리·매칭
  ├── translator.py   # 번역 파이프라인 오케스트레이션 + 후처리
  ├── qwen.py         # vLLM(OpenAI 호환) 클라이언트
  └── renderer.py     # 번역 결과를 원본 레이아웃에 재조판(PDF 출력)
        │  HTTP (OpenAI 호환 /v1/chat/completions)
        ▼
[vLLM 서버 — qwen3.6-35B-A3B-NVFP4-fast]
```

기존 개념 설계(Engine 7모듈)와 구현 모듈의 대응은 다음과 같다.

| 개념 모듈 (Engine) | 구현 위치 |
|---|---|
| PDF Parser | `translate_engine/layout.py` |
| Layout Analyzer | `translate_engine/layout.py` |
| OCR | `translate_engine/layout.py` (내부 서브컴포넌트) |
| Terminology | `translate_engine/terminology.py` |
| Translator (Qwen) | `translate_engine/translator.py` + `translate_engine/qwen.py` |
| Post Processor | `translate_engine/translator.py` (후처리 단계) |
| PDF Renderer | `translate_engine/renderer.py` |

## 용어 정의

| 용어 | 정의 |
|---|---|
| IR (Intermediate Representation) | 파싱·레이아웃 분석 결과를 담는 중간 표현 JSON. 모든 모듈 간 데이터 교환의 표준 스키마 |
| 블록(Block) | 레이아웃 분석 단위 — 문단, 제목, 캡션, 표, 그림, 수식 등 |
| 번역 단위(TU, Translation Unit) | LLM에 한 번에 투입되는 텍스트 묶음 (블록 1개 또는 문맥상 병합된 블록들) |
| 플레이스홀더 | 수식·인용·참조 등 번역 대상이 아닌 인라인 요소를 감싸는 토큰 (예: `⟦M1⟧`) |
| 사이드카(Sidecar) | Go 앱이 자식 프로세스로 실행·관리하는 Python 엔진 프로세스 |
| 골든 PDF | 회귀 테스트 기준이 되는 대표 논문 PDF 샘플 세트 |

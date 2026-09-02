# translate_engine — PaperKo 번역 엔진

학술 논문 PDF를 **원본 레이아웃을 유지한 채** 한국어로 번역하는 파이프라인.
설계 문서(`../00-overview.md` ~ `../06-test-plan.md`)의 `translate_engine` 파트 구현체다.

```
PDF ──layout──► 문서 IR ──translator──► 번역(TU) ──renderer──► 레이아웃 유지 번역 PDF
                                 │
                          qwen.py(vLLM/OpenAI 호환)  +  terminology.py(용어집)
```

## 모듈 (03-detailed-design.md 대응)

| 파일 | 책임 |
|---|---|
| `ir.py` | 문서 IR·번역 결과 데이터 모델 + JSON 직렬화 (모든 모듈의 단일 데이터 계약) |
| `layout.py` | PDF 파싱·레이아웃 분석·읽기순서·문단 연결·인라인 요소 감지 (+ OCR 폴백) |
| `terminology.py` | 용어집 로드·Aho-Corasick 매칭·강제 적용 검증 |
| `qwen.py` | vLLM(OpenAI 호환) 비동기 클라이언트 — 동시성·재시도·backpressure·thinking 제거 |
| `translator.py` | TU 구성·플레이스홀더 마스킹·프롬프트·8단계 후처리·체크포인트·취소 |
| `renderer.py` | 리댁션 + `insert_htmlbox` 재조판, 한글 폰트 임베딩, 4단계 맞춤(fitting) |
| `__main__.py` | 개발용 CLI + 앱 사이드카(stdio JSON-RPC 2.0) 서버 |

> 참고: 설계는 블록 감지에 DocLayout-YOLO(ONNX)를 지정한다. 이 빌드에는 모델 파일이
> 번들되어 있지 않아 문서 §2.4의 **폴백 경로**(PyMuPDF 자체 블록 추출 + 휴리스틱 분류)로
> 동작한다. 인터페이스는 모델 비의존적이라 ONNX 백엔드를 나중에 끼울 수 있다.

## 설치

```bash
python3 -m venv .venv && source .venv/bin/activate
pip install -e .            # 또는: pip install pymupdf httpx pysbd
pip install pyahocorasick  # 선택: 대용량 용어집 가속
```

## LLM 서버 설정 (OpenAI 호환)

환경변수 또는 CLI 플래그로 지정한다.

```bash
export PAPERKO_LLM_URL="http://localhost:1234/v1"      # vLLM / LM Studio / Ollama(/v1)
export PAPERKO_LLM_MODEL="qwen/qwen3.6-35b-a3b"
export PAPERKO_LLM_KEY="..."         # 필요 시 Bearer 토큰
export PAPERKO_LLM_CONCURRENCY=4
```

## CLI 사용법 (앱 없이 파이프라인 구동)

```bash
# 연결 점검
python -m translate_engine health

# 1) 분석만 → document.json
python -m translate_engine analyze paper.pdf --out document.json [--pages 1-10,15] [--ocr auto]

# 2) 전체 파이프라인: 분석→번역→재조판
python -m translate_engine translate paper.pdf out.pdf \
    [--glossary glossary.json] [--style formal] [--enforce-glossary] [--mode replace|interleaved]

# 3) 이미 있는 IR·번역으로 재조판만
python -m translate_engine render paper.pdf document.json translation.json out.pdf
```

> **주의(reasoning 모델)**: Qwen3.6처럼 "사고(thinking)" 모드가 켜진 모델은 답 이전에
> 많은 토큰을 소모한다. 엔진은 출력 토큰 예산을 넉넉히 잡고 잘림 시 자동 증액하지만,
> 서버에서 thinking을 끌 수 있으면(예: vLLM `enable_thinking=false`) 훨씬 빠르다.

## 앱 사이드카 모드 (JSON-RPC 2.0 / stdio)

인자 없이 실행하면 `Content-Length` 프레이밍(LSP 동일)으로 JSON-RPC 서버가 뜬다.
메서드: `initialize · document.open · document.analyze · document.translate ·
document.render · tu.retranslate · job.cancel(알림) · shutdown`.
알림: `progress · partial · tu_failed`. 명세는 `../04-api-spec.md` §2.

```bash
python -m translate_engine        # stdout=JSON-RPC 전용, stderr=로그
```

## 테스트

```bash
python tests/make_fixture.py                 # 샘플 논문 PDF 생성
python tests/mock_llm.py 8123 &              # OpenAI 호환 목 서버(오프라인 검증용)
PAPERKO_LLM_URL=http://127.0.0.1:8123/v1 python -m pytest tests/ -q
PAPERKO_LLM_URL=http://127.0.0.1:8123/v1 python tests/rpc_smoke.py   # 사이드카 수명주기
```

## 데이터 경로 / 잡 디렉터리

번역 잡은 작업 디렉터리(기본 `<out>_work/`)에 산출물을 남긴다:
`document.json`(IR) · `translation.json`(체크포인트) · `output.pdf` · (선택)`preview/`.
`translation.json`은 TU 완료마다 원자적으로 갱신되어 중단 후 재개(`resume`)가 가능하다.

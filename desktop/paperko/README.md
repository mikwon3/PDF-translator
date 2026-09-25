# PaperKo — 데스크탑 앱 (Wails v3 + React)

`translate_engine` Python 엔진을 사이드카로 구동하는 GUI 셸.
아키텍처 대응: `../../02-architecture.md` §1.2 / `../../04-api-spec.md` §1.

```
Frontend (React/TS)  ──Wails 바인딩/이벤트──►  Go 서비스 계층
                                                  │  stdio JSON-RPC 2.0
                                                  ▼
                                    python -m translate_engine (사이드카)
```

## 구성

| 계층 | 위치 | 역할 |
|---|---|---|
| Go 진입점 | `main.go` | 앱·윈도우 생성, 엔진 Supervisor 기동, 서비스 등록 |
| 엔진 브리지 | `internal/engine/` | JSON-RPC stdio 클라이언트(`rpc.go`) + 사이드카 수명주기(`supervisor.go`) |
| 서비스(바인딩) | `services/` | DocumentService·JobService·SettingsService, 엔진 알림→프론트 이벤트 변환 |
| 프론트엔드 | `frontend/src/` | 열기·분석·번역 진행률·원문/번역 대조 뷰·저장, 설정 모달 |

## 사전 준비

- 엔진 venv가 있어야 함: 저장소 루트에서 `python3.14 -m venv .venv && source .venv/bin/activate && pip install -e engine-py`
- LLM 서버(OpenAI 호환): 설정 화면에서 URL/모델 지정. 기본값 `http://localhost:1234/v1`, `qwen/qwen3.6-35b-a3b`.

## 개발 실행

```bash
cd desktop/paperko
wails3 dev            # 핫리로드 개발 모드 (엔진 경로는 main.go의 devRepoRoot 상수 사용)
```

엔진 위치를 옮겼다면 환경변수로 지정:
```bash
export PAPERKO_ENGINE_ROOT=/path/to/PDF-translator   # <root>/.venv/bin/python, <root>/engine-py 사용
export PAPERKO_ENGINE_PYTHON=/custom/python          # (선택) 인터프리터 직접 지정
```

## 빌드

```bash
cd desktop/paperko
wails3 build         # 프론트 빌드 + Go 바이너리 → bin/
```

## 테스트

```bash
# Go↔Python 브리지 (LLM 불필요)
go test ./internal/engine/ -run TestSidecarBridge -v
# 전체 번역·재조판 (실서버 필요)
PAPERKO_TEST_LLM=http://localhost:1234/v1 go test ./internal/engine/ -run TestSidecarTranslate -v
```

## 검증된 저널 레이아웃

PaperKo는 저널별 고정 템플릿을 두지 않고 **범용 레이아웃 분석**(2단 검출·front-matter·
무테표·절 제목 분리)으로 동작한다. 저널 전용 분기는 **MDPI 앞면 처리 하나**뿐이고, 나머지는
아래 논문들로 실제 재분석→번역→렌더 파이프라인을 돌려 **범용 로직을 다듬으며 검증**한 것이다.

> 새 저널/형식을 검증할 때마다 아래 표에 한 줄 추가하고, 굵직한 수정은 `CHANGELOG.md`에도 남긴다.

| 저널 | 발행 계열 | 다듬은/확인한 부분 | 검증일(버전) |
|---|---|---|---|
| Structural Engineering and Mechanics | Techno-Press (2단) | 1p abstract·저자 레이아웃, 절 제목이 앞 문단에 붙는 문제, 표·그림 겹침 | 1.8 |
| Elsevier (일반 2단) | Elsevier | 무테 표, 절 제목 분리 | 1.8 |
| KSCE Journal of Civil Engineering | KSCE/Springer | front-matter 오탐 제거, 주황(검증 실패) 오탐 | 1.8 |
| Earthquake Spectra | SAGE/EERI | 번호 없는 가운데정렬 헤딩, abstract 끝 DOI 오탐 | 1.8 |
| Construction and Building Materials | Elsevier | 무테표·헤딩 반향(echo) 재시도 | 1.8 |
| Engineering Structures | Elsevier | 무테 표, DOCX/HWPX 격자 복원 | 1.8 |
| Polymers | MDPI | 앞면 마스트헤드 로고 + 좌측 메타데이터 사이드바 원문 유지(전용 규칙) | 1.8.1 |
| ZAMM | Wiley | 약한 모델에서 페이지 커버리지 개선(재조립) | 1.7 |
| Structures | Elsevier | ScienceDirect front-matter·HIGHLIGHTS 박스, 초록 1단→본문 2단 혼합 레이아웃 유지 | 1.8.1 |
| Journal of Building Engineering | Elsevier (스캔 1단 OCR) | 스캔 페이지 감지·1단 OCR 흐름 재구성, ScienceDirect front-matter | 1.8.1 |
| Journal of Structural Engineering | ASCE (2단) | ASCE 2단(초록 1단), DOI·CE Database 헤더, 수식·표 유지 | 1.8.1 |
| Advances in Structural Engineering | SAGE (2단) | SAGE 2단(초록 1단) front-matter, 러닝헤드 처리 | 1.8.1 |
| Earthquake Engineering & Structural Dynamics | Wiley (1단) | 구형 Wiley 1단, DOI 10.1002/eqe 헤더 | 1.8.1 |
| Journal of Applied Mathematics | Hindawi | Hindawi Article-ID 메타 헤더, 초록 1단→본문 2단 혼합, 수식 다수 | 1.8.1 |
| ACI CODE-440.11-22 | ACI (시방서, 2단 Code/Commentary) | 조항 변경마커(`=`·제어문자 ◆) 및 소프트하이픈 제거; 좌 규정/우 해설 병렬 2단 유지, 표 복원, 조항 헤딩 인식 | 1.8.2 |
| CSA S806-12 | CSA (시방서, 단일 단 OCR 스캔) | 스캔 페이지 감지 → 이미지 픽셀 redaction + 헤딩 박스 우측 확장으로 스캔 영어 꼬리 제거 | 1.8.2 |
| ACI MNL-723 GFRP Design Handbook | ACI (설계 핸드북, 단일 단 OCR 스캔) | 본문·워크드 예제 번역 양호, 도면·수식(이미지) 보존; 스캔 redaction을 전 블록으로 확대. 원본 OCR이 놓친 수식 옆 라벨은 겹침 잔존(원본 OCR 품질 한계) | 1.8.2 |

그 외: **GFRP 논문**(초기 `paper.pdf`)으로 한→영처럼 번역문이 길어지는 **확장 레이아웃**에서
2단이 유지되는지 검증.

발행 계열로는 **저널 8개(Elsevier · MDPI · KSCE/Springer · SAGE · Wiley · Techno-Press ·
Hindawi · ASCE)** 에 더해 **설계기준·시방서 2개(ACI · CSA)** 를 커버한다. 학술 논문이 아닌 일반 PDF도 번역된다(위
학술 휴리스틱이 매칭되지 않으면 문서 전체가 번역 대상이 됨) — 단, 잡지·브로슈어·슬라이드처럼
그래픽 위주의 복잡한 편집은 재조판 정확도가 낮다.

시방서 관련 알려진 한계(추후 개선): ① 매우 긴 OCR 헤딩 하나가 스캔 배경과 겹칠 수 있음,
② 번역문(한국어)이 원문보다 크게 길어질 때 인접 목록 블록과 드물게 겹침, ③ 로컬 모델(qwen)이
일부 용어를 한국어 대신 중국어로 출력(예: cast-in-place→现场) — 용어집으로 완화 가능.

## 구현 범위 (v1 슬라이스)

구현됨: PDF 열기·메타·미리보기, 분석→번역→재조판 파이프라인 구동, 실시간 진행률/상태/로그
이벤트, 원문/번역 페이지 대조 뷰, 결과 저장, 설정(LLM URL/모델/키·동시성·폰트·문체·모드)과
연결 테스트, 엔진 크래시 자동 재시작(백오프).

미구현(후속): 용어집 UI·SQLite, 블록 수동 수정 오버레이(FR-25), 부분 재번역 UI(UC-03),
잡 이력·이어보기 UI, PyInstaller 번들 패키징.

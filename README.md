# PaperKo

**학술 논문·일반 문서 PDF를 원본 레이아웃을 유지한 채 한국어(및 다국어)로 번역하는 데스크탑 앱**

PaperKo는 논문·설계기준·시방서·가이드북 같은 PDF를 열어, 원문의 2단 편집·표·그림·수식 배치를
그대로 유지하면서 번역본 **PDF**를 만들고, 편집 가능한 **한글(HWPX)·Word(DOCX)** 로도 저장합니다.

- **플랫폼:** macOS (Apple Silicon) · Windows x64 (Linux 동일 구조)
- **앱 프레임워크:** Go + [Wails v3](https://wails.io)
- **번역 엔진:** Python 패키지 `translate_engine` (stdio JSON-RPC 사이드카)
- **프론트엔드:** React + TypeScript (Vite)
- **LLM:** OpenAI 호환 원격 서버(vLLM 등) 또는 번들 `llama.cpp`(오프라인)

---

## 주요 기능

- **레이아웃 보존 번역 PDF** — PyMuPDF 기반 레이아웃 분석 → 번역 → 제자리 재조판. 2단/1단, 표, 그림,
  수식, 절 제목, 참고문헌 구조를 유지합니다.
- **HWPX·DOCX 저장** — 완전 오프라인. 표는 실제 표 객체로 복원, 그림은 이미지로 삽입.
- **일반 문서 모드** — 긴 문서(설계기준·시방서·핸드북)를 **페이지 범위 배치**로 나누어 번역하고,
  하나의 출력본에 누적합니다.
- **중단 / 이어받기 / 재시작 후 이어하기** — TU 단위 체크포인트로, 중단해도 완료분을 잃지 않고
  앱을 껐다 켜도 이어서 번역합니다.
- **다국어** — 대상 언어: 한국어·English·日本語·中文·Español·Deutsch·Français. UI 언어 한/영 토글.
- **OCR** — 스캔·이미지 PDF도 번역(PyMuPDF 내장 Tesseract, 영어 데이터 번들). 스캔 문서의 그림도
  잘라 보존합니다.
- **오프라인 모드** — 서버 없이 번들 `llama.cpp` + 로컬 GGUF 모델로 번역 가능.
- **온라인 자동 업데이트** — 시작 시 새 판을 확인해(하루 1회, 끄기·건너뛰기 가능) 서명된 릴리스를
  받아 스스로 설치한다. macOS 는 새 `.app` 으로 교체 후 재실행, Windows 는 설치 프로그램이 이어받는다.
  릴리스 정보(manifest)는 Ed25519 서명으로 검증하고 설치본은 SHA-256 으로 대조한다(`internal/update`).

## 검증된 문서 형식

저널별 고정 템플릿이 아닌 **범용 레이아웃 분석**으로 동작하며, 아래 형식들로 엔드투엔드 검증되었습니다
(자세한 표는 [`desktop/paperko/README.md`](desktop/paperko/README.md) 참조):

- **저널:** Structural Engineering and Mechanics, Elsevier(2단), KSCE, Earthquake Spectra,
  Construction and Building Materials, Engineering Structures, MDPI(Polymers), ZAMM
- **설계기준·시방서:** ACI CODE-440.11-22, CSA S806-12, ACI MNL-723 (OCR 스캔 핸드북)

## 아키텍처

```
Frontend (React/TS)  ──Wails 바인딩/이벤트──►  Go 서비스 계층 (main.go, services/)
                                                   │  stdio JSON-RPC 2.0
                                                   ▼
                                     python -m translate_engine  (사이드카)
                                                   │
                        PDF 파싱·레이아웃 분석 → LLM 번역 → 레이아웃 보존 재렌더
                                                   │
                                     OpenAI 호환 LLM (원격 vLLM 또는 로컬 llama.cpp)
```

## 리포지토리 구조

| 경로 | 내용 |
|---|---|
| `engine-py/` | Python 번역 엔진(`translate_engine`) — 레이아웃 분석·번역·재렌더·DOCX/HWPX 내보내기, 번들 폰트·tessdata·hwpx 변환기 |
| `desktop/paperko/` | Wails 데스크탑 앱 — Go 진입점/서비스, React 프론트엔드, 빌드 스크립트 |
| `webapp/` | (선택) 웹 데모 서버 |
| `schemas/` | IR·번역 JSON 스키마 |
| `00~06-*.md` | 설계 문서(SRS, 아키텍처, 상세설계, API, 개발/시험 계획) |
| `BUILD-WINDOWS.md` | Windows 배포본 빌드 가이드 |
| `CHANGELOG.md` | 변경 이력 |

## 빌드

> 번들 런타임(파이썬·llama)과 빌드 산출물은 용량 때문에 git에 포함하지 않습니다.
> 아래 스크립트가 이를 내려받아 앱 옆에 번들합니다.

### 사전 준비
- Go 1.24+, Node.js 20+, [Wails v3 CLI](https://wails.io) (`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`)
- (Windows 인스톨러) NSIS `makensis`

### macOS
```bash
cd desktop/paperko
bash scripts/fetch-python-macos.sh          # 번들 CPython + 엔진 설치 (resources/python)
bash scripts/fetch-llama-macos.sh           # (선택) 오프라인 모드용 llama.cpp
bash scripts/build-macos-dmg.sh             # → ~/Downloads/PaperKo-<ver>-<arch>.dmg
```

### Windows
```powershell
cd desktop\paperko
powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1     # exe + 번들 파이썬
powershell -ExecutionPolicy Bypass -File scripts\package-windows.ps1   # → bin\PaperKo-<ver>-amd64-installer.exe
```
자세한 내용은 [`BUILD-WINDOWS.md`](BUILD-WINDOWS.md) 참조.

### 개발 실행(핫리로드)
```bash
cd desktop/paperko
wails3 dev
```

## 사용

1. 앱 실행 → **⚙ 설정**에서 LLM 지정
   - 원격: vLLM 등 OpenAI 호환 서버 URL·모델명 입력 후 **연결 테스트**
   - 오프라인: **로컬 모델** 탭에서 GGUF 다운로드 또는 파일 선택
2. **📂 PDF 열기** → (긴 문서면 자동으로 *일반 문서* 모드 추천) → **▶ 번역 시작** 또는 페이지 범위 배치 번역
3. 완료 후 **PDF / 한글(HWPX) / Word(DOCX)** 로 저장

## 릴리스 배포 (온라인 업데이트)

설치본과 서명된 `manifest.json` 을 **공개** 저장소(`mikwon3/PaperKo-releases`)의 GitHub 릴리스에
올리면, 설치된 앱이 그 최신 릴리스를 읽어 스스로 업데이트한다. 소스 저장소는 비공개다.

```bash
cd desktop/paperko
./scripts/release.sh 1.8.4 release-notes/1.8.4.md            # 판 올림·빌드·서명·업로드
./scripts/release.sh 1.8.4 release-notes/1.8.4.md --dry-run  # 올리지 않고 확인만
```

- **서명 개인키**(Ed25519)는 저장소 밖 `~/Library/Application Support/PaperKo/release-key/paperko-release.key`
  에만 둔다. **잃어버리면 이미 설치된 앱이 받아들일 새 판을 더는 만들 수 없으므로 꼭 백업**한다.
  (짝이 되는 공개키는 `internal/update/key.go` 에 심겨 있다.)
- 새 키를 만들려면: `go run ./cmd/releasetool keygen` → 출력된 공개키를 `key.go` 에 넣는다.
- 시험: `PAPERKO_UPDATE_BASE` 로 릴리스 주소를 로컬 서버로 바꿔 확인할 수 있다(서명은 언제나 심은
  공개키로 검증하므로 가짜 판은 받지 않는다).

## 라이선스 / 표기

PaperKo 는 **GNU Affero General Public License v3.0 (AGPL-3.0)** 으로 배포합니다
(전문 [`LICENSE`](LICENSE)). PDF 처리 핵심 의존성인 PyMuPDF 가 AGPL-3.0 이라, 그와
결합한 앱 전체가 AGPL-3.0 을 따르며 전체 소스를 같은 조건으로 공개합니다.

번들 폰트(Nanum, Noto Sans CJK)와 서드파티 구성요소의 저작권·라이선스 표기는
[`NOTICE.md`](NOTICE.md)를 참조하세요.

---

<sub>일부 개발은 [Claude Code](https://claude.com/claude-code)의 도움을 받았습니다.</sub>

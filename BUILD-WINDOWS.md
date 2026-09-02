# PaperKo — Windows 11 배포본 빌드 가이드

이 문서는 **Windows 11 머신(예: RTX 3090 박스)에서** PaperKo 데스크탑 앱의
설치용 실행본을 만드는 절차다. (PyInstaller가 크로스컴파일을 지원하지 않고
Wails의 웹뷰가 CGO를 쓰므로, 최종 빌드는 반드시 Windows에서 수행한다.)

배포 구조:
```
PaperKo.exe                     ← Wails 앱 (프론트 임베드)
resources\engine\
    translate-engine.exe        ← PyInstaller로 번들한 Python 엔진 (자체 포함)
    (그 외 엔진 런타임 파일들)
```
앱은 실행 시 `resources\engine\translate-engine.exe`를 사이드카로 띄운다
(`main.go`의 `resolveEngine()`가 자동 탐색). 한글 폰트는 Windows 기본
`malgun.ttf`(맑은 고딕)를 사용하므로 별도 폰트 번들이 필요 없다.

LLM은 원격(Spark vLLM)에 있으므로 각 클라이언트는 앱 설정에서 서버 URL만
지정하면 된다. 파싱·레이아웃·재조판은 클라이언트에서 로컬로 수행된다.

---

## 0. 사전 설치 (Windows 11)

| 도구 | 버전 | 비고 |
|---|---|---|
| Go | 1.24+ | https://go.dev/dl |
| Node.js | 20 LTS+ | https://nodejs.org |
| Python | **3.12** 권장 | 3.14는 일부 wheel 미비 가능 |
| Wails v3 CLI | beta | `go install github.com/wailsapp/wails/v3/cmd/wails3@latest` |
| C 컴파일러 | — | `wails3 doctor`가 요구하면 설치(예: winlibs gcc). WebView2 런타임은 Win11 기본 포함 |
| NSIS | (선택) | 인스톨러(.exe) 생성용. 미설치 시 앱 exe만 생성 |

먼저 진단:
```powershell
wails3 doctor
```
빠진 항목이 있으면 안내대로 설치한다.

---

## 1. 엔진 동봉 — 두 가지 방법

앱은 실행 시 `resources\`에서 다음 순서로 엔진을 찾는다(`main.go resolveEngine`):
① 환경변수 → **②a `resources\python\python.exe`(임베디드 파이썬)** →
**②b `resources\engine\translate-engine.exe`(PyInstaller)** → ③ 개발용 venv.
둘 중 하나만 준비하면 된다.

### 방법 A — standalone CPython (권장, `._pth` 문제 없음)

**python-build-standalone**(재배치 가능한 완전한 CPython)을 앱과 함께 넣는다. 완전한
CPython이라 **pip·import가 그냥 동작**하고, embeddable 패키지의 `._pth` 함정(“No module
named …”, PyMuPDF 로드 실패 등)이 **원천적으로 없다.** 스크립트가 자동화한다:

먼저 `wails3 build`로 `bin\`을 만든 뒤(2단계) 스크립트를 돌리면 **`bin\resources\python`에
바로 설치**되어 추가 복사가 필요 없다:
```powershell
cd <repo>\desktop\paperko
wails3 build                                                   # bin\paperko.exe 생성
powershell -ExecutionPolicy Bypass -File scripts\fetch-python-windows.ps1
```
→ `bin\resources\python\python.exe`(엔진·PyMuPDF·httpx·pysbd·ahocorasick 설치 완료)

점검:
```powershell
bin\resources\python\python.exe -s -m translate_engine health
```
→ `{ "ok": true, ... }` 이면 성공. 이제 `bin\paperko.exe` 실행 시 사이드카가 정상 구동된다
(검은 콘솔 창 없음).

> 앱은 실행 시 `resources\python\python.exe -s -m translate_engine`로 사이드카를 띄운다
> (`-s`로 시스템 Python과 격리). **콘솔 창(검은 창)은 코드에서 숨기도록 반영됨**
> (Windows에서 `CREATE_NO_WINDOW`) — 앱을 다시 빌드하면 사라진다.

### 방법 B — 임베디드 Python (더 가볍지만 `._pth` 주의)

python.org의 Windows embeddable package. 더 작지만 **`._pth`를 반드시 편집**해야 하며,
누락 시 import 오류가 난다. 문제가 잦으면 방법 A를 쓸 것.

```powershell
$app = "<repo>\desktop\paperko\bin"
Expand-Archive python-3.12.*-embed-amd64.zip -DestinationPath "$app\resources\python"
py -3.12 -m pip install --target "$app\resources\python\Lib\site-packages" `
    "<repo>\engine-py" pymupdf httpx pysbd pyahocorasick python-docx
# python312._pth 편집: 'Lib\site-packages' 추가 + 'import site' 주석 해제 (필수)
```

### 방법 C — PyInstaller (단일 폴더 번들)

```powershell
cd <repo>\engine-py
py -3.12 -m venv .venv-win
.\.venv-win\Scripts\activate
pip install -e . pyinstaller pyahocorasick
pyinstaller build\translate-engine.spec --noconfirm
```
결과: `engine-py\dist\translate-engine\translate-engine.exe` (+ 런타임 폴더)

점검:
```powershell
.\dist\translate-engine\translate-engine.exe analyze tests\fixtures\sample.pdf --out doc.json
```
(사전에 `python tests\make_fixture.py`로 sample.pdf 생성)

---

## 1.5 OCR (이미지·스캔 PDF) — 자동 번들, 별도 설치 불필요

이미지로만 구성된 PDF(텍스트 레이어 없음)도 번역됩니다. 엔진이 **텍스트 레이어가 없으면 자동으로 OCR**을 수행합니다(`analyze(ocr="auto")`).

- PyMuPDF 휠에 **Tesseract 엔진이 내장**되어 있어 시스템 Tesseract 설치가 필요 없습니다.
- 언어 데이터(`eng.traineddata`, ~4MB)는 엔진 패키지에 번들되어 있고(`engine-py/translate_engine/tessdata/`), `pip install` 시 함께 설치됩니다. 즉 **1단계로 엔진을 번들 파이썬에 설치하면 OCR도 그대로 포함**됩니다 — 추가 작업 없음.
- 엔진이 실행 시 `TESSDATA_PREFIX`를 번들 tessdata로 자동 지정합니다(환경변수를 이미 설정했다면 그 값을 존중).
- **다른 원문 언어**가 필요하면 https://github.com/tesseract-ocr/tessdata_fast 에서 `<lang>.traineddata`를 받아 `engine-py/translate_engine/tessdata/`에 넣고 다시 번들하면 됩니다(기본은 영어 `eng`).
- 한계: 스캔 품질·수식·2단 레이아웃에서는 인식·재조판 정확도가 텍스트 PDF보다 낮습니다.

---

## 1.5b HWPX·DOCX 저장 — 자동 번들, 별도 설치 불필요

번역본을 PDF 외에 **한글(.hwpx)·Word(.docx)** 로도 저장할 수 있습니다("완료" 패널의
`한글(HWPX)`/`Word(DOCX)` 버튼). 완전 오프라인으로 동작합니다(인터넷·한컴 설치 불필요).

- **의존성**: `python-docx` 하나뿐이며, 위 1단계 `pip install` 목록에 포함되어 있어
  엔진을 번들 파이썬에 설치하면 함께 설치됩니다 — 추가 작업 없음.
- **docx→hwpx 변환기**는 엔진 패키지에 번들되어 있습니다
  (`engine-py/translate_engine/hwpx/` — `convert.py`, `omml_to_hwp.py`,
  `default_skeleton.hwpx`). `pip install` 시 package-data로 함께 설치됩니다.
- 흐름형(편집 가능) 문서로 재구성하므로 PDF의 고정 레이아웃을 픽셀 단위로 복제하지는
  않습니다. **표는 실제 표 객체로 복원**됩니다(PyMuPDF 표 검출 + 가로 괘선으로 행 분리 →
  한글 표). 병합·다중행 셀은 근사입니다.

---

## 1.6 오프라인 모드 (로컬 llama.cpp 모델) — 선택 번들

서버 없이 앱만으로 번역하려면 `llama-server`(llama.cpp)를 번들합니다. 넣지 않으면
앱은 기존처럼 원격 vLLM/LM Studio로만 동작합니다.

```powershell
# Windows (CPU x64 기본; NVIDIA면 $env:LLAMA_ASSET="win-cuda-12.4-x64")
powershell -ExecutionPolicy Bypass -File scripts\fetch-llama-windows.ps1
```
```bash
# macOS (Apple Silicon, Metal 포함)
bash scripts/fetch-llama-macos.sh
```
- `resources/llama/`에 `llama-server`(+공유 라이브러리)가 스테이징되고, 빌드
  스크립트가 이를 앱의 `resources/llama`로 함께 번들합니다.
- **모델(GGUF)은 인스톨러에 미포함** — 앱 설정의 **로컬 모델** 탭에서 첫 실행 시
  다운로드하거나, 이미 받은 GGUF(예: LM Studio)를 **파일 선택**으로 지정합니다.
  권장 모델: 구글 `gemma-4-e2b-it-qat`.
- 앱 설정에서 **원격 서버 ↔ 로컬 모델(오프라인)** 토글로 전환합니다.
- Windows CUDA 빌드를 쓰면 `cudart-*` DLL도 함께 두거나 CUDA 런타임이 필요합니다.

---

## 1.7 출력 폰트 (한/영/일/중) — 자동 번들, 별도 설치 불필요

번역 언어(한국어·English·日本語·中文)에 따라 출력 PDF에 임베딩할 폰트가 **엔진 패키지에
번들**되어 있어, 1단계에서 엔진을 `pip install`하면 **자동으로 포함**됩니다(추가 작업 없음).

- 번들 폰트(`engine-py/translate_engine/fonts/`, 모두 SIL OFL):
  - 한국어: **Nanum Gothic / Nanum Myeongjo** (기본, 한글+라틴)
  - 일본어: **Noto Sans JP** (정적 TTF)
  - 중국어(간체): **Noto Sans SC** (정적 TTF)
- 렌더러가 **번역 언어에 따라 폰트를 자동 선택**합니다(일본어→JP, 중국어→SC, 그 외→나눔).
- 용량: CJK 폰트로 **엔진 패키지가 약 +15MB** 증가합니다.
- ⚠️ CJK는 반드시 **TTF(glyf)** 를 씁니다 — CFF/OTF CJK 폰트는 PyMuPDF의 htmlbox에서 글리프가
  깨집니다. 폰트를 교체·추가할 땐 TTF를 사용하세요(번체가 필요하면 `NotoSansTC-Regular.ttf` 추가).

> 참고(선택): **인라인 수식 렌더링**(`$…$`→수식 이미지)은 **기본 꺼짐**이며 matplotlib이
> 필요합니다. 번들에 넣지 않는 한 Windows 빌드에 영향이 없습니다(끄면 `$`만 제거).
> 켜려면 번들 파이썬에 `pip install matplotlib` 후 렌더 옵션 `render_inline_math=True`.

---

## 2. 앱 빌드 (Wails)

```powershell
cd <repo>\desktop\paperko
wails3 build
```
결과: `bin\paperko.exe` (프로덕션 빌드는 `-H windowsgui`라 **메인 앱은 콘솔 없음**).

---

## 3. 엔진을 앱 옆에 배치

- **방법 A(standalone, 권장)**: `fetch-python-windows.ps1`이 이미 `bin\resources\python\`에
  설치하므로 **추가 작업 없음**.
- **방법 C(PyInstaller)**: 번들 폴더를 `bin\resources\engine\`로 복사.
  ```powershell
  mkdir bin\resources\engine
  xcopy /E /I /Y ..\..\engine-py\dist\translate-engine bin\resources\engine
  ```

이제 `bin\paperko.exe`를 실행하면 엔진을 사이드카로 자동 구동한다(검은 콘솔 없음).
`bin\` 폴더 전체가 이식 가능한(portable) 배포 단위다.

---

## 4. NSIS 인스톨러 생성 (resources\ 포함, 설정 완료됨)

인스톨러 스크립트 `build\windows\nsis\project.nsi`는 이미 **`bin\resources\`를 통째로
설치 디렉터리에 포함**하도록 수정돼 있다(`File /r "..\..\..\bin\resources"`). 따라서
인스톨러를 만들기 **전에 `bin\resources\`가 채워져 있어야 한다**(1~3단계 완료 상태).

현재 wails3(beta)에서는 `-nsis` 플래그가 사라지고 **`wails3 package`** 로 바뀌었다.
NSIS(makensis)가 PATH에 있어야 한다(`winget install NSIS.NSIS` 또는 nsis.sourceforge.io).

```powershell
cd <repo>\desktop\paperko
# 반드시: 1~3단계로 bin\paperko.exe 와 bin\resources\python\ 이 준비된 상태에서
wails3 package
```
산출물: `bin\paperko-amd64-installer.exe`

또는 makensis 직접 호출(디버깅/수동):
```powershell
cd <repo>\desktop\paperko\build\windows\nsis
makensis -DARG_WAILS_AMD64_BINARY=..\..\..\bin\paperko.exe project.nsi
```

설치 결과: `%ProgramFiles%\...\PaperKo\` (또는 사용자 스코프 시 `%LocalAppData%\Programs\PaperKo\`)
아래에 `paperko.exe` + `resources\python\...`(엔진 런타임)이 함께 설치되고, 바탕화면·시작
메뉴 바로가기가 생성된다. 제거 시 `resources\`까지 모두 삭제된다.

> **순서 주의**: `wails3 package`가 앱을 다시 빌드하며 `bin\`을 정리할 수 있으니,
> **① `wails3 build` → ② `fetch-python-windows.ps1`로 bin\resources 채우기 → ③ `wails3 package`**
> 순서를 지킨다. (package가 resources를 지운다면, build만 하고 makensis를 직접 호출하는
> 위 두 번째 방법을 쓴다.) 사용자 스코프(UAC 없이 설치)는 `project.nsi`에서
> `!define WAILS_INSTALL_SCOPE "user"` 주석 해제, 코드서명은 하단 `!finalize 'signtool ...'` 활성화.

---

## 5. 클라이언트 최초 실행 설정

1. `PaperKo.exe` 실행 → 창이 열림
2. **⚙ 설정**에서:
   - vLLM 서버 URL: `http://<Spark-IP>:8567/v1`
   - 모델명: `unsloth/Qwen3.6-35B-A3B-NVFP4-Fast`
   - API 키: (vLLM에 `--api-key` 설정 시에만)
   - **연결 테스트** → `✓ 연결됨` 확인 → **저장**
3. **📂 PDF 열기 → ▶ 번역 시작**

설정은 `%AppData%\PaperKo\settings.json`에 저장된다. 30명 배포 시 이 파일을
사전 구성해 함께 배포하면 최초 설정을 생략할 수 있다.

---

## macOS / Linux 배포 (동일 구조 — 검증됨)

같은 자기완결 방식을 Mac/Linux에도 적용한다. Windows embeddable 대응물은
**python-build-standalone**(재배치 가능 CPython)이다. 앱의 `resolveEngine()`은
`resources/python/bin/python3`를 자동 탐색하므로 코드 변경이 필요 없다.

```bash
# 1) 재배치 가능 CPython 3.12 (예: macOS arm64) 내려받아 resources/에 풀기
#    https://github.com/astral-sh/python-build-standalone/releases 에서
#    cpython-3.12.*-<platform>-install_only.tar.gz 선택
tar -xzf cpython-3.12.*-aarch64-apple-darwin-install_only.tar.gz -C desktop/paperko/resources
#    → desktop/paperko/resources/python/bin/python3

# 2) 엔진 + 의존성을 번들 파이썬에 설치 (전체 CPython이라 pip 그대로 사용)
desktop/paperko/resources/python/bin/python3 -m pip install \
    ./engine-py pymupdf httpx pysbd pyahocorasick

# 3) 점검 (앱과 동일하게 -s 격리로 구동)
desktop/paperko/resources/python/bin/python3 -s -m translate_engine health
```

앱을 `resources/` 옆에서 실행하면 자동으로 이 번들 파이썬을 사이드카로 띄운다
(dev venv 대신). 이 저장소에서 실제로 검증됨:
`resources/python/bin/python3 -s -m translate_engine` 로 사이드카 구동 + Spark
서버 상대 전체 번역 성공. Windows embeddable도 **동일 코드 경로**를 쓴다.

> Windows embeddable과 달리 standalone 빌드는 완전한 CPython이라 `._pth` 편집이
> 불필요하고 pip이 그대로 동작한다. 시스템 Python과의 격리는 절대경로 실행 +
> `-s`(사용자 site-packages 무시)로 동일하게 보장된다.

## 문제 해결 (Troubleshooting)

- **앱과 함께 검은 콘솔 창이 뜬다** → Python 사이드카의 콘솔이다. 코드에서
  `CREATE_NO_WINDOW`로 숨기도록 반영됨(`internal/engine/proc_windows.go`).
  **최신 소스로 `wails3 build`를 다시** 하면 사라진다. (메인 앱은 `-H windowsgui`라 원래 콘솔 없음.)
- **Python import 오류(“No module named …”, PyMuPDF 로드 실패 등)** → 대개 embeddable
  패키지의 `._pth` 미설정이 원인. **방법 A(standalone CPython)** 로 바꾸면 `._pth`가 없어
  해결된다. 진단: `bin\resources\python\python.exe -s -m translate_engine health`.
- **`wails3 build -nsis`가 안 된다** → 현재 beta에선 **`wails3 package`** 로 바뀌었다.
  (makensis가 PATH에 있어야 함: `winget install NSIS.NSIS`.)
- **엔진을 못 찾는다** → 탐색 순서(`main.go resolveEngine`): ① 환경변수
  (`PAPERKO_ENGINE_PYTHON`/`PAPERKO_ENGINE_ROOT`) → **②a `resources\python\python.exe`(standalone/embeddable)**
  → ②b `resources\engine\translate-engine.exe`(PyInstaller) → ③ 개발용 venv.
  `bin\resources\python\python.exe`가 있는지 확인.

## 주의·팁

- **관리자 권한 불필요** — portable `bin\` 폴더만으로 실행된다(설치본은 Program Files).
- **백신/SmartScreen**: 미서명 시 SmartScreen 경고가 뜰 수 있다 — 사내 배포 시 `signtool`
  코드서명 권장(`project.nsi` 하단 `!finalize` 주석 활성화).
- **버전 고정**: standalone CPython 버전은 `scripts\fetch-python-windows.ps1` 상단
  `$Rel`/`$Ver`에서 바꾼다.

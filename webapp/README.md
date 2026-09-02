# PaperKo Web (구조 A: 로컬 실행 + 공개 터널)

무거운 처리(PDF 파싱·레이아웃·재조판)는 **이 로컬 머신**에서 돌고, LLM은 원격(Spark)입니다.
공개는 **Cloudflare 퀵 터널**로 — 큰 서버·고정 IP·방화벽 개방이 필요 없습니다.

```
브라우저 ──HTTPS──► cloudflared 터널 ──► 로컬 FastAPI(webapp) + translate_engine ──HTTP──► vLLM(Spark)
```

데스크탑 앱과 **엔진(translate_engine)을 그대로 공유**합니다.

## 1. 설치 (한 번)

```bash
cd <repo>            # PDF-translator
python3 -m venv .venv && source .venv/bin/activate     # 이미 있으면 생략
pip install -e engine-py                                # 엔진
pip install -r webapp/requirements.txt                  # 웹서버
```

## 2. 서버 실행 (로컬)

```bash
bash webapp/run.sh          # → http://localhost:8000
# 서버 URL/모델은 앱 우측 상단 ⚙ 설정에서 지정 (기본 Spark)
```
브라우저에서 `http://localhost:8000` 접속 → PDF 업로드 → 번역 → 대조뷰 → 저장.

## 3. 공개 (Cloudflare 퀵 터널 — 처음 쓰기 쉬움)

cloudflared 설치:
```bash
brew install cloudflared          # macOS
# Linux: https://pkg.cloudflare.com/ 또는 바이너리 다운로드
```
터널 열기 (서버가 켜진 상태에서 다른 터미널):
```bash
bash webapp/tunnel.sh
```
출력에 `https://<무작위>.trycloudflare.com` 주소가 뜹니다. **그 주소를 사용자에게 공유**하면 됩니다.
- 계정·도메인 불필요, 인바운드 포트 개방 불필요(로컬이 밖으로 연결).
- 터널을 끄면(또는 재시작하면) 주소가 바뀝니다 — 임시/테스트용.

## 4. 고정 주소·상시 운영 (선택, 나중에)

무작위 주소 대신 `paperko.example.com` 같은 고정 주소가 필요하면 **Named Tunnel**:
```bash
cloudflared tunnel login                       # Cloudflare 계정 로그인(도메인 필요)
cloudflared tunnel create paperko
cloudflared tunnel route dns paperko paperko.example.com
cloudflared tunnel run --url http://localhost:8000 paperko
```
서버·터널을 부팅 시 자동 실행하려면 systemd(리눅스) / launchd(macOS) 서비스로 등록.

## 참고
- 동시 처리 잡 수 제한: `PAPERKO_WEB_JOBS`(기본 2), 업로드 크기 `PAPERKO_WEB_MAX_MB`(기본 60).
- 기본 LLM: `PAPERKO_LLM_URL` / `PAPERKO_LLM_MODEL` 환경변수(기본 Spark).
- 업로드/결과는 `webapp/data/jobs/<id>/`에 저장 — 주기적으로 정리하세요(개인정보).
- 프론트엔드는 빌드 불필요(순수 HTML/JS). 데스크탑 앱과 동일 엔진이라 두 형태를 함께 유지 가능.

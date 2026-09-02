# PaperKo Web — 서버 배포 (Spark + Cloudflare Tunnel)

웹앱을 vLLM 서버(Spark)에 얹고 Cloudflare 터널로 공개합니다. **sudo/root 불필요** —
사용자 systemd 서비스 + linger로 로그아웃/재부팅에도 유지됩니다.

```
[사용자 브라우저] → Cloudflare → [cloudflared(Spark)] → 127.0.0.1:8700 (웹앱)
                                                        → localhost:8000 (vLLM, 로컬)
```

## 1. 웹앱 배포 (Mac에서)

```bash
ssh-add ~/spark-key                 # 키를 에이전트에 등록(한 번)
bash webapp/deploy/deploy.sh        # 기본값: mikwon@203.255.40.88:30, web:8700, LLM:localhost:8000
```

- 재배포(코드 갱신)도 같은 명령 한 줄. `webapp/data`(작업 결과)는 보존됩니다.
- 다른 서버로 바꾸려면 `HOST/SSH_PORT/SSH_KEY/APP_DIR/WEB_PORT/PAPERKO_LLM_URL` 환경변수로 override.

서버에서 하는 일(`remote-setup.sh`): venv 생성 → 의존성 설치 → `~/.config/systemd/user/paperko-web.service`
등록 → linger 활성화 → 시작 → `127.0.0.1:8700/api/info` 헬스체크.

## 2. 퀵 터널 (임시 URL, 즉시 확인용)

```bash
# (Spark에서)
~/.local/bin/cloudflared tunnel --url http://localhost:8700
# → https://<랜덤>.trycloudflare.com  (재시작마다 주소 변경)
```

## 3. 네임드 터널 (영구 URL) — 권장

**전제:** Cloudflare에 등록된 도메인(존)이 필요합니다. DuckDNS 등 외부 DNS는 불가.

```bash
# (Spark에서) 1) 브라우저 로그인 — URL이 출력되면 브라우저로 열어 도메인 선택
~/.local/bin/cloudflared tunnel login

# 2) 터널 생성 + DNS 라우팅 + 서비스 등록 (자동)
TUNNEL=paperko HOSTNAME=paperko.내도메인 WEB_PORT=8700 \
  bash ~/paperko/webapp/deploy/cloudflared-named.sh
# → https://paperko.내도메인  (영구, 재부팅에도 유지)
```

## 4. (나중) 인증 — Cloudflare Access

Zero Trust → Access → Application에서 `paperko.내도메인`에 이메일 OTP/구글 등
정책을 걸면, 터널 앞단에서 인증이 강제됩니다(앱 코드 변경 없음).

## 운영 명령 (Spark에서)

```bash
systemctl --user status paperko-web       # 웹앱 상태
systemctl --user status paperko-tunnel    # 터널 상태
journalctl --user -u paperko-web -n 50    # 웹앱 로그
systemctl --user restart paperko-web      # 재시작
```

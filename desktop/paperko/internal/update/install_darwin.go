//go:build darwin

package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrNotBundled 는 .app 꾸러미로 실행한 것이 아니라는 뜻이다(개발 중에 실행 파일을 바로 띄운 경우).
// 그때는 바꿔 끼울 꾸러미가 없으므로 받은 DMG 를 열어 사람이 설치하게 한다.
var ErrNotBundled = errors.New("앱 꾸러미(.app)로 실행한 것이 아니어서 자동으로 바꿔 끼우지 않습니다")

// ErrNotWritable 는 앱이 놓인 폴더에 쓸 수 없다는 뜻이다(관리자가 설치한 경우 등).
var ErrNotWritable = errors.New("앱이 설치된 폴더에 쓸 권한이 없어 자동으로 바꿔 끼우지 못합니다")

// bundlePath 는 실행 파일이 든 .app 꾸러미의 경로다. …/PaperKo.app/Contents/MacOS/paperko
func bundlePath(exe string) (string, bool) {
	macos := filepath.Dir(exe)
	contents := filepath.Dir(macos)
	app := filepath.Dir(contents)
	if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.HasSuffix(app, ".app") {
		return "", false
	}
	return app, filepath.IsAbs(app) && len(app) > len("/x.app")
}

// swapScript 는 앱이 끝나기를 기다렸다가 DMG 속 새 꾸러미로 바꿔 끼우고 다시 띄우는 셸 스크립트다.
// 바꿔 끼우다 실패하면 옛 꾸러미를 제자리로 돌린다.
const swapScript = `#!/bin/sh
# PaperKo 업데이트: 앱이 끝나기를 기다렸다가 새 판으로 바꿔 끼우고 다시 띄운다.
PID="$1"; DMG="$2"; APP="$3"
while kill -0 "$PID" 2>/dev/null; do sleep 0.3; done
MNT=$(mktemp -d "${TMPDIR:-/tmp}/paperko-update.XXXXXX") || exit 1
hdiutil attach -nobrowse -readonly -mountpoint "$MNT" "$DMG" >/dev/null || exit 1
NEW="$MNT/$(basename "$APP")"
if [ ! -d "$NEW" ]; then NEW=$(ls -d "$MNT"/*.app 2>/dev/null | head -n 1); fi
if [ -z "$NEW" ] || [ ! -d "$NEW" ]; then hdiutil detach "$MNT" -quiet; exit 1; fi
STAGE="$APP.update"
rm -rf "$STAGE"
if ! ditto "$NEW" "$STAGE"; then rm -rf "$STAGE"; hdiutil detach "$MNT" -quiet; exit 1; fi
hdiutil detach "$MNT" -quiet
OLD="$APP.old"
rm -rf "$OLD"
if mv "$APP" "$OLD" && mv "$STAGE" "$APP"; then
  rm -rf "$OLD"
else
  [ -d "$OLD" ] && [ ! -d "$APP" ] && mv "$OLD" "$APP"
  rm -rf "$STAGE"
fi
rm -f "$DMG"
open "$APP"
`

func install(dmg string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	app, ok := bundlePath(exe)
	if !ok {
		return ErrNotBundled
	}
	// 앱 옆에 쓸 수 있는지 먼저 본다 — 스크립트가 앱을 끝낸 뒤에 실패하면 되돌릴 길이 없다.
	probe, err := os.CreateTemp(filepath.Dir(app), ".paperko-update-*")
	if err != nil {
		return ErrNotWritable
	}
	probe.Close()
	os.Remove(probe.Name())

	script, err := os.CreateTemp("", "paperko-update-*.sh")
	if err != nil {
		return err
	}
	if _, err := script.WriteString(swapScript); err != nil {
		script.Close()
		return err
	}
	script.Close()
	cmd := exec.Command("/bin/sh", script.Name(), strconv.Itoa(os.Getpid()), dmg, app)
	// 앱이 끝나도 살아 있도록 새 세션으로 띄운다.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("업데이트를 시작하지 못했습니다: %w", err)
	}
	return cmd.Process.Release()
}

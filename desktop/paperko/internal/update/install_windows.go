//go:build windows

package update

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

// install 은 받은 NSIS 설치본을 띄운다. ShellExecute 로 띄워야 관리자 권한(UAC) 요청이 뜬다 —
// 그냥 실행하면 "권한 상승이 필요합니다" 로 실패한다. 설치본이 옛 판을 덮어쓰므로 앱은 곧 끝낸다.
func install(path string) error {
	verb, _ := syscall.UTF16PtrFromString("open")
	file, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	const swShowNormal = 1
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, swShowNormal)
	if r <= 32 {
		return fmt.Errorf("설치본을 띄우지 못했습니다 (코드 %d)", r)
	}
	return nil
}

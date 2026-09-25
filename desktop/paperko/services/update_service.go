package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"paperko/internal/update"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// UpdateProgressEvent is emitted while the installer downloads.
const UpdateProgressEvent = "update:progress"

// UpdateService finds, downloads and installs a newer release from GitHub
// (internal/update). It is bound to the frontend.
type UpdateService struct {
	B  *Backend
	mu sync.Mutex
	// latest is the last checked, offer-worthy release. Install downloads this.
	latest *update.Manifest
}

func (s *UpdateService) ServiceName() string { return "UpdateService" }

// UpdateInfo is the result of a check.
type UpdateInfo struct {
	// Checked reports whether we actually asked (an auto-check runs at most once a
	// day, so it is false when skipped).
	Checked   bool   `json:"checked"`
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Notes     string `json:"notes"`
	Published string `json:"published"`
	Size      int64  `json:"size"`
	// CanInstall reports whether this OS/CPU has an installer and the app can apply
	// it itself.
	CanInstall bool   `json:"canInstall"`
	Page       string `json:"page"`
}

// Check asks whether a newer release exists. When manual (from Settings) it ignores
// the interval and the skipped version and returns any error; an auto-check stays
// silent when offline — showing an error on every launch would be annoying.
func (s *UpdateService) Check(manual bool) (UpdateInfo, error) {
	prefs := s.B.Settings().Update
	info := UpdateInfo{Current: Version, Page: update.PageURL("")}
	if !update.DueForCheck(manual, prefs.NoAutoCheck, prefs.LastCheck, time.Now()) {
		return info, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := update.Fetch(ctx, update.PublicKey())
	if err != nil {
		if manual {
			return info, err
		}
		return info, nil
	}
	s.B.updateUpdatePrefs(func(p *UpdatePrefs) { p.LastCheck = time.Now().UTC().Format(time.RFC3339) })

	info.Checked = true
	info.Latest, info.Notes, info.Published = m.Version, m.Notes, m.Published
	if !update.ShouldOffer(Version, m.Version, prefs.SkipVersion, manual) {
		return info, nil
	}
	info.Available = true
	info.Page = update.PageURL(m.Version)
	if a, ok := m.AssetFor(runtime.GOOS, runtime.GOARCH); ok {
		info.Size = a.Size
		info.CanInstall = runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	}
	s.mu.Lock()
	s.latest = m
	s.mu.Unlock()
	return info, nil
}

// SetAutoCheck turns the automatic startup check on or off.
func (s *UpdateService) SetAutoCheck(on bool) error {
	s.B.updateUpdatePrefs(func(p *UpdatePrefs) { p.NoAutoCheck = !on })
	return nil
}

// Skip stops auto-checks from offering this version again.
func (s *UpdateService) Skip(v string) error {
	s.B.updateUpdatePrefs(func(p *UpdatePrefs) { p.SkipVersion = strings.TrimPrefix(v, "v") })
	return nil
}

// OpenPage opens the release page in the browser. Only a release-repo URL is allowed.
func (s *UpdateService) OpenPage(url string) error {
	if !strings.HasPrefix(url, "https://github.com/"+update.Repo+"/") {
		return errors.New("릴리스 페이지 주소가 아닙니다")
	}
	return application.Get().Browser.OpenURL(url)
}

// Install downloads the last checked release and installs it. On success it quits
// the app — macOS swaps in the new bundle and relaunches, Windows hands off to the
// installer. When it cannot self-apply (dev run, unwritable folder) it opens the
// downloaded installer for the user and reports that as an error.
func (s *UpdateService) Install() error {
	s.mu.Lock()
	m := s.latest
	s.mu.Unlock()
	if m == nil {
		return errors.New("먼저 새 판을 확인하십시오")
	}
	a, ok := m.AssetFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return fmt.Errorf("이 컴퓨터(%s/%s)용 설치본이 릴리스에 없습니다", runtime.GOOS, runtime.GOARCH)
	}
	dir, err := os.MkdirTemp("", "paperko-update-")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	app := application.Get()
	last := time.Time{}
	path, err := update.Download(ctx, update.AssetURL(m.Version, a), a, dir, func(done, total int64) {
		// throttle: at most every 0.1s, and once at the end.
		if now := time.Now(); now.Sub(last) > 100*time.Millisecond || done == total {
			last = now
			app.Event.Emit(UpdateProgressEvent, map[string]int64{"done": done, "total": total})
		}
	})
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	if err := update.Install(path); err != nil {
		// cannot self-apply → open the downloaded installer so the user can run it.
		if runtime.GOOS == "darwin" {
			_ = exec.Command("open", path).Start()
		}
		return fmt.Errorf("%w — 받은 설치본을 열었습니다. 직접 설치하십시오", err)
	}
	// give the response a moment to reach the UI, then quit.
	go func() {
		time.Sleep(400 * time.Millisecond)
		app.Quit()
	}()
	return nil
}

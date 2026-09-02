package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paperko/internal/engine"
)

// LocalModelInfo is the snapshot the UI shows for the offline (local) engine.
type LocalModelInfo struct {
	Mode         string `json:"mode"`          // remote | local
	BinAvailable bool   `json:"bin_available"` // llama-server binary bundled?
	ModelPresent bool   `json:"model_present"` // GGUF downloaded/selected?
	ModelPath    string `json:"model_path"`
	ModelName    string `json:"model_name"`
	ModelURL     string `json:"model_url"`
	SizeMB       int64  `json:"size_mb"`
	Running      bool   `json:"running"`
	BaseURL      string `json:"base_url"`
}

// LocalModelInfo reports the current offline-engine state to the UI.
func (s *SettingsService) LocalModelInfo() LocalModelInfo {
	b := s.B
	b.mu.Lock()
	info := LocalModelInfo{
		Mode:      b.settings.EngineMode,
		ModelName: b.settings.LocalModelName,
		ModelURL:  b.settings.LocalModelURL,
	}
	b.mu.Unlock()
	info.ModelPath = b.localModelPath()
	if fi, err := os.Stat(info.ModelPath); err == nil && !fi.IsDir() {
		info.ModelPresent = true
		info.SizeMB = fi.Size() / (1024 * 1024)
	}
	if b.llama != nil {
		info.BinAvailable = b.llama.Available()
		info.Running = b.llama.Running()
		info.BaseURL = b.llama.BaseURL()
	}
	return info
}

// SetEngineMode switches between "remote" (vLLM/LM Studio) and "local" (bundled
// llama.cpp). In local mode it starts the server if the model is present.
func (s *SettingsService) SetEngineMode(mode string) error {
	if mode != "local" && mode != "remote" {
		return fmt.Errorf("invalid mode: %s", mode)
	}
	b := s.B
	b.mu.Lock()
	b.settings.EngineMode = mode
	_ = b.saveSettingsFile()
	b.mu.Unlock()

	if mode == "local" {
		if _, err := os.Stat(b.localModelPath()); err == nil {
			return s.StartLocalEngine()
		}
		return nil // model not present yet; UI will prompt to download/select
	}
	// remote: stop the local server and re-point the engine at the remote config
	if b.llama != nil {
		b.llama.Stop()
	}
	return b.reconfigureEngine()
}

// StartLocalEngine boots the bundled llama.cpp server against the local GGUF and
// re-points the translate engine at it.
func (s *SettingsService) StartLocalEngine() error {
	b := s.B
	if b.llama == nil || !b.llama.Available() {
		return fmt.Errorf("로컬 추론 런타임(llama-server)이 번들되어 있지 않습니다")
	}
	model := b.localModelPath()
	if fi, err := os.Stat(model); err != nil || fi.IsDir() {
		return fmt.Errorf("로컬 모델 파일이 없습니다. 먼저 다운로드하거나 파일을 선택하세요")
	}
	b.emit("localllm:status", map[string]any{"state": "starting"})
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	if err := b.llama.Start(ctx, model, engine.LlamaOptions{CtxSize: 8192, Parallel: b.settings.MaxConcurrency}); err != nil {
		b.emit("localllm:status", map[string]any{"state": "error", "error": err.Error()})
		return err
	}
	// Persist the exact model path we just used + switch to local, so the run button
	// is enabled (and offline mode auto-starts) on the next launch.
	b.mu.Lock()
	b.settings.EngineMode = "local"
	b.settings.LocalModelPath = model
	_ = b.saveSettingsFile()
	b.mu.Unlock()
	if err := b.reconfigureEngine(); err != nil {
		b.emit("localllm:status", map[string]any{"state": "error", "error": err.Error()})
		return err
	}
	b.emit("localllm:status", map[string]any{"state": "ready", "base_url": b.llama.BaseURL(), "model": b.llama.ModelName()})
	return nil
}

// StopLocalEngine shuts the local server down (frees memory).
func (s *SettingsService) StopLocalEngine() error {
	if s.B.llama != nil {
		s.B.llama.Stop()
	}
	s.B.emit("localllm:status", map[string]any{"state": "stopped"})
	return nil
}

// PickLocalModel lets the user choose a GGUF they already have (e.g. from LM
// Studio), avoiding gated-download hassle. Returns the chosen path.
func (s *SettingsService) PickLocalModel() (string, error) {
	d := application.Get().Dialog.OpenFile()
	d.SetTitle("로컬 모델(GGUF) 선택")
	d.AddFilter("GGUF 모델", "*.gguf")
	d.CanChooseFiles(true)
	path, err := d.PromptForSingleSelection()
	if err != nil || path == "" {
		return path, err
	}
	b := s.B
	b.mu.Lock()
	b.settings.LocalModelPath = path
	b.settings.LocalModelSHA256 = "" // picked file: integrity hash not applicable
	_ = b.saveSettingsFile()
	b.mu.Unlock()
	return path, nil
}

// DownloadLocalModel downloads the GGUF from url (or the saved LocalModelURL) into
// paperKo/models/, with resumable HTTP Range requests, progress events, and SHA-256
// integrity. The resolved path is remembered (LocalModelPath) so the "run" button is
// enabled automatically on the next launch when the file is present.
//
//	UI → DownloadLocalModel → [Range/Resume/Progress/SHA-256] → models/<file>.gguf
func (s *SettingsService) DownloadLocalModel(url string) error {
	b := s.B
	b.mu.Lock()
	if url == "" {
		url = b.settings.LocalModelURL
	}
	token := b.HFToken()
	expected := strings.TrimSpace(b.settings.LocalModelSHA256)
	b.mu.Unlock()
	if url == "" {
		return fmt.Errorf("모델 다운로드 URL이 설정되지 않았습니다")
	}

	// dest = models/<basename of URL>
	name := path.Base(url)
	if name == "" || name == "." || name == "/" {
		name = "model.gguf"
	}
	dest := filepath.Join(b.DataDir, "models", name)
	tmp := dest + ".part"
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	// already downloaded? nothing to do.
	if fi, err := os.Stat(dest); err == nil && !fi.IsDir() {
		b.rememberModel(dest, url, expected)
		b.emit("localllm:download", map[string]any{"received": fi.Size(), "total": fi.Size(), "done": true})
		return nil
	}

	// resume: how many bytes are already in the .part file?
	var have int64
	if fi, err := os.Stat(tmp); err == nil {
		have = fi.Size()
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	hasher := sha256.New()
	var f *os.File
	switch resp.StatusCode {
	case http.StatusPartialContent: // 206 — server honored the range → resume
		if f, err = os.OpenFile(tmp, os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
			return err
		}
		if ex, e := os.Open(tmp); e == nil { // seed the hasher with the existing bytes
			_, _ = io.Copy(hasher, ex)
			ex.Close()
		}
	case http.StatusOK: // 200 — server ignored the range (or fresh) → start over
		have = 0
		if f, err = os.Create(tmp); err != nil {
			return err
		}
	case http.StatusRequestedRangeNotSatisfiable: // 416 — .part is already complete
		if e := finalizeDownload(tmp, dest, expected); e != nil {
			return e
		}
		b.rememberModel(dest, url, "")
		if fi, e := os.Stat(dest); e == nil {
			b.emit("localllm:download", map[string]any{"received": fi.Size(), "total": fi.Size(), "done": true})
		}
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("다운로드 실패: HTTP %d — 게이트 모델입니다. 설정의 Hugging Face 토큰을 입력하거나, 받아둔 GGUF를 [파일 선택]으로 지정하세요", resp.StatusCode)
	default:
		return fmt.Errorf("다운로드 실패: HTTP %d", resp.StatusCode)
	}

	total := have + resp.ContentLength // ContentLength is the REMAINING bytes on 206
	received := have
	buf := make([]byte, 1<<20)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			hasher.Write(buf[:n])
			received += int64(n)
			if time.Since(last) > 300*time.Millisecond {
				b.emit("localllm:download", map[string]any{"received": received, "total": total})
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr // keep .part for a later resume
		}
	}
	if err := f.Close(); err != nil {
		return err
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	if expected != "" && !strings.EqualFold(expected, sum) {
		os.Remove(tmp)
		return fmt.Errorf("SHA-256 검증 실패 (기대=%s, 실제=%s)", expected, sum)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	b.rememberModel(dest, url, sum)
	b.emit("localllm:download", map[string]any{"received": received, "total": total, "done": true, "sha256": sum})
	return nil
}

// rememberModel persists the resolved model path + url (+ sha) so the app re-enables
// the local engine automatically next launch when the file is present.
func (b *Backend) rememberModel(path, url, sha string) {
	b.mu.Lock()
	b.settings.LocalModelPath = path
	if url != "" {
		b.settings.LocalModelURL = url
	}
	if sha != "" {
		b.settings.LocalModelSHA256 = sha
	}
	_ = b.saveSettingsFile()
	b.mu.Unlock()
}

// HFToken returns the Hugging Face token (setting or HF_TOKEN env) for gated models.
func (b *Backend) HFToken() string {
	if b.settings.HFToken != "" {
		return b.settings.HFToken
	}
	return os.Getenv("HF_TOKEN")
}

// finalizeDownload verifies a completed .part (optional SHA) and renames it to dest.
func finalizeDownload(tmp, dest, expected string) error {
	if expected != "" {
		f, err := os.Open(tmp)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, _ = io.Copy(h, f)
		f.Close()
		if sum := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(expected, sum) {
			os.Remove(tmp)
			return fmt.Errorf("SHA-256 검증 실패 (기대=%s, 실제=%s)", expected, sum)
		}
	}
	return os.Rename(tmp, dest)
}

// BootLocalEngine is called at startup when EngineMode==local: start the local
// server (if the model is present) and point the engine at it. Best-effort.
func (b *Backend) BootLocalEngine() {
	if b.settings.EngineMode != "local" || b.llama == nil || !b.llama.Available() {
		return
	}
	model := b.localModelPath()
	if fi, err := os.Stat(model); err != nil || fi.IsDir() {
		return // no model downloaded/selected yet
	}
	b.emit("localllm:status", map[string]any{"state": "starting"})
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	if err := b.llama.Start(ctx, model, engine.LlamaOptions{CtxSize: 8192, Parallel: b.settings.MaxConcurrency}); err != nil {
		b.emit("localllm:status", map[string]any{"state": "error", "error": err.Error()})
		return
	}
	if err := b.reconfigureEngine(); err != nil {
		b.emit("localllm:status", map[string]any{"state": "error", "error": err.Error()})
		return
	}
	b.emit("localllm:status", map[string]any{"state": "ready", "base_url": b.llama.BaseURL(), "model": b.llama.ModelName()})
}

// StopLocalServer terminates the local server (called on app shutdown).
func (b *Backend) StopLocalServer() {
	if b.llama != nil {
		b.llama.Stop()
	}
}

// reconfigureEngine pushes the current (local or remote) LLM config to the engine.
func (b *Backend) reconfigureEngine() error {
	if b.Sup == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return b.Sup.Configure(ctx, b.llmConfig())
}

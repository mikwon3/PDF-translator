package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// LlamaOptions tunes the local llama.cpp server.
type LlamaOptions struct {
	CtxSize   int // context window (tokens); default 4096
	GPULayers int // layers to offload to GPU; default 999 (all); 0 = CPU-only
	Parallel  int // concurrent slots; default 4
}

// LlamaServer manages a local llama.cpp `llama-server` process that exposes an
// OpenAI-compatible API on 127.0.0.1, letting the translate engine run fully
// offline against a local GGUF model (no external vLLM/LM Studio needed).
type LlamaServer struct {
	bin string

	mu    sync.Mutex
	cmd   *exec.Cmd
	port  int
	model string // served model id (as reported by /v1/models)
}

func NewLlamaServer(binPath string) *LlamaServer { return &LlamaServer{bin: binPath} }

// BinPath returns the configured llama-server binary path ("" if unset).
func (l *LlamaServer) BinPath() string { return l.bin }

// Available reports whether a usable llama-server binary is present.
func (l *LlamaServer) Available() bool {
	if l.bin == "" {
		return false
	}
	fi, err := os.Stat(l.bin)
	return err == nil && !fi.IsDir()
}

func (l *LlamaServer) Running() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cmd != nil && l.cmd.Process != nil
}

// BaseURL is the OpenAI-compatible endpoint, e.g. http://127.0.0.1:8577/v1.
func (l *LlamaServer) BaseURL() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.port == 0 {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(l.port) + "/v1"
}

// ModelName is the served model id (usable as the OpenAI `model` field).
func (l *LlamaServer) ModelName() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.model
}

// Start launches llama-server for the given GGUF file and blocks until it answers
// /health (model loaded) or ctx expires. If already running, returns nil.
func (l *LlamaServer) Start(ctx context.Context, modelPath string, opts LlamaOptions) error {
	l.mu.Lock()
	if l.cmd != nil && l.cmd.Process != nil {
		l.mu.Unlock()
		return nil
	}
	l.mu.Unlock()

	if !l.Available() {
		return fmt.Errorf("llama-server binary not found (%q)", l.bin)
	}
	if fi, err := os.Stat(modelPath); err != nil || fi.IsDir() {
		return fmt.Errorf("model file not found: %s", modelPath)
	}

	port, err := freePort()
	if err != nil {
		return err
	}

	ctxSize := opts.CtxSize
	if ctxSize <= 0 {
		ctxSize = 4096
	}
	ngl := 999 // offload all layers by default; ignored by CPU-only builds
	if opts.GPULayers > 0 {
		ngl = opts.GPULayers
	}
	par := opts.Parallel
	if par <= 0 {
		par = 4
	}

	args := []string{
		"-m", modelPath,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"-c", strconv.Itoa(ctxSize),
		"-ngl", strconv.Itoa(ngl),
		"-np", strconv.Itoa(par),
	}
	cmd := exec.Command(l.bin, args...)
	cmd.Env = os.Environ()
	hideChildConsole(cmd) // no console window on Windows
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start llama-server: %w", err)
	}

	l.mu.Lock()
	l.cmd = cmd
	l.port = port
	l.mu.Unlock()

	if err := l.waitHealth(ctx, port); err != nil {
		l.Stop()
		return err
	}
	// Adopt the model id the server actually reports so the engine's health check
	// (configured model ∈ /v1/models) passes.
	if id := fetchModelID(port); id != "" {
		l.mu.Lock()
		l.model = id
		l.mu.Unlock()
	}
	return nil
}

// Stop terminates the server process.
func (l *LlamaServer) Stop() {
	l.mu.Lock()
	cmd := l.cmd
	l.cmd = nil
	l.port = 0
	l.model = ""
	l.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}

func (l *LlamaServer) waitHealth(ctx context.Context, port int) error {
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/health"
	deadline := time.NewTimer(0)
	defer deadline.Stop()
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("llama-server did not become ready: %w", ctx.Err())
		case <-deadline.C:
		}
		resp, err := client.Get(url)
		if err == nil {
			code := resp.StatusCode
			resp.Body.Close()
			if code == http.StatusOK {
				return nil
			}
		}
		deadline.Reset(500 * time.Millisecond)
	}
}

func fetchModelID(port int) string {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/v1/models")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) == nil && len(out.Data) > 0 {
		return out.Data[0].ID
	}
	return ""
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

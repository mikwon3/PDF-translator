package engine

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Config describes how to launch the Python sidecar.
type Config struct {
	Python   string   // interpreter path (dev) or bundled binary
	Args     []string // e.g. ["-m", "translate_engine"]
	Env      []string // extra environment (PYTHONPATH=…)
	DataDir  string
	LLM      LLMConfig
	LogLevel string
}

// Supervisor owns the sidecar process and a JSON-RPC client to it, restarting it
// on unexpected exit (02-architecture.md §2.1).
type Supervisor struct {
	cfg Config
	log *slog.Logger

	mu       sync.Mutex
	cmd      *exec.Cmd
	client   *rpcClient
	starting bool
	stopping bool
	restarts int

	OnNotify func(Notification)               // progress / partial / tu_failed / log
	OnStatus func(state string)               // starting|ready|crashed|restarting
	OnLog    func(level, source, msg string)  // stderr lines
}

func NewSupervisor(cfg Config, log *slog.Logger) *Supervisor {
	return &Supervisor{cfg: cfg, log: log}
}

// Start launches the sidecar and performs the initialize handshake.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.starting || s.client != nil {
		s.mu.Unlock()
		return nil
	}
	s.starting = true
	s.stopping = false
	s.mu.Unlock()

	s.emitStatus("starting")

	cmd := exec.Command(s.cfg.Python, s.cfg.Args...)
	cmd.Env = append(os.Environ(), s.cfg.Env...)
	hideChildConsole(cmd) // no console window for the sidecar on Windows

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		s.emitStatus("crashed")
		return err
	}

	go s.pumpStderr(stderr)

	client := newRPCClient(stdin, stdout, func(n Notification) {
		if s.OnNotify != nil {
			s.OnNotify(n)
		}
	})

	s.mu.Lock()
	s.cmd = cmd
	s.client = client
	s.starting = false
	s.mu.Unlock()

	go s.watch(cmd)

	// initialize handshake
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var res map[string]any
	if err := client.call(initCtx, "initialize", map[string]any{
		"protocol_version": "1.0",
		"data_dir":         s.cfg.DataDir,
		"llm":              s.cfg.LLM,
		"log_level":        s.cfg.LogLevel,
	}, &res); err != nil {
		return err
	}
	s.emitStatus("ready")
	return nil
}

func (s *Supervisor) watch(cmd *exec.Cmd) {
	_ = cmd.Wait()
	s.mu.Lock()
	stopping := s.stopping
	s.client = nil
	s.cmd = nil
	s.mu.Unlock()
	if stopping {
		return
	}
	// unexpected exit → crash + backoff restart (max 3 / 10 min)
	s.emitStatus("crashed")
	if s.restarts >= 3 {
		s.log.Error("engine exceeded restart budget")
		return
	}
	s.restarts++
	delay := time.Duration(s.restarts) * 2 * time.Second
	s.log.Warn("engine crashed; restarting", "in", delay, "attempt", s.restarts)
	s.emitStatus("restarting")
	time.AfterFunc(delay, func() {
		if err := s.Start(context.Background()); err != nil {
			s.log.Error("engine restart failed", "err", err)
		} else {
			s.restarts = 0
		}
	})
}

func (s *Supervisor) pumpStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if s.OnLog != nil {
			s.OnLog("info", "engine", line)
		}
	}
}

func (s *Supervisor) emitStatus(state string) {
	if s.OnStatus != nil {
		s.OnStatus(state)
	}
}

func (s *Supervisor) rpc() (*rpcClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, &EngineError{Code: "engine_down", Message: "engine not running"}
	}
	return s.client, nil
}

// Stop asks the sidecar to shut down, then kills it if it lingers.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	s.stopping = true
	client := s.client
	cmd := s.cmd
	s.mu.Unlock()
	if client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = client.call(ctx, "shutdown", map[string]any{}, nil)
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
		}
	}
}

// ---- typed RPC methods -------------------------------------------------- //

func (s *Supervisor) OpenDocument(ctx context.Context, path, password string) (*DocMeta, error) {
	c, err := s.rpc()
	if err != nil {
		return nil, err
	}
	var pw any
	if password != "" {
		pw = password
	}
	var out DocMeta
	err = c.call(ctx, "document.open", map[string]any{"path": path, "password": pw}, &out)
	return &out, err
}

func (s *Supervisor) Analyze(ctx context.Context, docID, pdfPath, jobDir, jobID string, pages []int, ocr, ocrLang string) (*AnalyzeResult, error) {
	c, err := s.rpc()
	if err != nil {
		return nil, err
	}
	var pagesParam any
	if pages != nil {
		pagesParam = pages
	}
	if ocrLang == "" {
		ocrLang = "eng"
	}
	var out AnalyzeResult
	err = c.call(ctx, "document.analyze", map[string]any{
		"doc_id": docID, "pdf_path": pdfPath, "job_dir": jobDir,
		"job_id": jobID, "pages": pagesParam, "ocr": ocr, "ocr_lang": ocrLang,
	}, &out)
	return &out, err
}

func (s *Supervisor) Translate(ctx context.Context, jobID, irPath, glossaryPath string, options map[string]any, resume bool) (*TranslateResult, error) {
	c, err := s.rpc()
	if err != nil {
		return nil, err
	}
	var out TranslateResult
	err = c.call(ctx, "document.translate", map[string]any{
		"job_id": jobID, "ir_path": irPath, "glossary_path": glossaryPath,
		"options": options, "resume": resume,
	}, &out)
	return &out, err
}

func (s *Supervisor) Render(ctx context.Context, jobID, pdfPath, irPath, translationPath, outPath string, options map[string]any) (*RenderResult, error) {
	c, err := s.rpc()
	if err != nil {
		return nil, err
	}
	var out RenderResult
	err = c.call(ctx, "document.render", map[string]any{
		"job_id": jobID, "pdf_path": pdfPath, "ir_path": irPath,
		"translation_path": translationPath, "out_path": outPath, "options": options,
	}, &out)
	return &out, err
}

// Export writes the translated document as a flowing .docx or .hwpx file.
// pdfPath (the job's source PDF) is used to reconstruct tables.
func (s *Supervisor) Export(ctx context.Context, irPath, translationPath, pdfPath, outPath, format string) (string, error) {
	c, err := s.rpc()
	if err != nil {
		return "", err
	}
	var out struct {
		OutPath string `json:"out_path"`
		Format  string `json:"format"`
	}
	err = c.call(ctx, "document.export", map[string]any{
		"ir_path": irPath, "translation_path": translationPath, "pdf_path": pdfPath,
		"out_path": outPath, "format": format,
	}, &out)
	return out.OutPath, err
}

func (s *Supervisor) PagePreview(ctx context.Context, pdfPath string, page int, zoom float64) (*PreviewResult, error) {
	c, err := s.rpc()
	if err != nil {
		return nil, err
	}
	var out PreviewResult
	err = c.call(ctx, "page.preview", map[string]any{"pdf_path": pdfPath, "page": page, "zoom": zoom}, &out)
	return &out, err
}

func (s *Supervisor) Health(ctx context.Context, llm *LLMConfig) (*HealthInfo, error) {
	c, err := s.rpc()
	if err != nil {
		return nil, err
	}
	params := map[string]any{}
	if llm != nil {
		params["llm"] = llm
	}
	var out HealthInfo
	err = c.call(ctx, "llm.health", params, &out)
	return &out, err
}

// Configure pushes an updated LLM config to the running sidecar so the next job
// uses it (no restart needed).
func (s *Supervisor) Configure(ctx context.Context, llm LLMConfig) error {
	c, err := s.rpc()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg.LLM = llm // so a later restart also uses the new config
	s.mu.Unlock()
	return c.call(ctx, "configure", map[string]any{"llm": llm}, nil)
}

// RetranslateUnit re-runs a single TU (UC-03) and updates translation.json.
func (s *Supervisor) RetranslateUnit(ctx context.Context, jobID, irPath, translationPath, tuID, glossaryPath string, options map[string]any) (map[string]any, error) {
	c, err := s.rpc()
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = c.call(ctx, "tu.retranslate", map[string]any{
		"job_id": jobID, "ir_path": irPath, "translation_path": translationPath, "tu_id": tuID,
		"glossary_path": glossaryPath, "options": options,
	}, &out)
	return out, err
}

func (s *Supervisor) CancelJob(jobID string) error {
	c, err := s.rpc()
	if err != nil {
		return err
	}
	return c.notify("job.cancel", map[string]any{"job_id": jobID})
}

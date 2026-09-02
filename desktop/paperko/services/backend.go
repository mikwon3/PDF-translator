// Package services holds the Wails-bound service layer.  It orchestrates jobs,
// owns the engine supervisor and settings, and translates engine notifications
// into frontend events (04-api-spec.md §1).
package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paperko/internal/engine"
)

// Job is the record of a translation job. It is persisted to <JobDir>/job.json so
// an interrupted translation can be resumed after the app restarts.
type Job struct {
	ID              string `json:"id"`
	DocID           string `json:"doc_id"`
	PDFPath         string `json:"pdf_path"`
	Title           string `json:"title"` // PDF basename, for the resume list
	JobDir          string `json:"job_dir"`
	IRPath          string `json:"ir_path"`
	TranslationPath string `json:"translation_path"`
	OutPath         string `json:"out_path"`
	State           string `json:"state"`
	Error           string `json:"error,omitempty"`
	Style           string `json:"style,omitempty"`         // remembered for retranslate/resume
	Mode            string `json:"mode,omitempty"`          // render mode, remembered for resume
	GlossaryPath    string `json:"glossary_path,omitempty"` // frozen glossary snapshot for retranslate/resume
	DoneTUs         int    `json:"done_tus,omitempty"`      // translation progress (checkpointed count)
	TotalTUs        int    `json:"total_tus,omitempty"`
	DonePages       int    `json:"done_pages,omitempty"`    // pages fully translated (for the resume list)
	TotalPages      int    `json:"total_pages,omitempty"`   // document page count
	DonePageList    []int  `json:"done_page_list,omitempty"` // 0-based fully-translated pages (to restore batches)
	UpdatedAt       string `json:"updated_at,omitempty"`
}

// Backend is shared by all bound services.
type Backend struct {
	DataDir  string
	Sup      *engine.Supervisor
	Glossary *GlossaryStore
	llama    *engine.LlamaServer // local (offline) inference server; may be nil
	log      *slog.Logger

	mu       sync.Mutex
	settings Settings
	jobs     map[string]*Job
	jobSeq   int
}

// AttachLlama wires the local llama.cpp server manager (offline mode).
func (b *Backend) AttachLlama(l *engine.LlamaServer) { b.llama = l }

func NewBackend(dataDir string, log *slog.Logger) *Backend {
	b := &Backend{DataDir: dataDir, log: log, jobs: map[string]*Job{}}
	b.settings = loadSettings(filepath.Join(dataDir, "settings.json"))
	b.Glossary = NewGlossaryStore(dataDir)
	b.loadJobs() // restore persisted jobs so interrupted translations can be resumed
	return b
}

// AttachEngine wires supervisor callbacks to frontend events.
func (b *Backend) AttachEngine(sup *engine.Supervisor) {
	b.Sup = sup
	sup.OnStatus = func(state string) { b.emit("engine:status", map[string]any{"state": state}) }
	sup.OnLog = func(level, source, msg string) {
		b.emit("log:line", map[string]any{"level": level, "source": source, "msg": msg,
			"ts": time.Now().Format(time.RFC3339)})
	}
	sup.OnNotify = b.onEngineNotify
}

// onEngineNotify maps Python→Go notifications to frontend events.
func (b *Backend) onEngineNotify(n engine.Notification) {
	switch n.Method {
	case "progress":
		var p struct {
			JobID string `json:"job_id"`
			Stage string `json:"stage"`
			Done  int    `json:"done"`
			Total int    `json:"total"`
			Page  *int   `json:"page"`
		}
		_ = json.Unmarshal(n.Params, &p)
		pct := 0
		if p.Total > 0 {
			pct = p.Done * 100 / p.Total
		}
		// Persist translate progress so a resume list can show how far it got.
		// Throttle disk writes: every 25 TUs and at the end.
		if p.Stage == "translate" {
			if j := b.job(p.JobID); j != nil {
				b.mu.Lock()
				j.DoneTUs, j.TotalTUs = p.Done, p.Total
				if p.Done%25 == 0 || p.Done == p.Total {
					b.saveJobLocked(j)
				}
				b.mu.Unlock()
			}
		}
		b.emit("job:progress", map[string]any{
			"job_id": p.JobID, "stage": p.Stage, "done": p.Done, "total": p.Total, "pct": pct,
		})
	case "partial":
		var p struct {
			JobID string `json:"job_id"`
			Page  int    `json:"page"`
		}
		_ = json.Unmarshal(n.Params, &p)
		b.emit("job:page_done", map[string]any{"job_id": p.JobID, "page": p.Page})
	case "tu_failed":
		b.emitRaw("job:tu_failed", n.Params)
	case "log":
		b.emitRaw("log:line", n.Params)
	}
}

func (b *Backend) emit(name string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}

func (b *Backend) emitRaw(name string, data json.RawMessage) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}

func (b *Backend) newJob(docID, pdfPath string) *Job {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.jobSeq++
	id := timeID(b.jobSeq)
	dir := filepath.Join(b.DataDir, "jobs", id)
	_ = os.MkdirAll(dir, 0o755)
	j := &Job{ID: id, DocID: docID, PDFPath: pdfPath, Title: filepath.Base(pdfPath),
		JobDir: dir, State: "PENDING"}
	b.jobs[id] = j
	b.saveJobLocked(j)
	return j
}

func (b *Backend) job(id string) *Job {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.jobs[id]
}

func (b *Backend) setState(j *Job, state string, errMsg string) {
	b.mu.Lock()
	j.State = state
	j.Error = errMsg
	b.saveJobLocked(j)
	b.mu.Unlock()
	b.emit("job:state", map[string]any{"job_id": j.ID, "state": state, "error": errMsg})
}

func bctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Minute)
}

func timeID(seq int) string {
	// stable, sortable-ish id without exposing wall clock precision issues
	return "j_" + time.Now().Format("20060102_150405") + "_" + itoa(seq)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

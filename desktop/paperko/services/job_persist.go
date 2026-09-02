package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// job.json lives inside each JobDir next to the IR and the translation checkpoint,
// so a job is fully self-describing on disk and survives an app restart.
const jobFile = "job.json"

// saveJobLocked writes the job record to <JobDir>/job.json. Caller holds b.mu
// (state mutations already run under the lock).
func (b *Backend) saveJobLocked(j *Job) {
	if j == nil || j.JobDir == "" {
		return
	}
	j.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return
	}
	tmp := filepath.Join(j.JobDir, jobFile+".tmp")
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, filepath.Join(j.JobDir, jobFile))
	}
}

// loadJobs restores every persisted job from DataDir/jobs/*/job.json. A job left
// in a live state (the app was killed mid-run) is marked INTERRUPTED so the UI can
// offer to resume it. Called once from NewBackend before any service is bound.
func (b *Backend) loadJobs() {
	root := filepath.Join(b.DataDir, "jobs")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, e.Name(), jobFile))
		if err != nil {
			continue
		}
		var j Job
		if json.Unmarshal(data, &j) != nil || j.ID == "" {
			continue
		}
		// A translation that was running when the app closed is resumable.
		switch j.State {
		case "TRANSLATING", "RENDERING", "ANALYZING", "PENDING":
			j.State = "INTERRUPTED"
		}
		jc := j
		b.jobs[jc.ID] = &jc
	}
}

// resumableJobs returns jobs that can be continued — interrupted, cancelled or
// failed — and that still have both the IR and a translation checkpoint on disk.
// Newest first. Used by JobService.ListResumable.
func (b *Backend) resumableJobs() []*Job {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*Job
	for _, j := range b.jobs {
		switch j.State {
		case "INTERRUPTED", "CANCELLED", "FAILED", "PARTIAL":
		default:
			continue
		}
		if !fileExists(j.IRPath) || !fileExists(filepath.Join(j.JobDir, "translation.json")) {
			continue
		}
		jc := *j
		out = append(out, &jc)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].UpdatedAt > out[k].UpdatedAt })
	return out
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

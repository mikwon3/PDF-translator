package services

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paperko/internal/engine"
)

// DocumentService: open, analyze and preview PDFs (04 §1.1 DocumentService).
type DocumentService struct{ B *Backend }

// PickPDF shows a native open dialog and returns the chosen PDF path ("" if cancelled).
func (s *DocumentService) PickPDF() (string, error) {
	d := application.Get().Dialog.OpenFile()
	d.SetTitle("번역할 논문 PDF 선택")
	d.AddFilter("PDF 문서", "*.pdf")
	d.CanChooseFiles(true)
	return d.PromptForSingleSelection()
}

// PickSavePath shows a native save dialog for the output file. The filter is
// chosen from the default filename's extension (.pdf / .docx / .hwpx).
func (s *DocumentService) PickSavePath(defaultName string) (string, error) {
	d := application.Get().Dialog.SaveFile()
	if defaultName == "" {
		defaultName = "translated.pdf"
	}
	d.SetFilename(defaultName)
	switch strings.ToLower(filepath.Ext(defaultName)) {
	case ".hwpx":
		d.AddFilter("한글 문서", "*.hwpx")
	case ".docx":
		d.AddFilter("Word 문서", "*.docx")
	default:
		d.AddFilter("PDF 문서", "*.pdf")
	}
	return d.PromptForSingleSelection()
}

// OpenDocument returns lightweight metadata (page count, text-layer, encryption).
func (s *DocumentService) OpenDocument(path, password string) (*engine.DocMeta, error) {
	ctx, cancel := bctx()
	defer cancel()
	return s.B.Sup.OpenDocument(ctx, path, password)
}

// AnalyzeDocument starts layout analysis for a PDF and returns a new job id.
// Progress arrives via `job:progress`; completion via `job:state` (ANALYZED).
func (s *DocumentService) AnalyzeDocument(path, password, pagesSpec string) (string, error) {
	meta, err := s.OpenDocument(path, password)
	if err != nil {
		return "", err
	}
	job := s.B.newJob(meta.DocID, path)
	s.B.mu.Lock()
	job.TotalPages = meta.PageCount
	s.B.mu.Unlock()
	pages := parsePages(pagesSpec)

	go func() {
		s.B.setState(job, "ANALYZING", "")
		ctx, cancel := bctx()
		defer cancel()
		res, err := s.B.Sup.Analyze(ctx, meta.DocID, path, job.JobDir, job.ID, pages, "auto", tesseractLang(s.B.Settings().SourceLang))
		if err != nil {
			s.B.setState(job, "FAILED", err.Error())
			return
		}
		s.B.mu.Lock()
		job.IRPath = res.IRPath
		s.B.mu.Unlock()
		s.B.emit("job:analyzed", map[string]any{
			"job_id": job.ID, "ir_path": res.IRPath, "stats": res.Stats, "warnings": res.Warnings,
		})
		s.B.setState(job, "ANALYZED", "")
	}()

	return job.ID, nil
}

// GetPagePreview rasterizes a page of the given PDF to a base64 PNG data URL.
func (s *DocumentService) GetPagePreview(pdfPath string, page int, zoom float64) (string, error) {
	if zoom <= 0 {
		zoom = 1.5
	}
	ctx, cancel := bctx()
	defer cancel()
	res, err := s.B.Sup.PagePreview(ctx, pdfPath, page, zoom)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + res.PNGBase64, nil
}

// parsePages converts "1-10,15" (1-based) to a sorted 0-based slice; "" → nil.
func parsePages(spec string) []int {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	seen := map[int]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			a, e1 := strconv.Atoi(strings.TrimSpace(lo))
			b, e2 := strconv.Atoi(strings.TrimSpace(hi))
			if e1 == nil && e2 == nil {
				for i := a; i <= b; i++ {
					if i >= 1 {
						seen[i-1] = true
					}
				}
			}
		} else if n, err := strconv.Atoi(part); err == nil && n >= 1 {
			seen[n-1] = true
		}
	}
	out := make([]int, 0, len(seen))
	for i := range seen {
		out = append(out, i)
	}
	// simple insertion sort (small slices)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

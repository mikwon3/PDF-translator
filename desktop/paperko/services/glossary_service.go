package services

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// GlossaryService exposes glossary CRUD + CSV I/O to the frontend
// (04-api-spec.md §1.1 GlossaryService).
type GlossaryService struct{ B *Backend }

func now() string { return time.Now().Format(time.RFC3339) }

func (s *GlossaryService) ListGlossaries() []GlossaryMeta {
	return s.B.Glossary.List()
}

func (s *GlossaryService) CreateGlossary(name string) (*Glossary, error) {
	return s.B.Glossary.Create(name, now())
}

func (s *GlossaryService) RenameGlossary(id int, name string) error {
	return s.B.Glossary.Rename(id, name, now())
}

func (s *GlossaryService) DeleteGlossary(id int) error {
	return s.B.Glossary.Delete(id)
}

// TermsPage is a paged term listing.
type TermsPage struct {
	Terms []*Term `json:"terms"`
	Total int     `json:"total"`
}

func (s *GlossaryService) ListTerms(glossaryID int, query string, offset, limit int) TermsPage {
	terms, total := s.B.Glossary.ListTerms(glossaryID, query, offset, limit)
	return TermsPage{Terms: terms, Total: total}
}

func (s *GlossaryService) UpsertTerm(glossaryID int, term Term) (*Term, error) {
	return s.B.Glossary.UpsertTerm(glossaryID, term, now())
}

func (s *GlossaryService) DeleteTerm(glossaryID, termID int) error {
	return s.B.Glossary.DeleteTerm(glossaryID, termID, now())
}

// ImportCSV opens a file dialog and imports the chosen CSV into the glossary.
func (s *GlossaryService) ImportCSV(glossaryID int) (*ImportResult, error) {
	d := application.Get().Dialog.OpenFile()
	d.SetTitle("용어집 CSV 가져오기")
	d.AddFilter("CSV", "*.csv")
	d.AddFilter("TSV/텍스트", "*.tsv;*.txt")
	path, err := d.PromptForSingleSelection()
	if err != nil || path == "" {
		return &ImportResult{}, err
	}
	return s.B.Glossary.ImportCSV(glossaryID, path, now())
}

// ExportCSV opens a save dialog and exports the glossary to CSV.
func (s *GlossaryService) ExportCSV(glossaryID int, suggestedName string) error {
	if suggestedName == "" {
		suggestedName = "glossary.csv"
	}
	d := application.Get().Dialog.SaveFile()
	d.SetFilename(suggestedName)
	d.AddFilter("CSV", "*.csv")
	path, err := d.PromptForSingleSelection()
	if err != nil || path == "" {
		return err
	}
	return s.B.Glossary.ExportCSV(glossaryID, path)
}

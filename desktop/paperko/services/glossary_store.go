package services

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Term is a single glossary entry (03-detailed-design.md §3, §8.2).
type Term struct {
	ID               int    `json:"id"`
	Src              string `json:"src"`
	Dst              string `json:"dst"`
	Domain           string `json:"domain"`
	Priority         int    `json:"priority"`
	CaseSensitive    bool   `json:"case_sensitive"`
	MatchInflections bool   `json:"match_inflections"`
	Note             string `json:"note"`
}

// Glossary is a named set of terms.
type Glossary struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	Terms     []*Term `json:"terms"`
}

// GlossaryMeta is a lightweight listing entry.
type GlossaryMeta struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	TermCount int    `json:"term_count"`
	UpdatedAt string `json:"updated_at"`
}

// ImportResult reports CSV import counts (FR-62).
type ImportResult struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

// GlossaryStore is a JSON-file-backed glossary store (CGO-free, portable).
// The design nominates SQLite; a single JSON document is equivalent for the
// expected sizes and keeps the build dependency-free.
type GlossaryStore struct {
	path string
	mu   sync.Mutex
	data struct {
		Seq        int         `json:"seq"`
		TermSeq    int         `json:"term_seq"`
		Glossaries []*Glossary `json:"glossaries"`
	}
}

func NewGlossaryStore(dataDir string) *GlossaryStore {
	s := &GlossaryStore{path: filepath.Join(dataDir, "glossaries.json")}
	s.load()
	return s
}

func (s *GlossaryStore) load() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &s.data)
}

func (s *GlossaryStore) save() error {
	b, _ := json.MarshalIndent(s.data, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *GlossaryStore) find(id int) *Glossary {
	for _, g := range s.data.Glossaries {
		if g.ID == id {
			return g
		}
	}
	return nil
}

// ---- CRUD ---------------------------------------------------------------- //

func (s *GlossaryStore) List() []GlossaryMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]GlossaryMeta, 0, len(s.data.Glossaries))
	for _, g := range s.data.Glossaries {
		out = append(out, GlossaryMeta{ID: g.ID, Name: g.Name, TermCount: len(g.Terms), UpdatedAt: g.UpdatedAt})
	}
	return out
}

func (s *GlossaryStore) Create(name, now string) (*Glossary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Seq++
	g := &Glossary{ID: s.data.Seq, Name: name, CreatedAt: now, UpdatedAt: now, Terms: []*Term{}}
	s.data.Glossaries = append(s.data.Glossaries, g)
	return g, s.save()
}

func (s *GlossaryStore) Rename(id int, name, now string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.find(id)
	if g == nil {
		return fmt.Errorf("glossary not found: %d", id)
	}
	g.Name = name
	g.UpdatedAt = now
	return s.save()
}

func (s *GlossaryStore) Delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, g := range s.data.Glossaries {
		if g.ID == id {
			s.data.Glossaries = append(s.data.Glossaries[:i], s.data.Glossaries[i+1:]...)
			return s.save()
		}
	}
	return nil
}

// ListTerms returns matching terms (substring on src/dst) plus the total count.
func (s *GlossaryStore) ListTerms(id int, query string, offset, limit int) ([]*Term, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.find(id)
	if g == nil {
		return nil, 0
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var filtered []*Term
	for _, t := range g.Terms {
		if q == "" || strings.Contains(strings.ToLower(t.Src), q) || strings.Contains(strings.ToLower(t.Dst), q) {
			filtered = append(filtered, t)
		}
	}
	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return filtered[offset:end], total
}

func (s *GlossaryStore) UpsertTerm(id int, t Term, now string) (*Term, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.find(id)
	if g == nil {
		return nil, fmt.Errorf("glossary not found: %d", id)
	}
	if t.Src == "" || t.Dst == "" {
		return nil, fmt.Errorf("src and dst are required")
	}
	if t.ID != 0 {
		for _, ex := range g.Terms {
			if ex.ID == t.ID {
				*ex = t
				g.UpdatedAt = now
				return ex, s.save()
			}
		}
	}
	s.data.TermSeq++
	nt := t
	nt.ID = s.data.TermSeq
	g.Terms = append(g.Terms, &nt)
	g.UpdatedAt = now
	return &nt, s.save()
}

func (s *GlossaryStore) DeleteTerm(id, termID int, now string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.find(id)
	if g == nil {
		return fmt.Errorf("glossary not found: %d", id)
	}
	for i, t := range g.Terms {
		if t.ID == termID {
			g.Terms = append(g.Terms[:i], g.Terms[i+1:]...)
			g.UpdatedAt = now
			return s.save()
		}
	}
	return nil
}

// ---- CSV import/export (FR-62) ------------------------------------------- //
// Columns: src,dst,domain,priority,case_sensitive,match_inflections,note

func (s *GlossaryStore) ImportCSV(id int, path, now string) (*ImportResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.find(id)
	if g == nil {
		return nil, fmt.Errorf("glossary not found: %d", id)
	}
	bySrc := map[string]*Term{}
	for _, t := range g.Terms {
		bySrc[strings.ToLower(t.Src)] = t
	}
	res := &ImportResult{}
	for i, row := range rows {
		if len(row) < 2 {
			res.Skipped++
			continue
		}
		src := strings.TrimSpace(row[0])
		if i == 0 && strings.EqualFold(src, "src") {
			continue // header row
		}
		dst := strings.TrimSpace(row[1])
		if src == "" || dst == "" {
			res.Skipped++
			continue
		}
		term := Term{Src: src, Dst: dst, MatchInflections: true}
		if len(row) > 2 {
			term.Domain = strings.TrimSpace(row[2])
		}
		if len(row) > 3 {
			term.Priority, _ = strconv.Atoi(strings.TrimSpace(row[3]))
		}
		if len(row) > 4 {
			term.CaseSensitive = parseBool(row[4])
		}
		if len(row) > 5 {
			term.MatchInflections = parseBool(row[5])
		}
		if len(row) > 6 {
			term.Note = strings.TrimSpace(row[6])
		}
		if ex, ok := bySrc[strings.ToLower(src)]; ok {
			term.ID = ex.ID
			*ex = term
			res.Updated++
		} else {
			s.data.TermSeq++
			term.ID = s.data.TermSeq
			nt := term
			g.Terms = append(g.Terms, &nt)
			bySrc[strings.ToLower(src)] = &nt
			res.Added++
		}
	}
	g.UpdatedAt = now
	return res, s.save()
}

func (s *GlossaryStore) ExportCSV(id int, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.find(id)
	if g == nil {
		return fmt.Errorf("glossary not found: %d", id)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"src", "dst", "domain", "priority", "case_sensitive", "match_inflections", "note"})
	for _, t := range g.Terms {
		_ = w.Write([]string{
			t.Src, t.Dst, t.Domain, strconv.Itoa(t.Priority),
			strconv.FormatBool(t.CaseSensitive), strconv.FormatBool(t.MatchInflections), t.Note,
		})
	}
	return nil
}

// Snapshot writes the merged terms of the selected glossaries as the engine's
// glossary.json (03 §8.2 — reproducibility: the job uses a frozen copy).
func (s *GlossaryStore) Snapshot(ids []int, outPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	type engineTerm struct {
		Src              string `json:"src"`
		Dst              string `json:"dst"`
		Domain           string `json:"domain"`
		Priority         int    `json:"priority"`
		CaseSensitive    bool   `json:"case_sensitive"`
		MatchInflections bool   `json:"match_inflections"`
		Note             string `json:"note"`
	}
	var terms []engineTerm
	seen := map[string]bool{}
	for _, id := range ids {
		g := s.find(id)
		if g == nil {
			continue
		}
		for _, t := range g.Terms {
			key := strings.ToLower(t.Src)
			if seen[key] {
				continue
			}
			seen[key] = true
			terms = append(terms, engineTerm{
				Src: t.Src, Dst: t.Dst, Domain: t.Domain, Priority: t.Priority,
				CaseSensitive: t.CaseSensitive, MatchInflections: t.MatchInflections, Note: t.Note,
			})
		}
	}
	b, _ := json.MarshalIndent(map[string]any{"terms": terms}, "", "  ")
	return os.WriteFile(outPath, b, 0o644)
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true" || s == "y" || s == "yes" || s == "o"
}

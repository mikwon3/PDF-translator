package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGlossaryStore(t *testing.T) {
	dir := t.TempDir()
	s := NewGlossaryStore(dir)
	const now = "2026-01-01T00:00:00Z"

	g, err := s.Create("ML 용어", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertTerm(g.ID, Term{Src: "attention", Dst: "어텐션", Priority: 10}, now); err != nil {
		t.Fatal(err)
	}
	tm, err := s.UpsertTerm(g.ID, Term{Src: "transformer", Dst: "트랜스포머"}, now)
	if err != nil {
		t.Fatal(err)
	}

	// list + search
	terms, total := s.ListTerms(g.ID, "", 0, 100)
	if total != 2 || len(terms) != 2 {
		t.Fatalf("expected 2 terms, got %d", total)
	}
	if _, n := s.ListTerms(g.ID, "trans", 0, 100); n != 1 {
		t.Fatalf("search 'trans' expected 1, got %d", n)
	}

	// update existing (same id) then delete
	if _, err := s.UpsertTerm(g.ID, Term{ID: tm.ID, Src: "transformer", Dst: "트랜스포머(수정)"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTerm(g.ID, tm.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, n := s.ListTerms(g.ID, "", 0, 100); n != 1 {
		t.Fatalf("after delete expected 1, got %d", n)
	}

	// CSV round-trip
	csvPath := filepath.Join(dir, "out.csv")
	if err := s.ExportCSV(g.ID, csvPath); err != nil {
		t.Fatal(err)
	}
	g2, _ := s.Create("import target", now)
	res, err := s.ImportCSV(g2.ID, csvPath, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 {
		t.Fatalf("expected 1 imported, got %+v", res)
	}

	// snapshot to engine glossary.json
	snap := filepath.Join(dir, "glossary.json")
	if err := s.Snapshot([]int{g.ID}, snap); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(snap)
	var doc struct {
		Terms []map[string]any `json:"terms"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Terms) != 1 || doc.Terms[0]["src"] != "attention" || doc.Terms[0]["dst"] != "어텐션" {
		t.Fatalf("snapshot mismatch: %v", doc.Terms)
	}

	// persistence: reload from disk
	s2 := NewGlossaryStore(dir)
	if len(s2.List()) != 2 {
		t.Fatalf("expected 2 glossaries after reload, got %d", len(s2.List()))
	}
}

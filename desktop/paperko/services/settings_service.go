package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"paperko/internal/engine"
)

// Settings is the persisted app configuration (subset of FR-72 for v1 slice).
type Settings struct {
	LLMBaseURL     string `json:"llm_base_url"`
	LLMModel       string `json:"llm_model"`
	LLMAPIKey      string `json:"llm_api_key"`
	MaxConcurrency int    `json:"max_concurrency"`
	Style          string `json:"style"`       // formal | concise
	FontFamily     string `json:"font_family"` // noto | nanum
	Mode           string `json:"mode"`        // replace | interleaved
	UILanguage     string `json:"ui_language"` // ko | en
	SourceLang     string `json:"source_lang"` // auto | en | ko | ja | zh … (document's language)
	TargetLang     string `json:"target_lang"` // Korean | English | …          (translation language)

	// Local inference (offline) — run a bundled llama.cpp server against a local GGUF.
	EngineMode       string `json:"engine_mode"`        // remote | local  (default remote)
	LocalModelURL    string `json:"local_model_url"`    // download source for the GGUF
	LocalModelName   string `json:"local_model_name"`   // display name
	LocalModelPath   string `json:"local_model_path"`   // remembered GGUF path (downloaded or picked)
	LocalModelSHA256 string `json:"local_model_sha256"` // integrity hash (computed on download; verified if set)
	HFToken          string `json:"hf_token"`           // optional Hugging Face token for gated models

	// Update — online update checking (see internal/update).
	Update UpdatePrefs `json:"update"`
}

// UpdatePrefs holds the online-update settings.
type UpdatePrefs struct {
	// NoAutoCheck, when true, skips the automatic check on startup (a manual check
	// from Settings still works). Stored as "off" so an older settings.json without
	// the field still starts with auto-check on.
	NoAutoCheck bool `json:"noAutoCheck,omitempty"`
	// SkipVersion is the version the user chose to skip; honored only by auto-checks.
	SkipVersion string `json:"skipVersion,omitempty"`
	// LastCheck is when we last asked (RFC3339); auto-check runs at most once a day.
	LastCheck string `json:"lastCheck,omitempty"`
}

// updateUpdatePrefs mutates the update settings under the lock and persists them,
// returning a snapshot. Used by UpdateService (mirrors a settings store's Update).
func (b *Backend) updateUpdatePrefs(fn func(*UpdatePrefs)) Settings {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn(&b.settings.Update)
	_ = b.saveSettingsFile()
	return b.settings
}

// defaultGemmaURL is the recommended offline model (Google Gemma-4 E2B, int4 QAT).
const defaultGemmaURL = "https://huggingface.co/google/gemma-4-E2B-it-qat-q4_0-gguf/resolve/main/gemma-4-E2B_q4_0-it.gguf"

func defaultSettings() Settings {
	base := os.Getenv("PAPERKO_LLM_URL")
	if base == "" {
		base = "http://localhost:1234/v1"
	}
	model := os.Getenv("PAPERKO_LLM_MODEL")
	if model == "" {
		model = "qwen/qwen3.6-35b-a3b"
	}
	return Settings{
		LLMBaseURL: base, LLMModel: model, MaxConcurrency: 6,
		Style: "formal", FontFamily: "nanum", Mode: "replace", UILanguage: "ko",
		EngineMode:     "remote",
		LocalModelName: "gemma-4-E2B-it-qat",
		LocalModelURL:  localModelURLDefault(),
		SourceLang:     "auto",
		TargetLang:     "Korean",
	}
}

// tesseractLang maps a source-language code to a Tesseract traineddata code.
// Only "eng" ships bundled; other languages need their traineddata added.
func tesseractLang(src string) string {
	switch strings.ToLower(strings.TrimSpace(src)) {
	case "ko", "korean":
		return "kor"
	case "ja", "japanese":
		return "jpn"
	case "zh", "chinese":
		return "chi_sim"
	case "de":
		return "deu"
	case "fr":
		return "fra"
	case "es":
		return "spa"
	case "ru":
		return "rus"
	default: // auto, en, unknown
		return "eng"
	}
}

func localModelURLDefault() string {
	if u := os.Getenv("PAPERKO_LOCAL_MODEL_URL"); u != "" {
		return u
	}
	return defaultGemmaURL
}

func loadSettings(path string) Settings {
	s := defaultSettings()
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	// Backfill newer fields that an older settings.json may have left empty, so the
	// defaults (e.g. the Gemma download URL) still apply to existing installs.
	if s.LocalModelURL == "" {
		s.LocalModelURL = localModelURLDefault()
	}
	if s.LocalModelName == "" {
		s.LocalModelName = "gemma-4-E2B-it-qat"
	}
	if s.EngineMode == "" {
		s.EngineMode = "remote"
	}
	if s.SourceLang == "" {
		s.SourceLang = "auto"
	}
	if s.TargetLang == "" {
		s.TargetLang = "Korean"
	}
	if s.UILanguage == "" {
		s.UILanguage = "ko"
	}
	return s
}

func (b *Backend) saveSettingsFile() error {
	path := filepath.Join(b.DataDir, "settings.json")
	data, _ := json.MarshalIndent(b.settings, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// llmConfig builds the engine LLM config from current settings. In local mode it
// points the engine at the bundled llama.cpp server (once it is running).
func (b *Backend) llmConfig() engine.LLMConfig {
	if b.settings.EngineMode == "local" && b.llama != nil && b.llama.Running() {
		return engine.LLMConfig{
			BaseURL: b.llama.BaseURL(), Model: b.llama.ModelName(),
			MaxConcurrency: b.settings.MaxConcurrency, TimeoutS: 600,
		}
	}
	var key *string
	if b.settings.LLMAPIKey != "" {
		k := b.settings.LLMAPIKey
		key = &k
	}
	return engine.LLMConfig{
		BaseURL: b.settings.LLMBaseURL, Model: b.settings.LLMModel, APIKey: key,
		MaxConcurrency: b.settings.MaxConcurrency, TimeoutS: 120,
	}
}

// localModelPath resolves where the GGUF lives: an explicit path if set, else the
// per-user cache under the data dir.
func (b *Backend) localModelPath() string {
	if b.settings.LocalModelPath != "" {
		return b.settings.LocalModelPath
	}
	// default cache path = models/<basename of the download URL>
	name := path.Base(b.settings.LocalModelURL)
	if name == "" || name == "." || name == "/" {
		name = b.settings.LocalModelName
		if name == "" {
			name = "local-model"
		}
		name += ".gguf"
	}
	return filepath.Join(b.DataDir, "models", name)
}

// Settings returns a snapshot of the current settings (used by main to build
// the engine config).
func (b *Backend) Settings() Settings {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.settings
}

// SettingsService exposes settings + LLM health to the frontend.
type SettingsService struct{ B *Backend }

func (s *SettingsService) GetSettings() Settings {
	s.B.mu.Lock()
	defer s.B.mu.Unlock()
	return s.B.settings
}

func (s *SettingsService) SaveSettings(next Settings) error {
	s.B.mu.Lock()
	// The update prefs (auto-check, skip, last-check) are owned by UpdateService and
	// written live; keep the current ones so a general save can't clobber them with a
	// stale copy from the frontend.
	next.Update = s.B.settings.Update
	s.B.settings = next
	err := s.B.saveSettingsFile()
	s.B.mu.Unlock()
	if err != nil {
		return err
	}
	// Push the new LLM config to the running engine so the change takes effect
	// immediately (otherwise the sidecar keeps the config from initialize).
	if s.B.Sup != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cerr := s.B.Sup.Configure(ctx, s.B.llmConfig()); cerr != nil {
			return cerr
		}
	}
	return nil
}

// ListModels fetches the available model ids from an OpenAI-compatible server
// (GET {url}/models) so the UI can offer them as a dropdown instead of a
// hand-typed model name.
func (s *SettingsService) ListModels(url, apiKey string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	endpoint := strings.TrimRight(strings.TrimSpace(url), "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, &httpError{resp.StatusCode, strings.TrimSpace(string(body))}
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

type httpError struct {
	code int
	body string
}

func (e *httpError) Error() string {
	msg := e.body
	if len(msg) > 160 {
		msg = msg[:160]
	}
	return "HTTP " + itoa(e.code) + ": " + msg
}

// TestLLMConnection performs a health check against the given server (FR-36).
func (s *SettingsService) TestLLMConnection(url, model, apiKey string) (*engine.HealthInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var key *string
	if apiKey != "" {
		key = &apiKey
	}
	cfg := engine.LLMConfig{BaseURL: url, Model: model, APIKey: key, MaxConcurrency: 4, TimeoutS: 30}
	return s.B.Sup.Health(ctx, &cfg)
}

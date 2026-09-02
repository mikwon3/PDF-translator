// Package engine talks to the Python translate_engine sidecar over stdio
// JSON-RPC 2.0 and manages its process lifecycle (04-api-spec.md §2).
package engine

import "encoding/json"

// DocMeta is the result of document.open.
type DocMeta struct {
	DocID            string `json:"doc_id"`
	PageCount        int    `json:"page_count"`
	Title            string `json:"title"`
	HasTextLayer     bool   `json:"has_text_layer"`
	Encrypted        bool   `json:"encrypted"`
	PagesWithoutText []int  `json:"pages_without_text"`
}

// AnalyzeResult is the result of document.analyze.
type AnalyzeResult struct {
	IRPath   string           `json:"ir_path"`
	Stats    map[string]any   `json:"stats"`
	Warnings []map[string]any `json:"warnings"`
}

// TranslateResult is the result of document.translate.
type TranslateResult struct {
	TranslationPath string         `json:"translation_path"`
	Stats           map[string]any `json:"stats"`
}

// RenderResult is the result of document.render.
type RenderResult struct {
	OutPath string         `json:"out_path"`
	Report  map[string]any `json:"report"`
}

// PreviewResult is the result of page.preview.
type PreviewResult struct {
	PNGBase64 string `json:"png_base64"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// HealthInfo mirrors the engine/vLLM health check.
type HealthInfo struct {
	OK         bool    `json:"ok"`
	LatencyMS  float64 `json:"latency_ms"`
	ModelFound bool    `json:"model_found"`
	Error      string  `json:"error,omitempty"`
}

// LLMConfig is passed to the engine on initialize.
type LLMConfig struct {
	BaseURL        string  `json:"base_url"`
	Model          string  `json:"model"`
	APIKey         *string `json:"api_key"`
	MaxConcurrency int     `json:"max_concurrency"`
	TimeoutS       float64 `json:"timeout_s"`
}

// Notification is a Python→Go message (progress / partial / tu_failed / log).
type Notification struct {
	Method string
	Params json.RawMessage
}

// rpcError is the JSON-RPC error object; data.code carries the engine code.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Code   string `json:"code"`
		Detail any    `json:"detail"`
	} `json:"data"`
}

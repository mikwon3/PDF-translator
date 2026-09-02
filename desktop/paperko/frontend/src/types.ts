export interface Settings {
  llm_base_url: string
  llm_model: string
  llm_api_key: string
  max_concurrency: number
  style: string
  font_family: string
  mode: string
  ui_language: string
  source_lang: string       // auto | en | ko | ja | zh …
  target_lang: string       // Korean | English
  engine_mode: string       // remote | local
  local_model_url: string
  local_model_name: string
  local_model_path: string
  local_model_sha256: string
  hf_token: string
}

// A translation that can be continued (interrupted/cancelled/failed with a checkpoint).
export interface ResumableJob {
  id: string
  title: string
  pdf_path: string
  out_path: string       // last rendered output.pdf (temp translated preview)
  state: string          // INTERRUPTED | CANCELLED | FAILED | PARTIAL
  done_tus: number
  total_tus: number
  done_pages: number
  total_pages: number
  done_page_list: number[]   // 0-based pages fully translated (to restore batches)
  updated_at: string
}

export interface LocalModelInfo {
  mode: string
  bin_available: boolean
  model_present: boolean
  model_path: string
  model_name: string
  model_url: string
  size_mb: number
  running: boolean
  base_url: string
}

export interface HealthInfo {
  ok: boolean
  latency_ms: number
  model_found: boolean
  error?: string
}

export interface GlossaryMeta {
  id: number
  name: string
  term_count: number
  updated_at: string
}

export interface Term {
  id: number
  src: string
  dst: string
  domain: string
  priority: number
  case_sensitive: boolean
  match_inflections: boolean
  note: string
}

export interface TermsPage {
  terms: Term[]
  total: number
}

export interface ImportResult {
  added: number
  updated: number
  skipped: number
}

export interface FailedRegion {
  tu_id: string
  block_ids: string[]
  page?: number
  message: string
  retrying?: boolean
}

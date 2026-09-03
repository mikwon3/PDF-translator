import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { SettingsService } from '../bindings/paperko/services'
import type { HealthInfo, LocalModelInfo, Settings } from './types'
import { useT, tr, type UILang } from './i18n'

interface Props {
  initial: Settings
  onClose: () => void
  onSaved: (s: Settings) => void
}

// Commercial LLM providers reachable through their OpenAI-compatible endpoints, plus
// "custom" for a self-hosted vLLM/LM Studio server. Model lists are convenient starters
// — the exact model can be typed or fetched with [모델 불러오기].
interface Provider { label: string; url: string; models: string[]; keyHint: string; keyUrl: string }
const PROVIDERS: Record<string, Provider> = {
  custom: { label: '직접 입력 (OpenAI 호환 서버)', url: '', models: [], keyHint: '비어 있으면 미사용', keyUrl: '' },
  openai: {
    label: 'OpenAI (ChatGPT)', url: 'https://api.openai.com/v1',
    models: ['gpt-4o', 'gpt-4o-mini', 'gpt-4.1', 'gpt-4.1-mini'],
    keyHint: 'sk-…', keyUrl: 'https://platform.openai.com/api-keys',
  },
  anthropic: {
    label: 'Anthropic (Claude)', url: 'https://api.anthropic.com/v1',
    models: ['claude-sonnet-4-5', 'claude-opus-4-1', 'claude-haiku-4-5'],
    keyHint: 'sk-ant-…', keyUrl: 'https://console.anthropic.com/settings/keys',
  },
  gemini: {
    label: 'Google (Gemini)', url: 'https://generativelanguage.googleapis.com/v1beta/openai',
    models: ['gemini-2.5-flash', 'gemini-2.5-pro', 'gemini-2.0-flash'],
    keyHint: 'AIza…', keyUrl: 'https://aistudio.google.com/apikey',
  },
}
function providerFromUrl(url: string): string {
  const u = (url || '').toLowerCase()
  if (u.includes('api.openai.com')) return 'openai'
  if (u.includes('api.anthropic.com')) return 'anthropic'
  if (u.includes('generativelanguage.googleapis.com')) return 'gemini'
  return 'custom'
}

export default function SettingsModal({ initial, onClose, onSaved }: Props) {
  const [s, setS] = useState<Settings>({ ...initial })
  const { lang: ctxLang } = useT()
  const lang: UILang = (s.ui_language as UILang) || ctxLang       // live preview as you toggle
  const t = (ko: string) => tr(lang, ko)
  const [health, setHealth] = useState<HealthInfo | null>(null)
  const [testing, setTesting] = useState(false)

  const [models, setModels] = useState<string[]>([])
  const [loadingModels, setLoadingModels] = useState(false)
  const [modelError, setModelError] = useState('')
  const [provider, setProvider] = useState<string>(providerFromUrl(initial.llm_base_url))

  // Pick a provider: fill its OpenAI-compatible endpoint and suggest its models. The
  // URL/model/key stay editable, and "custom" keeps whatever the user typed.
  function applyProvider(p: string) {
    setProvider(p)
    const pr = PROVIDERS[p]
    if (p === 'custom' || !pr) return
    set('llm_base_url', pr.url)
    setModels([]); setModelError(''); setHealth(null)
    if (pr.models.length && !pr.models.includes(s.llm_model)) set('llm_model', pr.models[0])
  }
  // model choices: fetched list if available, else the selected provider's starters
  const modelList = models.length ? models : (PROVIDERS[provider]?.models || [])

  // local (offline) engine state
  const [info, setInfo] = useState<LocalModelInfo | null>(null)
  const [dl, setDl] = useState<{ received: number; total: number; done?: boolean } | null>(null)
  const [localBusy, setLocalBusy] = useState('')
  const [localErr, setLocalErr] = useState('')

  function set<K extends keyof Settings>(k: K, v: Settings[K]) {
    setS((prev) => ({ ...prev, [k]: v }))
  }

  async function refreshInfo() {
    try {
      setInfo((await SettingsService.LocalModelInfo()) as LocalModelInfo)
      // Re-sync the model fields the backend owns (path/url/sha set by pick/download)
      // into our editable copy, so clicking [저장] doesn't overwrite them with stale
      // (empty) values — the bug that dropped the remembered model path on restart.
      const gs = (await SettingsService.GetSettings()) as Settings
      setS((prev) => ({
        ...prev,
        local_model_path: gs.local_model_path,
        local_model_url: gs.local_model_url,
        local_model_sha256: gs.local_model_sha256,
        engine_mode: gs.engine_mode,
      }))
    } catch { /* ignore */ }
  }
  useEffect(() => {
    refreshInfo()
    const offs = [
      Events.On('localllm:download', (e: any) => setDl(e.data)),
      Events.On('localllm:status', (e: any) => { refreshInfo(); if (e.data?.error) setLocalErr(String(e.data.error)) }),
    ]
    return () => { offs.forEach((f: any) => f && f()) }
  }, [])

  async function switchMode(mode: string) {
    set('engine_mode', mode); setLocalErr('')
    try { await SettingsService.SetEngineMode(mode) } catch (e) { setLocalErr(String(e)) }
    refreshInfo()
  }
  async function pickModel() {
    setLocalErr('')
    try {
      const p = await SettingsService.PickLocalModel()
      if (p) { set('local_model_path', p); refreshInfo() }
    } catch (e) { setLocalErr(String(e)) }
  }
  async function downloadModel() {
    setLocalErr(''); setLocalBusy('download'); setDl({ received: 0, total: 0 })
    try {
      await SettingsService.SaveSettings(s as any) // persist URL + HF token first
      await SettingsService.DownloadLocalModel(s.local_model_url)
      refreshInfo()
    } catch (e) { setLocalErr(String(e)) }
    finally { setLocalBusy('') }
  }
  async function startLocal() {
    setLocalErr(''); setLocalBusy('start')
    try { await SettingsService.StartLocalEngine(); refreshInfo() }
    catch (e) { setLocalErr(String(e)) }
    finally { setLocalBusy('') }
  }
  async function stopLocal() {
    try { await SettingsService.StopLocalEngine(); refreshInfo() } catch (e) { setLocalErr(String(e)) }
  }
  const mb = (n: number) => (n / (1024 * 1024)).toFixed(0)

  async function loadModels() {
    if (!s.llm_base_url.trim()) return
    setLoadingModels(true); setModelError('')
    try {
      const list = (await SettingsService.ListModels(s.llm_base_url, s.llm_api_key)) as string[]
      setModels(list || [])
      if ((list || []).length === 0) setModelError('모델이 없습니다')
    } catch (e) {
      setModels([]); setModelError('목록을 가져오지 못했습니다: ' + String(e))
    } finally { setLoadingModels(false) }
  }

  // auto-load once when the dialog opens (if a URL is already configured)
  useEffect(() => { if (initial.llm_base_url.trim()) loadModels() }, [])

  async function test() {
    setTesting(true); setHealth(null)
    try {
      const h = await SettingsService.TestLLMConnection(s.llm_base_url, s.llm_model, s.llm_api_key)
      setHealth(h as HealthInfo)
    } catch (e) {
      setHealth({ ok: false, latency_ms: 0, model_found: false, error: String(e) })
    } finally { setTesting(false) }
  }

  async function save() {
    await SettingsService.SaveSettings(s as any)
    onSaved(s)
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>{t('설정')}</h2>

        <div className="field">
          <label>{t('언어 / Language')}</label>
          <div className="seg">
            <button className={lang !== 'en' ? 'seg-on' : ''} onClick={() => set('ui_language', 'ko')}>한국어</button>
            <button className={lang === 'en' ? 'seg-on' : ''} onClick={() => set('ui_language', 'en')}>English</button>
          </div>
        </div>

        <div className="field">
          <label>{t('번역 엔진')}</label>
          <div className="seg">
            <button className={s.engine_mode !== 'local' ? 'seg-on' : ''} onClick={() => switchMode('remote')}>
              {t('🌐 원격 서버 (vLLM)')}
            </button>
            <button className={s.engine_mode === 'local' ? 'seg-on' : ''} onClick={() => switchMode('local')}>
              {t('💻 로컬 모델 (오프라인)')}
            </button>
          </div>
        </div>

        {s.engine_mode === 'local' ? (
          <div className="local-panel">
            {info && !info.bin_available && (
              <div className="dim sm" style={{ color: '#dc2626' }}>
                {t('⚠ 이 빌드에 llama-server가 번들되어 있지 않습니다. (빌드 시 resources/llama 포함 필요)')}
              </div>
            )}
            <div className="field">
              <label>{t('모델 (GGUF)')}</label>
              <div className="local-status">
                {info?.model_present
                  ? <span className="health ok">✓ {t('준비됨 · ')}{info.size_mb}MB{info.running ? t(' · 실행 중') : ''}</span>
                  : <span className="dim sm">{t('모델 파일이 없습니다 — 아래에서 다운로드하거나 파일을 선택하세요.')}</span>}
              </div>
              <div className="dim sm" style={{ wordBreak: 'break-all' }}>{info?.model_path}</div>
            </div>

            <div className="field">
              <label>{t('모델 다운로드 URL (GGUF 직링크)')}</label>
              <div className="inline">
                <input value={s.local_model_url} onChange={(e) => set('local_model_url', e.target.value)}
                  placeholder="https://…/gemma-4-e2b-it-qat.gguf" />
                <button className="ghost sm" onClick={downloadModel} disabled={!!localBusy || !s.local_model_url.trim()}>
                  {localBusy === 'download' ? t('받는 중…') : t('다운로드')}
                </button>
                <button className="ghost sm" onClick={pickModel} disabled={!!localBusy}>{t('파일 선택')}</button>
              </div>
              {dl && localBusy === 'download' && (
                <div className="dim sm">
                  {dl.total > 0
                    ? `${mb(dl.received)} / ${mb(dl.total)} MB (${Math.round(dl.received / dl.total * 100)}%)${t(' · 이어받기 지원')}`
                    : `${mb(dl.received)} MB…`}
                </div>
              )}
              <input type="password" value={s.hf_token} onChange={(e) => set('hf_token', e.target.value)}
                placeholder={t('Hugging Face 토큰 (게이트 모델 다운로드 시, 선택)')} style={{ marginTop: 6 }} />
              <div className="dim sm">{t('Gemma는 게이트 모델이라 다운로드하려면 HF 토큰이 필요할 수 있습니다. 이미 받아둔 GGUF가 있으면 ')}<b>{t('파일 선택')}</b>{t('이 가장 간단합니다. (Range 이어받기·SHA-256 검증 포함)')}</div>
            </div>

            <div className="test-row">
              {info?.running
                ? <button className="ghost" onClick={stopLocal} disabled={!!localBusy}>{t('■ 로컬 엔진 중지')}</button>
                : <button className="primary" onClick={startLocal} disabled={!!localBusy || !info?.model_present || !info?.bin_available}>
                    {localBusy === 'start' ? t('시작 중…') : t('▶ 로컬 엔진 시작')}
                  </button>}
              {info?.running && <span className="health ok">✓ {t('로컬 서버 실행 중 (')}{info.model_name || 'gemma'})</span>}
            </div>
            {localErr && <div className="dim sm" style={{ color: '#dc2626' }}>{localErr}</div>}
          </div>
        ) : (
        <>
        <div className="field">
          <label>{t('LLM 공급자')}</label>
          <select value={provider} onChange={(e) => applyProvider(e.target.value)}>
            <option value="custom">{t('직접 입력 (OpenAI 호환 서버)')}</option>
            <option value="openai">OpenAI (ChatGPT)</option>
            <option value="anthropic">Anthropic (Claude)</option>
            <option value="gemini">Google (Gemini)</option>
          </select>
        </div>
        <div className="field">
          <label>{provider === 'custom' ? t('vLLM 서버 URL (OpenAI 호환)') : t('API 주소')}</label>
          <div className="inline">
            <input value={s.llm_base_url} onChange={(e) => { set('llm_base_url', e.target.value); setProvider(providerFromUrl(e.target.value)) }}
              placeholder="http://203.255.40.88:8567/v1" />
            <button className="ghost sm" onClick={loadModels} disabled={loadingModels || !s.llm_base_url.trim()}>
              {loadingModels ? t('불러오는 중…') : t('모델 불러오기')}
            </button>
          </div>
        </div>
        <div className="field">
          <label>{t('모델 선택')}</label>
          {modelList.length > 0 ? (
            <select value={s.llm_model} onChange={(e) => set('llm_model', e.target.value)}>
              {!modelList.includes(s.llm_model) && s.llm_model && (
                <option value={s.llm_model}>{s.llm_model}{t(' (현재)')}</option>
              )}
              {modelList.map((m) => <option key={m} value={m}>{m}</option>)}
            </select>
          ) : (
            <input value={s.llm_model} onChange={(e) => set('llm_model', e.target.value)}
              placeholder={t('URL 입력 후 [모델 불러오기] 또는 직접 입력')} />
          )}
          {modelError && <div className="dim sm" style={{ color: '#dc2626' }}>{modelError}</div>}
          {models.length > 0
            ? <div className="dim sm">{models.length}{t('개 모델 · 목록에서 선택')}</div>
            : (provider !== 'custom' && <div className="dim sm">{t('추천 모델 · 정확한 모델명은 [모델 불러오기] 또는 직접 입력')}</div>)}
        </div>
        <div className="field">
          <label>{provider === 'custom' ? t('API 키 (필요 시)') : t('API 키')}</label>
          <input type="password" value={s.llm_api_key} onChange={(e) => set('llm_api_key', e.target.value)}
            placeholder={PROVIDERS[provider]?.keyHint ? t(PROVIDERS[provider].keyHint) : t('비어 있으면 미사용')} />
          {provider !== 'custom' && PROVIDERS[provider]?.keyUrl && (
            <div className="dim sm">{t('키 발급')}: <a href={PROVIDERS[provider].keyUrl} target="_blank" rel="noreferrer">{PROVIDERS[provider].keyUrl}</a></div>
          )}
        </div>

        <div className="test-row">
          <button className="ghost" onClick={test} disabled={testing}>
            {testing ? t('확인 중…') : t('연결 테스트')}
          </button>
          {health && (
            <span className={`health ${health.ok && health.model_found ? 'ok' : 'bad'}`}>
              {health.ok
                ? (health.model_found ? `✓ ${t('연결됨 (')}${Math.round(health.latency_ms)}ms)` : t('⚠ 서버 OK, 모델명 불일치'))
                : `✕ ${t('실패: ')}${health.error || t('연결 불가')}`}
            </span>
          )}
        </div>
        </>
        )}

        <div className="row">
          <div className="field">
            <label>{t('동시성')}</label>
            <input type="number" min={1} max={16} value={s.max_concurrency}
              onChange={(e) => set('max_concurrency', parseInt(e.target.value || '1'))} />
          </div>
          <div className="field">
            <label>{t('출력 한글 폰트 (PDF 임베딩)')}</label>
            <select value={s.font_family} onChange={(e) => set('font_family', e.target.value)}>
              <option value="nanum">{t('나눔고딕 (기본, 고딕체)')}</option>
              <option value="nanum-myeongjo">{t('나눔명조 (세리프/명조체)')}</option>
              <option value="noto">{t('Noto Sans KR (fonts 폴더에 있을 때)')}</option>
            </select>
          </div>
        </div>

        <div className="modal-actions">
          <button className="ghost" onClick={onClose}>{t('취소')}</button>
          <button className="primary" onClick={save}>{t('저장')}</button>
        </div>
      </div>
    </div>
  )
}

'use strict'
const $ = (id) => document.getElementById(id)
const state = {
  jobId: null, meta: null, pages: 0, page: 0, zoom: 1, viewW: 900,
  hasOutput: false, llm: { url: '', model: '', key: '' }, models: [],
}

// ---- helpers ----
async function jget(url) { const r = await fetch(url); if (!r.ok) throw new Error((await r.text()) || r.status); return r.json() }
function show(el, on) { el.classList.toggle('hidden', !on) }

// ---- init ----
jget('/api/info').then((i) => {
  $('version').textContent = 'v' + i.version
  state.llm.url = i.default_llm.url; state.llm.model = i.default_llm.model
  $('sUrl').value = i.default_llm.url
  state.about = i.about || null
}).catch(() => {})

// ---- upload ----
$('pickBtn').onclick = () => $('fileInput').click()
$('fileInput').onchange = (e) => { if (e.target.files[0]) uploadFile(e.target.files[0]) }
const drop = $('uploadCard')
;['dragover', 'dragenter'].forEach((ev) => drop.addEventListener(ev, (e) => { e.preventDefault(); drop.classList.add('over') }))
;['dragleave', 'drop'].forEach((ev) => drop.addEventListener(ev, (e) => { e.preventDefault(); drop.classList.remove('over') }))
drop.addEventListener('drop', (e) => { const f = e.dataTransfer.files[0]; if (f && f.type === 'application/pdf') uploadFile(f) })

async function uploadFile(file) {
  $('uploadHint').textContent = '업로드 중… ' + file.name
  const fd = new FormData(); fd.append('file', file)
  try {
    const r = await fetch('/api/upload', { method: 'POST', body: fd })
    if (!r.ok) throw new Error(await r.text())
    const d = await r.json()
    state.jobId = d.job_id; state.meta = d.meta; state.pages = d.meta.page_count
    state.page = 0; state.hasOutput = false
    renderMeta(d.filename)
    show($('uploadCard'), false); show($('jobCard'), true); show($('viewer'), true)
    show($('progress'), false)
    loadPreview()
  } catch (e) {
    $('uploadHint').textContent = '업로드 실패: ' + e.message
  }
}

function renderMeta(filename) {
  const m = state.meta
  $('meta').innerHTML = `<div class="meta-title">${escapeHtml(m.title || filename || '(제목 없음)')}</div>
    <div class="meta-row"><span>페이지</span><b>${m.page_count}</b></div>
    <div class="meta-row"><span>텍스트 레이어</span><b>${m.has_text_layer ? '있음' : '없음(OCR)'}</b></div>`
}
function escapeHtml(s) { return String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c])) }

// ---- glossary term counter (shown on the collapsed summary) ----
function countGlossary() {
  const lines = ($('glossary').value || '').split('\n')
  let n = 0
  for (const l of lines) { const t = l.trim(); if (t && !t.startsWith('#') && t.split(/[,\t]/).filter(x => x.trim()).length >= 2) n++ }
  $('glossCount').textContent = n ? `· ${n}개 등록됨` : ''
}
$('glossary').addEventListener('input', countGlossary)

// ---- translate ----
$('resetBtn').onclick = () => location.reload()
$('translateBtn').onclick = startTranslate

function startTranslate() {
  const fd = new FormData()
  fd.append('job_id', state.jobId)
  fd.append('pages', $('pages').value)
  fd.append('style', $('style').value)
  fd.append('mode', $('mode').value)
  fd.append('llm_url', state.llm.url)
  fd.append('llm_model', state.llm.model)
  fd.append('glossary', $('glossary').value)
  fd.append('enforce_glossary', $('enforce').checked ? 'true' : 'false')
  $('translateBtn').disabled = true
  show($('progress'), true); show($('err'), false); show($('failed'), false)
  $('failed').innerHTML = ''
  fetch('/api/translate', { method: 'POST', body: fd }).then((r) => {
    if (!r.ok) throw new Error()
    listenEvents()
    startStatusPoll()
  }).catch(() => { setErr('번역 시작 실패'); $('translateBtn').disabled = false })
}

// Fallback: even if the SSE stream stalls (proxy/network), poll job status so the
// UI still reaches completion instead of sitting on "대기".
let statusPoll = null
function stopStatusPoll() { if (statusPoll) { clearInterval(statusPoll); statusPoll = null } }
function startStatusPoll() {
  stopStatusPoll()
  statusPoll = setInterval(async () => {
    try {
      const s = await jget(`/api/jobs/${state.jobId}/status`)
      if (s.state === 'done' && s.has_out) { stopStatusPoll(); onJobDone() }
      else if (s.state === 'failed') { stopStatusPoll(); if (!state.hasOutput) { setErr('번역 실패'); $('translateBtn').disabled = false } }
      else if (!state.hasOutput) {
        // Drive stage label + progress bar from polling — works even when the SSE
        // stream is buffered by the proxy (Cloudflare) and arrives only at the end.
        const p = s.progress
        if (p && p.total) {
          const pct = Math.round(p.done / p.total * 100)
          $('stageLabel').textContent = stageKo(p.stage); $('pct').textContent = pct + '%'
          $('barFill').style.width = pct + '%'
        } else if (s.state) { $('stageLabel').textContent = stageKo(s.state) }
      }
    } catch (e) { /* keep polling */ }
  }, 1200)
}
function onJobDone() {
  if (state.hasOutput) return
  $('stageLabel').textContent = '완료'; $('pct').textContent = '100%'; $('barFill').style.width = '100%'
  state.hasOutput = true; $('downloadBtn').disabled = false; $('retransPageBtn').disabled = false; $('dstCap').textContent = '번역'
  $('translateBtn').disabled = false
  loadPreview()
}

function listenEvents() {
  const es = new EventSource(`/api/jobs/${state.jobId}/events`)
  es.onmessage = (m) => {
    const d = JSON.parse(m.data)
    if (d.type === 'progress') {
      const pct = d.total ? Math.round(d.done / d.total * 100) : 0
      $('stageLabel').textContent = stageKo(d.stage); $('pct').textContent = pct + '%'
      $('barFill').style.width = pct + '%'
    } else if (d.type === 'state') {
      $('stageLabel').textContent = stageKo(d.state)
    } else if (d.type === 'page_done') {
      if (d.page === state.page && state.hasOutput) loadPreview()
    } else if (d.type === 'tu_failed') {
      addFailed(d)
    } else if (d.type === 'done') {
      stopStatusPoll(); onJobDone()
    } else if (d.type === 'error') {
      stopStatusPoll(); setErr(d.message); $('translateBtn').disabled = false
    } else if (d.type === 'end') {
      es.close()
    }
  }
  es.onerror = () => { es.close() }  // status poll keeps the UI alive if SSE drops
}
function stageKo(s) { return ({ analyze: '레이아웃 분석', analyzing: '레이아웃 분석', translate: '번역', translating: '번역', render: '재조판', rendering: '재조판', queued: '대기' })[s] || s }
function setErr(msg) { const e = $('err'); e.textContent = '⚠ ' + msg; show(e, true) }

const failedSet = new Set()
function addFailed(d) {
  if (failedSet.has(d.tu_id)) return
  failedSet.add(d.tu_id)
  const f = $('failed'); show(f, true)
  if (!f.querySelector('.fail-head')) f.appendChild(Object.assign(document.createElement('span'), { className: 'fail-head', textContent: '⚠ 재번역 필요' }))

  const row = document.createElement('span')
  row.className = 'fail-item'; row.dataset.tu = d.tu_id
  const pageBtn = document.createElement('button')
  pageBtn.className = 'fail-chip'
  pageBtn.textContent = (typeof d.page === 'number' ? (d.page + 1) + '쪽' : d.tu_id)
  pageBtn.title = '해당 페이지로 이동'
  pageBtn.onclick = () => { if (typeof d.page === 'number') { state.page = d.page; loadPreview() } }
  const reBtn = document.createElement('button')
  reBtn.className = 'ghost sm'
  reBtn.textContent = '다시 번역'
  reBtn.onclick = () => retranslateTU(d, row, reBtn)
  row.appendChild(pageBtn); row.appendChild(reBtn)
  f.appendChild(row)
}

async function retranslateTU(d, row, btn) {
  btn.disabled = true; btn.textContent = '재번역 중…'
  const fd = new FormData()
  fd.append('tu_id', d.tu_id)
  fd.append('mode', $('mode').value)
  fd.append('llm_url', state.llm.url)
  fd.append('llm_model', state.llm.model)
  try {
    const r = await fetch(`/api/jobs/${state.jobId}/retranslate`, { method: 'POST', body: fd })
    if (!r.ok) throw new Error(await r.text())
    const res = await r.json()
    if (res.status === 'done') {
      failedSet.delete(d.tu_id)
      row.remove()
      if (!$('failed').querySelector('.fail-item')) show($('failed'), false)
      if (typeof d.page === 'number') { state.page = d.page }
      loadPreview()  // re-rendered PDF → refresh
    } else {
      btn.disabled = false; btn.textContent = '다시 번역'
    }
  } catch (e) {
    btn.disabled = false; btn.textContent = '다시 번역'
    setErr('재번역 실패: ' + e.message)
  }
}

// ---- preview / compare ----
function colWidth() { return Math.max(120, ((state.viewW - 22) / 2) * state.zoom) }
function applySizes() {
  const w = colWidth()
  document.querySelectorAll('.compare figure').forEach((f) => (f.style.width = w + 'px'))
  ;[$('srcImg'), $('dstImg')].forEach((im) => (im.style.width = w + 'px'))
}
async function loadPreview() {
  if (state.jobId == null) return
  $('pageLabel').textContent = (state.page + 1) + ' / ' + (state.pages || '?')
  $('previewLoading').textContent = '렌더링…'
  const dpi = 2.2
  const bust = Date.now()
  $('srcImg').src = `/api/jobs/${state.jobId}/preview/${state.page}?kind=source&zoom=${dpi}&_=${bust}`
  if (state.hasOutput) {
    $('dstImg').src = `/api/jobs/${state.jobId}/preview/${state.page}?kind=translated&zoom=${dpi}&_=${bust}`
    $('dstImg').style.display = ''
  } else { $('dstImg').removeAttribute('src'); $('dstImg').style.display = 'none' }
  $('srcImg').onload = () => { $('previewLoading').textContent = ''; applySizes() }
  applySizes()
}
$('prevPage').onclick = () => { if (state.page > 0) { state.page--; loadPreview() } }
$('nextPage').onclick = () => { if (state.page < state.pages - 1) { state.page++; loadPreview() } }
$('zoomIn').onclick = () => setZoom(state.zoom + 0.25)
$('zoomOut').onclick = () => setZoom(state.zoom - 0.25)
$('zoomVal').onclick = () => setZoom(1)
function setZoom(z) { state.zoom = Math.min(4, Math.max(0.5, Math.round(z * 100) / 100)); $('zoomVal').textContent = Math.round(state.zoom * 100) + '%'; applySizes() }
$('downloadBtn').onclick = () => { window.location = `/api/jobs/${state.jobId}/download` }

// retranslate the whole current page (always available once translated)
$('retransPageBtn').onclick = async () => {
  const btn = $('retransPageBtn'); const label = btn.textContent
  btn.disabled = true; btn.textContent = '재번역 중…'
  const fd = new FormData()
  fd.append('page', state.page); fd.append('mode', $('mode').value)
  fd.append('llm_url', state.llm.url); fd.append('llm_model', state.llm.model)
  try {
    const r = await fetch(`/api/jobs/${state.jobId}/retranslate_page`, { method: 'POST', body: fd })
    if (!r.ok) throw new Error(await r.text())
    await r.json()
    loadPreview()
  } catch (e) { setErr('페이지 재번역 실패: ' + e.message) }
  finally { btn.disabled = false; btn.textContent = label }
}

// click-and-drag panning
const scroll = $('scroll')
let pan = null
scroll.addEventListener('mousedown', (e) => { if (e.button !== 0) return; pan = { x: e.clientX, y: e.clientY, sl: scroll.scrollLeft, st: scroll.scrollTop }; scroll.classList.add('grabbing'); e.preventDefault() })
window.addEventListener('mousemove', (e) => { if (!pan) return; scroll.scrollLeft = pan.sl - (e.clientX - pan.x); scroll.scrollTop = pan.st - (e.clientY - pan.y) })
window.addEventListener('mouseup', () => { pan = null; scroll.classList.remove('grabbing') })
scroll.addEventListener('wheel', (e) => { if (e.ctrlKey || e.metaKey) { e.preventDefault(); setZoom(state.zoom + (e.deltaY < 0 ? 0.15 : -0.15)) } }, { passive: false })
new ResizeObserver(() => { state.viewW = scroll.clientWidth; applySizes() }).observe(scroll)

// ---- about ----
$('aboutBtn').onclick = async () => {
  let a = state.about
  if (!a) { try { a = await jget('/api/about') } catch (e) { a = null } }
  if (!a) return
  $('aboutName').textContent = (a.name || 'PaperKo') + ' v' + (a.version || '')
  $('aboutDesc').textContent = a.description || ''
  const rows = [
    ['개발', a.author],
    ['부서', a.department],
    ['소속', a.organization],
    ['문의', a.contact],
    ['라이선스', a.license],
    ['©', a.year ? a.year + ' ' + (a.author || '') : ''],
  ]
  $('aboutList').innerHTML = rows
    .filter(([, v]) => v)
    .map(([k, v]) => `<dt>${escapeHtml(k)}</dt><dd>${escapeHtml(v)}</dd>`)
    .join('')
  show($('aboutModal'), true)
}
$('closeAbout').onclick = () => show($('aboutModal'), false)
$('aboutModal').onclick = (e) => { if (e.target.id === 'aboutModal') show($('aboutModal'), false) }

// ---- settings ----
$('settingsBtn').onclick = () => { $('sUrl').value = state.llm.url; $('sKey').value = state.llm.key; show($('settingsModal'), true); loadModels() }
$('closeSettings').onclick = () => { state.llm.url = $('sUrl').value.trim(); state.llm.key = $('sKey').value; if ($('sModel').value) state.llm.model = $('sModel').value; show($('settingsModal'), false) }
$('settingsModal').onclick = (e) => { if (e.target.id === 'settingsModal') $('closeSettings').click() }
$('loadModels').onclick = loadModels
async function loadModels() {
  const url = $('sUrl').value.trim(); if (!url) return
  $('modelHint').textContent = '불러오는 중…'
  try {
    const d = await jget(`/api/models?url=${encodeURIComponent(url)}&key=${encodeURIComponent($('sKey').value)}`)
    const sel = $('sModel'); sel.innerHTML = ''
    d.models.forEach((m) => { const o = document.createElement('option'); o.value = m; o.textContent = m; sel.appendChild(o) })
    if (d.models.includes(state.llm.model)) sel.value = state.llm.model
    $('modelHint').textContent = d.models.length + '개 모델'
  } catch (e) { $('modelHint').textContent = '목록 실패: ' + e.message }
}

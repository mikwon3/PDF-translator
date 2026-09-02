import { useEffect, useState } from 'react'
import { GlossaryService } from '../bindings/paperko/services'
import type { GlossaryMeta, Term } from './types'
import { useT } from './i18n'

const emptyTerm = (): Term => ({
  id: 0, src: '', dst: '', domain: '', priority: 0,
  case_sensitive: false, match_inflections: true, note: '',
})

export default function GlossaryModal({ onClose }: { onClose: () => void }) {
  const { t } = useT()
  const [glossaries, setGlossaries] = useState<GlossaryMeta[]>([])
  const [selected, setSelected] = useState<number | null>(null)
  const [terms, setTerms] = useState<Term[]>([])
  const [total, setTotal] = useState(0)
  const [query, setQuery] = useState('')
  const [draft, setDraft] = useState<Term>(emptyTerm())
  const [msg, setMsg] = useState('')

  async function refreshGlossaries(select?: number) {
    const gs = (await GlossaryService.ListGlossaries()) as GlossaryMeta[]
    setGlossaries(gs)
    if (select !== undefined) setSelected(select)
    else if (selected === null && gs.length) setSelected(gs[0].id)
  }
  useEffect(() => { refreshGlossaries() }, [])

  async function refreshTerms() {
    if (selected === null) { setTerms([]); setTotal(0); return }
    const page = await GlossaryService.ListTerms(selected, query, 0, 500)
    setTerms((page as any).terms || [])
    setTotal((page as any).total || 0)
  }
  useEffect(() => { refreshTerms() }, [selected, query])

  async function createGlossary() {
    const name = prompt(t('새 용어집 이름'))
    if (!name) return
    const g = await GlossaryService.CreateGlossary(name)
    await refreshGlossaries((g as any).id)
  }
  async function renameGlossary() {
    if (selected === null) return
    const cur = glossaries.find((g) => g.id === selected)
    const name = prompt(t('용어집 이름 변경'), cur?.name || '')
    if (!name) return
    await GlossaryService.RenameGlossary(selected, name)
    await refreshGlossaries(selected)
  }
  async function deleteGlossary() {
    if (selected === null) return
    if (!confirm(t('이 용어집을 삭제할까요?'))) return
    await GlossaryService.DeleteGlossary(selected)
    setSelected(null)
    await refreshGlossaries()
  }

  async function saveTerm() {
    if (selected === null || !draft.src.trim() || !draft.dst.trim()) return
    await GlossaryService.UpsertTerm(selected, draft as any)
    setDraft(emptyTerm())
    await refreshTerms(); await refreshGlossaries(selected)
  }
  async function editTerm(t: Term) { setDraft({ ...t }) }
  async function removeTerm(t: Term) {
    if (selected === null) return
    await GlossaryService.DeleteTerm(selected, t.id)
    await refreshTerms(); await refreshGlossaries(selected)
  }

  async function importCsv() {
    if (selected === null) return
    const r: any = await GlossaryService.ImportCSV(selected)
    setMsg(`${t('가져오기: 추가 ')}${r.added}${t(' · 갱신 ')}${r.updated}${t(' · 건너뜀 ')}${r.skipped}`)
    await refreshTerms(); await refreshGlossaries(selected)
  }
  async function exportCsv() {
    if (selected === null) return
    const name = (glossaries.find((g) => g.id === selected)?.name || 'glossary') + '.csv'
    await GlossaryService.ExportCSV(selected, name)
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal wide" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>{t('용어집 관리')}</h2>
          <button className="ghost sm" onClick={onClose}>{t('닫기')}</button>
        </div>

        <div className="gloss">
          <div className="gloss-list">
            <div className="gloss-list-head">
              <span>{t('용어집')}</span>
              <button className="ghost sm" onClick={createGlossary}>＋</button>
            </div>
            {glossaries.map((g) => (
              <div key={g.id} className={`gloss-item ${g.id === selected ? 'active' : ''}`}
                onClick={() => setSelected(g.id)}>
                <span className="gname">{g.name}</span>
                <span className="gcount">{g.term_count}</span>
              </div>
            ))}
            {glossaries.length === 0 && <div className="dim sm">{t('＋로 용어집을 만드세요')}</div>}
            {selected !== null && (
              <div className="gloss-actions">
                <button className="ghost sm" onClick={renameGlossary}>{t('이름변경')}</button>
                <button className="ghost sm" onClick={deleteGlossary}>{t('삭제')}</button>
              </div>
            )}
          </div>

          <div className="gloss-terms">
            {selected === null ? (
              <div className="empty sm">{t('용어집을 선택하세요')}</div>
            ) : (
              <>
                <div className="terms-toolbar">
                  <input placeholder={t('검색 (원어/역어)')} value={query} onChange={(e) => setQuery(e.target.value)} />
                  <span className="dim sm">{total}{t('개')}</span>
                  <div className="spacer" />
                  <button className="ghost sm" onClick={importCsv}>{t('CSV 가져오기')}</button>
                  <button className="ghost sm" onClick={exportCsv}>{t('CSV 내보내기')}</button>
                </div>

                <div className="term-form">
                  <input placeholder={t('원어 (예: attention)')} value={draft.src}
                    onChange={(e) => setDraft({ ...draft, src: e.target.value })} />
                  <span className="arrow">→</span>
                  <input placeholder={t('역어 (예: 어텐션)')} value={draft.dst}
                    onChange={(e) => setDraft({ ...draft, dst: e.target.value })} />
                  <input className="pri" type="number" title={t('우선순위')} value={draft.priority}
                    onChange={(e) => setDraft({ ...draft, priority: parseInt(e.target.value || '0') })} />
                  <label className="chk" title={t('대소문자 구분')}>
                    <input type="checkbox" checked={draft.case_sensitive}
                      onChange={(e) => setDraft({ ...draft, case_sensitive: e.target.checked })} />Aa
                  </label>
                  <button className="primary sm" onClick={saveTerm}>{draft.id ? t('수정') : t('추가')}</button>
                  {draft.id !== 0 && <button className="ghost sm" onClick={() => setDraft(emptyTerm())}>{t('취소')}</button>}
                </div>
                {msg && <div className="dim sm">{msg}</div>}

                <div className="term-table">
                  {terms.map((tm) => (
                    <div key={tm.id} className="term-row">
                      <span className="t-src">{tm.src}</span>
                      <span className="t-arrow">→</span>
                      <span className="t-dst">{tm.dst}</span>
                      {tm.priority !== 0 && <span className="t-pri">p{tm.priority}</span>}
                      {tm.case_sensitive && <span className="t-flag">Aa</span>}
                      <div className="spacer" />
                      <button className="link" onClick={() => editTerm(tm)}>{t('수정')}</button>
                      <button className="link danger" onClick={() => removeTerm(tm)}>{t('삭제')}</button>
                    </div>
                  ))}
                  {terms.length === 0 && <div className="dim sm">{t('용어가 없습니다. 위에서 추가하세요.')}</div>}
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

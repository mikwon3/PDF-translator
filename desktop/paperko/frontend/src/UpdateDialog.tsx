import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { UpdateService } from '../bindings/paperko/services'
import type { UpdateInfo } from './types'
import { useT, tr } from './i18n'

/**
 * New-version prompt. Offers, never forces: install now · open release page · skip
 * this version · later. Choosing install shows download progress; when it finishes
 * the app closes itself — macOS swaps in the new build and relaunches, Windows hands
 * off to the installer.
 */
export interface UpdateDialogProps {
  info: UpdateInfo
  onClose: () => void
}

const isMac =
  typeof navigator !== 'undefined' && /Mac/i.test(navigator.platform || navigator.userAgent)

function megabytes(n: number): string {
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

export default function UpdateDialog({ info, onClose }: UpdateDialogProps) {
  const { lang } = useT()
  const t = (ko: string) => tr(lang, ko)
  const [phase, setPhase] = useState<'ask' | 'downloading' | 'installing' | 'error'>('ask')
  const [done, setDone] = useState(0)
  const [total, setTotal] = useState(info.size)
  const [error, setError] = useState('')

  useEffect(
    () =>
      Events.On('update:progress', (e: { data: unknown }) => {
        const d = e.data as { done?: number; total?: number } | undefined
        if (typeof d?.done === 'number') setDone(d.done)
        if (typeof d?.total === 'number' && d.total > 0) setTotal(d.total)
      }),
    [],
  )

  const install = () => {
    setPhase('downloading')
    UpdateService.Install()
      .then(() => setPhase('installing'))
      .catch((e: unknown) => {
        setError((e as Error)?.message ?? String(e))
        setPhase('error')
      })
  }
  const openPage = () => void UpdateService.OpenPage(info.page).catch(() => {})
  const skip = () => void UpdateService.Skip(info.latest).finally(onClose)

  const busy = phase === 'downloading' || phase === 'installing'
  const percent = total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0

  return (
    <div className="modal-backdrop" onClick={busy ? undefined : onClose}>
      <div className="modal update" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head"><h2>{t('새 판이 나왔습니다')}</h2></div>
        <p>
          {t('PaperKo')} <b>{info.latest}</b> {t('을(를) 받을 수 있습니다. 지금 판은')} {info.current}{t('입니다.')}
          {info.published ? <span className="dim"> ({info.published.slice(0, 10)})</span> : null}
        </p>
        {info.notes && <div className="update-notes">{info.notes}</div>}

        {phase === 'ask' && info.canInstall && (
          <p className="dim sm">
            {isMac
              ? t('설치하면 앱이 잠깐 닫혔다가 새 판으로 다시 열립니다. 진행 중인 번역은 먼저 마치거나 저장하십시오.')
              : t('설치 프로그램이 열리고 앱은 닫힙니다(관리자 권한을 물을 수 있습니다). 진행 중인 번역은 먼저 마치거나 저장하십시오.')}
          </p>
        )}
        {phase === 'ask' && !info.canInstall && (
          <p className="dim sm">{t('이 컴퓨터에서는 자동 설치를 하지 않습니다. 릴리스 페이지에서 받아 설치하십시오.')}</p>
        )}

        {busy && (
          <div className="update-progress" role="progressbar" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}>
            <div className="bar"><div className="bar-fill" style={{ width: `${phase === 'installing' ? 100 : percent}%` }} /></div>
            <span className="dim sm">
              {phase === 'installing'
                ? t('설치를 시작했습니다. 앱이 곧 닫힙니다…')
                : `${t('받는 중…')} ${megabytes(done)} / ${megabytes(total)} (${percent}%)`}
            </span>
          </div>
        )}
        {phase === 'error' && <p className="err">⚠ {error}</p>}

        <div className="modal-actions">
          {phase === 'ask' && info.canInstall && (
            <button className="primary" onClick={install}>
              {t('지금 설치')}{info.size ? ` (${megabytes(info.size)})` : ''}
            </button>
          )}
          {!busy && <button className="ghost" onClick={openPage}>{t('릴리스 페이지 열기')}</button>}
          {phase === 'ask' && <button className="ghost" onClick={skip}>{t('이 판 건너뛰기')}</button>}
          {!busy && <button className="ghost" onClick={onClose}>{t('나중에')}</button>}
        </div>
      </div>
    </div>
  )
}

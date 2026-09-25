import { useEffect, useState } from 'react'
import { SettingsService } from '../bindings/paperko/services'
import { useT } from './i18n'

interface About {
  name: string
  version: string
  description: string
  author: string
  department: string
  organization: string
  year: string
  contact: string
  license: string
}

export default function AboutModal({ onClose }: { onClose: () => void }) {
  const { t } = useT()
  const [info, setInfo] = useState<About | null>(null)
  useEffect(() => { SettingsService.AppInfo().then((i) => setInfo(i as About)).catch(() => {}) }, [])

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal about" onClick={(e) => e.stopPropagation()}>
        <div className="about-hero">
          <div className="about-logo">
            <span className="en">A</span><span className="arrow">→</span><span className="ko">가</span>
          </div>
          <div className="about-name">{info?.name || 'PaperKo'}</div>
          {info?.version && <div className="about-ver">v{info.version}</div>}
          {info?.description && <div className="about-desc">{info.description}</div>}
        </div>

        <div className="about-rows">
          {info?.author && <div className="about-row"><span>{t('개발')}</span><b>{info.author}</b></div>}
          {info?.department && <div className="about-row"><span>{t('부서')}</span><b>{info.department}</b></div>}
          {info?.organization && <div className="about-row"><span>{t('소속')}</span><b>{info.organization}</b></div>}
          {info?.contact && <div className="about-row"><span>{t('문의')}</span><b>{info.contact}</b></div>}
          {info?.license && <div className="about-row"><span>{t('라이선스')}</span><b>{info.license}</b></div>}
          {(info?.year || info?.organization || info?.author) && (
            <div className="about-copy">© {info?.year} {info?.organization || info?.author}</div>
          )}
        </div>

        <div className="modal-actions">
          <button className="primary" onClick={onClose}>{t('닫기')}</button>
        </div>
      </div>
    </div>
  )
}

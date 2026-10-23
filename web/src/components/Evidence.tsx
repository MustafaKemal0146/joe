import { Eye, EyeOff, X } from 'lucide-react'
import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { dateTime } from '@/lib/fmt'
import { href } from '@/lib/router'
import type { EvidenceItem } from '@/lib/types'

interface Ctx {
  open: (id: string) => void
  hidden: boolean
  setHidden: (v: boolean) => void
}
const EvidenceCtx = createContext<Ctx>({ open: () => {}, hidden: false, setHidden: () => {} })

/**
 * Kanıt kimliklerini (K1…) tıklanabilir yapar. Çekmece, sayfa animasyonunun (transform) oluşturduğu
 * kapsayıcı bloğun dışına, document.body'ye taşınır: aksi hâlde `fixed` konumlandırma uzun sayfanın
 * tepesine göre hesaplanır ve çekmece kullanıcının göremediği yerde açılırdı.
 */
export function EvidenceProvider({ items, children }: { items: EvidenceItem[]; children: ReactNode }) {
  const [openId, setOpenId] = useState<string | null>(null)
  const [hidden, setHidden] = useState(false)
  const byId = useMemo(() => new Map(items.map((i) => [i.id, i])), [items])
  const value = useMemo(() => ({ open: setOpenId, hidden, setHidden }), [hidden])
  return (
    <EvidenceCtx.Provider value={value}>
      {children}
      {openId && createPortal(<Drawer id={openId} item={byId.get(openId)} onClose={() => setOpenId(null)} />, document.body)}
    </EvidenceCtx.Provider>
  )
}

function Drawer({ id, item, onClose }: { id: string; item?: EvidenceItem; onClose: () => void }) {
  const close = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    close.current?.focus()
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return (
    <div className="fixed inset-0 z-[100]" role="dialog" aria-modal aria-label={`Kanıt ${id}`}>
      <div className="absolute inset-0 bg-black/40 backdrop-blur-sm" onClick={onClose} />
      <aside className="absolute inset-y-0 right-0 flex w-full max-w-md flex-col border-l border-line bg-surface shadow-2xl">
        <header className="flex items-start gap-3 border-b border-line px-5 py-4">
          <div className="min-w-0 flex-1">
            <div className="text-[12px] font-semibold uppercase tracking-wider text-accent">{id}</div>
            <h2 className="text-[16px] font-semibold leading-snug tracking-tight">{item?.title ?? 'Pakette bulunamadı'}</h2>
            {item && <p className="mt-1 text-[12.5px] text-muted">{item.why}</p>}
          </div>
          <button ref={close} onClick={onClose} aria-label="Kapat" title="Kapat (Esc)"
            className="grid size-9 shrink-0 place-items-center rounded-full bg-raised text-fg hover:bg-sunken">
            <X className="size-5" />
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {item ? (
            <>
              <ol className="space-y-1.5 text-[13px]">
                {item.messages.map((m) => (
                  <li key={m.id} className="rounded-xl bg-raised px-3 py-2">
                    <div className="text-[11.5px] text-faint">{m.sender} · {dateTime(m.ts)}</div>
                    <div className="whitespace-pre-wrap break-words">{m.text || '—'}</div>
                  </li>
                ))}
              </ol>
              <a className="mt-4 inline-block text-[13px] text-accent" href={href(`/sohbet/${item.conv_id}?mesaj=${item.messages[0]?.id ?? ''}`)} onClick={onClose}>Sohbette bağlamıyla oku →</a>
            </>
          ) : <p className="text-[13px] text-bad">Bu kimlik pakette yok; model kanıt uydurmuş olabilir.</p>}
        </div>
      </aside>
    </div>
  )
}

const VISIBLE = 2

/** Bir iddiaya bağlı kanıt kimlikleri: ilk ikisi görünür, kalanı "+N" ile açılır; sessiz ve küçük. */
export function Cites({ ids }: { ids?: string[] | null }) {
  const { open, hidden } = useContext(EvidenceCtx)
  const [all, setAll] = useState(false)
  if (!ids?.length || hidden) return null
  const unique = [...new Set(ids)]
  const shown = all ? unique : unique.slice(0, VISIBLE)
  return (
    <span className="ml-1 inline-flex flex-wrap items-center gap-0.5 align-baseline">
      {shown.map((id) => (
        <button key={id} onClick={() => open(id)} title={`${id} kanıtını aç`}
          className="rounded px-1 py-px font-mono text-[10.5px] font-semibold leading-none text-accent/90 ring-1 ring-accent/25 hover:bg-accent-soft">{id}</button>
      ))}
      {unique.length > VISIBLE && (
        <button onClick={() => setAll(!all)} className="px-1 text-[10.5px] text-faint hover:text-fg" aria-label={all ? 'Kanıtları daralt' : `${unique.length - VISIBLE} kanıt daha`}>
          {all ? '−' : `+${unique.length - VISIBLE}`}
        </button>
      )}
    </span>
  )
}

/** Sayfadaki tüm kanıt rozetlerini gizleyip gösteren düğme: uzun çıktılarda okumayı rahatlatır. */
export function CiteToggle() {
  const { hidden, setHidden } = useContext(EvidenceCtx)
  return (
    <button onClick={() => setHidden(!hidden)} className="inline-flex items-center gap-1.5 rounded-full bg-raised px-3 py-1.5 text-[12.5px] text-muted hover:text-fg" aria-pressed={hidden}>
      {hidden ? <Eye className="size-3.5" /> : <EyeOff className="size-3.5" />} {hidden ? 'Kanıt rozetlerini göster' : 'Kanıt rozetlerini gizle'}
    </button>
  )
}

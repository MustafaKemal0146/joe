import { AlertTriangle, Trash2 } from 'lucide-react'
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { cn } from '@/lib/cn'

export interface ConfirmOptions {
  title: string
  /** Satır sonları korunur; boş satır paragraf ayırır. */
  message?: string
  confirmLabel?: string
  cancelLabel?: string
  /** danger: silme gibi geri alınamaz işlemler (kırmızı); varsayılan danger. */
  tone?: 'danger' | 'default'
}

type Ask = (o: ConfirmOptions) => Promise<boolean>
const ConfirmCtx = createContext<Ask>(() => Promise.resolve(false))

/** Tarayıcının yerel onay penceresi yerine, tema uyumlu uygulama içi onay kutusu: `await confirm({...})`. */
export const useConfirm = () => useContext(ConfirmCtx)

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [opts, setOpts] = useState<ConfirmOptions | null>(null)
  const resolver = useRef<((v: boolean) => void) | null>(null)

  const ask = useCallback<Ask>((o) => new Promise<boolean>((resolve) => {
    resolver.current?.(false) // açık bir kutu varsa reddedilmiş sayılır
    resolver.current = resolve
    setOpts(o)
  }), [])

  const close = useCallback((v: boolean) => {
    resolver.current?.(v)
    resolver.current = null
    setOpts(null)
  }, [])

  return (
    <ConfirmCtx.Provider value={ask}>
      {children}
      {opts && createPortal(<Dialog o={opts} onClose={close} />, document.body)}
    </ConfirmCtx.Provider>
  )
}

function Dialog({ o, onClose }: { o: ConfirmOptions; onClose: (v: boolean) => void }) {
  const danger = (o.tone ?? 'danger') === 'danger'
  const cancel = useRef<HTMLButtonElement>(null)
  const okBtn = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    // Geri alınamaz işlemde odak "Vazgeç" üzerindedir: yanlışlıkla Enter'a basmak silmesin.
    ;(danger ? cancel : okBtn).current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.preventDefault(); onClose(false) }
      if (e.key === 'Tab') { // odak kutunun içinde kalır
        const els = [cancel.current, okBtn.current].filter(Boolean) as HTMLElement[]
        const i = els.indexOf(document.activeElement as HTMLElement)
        e.preventDefault()
        els[(i + (e.shiftKey ? els.length - 1 : 1)) % els.length]?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [danger, onClose])

  return (
    <div className="fixed inset-0 z-[200] grid place-items-center p-4" role="alertdialog" aria-modal aria-labelledby="confirm-title" aria-describedby={o.message ? 'confirm-msg' : undefined}>
      <div className="pop-fade absolute inset-0 bg-black/45 backdrop-blur-sm" onClick={() => onClose(false)} />
      <div className="pop relative w-full max-w-md overflow-hidden rounded-3xl border border-line bg-surface shadow-2xl">
        <div className="px-6 pb-2 pt-6 text-center">
          <div className={cn('mx-auto mb-4 grid size-14 place-items-center rounded-full', danger ? 'bg-bad/12 text-bad' : 'bg-accent-soft text-accent')}>
            {danger ? <Trash2 className="size-6" /> : <AlertTriangle className="size-6" />}
          </div>
          <h2 id="confirm-title" className="text-[18px] font-semibold tracking-tight">{o.title}</h2>
          {o.message && (
            <div id="confirm-msg" className="mt-2 space-y-2 text-left text-[13.5px] leading-relaxed text-muted">
              {o.message.split(/\n{2,}/).map((para, i) => <p key={i} className="whitespace-pre-line">{para}</p>)}
            </div>
          )}
        </div>
        <div className="flex gap-3 px-6 pb-6 pt-4">
          <button ref={cancel} onClick={() => onClose(false)}
            className="h-11 flex-1 rounded-2xl border border-line bg-raised text-[14px] font-medium transition hover:bg-sunken focus-visible:outline-2 focus-visible:outline-accent">
            {o.cancelLabel ?? 'Vazgeç'}
          </button>
          <button ref={okBtn} onClick={() => onClose(true)}
            className={cn('h-11 flex-1 rounded-2xl text-[14px] font-semibold text-white transition focus-visible:outline-2 focus-visible:outline-offset-2',
              danger ? 'bg-bad hover:opacity-90 focus-visible:outline-bad' : 'bg-accent hover:opacity-90 focus-visible:outline-accent')}>
            {o.confirmLabel ?? (danger ? 'Sil' : 'Onayla')}
          </button>
        </div>
      </div>
    </div>
  )
}

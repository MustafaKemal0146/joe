import { MessageCircle, Maximize2, X } from 'lucide-react'
import { useState } from 'react'
import { api } from '@/lib/api'
import { go } from '@/lib/router'
import { Conversation } from '@/pages/JoePage'

/** Her sayfada erişilebilen küçük Joe paneli. Son sohbeti açar; yoksa ilk açılışta yeni sohbet oluşturur. */
export default function JoeDock() {
  const [open, setOpen] = useState(false)
  const [id, setId] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)

  async function toggle() {
    if (open) { setOpen(false); return }
    setOpen(true)
    if (id) return
    try {
      const list = await api.chats()
      setId(list[0]?.id ?? (await api.createChat()).id)
    } catch (e) { setErr(e instanceof Error ? e.message : 'Sohbet açılamadı') }
  }

  return (
    <>
      {open && (
        <div className="fixed bottom-24 right-4 z-[60] flex h-[min(620px,70dvh)] w-[min(400px,calc(100vw-2rem))] flex-col overflow-hidden rounded-2xl border border-line bg-surface shadow-2xl" role="dialog" aria-label="Joe">
          <div className="flex items-center gap-2 border-b border-line px-3 py-2 text-[13px] font-semibold">
            <span className="flex-1">Joe</span>
            {id && <button aria-label="Tam ekranda aç" title="Tam ekranda aç" className="rounded-lg p-1.5 text-muted hover:bg-raised" onClick={() => { setOpen(false); go(`/joe/${id}`) }}><Maximize2 className="size-4" /></button>}
            <button aria-label="Paneli kapat" className="rounded-lg p-1.5 text-muted hover:bg-raised" onClick={() => setOpen(false)}><X className="size-4" /></button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-3">
            {err ? <p className="text-[13px] text-bad">{err}</p> : id ? <Conversation id={id} onTitle={() => {}} /> : <p className="text-[13px] text-muted">Açılıyor…</p>}
          </div>
        </div>
      )}
      <button aria-label="Joe ile konuş" title="Joe ile konuş" onClick={() => void toggle()}
        className="fixed bottom-12 right-4 z-[60] grid size-12 place-items-center rounded-full bg-accent text-accent-fg shadow-xl hover:opacity-90">
        <MessageCircle className="size-5" />
      </button>
    </>
  )
}

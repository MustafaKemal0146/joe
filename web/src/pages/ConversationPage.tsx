import { X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Badge, Button, Empty, ErrorNote, Spinner } from '@/components/ui'
import { ApiError } from '@/lib/api'
import { dateTime, num } from '@/lib/fmt'
import { Highlights } from '@/pages/GuidePage'
import { cn } from '@/lib/cn'
import { go } from '@/lib/router'

interface Msg { id: number; sender: string; ts: string; kind: string; text: string; reactions?: { actor: string; emoji: string }[]; meta?: Record<string, unknown> }

const PAGE = 300
const label = (m: Msg): string => {
  switch (m.kind) {
    case 'media': return `[${({ photo: 'fotoğraf', video: 'video', audio: 'ses', gif: 'gif', sticker: 'çıkartma' } as Record<string, string>)[String(m.meta?.media)] ?? 'medya'}]`
    case 'unsent': return '[mesaj geri alındı]'
    case 'deleted': return '[mesaj silindi]'
    case 'missed_call': return '[cevapsız arama]'
    case 'call': return `[arama · ${m.meta?.seconds ?? 0} sn]`
    case 'share': return m.text || '[paylaşım]'
    default: return m.text
  }
}

/** Sağ üstte sabit kapat düğmesi: geldiğin sayfaya (kişi dosyası) döner; geçmiş yoksa kişiler listesine. */
function CloseButton() {
  const close = () => { if (window.history.length > 1) window.history.back(); else go('/kisiler') }
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') close() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  return createPortal(
    <button aria-label="Kapat" title="Kapat (Esc)" onClick={close}
      className="fixed right-4 top-4 z-[90] grid size-10 place-items-center rounded-full border border-line bg-surface shadow-lg hover:bg-raised">
      <X className="size-5" />
    </button>, document.body)
}

export default function ConversationPage({ id, focus, person = 0 }: { id: number; focus: number; person?: number }) {
  const [msgs, setMsgs] = useState<Msg[]>([])
  const [start, setStart] = useState(0) // msgs[0]'ın sohbet içindeki sırası
  const [showHl, setShowHl] = useState(false)
  const [more, setMore] = useState(true)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [self, setSelf] = useState<string | null>(null)
  const focusRef = useRef<HTMLDivElement>(null)

  /** around verilirse o mesajı ortalayan bir pencere, yoksa offset'ten itibaren sayfa yüklenir. */
  async function load(opts: { offset?: number; around?: number; prepend?: boolean; limit?: number }) {
    setLoading(true)
    try {
      const { rows, offset } = await fetchPage(id, opts)
      if (opts.prepend) { setMsgs((m) => [...rows, ...m]); setStart(offset) }
      else if (opts.offset !== undefined && opts.offset > 0 && !opts.around) { setMsgs((m) => [...m, ...rows]) }
      else { setMsgs(rows); setStart(offset) }
      if (!opts.prepend) setMore(rows.length === PAGE)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Sohbet okunamadı')
    } finally { setLoading(false) }
  }

  useEffect(() => { void load(focus ? { around: focus } : { offset: 0 }) }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  // Aynı sohbette başka bir ana atlanırsa: pencerede varsa kaydır, yoksa o anı ortalayan pencereyi yükle.
  useEffect(() => {
    if (!focus || loading) return
    if (!msgs.some((m) => m.id === focus)) void load({ around: focus })
  }, [focus]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { focusRef.current?.scrollIntoView({ block: 'center' }) }, [msgs.length, focus, start])

  // Mesajların ilk göndereni "ben" tarafı olarak sağa dizilir.
  const left = self ?? msgs.find((m) => m.sender)?.sender ?? ''
  const senders = [...new Set(msgs.map((m) => m.sender).filter(Boolean))]

  if (error) return <><CloseButton /><ErrorNote>{error}</ErrorNote></>
  if (!loading && msgs.length === 0) return <><CloseButton /><Empty title="Bu sohbette mesaj yok" /></>
  return (
    <>
      <CloseButton />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <h1 className="text-[22px] font-semibold tracking-tight">Sohbet</h1>
        {person > 0 && <Button size="sm" onClick={() => setShowHl((v) => !v)}>{showHl ? 'Önemli anları gizle' : 'Önemli anlar'}</Button>}
        {senders.length > 1 && senders.length <= 4 && (
          <div className="ml-auto flex items-center gap-2 text-[12.5px] text-muted">Solda göster:
            {senders.map((s) => <button key={s} onClick={() => setSelf(s)} className={cn('rounded-full px-2.5 py-1', s === left ? 'bg-accent text-accent-fg' : 'bg-raised')}>{s}</button>)}
          </div>
        )}
      </div>
      {showHl && person > 0 && <div className="mb-4"><Highlights convId={id} personId={person} /></div>}
      {start > 0 && (
        <div className="mb-4 flex justify-center">{loading ? <Spinner /> : <Button onClick={() => void load({ offset: Math.max(0, start - PAGE), limit: start - Math.max(0, start - PAGE), prepend: true })}>Öncekileri yükle ({num(start)} mesaj daha)</Button>}</div>
      )}
      <div className="space-y-1.5">
        {msgs.map((m, i) => {
          const mine = m.sender === left
          const day = m.ts.slice(0, 10)
          const newDay = i === 0 || msgs[i - 1]!.ts.slice(0, 10) !== day
          const system = m.kind === 'system'
          return (
            <div key={m.id}>
              {newDay && <div className="my-4 text-center text-[12px] text-faint">{dateTime(m.ts).split(' ').slice(0, 3).join(' ')}</div>}
              {system ? <div className="text-center text-[12px] text-faint">{m.text}</div> : (
                <div ref={m.id === focus ? focusRef : undefined} className={cn('flex', mine ? 'justify-start' : 'justify-end')}>
                  <div className={cn('max-w-[78%] rounded-2xl px-3.5 py-2 text-[14px]', mine ? 'bg-raised' : 'bg-accent text-accent-fg', m.id === focus && 'ring-2 ring-warn')} title={`${m.sender} · ${dateTime(m.ts)}`}>
                    <div className={cn('whitespace-pre-wrap break-words', m.kind !== 'text' && 'italic opacity-80')}>{label(m)}</div>
                    <div className={cn('mt-0.5 text-[11px]', mine ? 'text-faint' : 'text-accent-fg/70')}>{m.sender} · {dateTime(m.ts).split(' ').pop()}{m.reactions?.length ? ` · ${m.reactions.map((r) => r.emoji).join('')}` : ''}</div>
                  </div>
                </div>
              )}
            </div>
          )
        })}
      </div>
      <div className="mt-6 flex justify-center">
        {loading ? <Spinner /> : more ? <Button onClick={() => void load({ offset: start + msgs.length })}>Daha fazla yükle</Button> : <Badge>sohbetin sonu</Badge>}
      </div>
    </>
  )
}

async function fetchPage(id: number, o: { offset?: number; around?: number; prepend?: boolean; limit?: number }) {
  const limit = o.limit ?? PAGE
  const q = o.around ? `limit=${limit}&around=${o.around}` : `limit=${limit}&offset=${o.offset ?? 0}`
  const res = await fetch(`/api/conversations/${id}/messages?${q}`)
  if (!res.ok) throw new ApiError(((await res.json().catch(() => ({}))) as { error?: string }).error ?? `HTTP ${res.status}`, res.status)
  const offset = Number(res.headers.get('X-Offset') ?? o.offset ?? 0)
  return { rows: (await res.json()) as Msg[], offset }
}

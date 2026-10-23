import { useConfirm } from '@/components/Confirm'
import { ArrowUp, Check, Loader2, MessageSquarePlus, Trash2, Wand2, X } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import Onboarding from '@/components/Onboarding'
import { Badge, Button, Card, Spinner } from '@/components/ui'
import { api } from '@/lib/api'
import { cn } from '@/lib/cn'
import { useAsync, useData } from '@/lib/data'
import { dateShort, usd } from '@/lib/fmt'
import { go, href } from '@/lib/router'
import type { ChatAction, ChatDetail, ChatMessage } from '@/lib/types'

const SUGGEST = ['kişiler', 'kaynaklar', 'harcama', 'yardım']

export default function JoePage({ id }: { id?: string }) {
  const list = useAsync(() => api.chats(), [id])
  const { toast, sources, loaded } = useData()
  const confirm = useConfirm()
  const creating = useRef(false)
  // Hiç veri yokken sihirbaz kendiliğinden açılır; sonradan düğmeyle yeniden açılır. Sohbete dosya bırakmak da açar.
  const [wizard, setWizard] = useState(false)
  const auto = useRef(false)
  useEffect(() => {
    const check = () => { if (window.location.hash.includes('sihirbaz=1')) { setDropped([]); setWizard(true) } }
    check()
    window.addEventListener('hashchange', check)
    return () => window.removeEventListener('hashchange', check)
  }, [])
  useEffect(() => { if (loaded && !auto.current) { auto.current = true; if (sources.length === 0) setWizard(true) } }, [loaded, sources.length])
  const [dropped, setDropped] = useState<File[]>([])

  async function create() {
    if (creating.current) return // StrictMode çift çalıştırmasında ikinci sohbet açılmasın
    creating.current = true
    try { const c = await api.createChat(); go(`/joe/${c.id}`) } catch (e) { toast(e instanceof Error ? e.message : 'Açılamadı', 'bad') } finally { creating.current = false }
  }
  // /joe → en son sohbet ya da yenisi
  useEffect(() => {
    if (id || !list.data) return
    if (list.data.length > 0) go(`/joe/${list.data[0]!.id}`)
    else void create()
  }, [id, list.data]) // eslint-disable-line react-hooks/exhaustive-deps

  if (!id) return <Spinner />
  return (
    <div className="grid gap-4 lg:grid-cols-[220px_1fr]"
      onDragOver={(e) => { if (e.dataTransfer.types.includes('Files')) e.preventDefault() }}
      onDrop={(e) => { if (e.dataTransfer.files.length) { e.preventDefault(); setDropped([...e.dataTransfer.files]); setWizard(true) } }}>
      <aside className="space-y-2">
        <Button className="w-full" onClick={create}><MessageSquarePlus className="size-4" /> Yeni sohbet</Button>
        <Button className="w-full" onClick={() => { setDropped([]); setWizard(true) }}><Wand2 className="size-4" /> Kurulum sihirbazı</Button>
        <ul className="space-y-1">
          {(list.data ?? []).map((c) => (
            <li key={c.id} className="group flex items-center">
              <a href={href(`/joe/${c.id}`)} className={cn('min-w-0 flex-1 rounded-xl px-3 py-2 text-[13px]', c.id === id ? 'bg-raised font-medium' : 'text-muted hover:bg-raised')}>
                <div className="truncate">{c.title}</div>
                <div className="text-[11.5px] text-faint">{dateShort(c.updated_at)} · {usd(c.spent_usd)}</div>
              </a>
              <button aria-label="Sohbeti sil" className="p-1.5 text-faint opacity-0 hover:text-bad group-hover:opacity-100" onClick={async () => {
                if (!await confirm({ title: 'Bu sohbet silinsin mi?', message: 'Sohbetin tüm mesajları silinir.', confirmLabel: 'Sohbeti sil' })) return
                try { await api.deleteChat(c.id); if (c.id === id) go('/joe'); else list.setData((list.data ?? []).filter((x) => x.id !== c.id)) } catch (e) { toast(e instanceof Error ? e.message : 'Silinemedi', 'bad') }
              }}><Trash2 className="size-3.5" /></button>
            </li>
          ))}
        </ul>
      </aside>
      <div className="min-w-0">
        {wizard && <Onboarding key={dropped.map((f) => f.name + f.size).join('|')} initialFiles={dropped} onClose={() => { setWizard(false); setDropped([]) }} />}
        <Conversation id={id} key={id} onTitle={() => void list.setData(list.data)} />
      </div>
    </div>
  )
}

export function Conversation({ id }: { id: string; onTitle: () => void }) {
  const { toast, reload } = useData()
  const [detail, setDetail] = useState<ChatDetail | null>(null)
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const end = useRef<HTMLDivElement>(null)

  const load = useCallback(async () => {
    try { setDetail(await api.chat(id)); setError(null) } catch (e) { setError(e instanceof Error ? e.message : 'Sohbet okunamadı') }
  }, [id])
  useEffect(() => { void load() }, [load])
  useEffect(() => { end.current?.scrollIntoView({ block: 'end' }) }, [detail?.messages.length, busy])
  // Yanıt gelirken sunucu her adımı (araç çağrısı vb.) kaydeder; ilerlemeyi 1 sn'de bir okuyup durum satırında gösteririz.
  useEffect(() => {
    if (!busy) return
    const t = setInterval(() => { api.chat(id).then((d) => setDetail((cur) => (cur && d.messages.length >= cur.messages.filter((m) => m.id !== -1).length ? d : cur))).catch(() => {}) }, 1000)
    return () => clearInterval(t)
  }, [busy, id])

  async function send(t: string) {
    const v = t.trim()
    if (!v || busy) return
    setBusy(true)
    setText('')
    // iyimser: kullanıcı mesajı hemen görünsün
    setDetail((d) => d && { ...d, messages: [...d.messages, { id: -1, session_id: id, role: 'user', content: v, cost_usd: 0, created_at: new Date().toISOString() }] })
    try { await api.sendChat(id, v) } catch (e) { toast(e instanceof Error ? e.message : 'Gönderilemedi', 'bad') } finally { await load(); setBusy(false) }
  }

  async function act(a: ChatAction, ok: boolean) {
    try { setDetail(ok ? await api.confirmAction(id, a.id) : await api.rejectAction(id, a.id)); if (ok) await reload() } catch (e) { toast(e instanceof Error ? e.message : 'İşlem başarısız', 'bad'); await load() }
  }

  if (error && !detail) return <Card className="p-6 text-[14px] text-bad">{error}</Card>
  if (!detail) return <Spinner />
  const actions = new Map(detail.actions.map((a) => [a.id, a]))
  const spent = detail.session.spent_usd

  return (
    <div className="flex min-h-[70vh] flex-col">
      <div className="mb-3 flex items-center gap-2 text-[12.5px] text-muted">
        <span className="min-w-0 flex-1 truncate font-medium text-fg">{detail.session.title}</span>
        <Badge>{usd(spent)} / {usd(detail.cap_usd)}</Badge>
      </div>
      <Card className="flex-1 space-y-3 p-4">
        {detail.messages.length === 0 && (
          <div className="py-10 text-center">
            <h2 className="text-[22px] font-semibold tracking-tight">Merhaba, ben Joe.</h2>
            <p className="mx-auto mt-2 max-w-md text-[14px] text-muted">Kimi anlamak istiyorsun? Kişileri, kaynakları ve harcamayı sorabilir, kişi dosyası açtırabilir, konsey analizini hazırlatabilirsin. Önemli hiçbir şey onayın olmadan yapılmaz.</p>
          </div>
        )}
        {detail.messages.map((m, i) => <Bubble key={m.id + ':' + i} m={m} actions={actions} onAct={act} />)}
        {busy && <div className="flex items-center gap-2 text-[13px] text-muted"><Loader2 className="spin size-4" /> {status(detail.messages)}</div>}
        <div ref={end} />
      </Card>
      <div className="mt-3 flex flex-wrap gap-1.5">
        {SUGGEST.map((s) => <button key={s} onClick={() => void send(s)} disabled={busy} className="rounded-full bg-raised px-3 py-1 text-[12.5px] text-muted hover:text-fg disabled:opacity-50">{s}</button>)}
      </div>
      <form className="mt-2 flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); void send(text) }}>
        <textarea value={text} onChange={(e) => setText(e.target.value)} rows={1} maxLength={4000} placeholder="Joe'ya yaz…" aria-label="Mesaj"
          onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void send(text) } }}
          className="max-h-40 min-h-11 flex-1 resize-none rounded-2xl border border-line bg-raised px-4 py-3 text-[14px] outline-none focus:border-accent focus:bg-surface" />
        <Button type="submit" variant="primary" disabled={!text.trim() || busy} aria-label="Gönder" className="size-11 !px-0"><ArrowUp className="size-5" /></Button>
      </form>
    </div>
  )
}

/** Son araç adımından canlı durum yazısı üretir. */
function status(msgs: ChatMessage[]): string {
  const last = msgs[msgs.length - 1]
  if (last?.role === 'tool') return `${last.meta?.tool ?? 'araç'} çalıştı, yanıt yazılıyor…`
  return 'düşünüyor…'
}

function Bubble({ m, actions, onAct }: { m: ChatMessage; actions: Map<string, ChatAction>; onAct: (a: ChatAction, ok: boolean) => void }) {
  if (m.role === 'tool') return <div className="text-center text-[11.5px] text-faint">araç çalıştı: {m.meta?.tool}</div>
  if (m.role === 'notice') return <div className="rounded-xl bg-raised px-3 py-2 text-center text-[12.5px] text-muted">{m.content}</div>
  const mine = m.role === 'user'
  return (
    <div className={cn('flex', mine ? 'justify-end' : 'justify-start')}>
      <div className={cn('max-w-[85%] space-y-2', mine && 'items-end')}>
        {m.content && (
          <div className={cn('whitespace-pre-wrap break-words rounded-2xl px-3.5 py-2.5 text-[14px]', mine ? 'bg-accent text-accent-fg' : 'bg-raised', m.meta?.safety && 'border border-warn/40')}>{m.content}</div>
        )}
        {(m.meta?.cards ?? []).map((c, i) => c.type === 'nav' ? (
          <a key={i} href={href(c.to ?? '/')} className="inline-flex rounded-full border border-accent/40 px-3 py-1 text-[13px] text-accent hover:bg-accent-soft">{c.label} →</a>
        ) : <ActionCard key={i} card={c} a={actions.get(c.action_id ?? '')} onAct={onAct} />)}
        {!mine && m.cost_usd > 0 && <div className="text-[11px] text-faint">{usd(m.cost_usd)}</div>}
      </div>
    </div>
  )
}

function ActionCard({ card, a, onAct }: { card: { summary?: string; danger?: boolean }; a?: ChatAction; onAct: (a: ChatAction, ok: boolean) => void }) {
  const danger = card.danger
  const done = a && a.status !== 'pending'
  return (
    <div className={cn('rounded-2xl border p-3 text-[13px]', danger ? 'border-bad/50 bg-bad/8' : 'border-line bg-surface')}>
      <div className={cn('mb-1 text-[11.5px] font-semibold uppercase tracking-wide', danger ? 'text-bad' : 'text-muted')}>{danger ? 'Geri alınamaz işlem — onayın gerekir' : 'Onayın gerekir'}</div>
      <p>{card.summary}</p>
      {a && !done && (
        <div className="mt-2.5 flex gap-2">
          <Button size="sm" variant={danger ? 'danger' : 'primary'} onClick={() => onAct(a, true)}><Check className="size-3.5" /> Onayla</Button>
          <Button size="sm" onClick={() => onAct(a, false)}><X className="size-3.5" /> Reddet</Button>
        </div>
      )}
      {a && done && <div className="mt-2 text-[12px] text-muted">{a.status === 'done' ? '✓ Yapıldı' : a.status === 'rejected' ? '✗ Reddedildi' : a.status === 'failed' ? `Başarısız: ${a.result}` : 'İşleniyor…'}</div>}
    </div>
  )
}

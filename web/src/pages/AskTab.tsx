import { Loader2, MessageSquareReply, Send, X } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { EvidenceProvider } from '@/components/Evidence'
import { Badge, Button, Card, ErrorNote, Spinner } from '@/components/ui'
import { api } from '@/lib/api'
import { parseMention, shown } from '@/lib/ask'
import { useData } from '@/lib/data'
import { usd } from '@/lib/fmt'
import type { CouncilDetail, CouncilPlan, Person, PersonaInfo } from '@/lib/types'
import { AnswerCard } from './CouncilSessionPage'

const MAX_DEBATE = 4

interface Job { body: Record<string, unknown>; plan: CouncilPlan }

export default function AskTab({ person }: { person: Person }) {
  const { toast } = useData()
  const [thread, setThread] = useState<CouncilDetail[] | null>(null)
  const [personas, setPersonas] = useState<PersonaInfo[]>([])
  const [moderatorQ, setModeratorQ] = useState('')
  const [text, setText] = useState('')
  const [picked, setPicked] = useState<string>('')
  const [debate, setDebate] = useState(false)
  const [panel, setPanel] = useState<string[]>([])
  const [consent, setConsent] = useState(false)
  const [follow, setFollow] = useState(true)
  const [target, setTarget] = useState<string | null>(null) // itiraz edilen oturum
  const [jobs, setJobs] = useState<Job[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const end = useRef<HTMLDivElement>(null)

  const load = useCallback(async () => {
    const rows = (await api.councils()).filter((c) => c.kind === 'ask' && c.subject_ids.length === 1 && c.subject_ids[0] === person.id).slice(0, 12).reverse()
    setThread(await Promise.all(rows.map((r) => api.council(r.id))))
  }, [person.id])
  useEffect(() => { void load().catch((e) => setError(e instanceof Error ? e.message : 'Okunamadı')) }, [load])
  useEffect(() => { api.personas().then((r) => { setPersonas(r.personas); setModeratorQ(r.moderator_question) }).catch(() => {}) }, [])
  useEffect(() => { end.current?.scrollIntoView({ behavior: 'smooth', block: 'end' }) }, [thread?.length])

  const mention = useMemo(() => parseMention(text, personas), [text, personas])
  const persona = mention.persona?.id ?? picked
  const last = thread?.[thread.length - 1]
  const followId = target ?? (follow && last ? last.session.id : undefined)
  const base = useMemo(() => ({ kind: 'ask', subject_id: person.id, consent: person.is_self || consent }), [person, consent])
  const ready = mention.question.length >= 3 && (person.is_self || consent) && (!debate || panel.length >= 2)

  // Soru ya da seçenek değişince eski maliyet onayı geçersizdir.
  useEffect(() => { setJobs(null) }, [text, picked, debate, panel, consent, follow, target])

  const bodies = (): Record<string, unknown>[] => debate
    ? panel.map((p) => ({ ...base, question: mention.question, persona_ids: [p] }))
    : [{ ...base, question: mention.question, ...(persona ? { persona_ids: [persona] } : {}), ...(followId ? { follow_up_of: followId } : {}) }]

  async function calc(list = bodies()) {
    setBusy(true); setError(null)
    try { setJobs(await Promise.all(list.map(async (body) => ({ body, plan: await api.councilPlan(body) })))) } catch (e) { setError(e instanceof Error ? e.message : 'Hesaplanamadı') } finally { setBusy(false) }
  }
  async function send() {
    if (!jobs || jobs.some((j) => !j.plan.allowed)) return
    setBusy(true); setError(null)
    try {
      const ids = await Promise.all(jobs.map((j) => api.councilStart({ ...j.body, approval_id: j.plan.approval_id }).then((r) => r.id)))
      setText(''); setJobs(null); setTarget(null)
      for (let i = 0; i < 120; i++) {
        const ds = await Promise.all(ids.map((id) => api.council(id)))
        if (ds.every((d) => !d.running && !['queued', 'running'].includes(d.session.status))) break
        await new Promise((ok) => setTimeout(ok, 1500))
      }
      await load()
    } catch (e) { setError(e instanceof Error ? e.message : 'Gönderilemedi'); setJobs(null); toast('Soru gönderilemedi.', 'bad') } finally { setBusy(false) }
  }
  /** Son turdaki mercek yanıtlarını karşılaştıran moderatör özeti (ayrı bir maliyetle, onaylı). */
  async function moderate(ids: string[]) {
    await calc([{ ...base, question: moderatorQ, context_of: ids }])
  }
  function object(d: CouncilDetail) {
    const p = d.session.persona_ids[0]
    setTarget(d.session.id); setDebate(false)
    if (p) setText(`@${p} `)
    document.getElementById('ask-box')?.focus()
  }

  if (!thread) return error ? <ErrorNote>{error}</ErrorNote> : <Spinner />
  // Moderatör özeti, art arda gelen tek-mercek yanıtlarının sonunda sunulur.
  const tail: CouncilDetail[] = []
  for (let i = thread.length - 1; i >= 0 && thread[i]!.session.persona_ids.length === 1 && !shown(thread[i]!.session.question).startsWith(moderatorQ.slice(0, 20)); i--) tail.unshift(thread[i]!)
  const tailPersonas = new Set(tail.map((d) => d.session.persona_ids[0]))
  const canModerate = tail.length >= 2 && tailPersonas.size >= 2 && tail.every((d) => d.outputs.some((o) => o.stage === 'answer' && o.status === 'done'))
  const total = jobs?.reduce((a, j) => a + j.plan.estimate.high_usd, 0) ?? 0
  const lowTotal = jobs?.reduce((a, j) => a + j.plan.estimate.low_usd, 0) ?? 0
  const blocked = jobs?.find((j) => !j.plan.allowed)

  return (
    <div className="space-y-4">
      <Card className="p-4 text-[13px] text-muted">
        {person.name} dosyası hakkında serbestçe sor. Yanıt yalnızca metriklere ve K kanıtlarına dayanır; veri yoksa "veri yetersiz" der. Tek bir kuramsal merceğe sormak için <code className="rounded bg-raised px-1">@jung</code> gibi başla; <b>tartışma modunda</b> aynı soruyu birkaç merceğe birden sorar, sonra aralarındaki ayrışmayı özetletebilirsin. Her çağrı göndermeden önce maliyetiyle gösterilir.
      </Card>
      {thread.length === 0 && <p className="py-6 text-center text-[13.5px] text-muted">Henüz soru yok. Örnek: "Geceleri neden daha çok yazıyor?" ya da "@klein ilişkide neler değişti?"</p>}
      {thread.map((d) => {
        const isMod = shown(d.session.question).startsWith(moderatorQ.slice(0, 20))
        const p = d.session.persona_ids.length === 1 ? d.session.persona_ids[0] : ''
        return (
          <EvidenceProvider key={d.session.id} items={d.session.pack?.items ?? []}>
            <div className="ml-auto max-w-[85%] rounded-2xl bg-accent px-4 py-2.5 text-[14px] text-white">
              {p && <Badge tone="neutral">@{p}</Badge>} {isMod ? 'Mercek yanıtları arasındaki uzlaşı ve ayrışmaları özetle' : shown(d.session.question)}
            </div>
            <div className="mr-auto max-w-full">
              {d.outputs.find((o) => o.stage === 'answer')
                ? <AnswerCard o={d.outputs.find((o) => o.stage === 'answer')!} />
                : d.session.status === 'failed' ? <ErrorNote>{d.session.error ?? 'Yanıt üretilemedi.'}</ErrorNote> : <Spinner label="Yanıt hazırlanıyor…" />}
              <div className="mt-1 flex items-center gap-3 text-[11.5px] text-faint">
                <span>{p ? `@${p} · ` : ''}{usd(d.spent_usd)} · {d.session.model}</span>
                {p && !isMod && d.outputs.some((o) => o.stage === 'answer' && o.status === 'done') && (
                  <button className="flex items-center gap-1 text-accent hover:underline" onClick={() => object(d)}><MessageSquareReply className="size-3.5" /> İtiraz et / ek soru</button>
                )}
              </div>
            </div>
          </EvidenceProvider>
        )
      })}
      {canModerate && !busy && !jobs && (
        <Card className="flex flex-wrap items-center gap-3 p-4 text-[13px]">
          <span className="flex-1">{tail.length} mercek yanıt verdi. Aralarındaki uzlaşı ve ayrışmayı tek yanıtta özetletebilirsin (ek maliyet).</span>
          <Button size="sm" onClick={() => void moderate(tail.map((d) => d.session.id))}>Ayrışmaları özetlet · maliyeti göster</Button>
        </Card>
      )}
      <div ref={end} />
      <Card className="sticky bottom-3 space-y-3 p-4 shadow-lg">
        {target && <div className="flex items-center gap-2 rounded-lg bg-raised px-3 py-1.5 text-[12.5px]">Seçili yanıta itiraz ediyorsun; yalnızca o mercek yeniden çalışır. <button aria-label="İtirazı iptal et" className="ml-auto text-muted hover:text-fg" onClick={() => setTarget(null)}><X className="size-3.5" /></button></div>}
        <textarea id="ask-box" className="min-h-[72px] w-full resize-y rounded-xl border border-line bg-surface p-3 text-[14px] outline-none focus:border-accent" placeholder={debate ? 'Merceklere sorulacak soru…' : 'Bir soru yaz…'} value={text} maxLength={2000}
          onChange={(e) => setText(e.target.value)} aria-label="Soru" />
        <div className="flex flex-wrap items-center gap-3 text-[13px]">
          <label className="flex items-center gap-1.5"><input type="checkbox" className="accent-[var(--accent)]" checked={debate} onChange={(e) => { setDebate(e.target.checked); setTarget(null) }} /> Tartışma modu</label>
          {!debate && (
            <select className="h-8 rounded-lg border border-line bg-surface px-2" value={mention.persona ? mention.persona.id : picked} disabled={!!mention.persona} onChange={(e) => setPicked(e.target.value)} aria-label="Mercek">
              <option value="">Genel (tek mercek yok)</option>
              {personas.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
          )}
          {!debate && last && !target && <label className="flex items-center gap-1.5"><input type="checkbox" className="accent-[var(--accent)]" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> Önceki soruya devam</label>}
          {!person.is_self && <label className="flex items-center gap-1.5"><input type="checkbox" className="accent-[var(--accent)]" checked={consent} onChange={(e) => setConsent(e.target.checked)} /> Verinin bana ait / rızalı olduğunu onaylıyorum</label>}
        </div>
        {debate && (
          <div className="flex flex-wrap gap-1.5">
            {personas.map((p) => {
              const on = panel.includes(p.id)
              return <button key={p.id} title={p.framework} disabled={!on && panel.length >= MAX_DEBATE} onClick={() => setPanel(on ? panel.filter((x) => x !== p.id) : [...panel, p.id])}
                className={`rounded-full px-2.5 py-1 text-[12.5px] ${on ? 'bg-accent text-accent-fg' : 'bg-raised text-muted hover:text-fg disabled:opacity-40'}`}>{p.name.split(' ').pop()}</button>
            })}
            <span className="self-center text-[12px] text-muted">{panel.length}/{MAX_DEBATE} mercek (en az 2)</span>
          </div>
        )}
        {blocked && <ErrorNote>{blocked.plan.reason ?? 'Çalıştırılamaz.'}</ErrorNote>}
        {error && <ErrorNote>{error}</ErrorNote>}
        <div className="flex flex-wrap items-center gap-3">
          {jobs && !blocked
            ? <><Button variant="primary" busy={busy} onClick={send}>{busy ? <Loader2 className="spin size-4" /> : <Send className="size-4" />} Gönder · en çok {usd(total)}</Button>
              <span className="text-[12.5px] text-muted">{jobs.length} çağrı · tahmin {usd(lowTotal)}–{usd(total)} · {jobs[0]!.plan.model}</span></>
            : <Button busy={busy} disabled={!ready} onClick={() => void calc()}>Maliyeti göster</Button>}
        </div>
      </Card>
    </div>
  )
}

import { AlertTriangle, ChevronDown, Download, Loader2, RotateCcw, Square } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { CiteToggle, Cites, EvidenceProvider } from '@/components/Evidence'
import { Avatar, Badge, Button, Card, Empty, ErrorNote, Panel, Spinner } from '@/components/ui'
import { api } from '@/lib/api'
import { useAsync, useData } from '@/lib/data'
import { dateTime, num, usd } from '@/lib/fmt'
import { href } from '@/lib/router'
import type {
  AnswerOutput, CouncilDetail, CouncilOutputRow, DigestOutput, ModeratorOutput, PersonaOutput, SynthesisOutput, VerificationOutput,
} from '@/lib/types'
import { STATUS_LABEL } from './CouncilPage'

const RULE: Record<string, string> = {
  alinti_bulunamadi: 'alıntı pakette bulunamadı', alinti_baska_kesitte: 'alıntı başka kesitte', alintisiz: 'alıntısız gözlem', ayiklandi: 'geçersiz öğe ayıklandı',
  kisa_degerlendirme: 'Kısa genel değerlendirme', ayrisma_kayboldu: 'Kaybolan ayrışma',
  gecersiz_kanit: 'pakette olmayan kanıt kimliği', kanitsiz_iddia: 'kanıtsız iddia', teshis_dili: 'tanı dili',
  tehlike_suclama: 'tehlikelilik/suçlama dili', kesinlik_dili: 'kesinlik dili',
}
const ABOUT: Record<string, string> = { a: 'birinci kişi', b: 'ikinci kişi', ikisi: 'ikisi' }
const CONF: Record<string, string> = { low: 'düşük güven', medium: 'orta güven', high: 'yüksek güven' }
const SUPPORT: Record<string, { text: string; tone: 'ok' | 'warn' | 'bad' }> = {
  supported: { text: 'kanıtla destekleniyor', tone: 'ok' }, partial: { text: 'kısmen destekleniyor', tone: 'warn' }, unsupported: { text: 'kanıtla desteklenmiyor', tone: 'bad' },
}
type Checks = Map<string, { support: string; note: string }>
function Check({ id, checks }: { id: string; checks?: Checks }) {
  const c = checks?.get(id)
  if (!c) return null
  const m = SUPPORT[c.support]
  if (!m) return null
  return <span className="ml-1.5 align-middle" title={c.note}><Badge tone={m.tone} className="font-normal">{m.text}</Badge></span>
}
const TERMINAL = new Set(['done', 'partial', 'failed', 'budget_stopped', 'canceled', 'interrupted'])

function Violations({ o }: { o: CouncilOutputRow }) {
  const v = o.audit?.violations ?? []
  if (!v.length) return null
  return (
    <div className="mt-3 space-y-1 rounded-xl border border-warn/40 bg-warn/8 p-2.5 text-[12.5px] text-warn" role="note">
      <div className="flex items-center gap-1.5 font-semibold"><AlertTriangle className="size-3.5" /> Denetim uyarısı: bu çıktıya temkinle yaklaş</div>
      {v.map((x, i) => <div key={i}>• {RULE[x.rule] ?? x.rule}: {x.message}{x.match ? ` ("${x.match.trim()}")` : ''}</div>)}
    </div>
  )
}

const List = ({ items, title }: { items?: string[] | null; title: string }) =>
  items?.length ? (
    <div className="mt-3"><div className="text-[12px] font-semibold uppercase tracking-wide text-muted">{title}</div>
      <ul className="mt-1 list-disc space-y-0.5 pl-5 text-[13px]">{items.map((t, i) => <li key={i}>{t}</li>)}</ul></div>
  ) : null

function PersonaCard({ o, name, school }: { o: CouncilOutputRow; name: string; school?: string }) {
  const out = o.output as PersonaOutput | undefined
  const st = { running: ['çalışıyor', 'accent'], pending: ['bekliyor', 'neutral'], done: ['tamam', 'ok'], failed: ['hata', 'bad'], skipped: ['atlandı (bütçe)', 'warn'] }[o.status] as [string, 'accent' | 'neutral' | 'ok' | 'bad' | 'warn']
  return (
    <Card className="p-4">
      <div className="mb-3 flex items-center gap-2.5">
        <Avatar name={name} size={30} />
        <div className="min-w-0 flex-1"><div className="truncate text-[14px] font-semibold">{name}</div><div className="truncate text-[12px] text-muted">{school}</div></div>
        <Badge tone={st[1]}>{o.status === 'running' && <Loader2 className="spin size-3" />}{st[0]}</Badge>
      </div>
      {o.status === 'failed' || o.status === 'skipped' ? <p className="text-[12.5px] text-muted">{o.error}</p> : null}
      {out?.abstain && <p className="rounded-xl bg-raised p-3 text-[13px]"><b>Çekimser:</b> {out.abstain_reason}</p>}
      {out && !out.abstain && (
        <div className="space-y-3 text-[13px]">
          {(out.observations ?? []).map((ob, i) => <div key={i}><p><span className="text-muted">Gözlem{ob.about ? ` (${ABOUT[ob.about] ?? ob.about})` : ''} · </span>{ob.text}<Cites ids={ob.evidence} /></p>{ob.quote && <p className="mt-1 border-l-2 border-line pl-2.5 text-[12.5px] italic text-muted">“{ob.quote}”</p>}</div>)}
          {(out.hypotheses ?? []).map((h, i) => (
            <div key={i} className="rounded-xl bg-raised p-3">
              <div className="flex items-start gap-2"><p className="flex-1"><b>{h.claim}</b><Cites ids={h.evidence} /></p><Badge>{CONF[h.confidence] ?? h.confidence}</Badge></div>
              <p className="mt-1.5 text-muted"><b>Alternatif:</b> {h.counter_hypothesis}</p>
              <p className="mt-0.5 text-muted"><b>Çürüten:</b> {h.falsified_by}</p>
            </div>
          ))}
          <List items={out.open_questions} title="Açık sorular" />
          <List items={out.data_needed} title="Gereken veri" />
        </div>
      )}
      <Violations o={o} />
      {o.status === 'done' && <div className="mt-3 text-[11.5px] text-faint">{o.from_cache ? 'önbellekten · ücretsiz' : `${num(o.prompt_tokens)}+${num(o.completion_tokens)} token · ${usd(o.cost_usd)}`}{o.attempts > 1 ? ' · biçim onarıldı' : ''}</div>}
    </Card>
  )
}

export default function CouncilSessionPage({ id }: { id: string }) {
  const { people, toast } = useData()
  const personas = useAsync(() => api.personas(), [])
  const [detail, setDetail] = useState<CouncilDetail | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try { setDetail(await api.council(id)); setError(null) } catch (e) { setError(e instanceof Error ? e.message : 'Okunamadı') }
  }, [id])
  useEffect(() => { void load() }, [load])
  const active = !!detail && (detail.running || !TERMINAL.has(detail.session.status))
  useEffect(() => {
    if (!active) return
    const t = setInterval(() => void load(), 1500)
    return () => clearInterval(t)
  }, [active, load])

  if (error && !detail) return <ErrorNote>{error}</ErrorNote>
  if (!detail) return <Spinner />
  const { session: s, outputs } = detail
  const st = STATUS_LABEL[s.status] ?? { text: s.status, tone: 'neutral' as const }
  const byPersona = new Map(personas.data?.personas.map((p) => [p.id, p]))
  const who = s.subject_ids.map((i) => people.find((p) => p.id === i)?.name ?? `#${i}`).join(' × ')
  const synth = outputs.find((o) => o.stage === 'synthesis' && o.status === 'done')
  const mod = outputs.find((o) => o.stage === 'moderator' && o.status === 'done')
  const answer = outputs.find((o) => o.stage === 'answer')
  const ver = outputs.find((o) => o.stage === 'verify' && o.status === 'done')?.output as VerificationOutput | undefined
  const checks: Checks = new Map((ver?.results ?? []).map((r) => [r.id, r]))
  const counts = { supported: 0, partial: 0, unsupported: 0 }
  for (const r of ver?.results ?? []) if (r.support in counts) counts[r.support as keyof typeof counts]++
  const verLine = ver?.results?.length ? `Kanıt doğrulaması: ${counts.supported} iddia destekleniyor, ${counts.partial} kısmen, ${counts.unsupported} desteklenmiyor.` : undefined
  const digests = outputs.filter((o) => o.stage === 'digest' && o.status === 'done')
  const personaOuts = s.persona_ids.map((pid) => outputs.find((o) => o.stage === 'persona' && o.persona_id === pid) ?? ({ id: 0, stage: 'persona', persona_id: pid, status: 'pending', prompt_tokens: 0, completion_tokens: 0, cost_usd: 0, from_cache: false, attempts: 0 } as CouncilOutputRow))
  const done = personaOuts.filter((o) => o.status === 'done').length

  async function act(fn: () => Promise<unknown>, ok?: string) {
    try { await fn(); if (ok) toast(ok); await load() } catch (e) { toast(e instanceof Error ? e.message : 'İşlem başarısız', 'bad') }
  }

  return (
    <EvidenceProvider items={s.pack?.items ?? []}>
      <div className="mb-6 flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1">
          <a className="text-[13px] text-accent" href={href('/konsey')}>← Konsey</a>
          <h1 className="mt-1 flex flex-wrap items-center gap-2 text-[26px] font-semibold tracking-tight">{who}<Badge tone={st.tone}>{active && <Loader2 className="spin size-3" />}{st.text}</Badge></h1>
          <p className="text-[13px] text-muted">{s.kind === 'ask' ? 'Soru' : s.kind === 'pair' ? 'İki kişilik analiz' : 'Kişi analizi'} · {s.model} · {dateTime(s.created_at)} · harcama <b className="text-fg">{usd(detail.spent_usd)}</b></p>
        </div>
        <CiteToggle />
        {detail.outputs.some((o) => o.stage === 'synthesis' && o.status === 'done') && <a href={`/api/council/${id}/report.md`} download><Button size="sm"><Download className="size-4" /> Markdown</Button></a>}
        {detail.running && <Button onClick={() => act(() => api.councilCancel(id), 'İptal edildi.')}><Square className="size-4" /> Durdur</Button>}
        {!detail.running && ['interrupted', 'partial', 'failed', 'canceled'].includes(s.status) && <Button variant="primary" onClick={() => act(() => api.councilResume(id), 'Devam ediyor.')}><RotateCcw className="size-4" /> Devam et</Button>}
      </div>

      {s.error && <div className="mb-4"><ErrorNote>{s.error}</ErrorNote></div>}
      <Card className="mb-5 flex gap-3 border-line p-3.5 text-[12.5px] text-muted">
        <AlertTriangle className="mt-0.5 size-4 shrink-0" />
        <span>Bunlar kuramsal okumalardır; <b>klinik tanı ya da yargı değildir</b>. Her iddia, tıklanabilir kanıt kimliklerine bağlıdır ve yanında alternatif açıklama vardır.</span>
      </Card>

      {s.kind === 'ask' ? (
        answer ? <AnswerCard o={answer} /> : <Spinner label="Yanıt hazırlanıyor…" />
      ) : (
        <>
          {synth?.output ? <Synthesis o={synth} checks={checks} summaryLine={verLine} /> : active ? <Card className="mb-5 p-5 text-[13.5px] text-muted"><Loader2 className="spin mr-2 inline size-4" />Merceklerin çıktıları geldikçe aşağıda belirir; sentez sonda hazırlanır ({done}/{s.persona_ids.length}).</Card> : null}
          <Digests rows={digests} />
          {mod?.output ? <Moderator o={mod} names={new Map([...byPersona].map(([id, p]) => [id, p.name]))} /> : null}
          <h2 className="mb-3 mt-8 text-[17px] font-semibold tracking-tight">Mercekler <span className="text-[13px] font-normal text-muted">{done}/{s.persona_ids.length}</span></h2>
          <div className="grid gap-4 md:grid-cols-2">
            {personaOuts.map((o) => <PersonaCard key={o.persona_id} o={o} name={byPersona.get(o.persona_id)?.name ?? o.persona_id} school={byPersona.get(o.persona_id)?.school} />)}
          </div>
        </>
      )}
      {!active && outputs.length === 0 && <Empty title="Çıktı yok" />}
    </EvidenceProvider>
  )
}

function Synthesis({ o, checks, summaryLine }: { o: CouncilOutputRow; checks?: Checks; summaryLine?: string }) {
  const s = o.output as SynthesisOutput
  return (
    <Panel title="Genel değerlendirme" hint="Tüm merceklerin okumasının sade bir dille birleştirilmiş hâli. Bir tanı değil, verinin genel bir okumasıdır." action={<Badge tone="accent">{CONF[s.confidence] ?? s.confidence}</Badge>} className="mb-5">
      {summaryLine && <p className="mb-3 text-[12.5px] text-muted">{summaryLine}</p>}
      {(s.sections ?? []).length > 0 ? (
        <div className="space-y-2">
          {s.sections!.map((sec, i) => (
            <details key={i} open={i < 2} className="group rounded-xl border border-line px-4 py-3">
              <summary className="flex cursor-pointer list-none items-center gap-2 text-[15px] font-semibold"><ChevronDown className="size-4 shrink-0 text-muted transition group-open:rotate-180" />{sec.title}<Check id={`s${i + 1}`} checks={checks} /></summary>
              <div className="mt-2 space-y-3 text-[15px] leading-relaxed">{sec.text.split(/\n{2,}/).map((para, j) => <p key={j}>{para}</p>)}<Cites ids={sec.evidence} /></div>
            </details>
          ))}
        </div>
      ) : <div className="space-y-3 text-[15px] leading-relaxed">{s.summary.split(/\n{2,}/).map((para, i) => <p key={i}>{para}</p>)}</div>}
      {s.evolution && (
        <div className="mt-5 rounded-xl bg-raised p-4">
          <div className="mb-2 text-[12px] font-semibold uppercase tracking-wide text-muted">Zaman içinde gelişim<Cites ids={s.evolution_evidence} /><Check id="e1" checks={checks} /></div>
          <div className="space-y-3 text-[14px] leading-relaxed">{s.evolution.split(/\n{2,}/).map((para, i) => <p key={i}>{para}</p>)}</div>
        </div>
      )}
      {(s.convergences ?? []).length > 0 && <div className="mt-4"><div className="text-[12px] font-semibold uppercase tracking-wide text-muted">Ortak okumalar</div>
        <ul className="mt-1 space-y-1.5 text-[13.5px]">{s.convergences!.map((p, i) => <li key={i}>• {p.text}<Cites ids={p.evidence} /><Check id={`c${i + 1}`} checks={checks} /></li>)}</ul></div>}
      {(s.divergences ?? []).length > 0 && <div className="mt-4"><div className="text-[12px] font-semibold uppercase tracking-wide text-muted">Ayrışan okumalar</div>
        <ul className="mt-1 space-y-1.5 text-[13.5px]">{s.divergences!.map((p, i) => <li key={i}>• {p.text}<Cites ids={p.evidence} /><Check id={`d${i + 1}`} checks={checks} /></li>)}</ul></div>}
      <List items={s.alternative_explanations} title="Alternatif açıklamalar" />
      <List items={s.open_questions} title="Açık sorular" />
      <List items={s.data_needed} title="Daha fazla hangi veri gerekir" />
      {s.limits && <p className="mt-4 text-[12.5px] text-muted"><b>Sınırlar:</b> {s.limits}</p>}
      <Violations o={o} />
    </Panel>
  )
}

function Digests({ rows }: { rows: CouncilOutputRow[] }) {
  const items = rows.map((r) => r.output as DigestOutput).filter((d) => d?.summary)
  if (!items.length) return null
  return (
    <Panel title="Dönem özetleri" hint="Derin analizde her dönem ayrı özetlenir; mercekler bu özetleri de görür. Özetler kanıt sayılmaz, yalnızca yön gösterir." className="mb-5">
      <details>
        <summary className="cursor-pointer text-[13px] text-accent">{items.length} dönemi göster</summary>
        <div className="mt-3 space-y-3 text-[13.5px]">
          {items.map((d, i) => <div key={i} className="rounded-xl bg-raised p-3"><div className="text-[12px] font-semibold text-muted">{d.period}{d.tone ? ` · ${d.tone}` : ''}</div><p className="mt-1">{d.summary}<Cites ids={d.evidence} /></p></div>)}
        </div>
      </details>
    </Panel>
  )
}

function Moderator({ o, names }: { o: CouncilOutputRow; names: Map<string, string> }) {
  const m = o.output as ModeratorOutput
  const nm = (id: string) => names.get(id) ?? id
  return (
    <Panel title="Moderatör" hint="Persona çıktılarındaki uzlaşı ve ayrışmalar." className="mb-5">
      {(m.agreements ?? []).map((a, i) => <p key={i} className="mb-2 text-[13px]"><span className="text-muted">Uzlaşı · </span>{a.point}<Cites ids={a.evidence} /> <span className="text-[11.5px] text-faint">({a.personas.map(nm).join(', ')})</span></p>)}
      {(m.disagreements ?? []).map((d, i) => (
        <div key={i} className="mb-3 rounded-xl bg-raised p-4 text-[13.5px] leading-relaxed"><b className="text-[14.5px]">{d.topic}</b>
          {d.meaning && <p className="mt-2 rounded-lg bg-surface p-3 text-[13px]"><span className="font-semibold">Bu ne demek? </span>{d.meaning}</p>}
          {d.positions.map((p, j) => <p key={j} className="mt-3"><span className="font-semibold">{nm(p.persona)}: </span>{p.view}<Cites ids={p.evidence} /></p>)}</div>
      ))}
      <List items={m.gaps} title="Eksik kalanlar" />
      <Violations o={o} />
    </Panel>
  )
}

export function AnswerCard({ o }: { o: CouncilOutputRow }) {
  const a = o.output as AnswerOutput | undefined
  if (o.status === 'failed' || o.status === 'skipped') return <ErrorNote>{o.error}</ErrorNote>
  if (!a) return <Spinner label="Yanıt hazırlanıyor…" />
  return (
    <Panel title="Yanıt">
      {a.abstain ? <p className="text-[14px]"><b>Veri yetersiz:</b> {a.abstain_reason}</p> : <p className="text-[15px] leading-relaxed">{a.answer}<Cites ids={a.evidence} /></p>}
      {a.limits && <p className="mt-3 text-[12.5px] text-muted"><b>Sınırlar:</b> {a.limits}</p>}
      <Violations o={o} />
      <div className="mt-3 text-[11.5px] text-faint">{o.from_cache ? 'önbellekten · ücretsiz' : `${num(o.prompt_tokens)}+${num(o.completion_tokens)} token · ${usd(o.cost_usd)}`}</div>
    </Panel>
  )
}

import { useConfirm } from '@/components/Confirm'
import { ChevronDown, Sparkles, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Avatar, Badge, Button, Card, ErrorNote, Field, inputCls, PageHeader, Panel, Spinner, Tabs } from '@/components/ui'
import { api } from '@/lib/api'
import { cn } from '@/lib/cn'
import { useAsync, useData } from '@/lib/data'
import { dateTime, num, usd } from '@/lib/fmt'
import { go, href } from '@/lib/router'
import type { CouncilPlan } from '@/lib/types'

type Kind = 'single' | 'pair' | 'ask'
const KINDS: { id: Kind; label: string }[] = [
  { id: 'single', label: 'Kişi analizi' },
  { id: 'pair', label: 'İki kişi' },
  { id: 'ask', label: 'Soru sor' },
]

export const STATUS_LABEL: Record<string, { text: string; tone: 'neutral' | 'accent' | 'ok' | 'warn' | 'bad' }> = {
  queued: { text: 'sırada', tone: 'neutral' }, running: { text: 'çalışıyor', tone: 'accent' }, done: { text: 'tamamlandı', tone: 'ok' },
  partial: { text: 'kısmi', tone: 'warn' }, failed: { text: 'başarısız', tone: 'bad' }, budget_stopped: { text: 'bütçe doldu', tone: 'warn' },
  canceled: { text: 'iptal', tone: 'neutral' }, interrupted: { text: 'kesildi', tone: 'warn' },
}

export default function CouncilPage({ query }: { query: URLSearchParams }) {
  const { people, state, toast, reload, sources } = useData()
  const personas = useAsync(() => api.personas(), [])
  const history = useAsync(() => api.councils(), [])
  const confirm = useConfirm()
  const [kind, setKind] = useState<Kind>(query.get('tur') === 'pair' ? 'pair' : 'single')
  const [subject, setSubject] = useState(Number(query.get('kisi')) || 0)
  const [partner, setPartner] = useState(Number(query.get('ikinci')) || 0)
  const convScope = Number(query.get('sohbet')) || 0 // aynı adı paylaşan sohbetlerden yalnızca biri
  const [selected, setSelected] = useState<string[] | null>(null)
  const [question, setQuestion] = useState('')
  const [consent, setConsent] = useState(false)
  const [osint, setOsint] = useState(false)
  const [goal, setGoal] = useState('')
  const [deep, setDeep] = useState(false)
  const osintFindings = useAsync(() => (subject ? api.personOsint(subject) : Promise.resolve([])), [subject])
  const hasOsint = (osintFindings.data?.length ?? 0) > 0
  const [plan, setPlan] = useState<CouncilPlan | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => { if (!subject && people.length) setSubject((people.find((p) => p.is_self) ?? people[0]!).id) }, [people, subject])
  const all = personas.data?.personas ?? []
  const chosen = selected ?? (kind === 'ask' ? [] : all.map((p) => p.id))
  useEffect(() => { setPlan(null); setError(null) }, [kind, subject, partner, question, consent, selected, osint, goal, deep])

  const subjectPerson = people.find((p) => p.id === subject)
  const needsConsent = kind === 'pair' || (!!subjectPerson && !subjectPerson.is_self)
  // İki kişilik analizde ikinci kişi seçili değilse (ya da birinci kişiyle aynıysa) ilk uygun kişi seçilir.
  const others = people.filter((p) => p.id !== subject)
  useEffect(() => {
    if (kind === 'pair' && others.length > 0 && (partner === 0 || partner === subject || !others.some((p) => p.id === partner))) setPartner(others[0]!.id)
  }, [kind, subject, people.length]) // eslint-disable-line react-hooks/exhaustive-deps
  const [fixing, setFixing] = useState(false)
  async function completePartners() {
    setFixing(true)
    try {
      let made = 0
      for (const src of sources) made += (await api.sourcePartners(src.id)).created.length
      await reload()
      toast(made ? `${made} kişi dosyası açıldı.` : 'Açılacak yeni kişi bulunamadı; Kaynaklar sayfasından sohbet karşısı için "Dosya aç" de.', made ? 'ok' : 'bad')
    } catch (e) { toast(e instanceof Error ? e.message : 'Tamamlanamadı', 'bad') } finally { setFixing(false) }
  }
  const body = useMemo(() => ({ kind, subject_id: subject, partner_id: kind === 'pair' ? partner : 0, ...(kind === 'pair' && convScope ? { partner_conv_id: convScope } : {}), persona_ids: chosen, question, consent, include_osint: osint && hasOsint, ...(kind !== 'ask' && goal.trim() ? { goal: goal.trim() } : {}), ...(kind !== 'ask' && deep ? { depth: 'deep' } : {}) }), [kind, subject, partner, convScope, chosen, question, consent, osint, hasOsint, goal, deep])
  const ready = subject > 0 && (kind !== 'pair' || (partner > 0 && partner !== subject)) && (kind === 'ask' ? question.trim().length >= 3 && chosen.length <= 1 : chosen.length > 0) && (!needsConsent || consent)

  async function calc() {
    setBusy(true); setError(null); setPlan(null)
    try { setPlan(await api.councilPlan(body)) } catch (e) { setError(e instanceof Error ? e.message : 'Hesaplanamadı') } finally { setBusy(false) }
  }
  async function start() {
    if (!plan?.approval_id) return
    setBusy(true); setError(null)
    try { const r = await api.councilStart({ ...body, approval_id: plan.approval_id }); go(`/konsey/${r.id}`) }
    catch (e) { setError(e instanceof Error ? e.message : 'Başlatılamadı'); setPlan(null) } finally { setBusy(false) }
  }

  if (people.length === 0) return (
    <>
      <PageHeader title="Konsey" sub="13 kuramsal merceğin kanıta bağlı okuması." />
      <Card className="p-6 text-[14px] text-muted">Önce bir kaynak içe aktarıp kişi dosyası aç. <a className="text-accent" href={href('/kaynaklar')}>Kaynaklara git →</a></Card>
    </>
  )

  return (
    <>
      <PageHeader title="Konsey" sub="Önce maliyet ve gönderilecek metin gösterilir; onay vermeden hiçbir şey sağlayıcıya gitmez." />
      {state && !state.provider_ready && <Card className="mb-4 border-warn/40 p-4 text-[13px]">Yapay zekâ sağlayıcısı bağlı değil. <a className="text-accent" href={href('/ayarlar')}>Ayarlar'dan anahtar ekle →</a></Card>}
      <Panel title="Yeni analiz">
        <div className="mb-4 overflow-x-auto"><Tabs tabs={KINDS} value={kind} onChange={(k) => { setKind(k); setSelected(null) }} /></div>
        <div className="grid gap-4 md:grid-cols-2">
          <Field label={kind === 'pair' ? 'Birinci kişi' : 'Kişi'}>
            <select className={inputCls} value={subject} onChange={(e) => setSubject(Number(e.target.value))}>
              {people.map((p) => <option key={p.id} value={p.id}>{p.name}{p.is_self ? ' (ben)' : ''}</option>)}
            </select>
          </Field>
          {kind === 'pair' && (
            <Field label="İkinci kişi">
              <select className={inputCls} value={partner} onChange={(e) => setPartner(Number(e.target.value))}>
                <option value={0}>{people.filter((p) => p.id !== subject).length ? 'Seç…' : 'Henüz ikinci kişi yok'}</option>
                {people.filter((p) => p.id !== subject).map((p) => <option key={p.id} value={p.id}>{p.name}{p.is_self ? ' (ben)' : ''}</option>)}
              </select>
            </Field>
          )}
        </div>
        {kind === 'pair' && others.length === 0 && (
          <div className="mt-4 flex flex-wrap items-center gap-3 rounded-xl border border-warn/40 bg-warn/8 p-4 text-[13px]">
            <span className="min-w-0 flex-1">İki kişilik analiz için karşı tarafın da bir <b>kişi dosyası</b> olması gerekir. WhatsApp gibi tek sohbetlerde karşı taraf için dosyayı senin yerine açabilirim.</span>
            <Button size="sm" variant="primary" busy={fixing} onClick={() => void completePartners()}>Karşı tarafı kişi olarak ekle</Button>
            <a href={href('/kaynaklar')}><Button size="sm">Kaynaklara git</Button></a>
          </div>
        )}
        {kind === 'ask' && (
          <div className="mt-4">
            <Field label="Sorun" hint="Yanıt yalnızca kanıt paketine dayanır; veri yetmiyorsa Joe bunu söyler.">
              <textarea className={cn(inputCls, 'h-24 resize-none py-2')} value={question} onChange={(e) => setQuestion(e.target.value)} placeholder="Örn. Geceleri neden daha çok yazıyor?" maxLength={2000} />
            </Field>
          </div>
        )}
        <div className="mt-5">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-[13px] font-medium">{kind === 'ask' ? 'Belirli bir mercekle sor (isteğe bağlı)' : `Merceğler (${chosen.length}/${all.length})`}</span>
            {kind !== 'ask' && (
              <span className="flex gap-3 text-[12.5px]">
                <button className="text-accent" onClick={() => setSelected(all.map((p) => p.id))}>Tümü</button>
                <button className="text-muted" onClick={() => setSelected([])}>Hiçbiri</button>
              </span>
            )}
          </div>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {all.map((p) => {
              const on = chosen.includes(p.id)
              return (
                <button key={p.id} title={p.framework} onClick={() => setSelected(kind === 'ask' ? (on ? [] : [p.id]) : on ? chosen.filter((x) => x !== p.id) : [...chosen, p.id])}
                  className={cn('flex items-center gap-2 rounded-xl border px-3 py-2 text-left text-[13px] transition', on ? 'border-accent bg-accent-soft' : 'border-line hover:bg-raised')}>
                  <Avatar name={p.name} size={24} />
                  <span className="min-w-0"><span className="block truncate font-medium">{p.name}</span><span className="block truncate text-[11.5px] text-muted">{p.school}</span></span>
                </button>
              )
            })}
          </div>
        </div>
        {kind !== 'ask' && (
          <div className="mt-5 space-y-3">
            <Field label="Amacın (isteğe bağlı)" hint="Örn. “Son aylarda neden uzaklaştığımızı anlamak istiyorum.” Okuma bu soruya göre odaklanır; kanıt kuralları değişmez.">
              <textarea className={cn(inputCls, 'h-20 resize-none py-2')} value={goal} onChange={(e) => setGoal(e.target.value)} maxLength={600} placeholder="Bu analizden ne öğrenmek istiyorsun?" />
            </Field>
            <label className="flex cursor-pointer items-start gap-2.5 rounded-xl bg-raised p-3 text-[13px]">
              <input type="checkbox" className="mt-0.5 size-4 accent-[var(--accent)]" checked={deep} onChange={(e) => setDeep(e.target.checked)} />
              <span><b>Derin analiz:</b> önce tüm dönemler ayrı ayrı özetlenir, mercekler bu dönem özetlerini de görür. Uzun yazışmalarda daha dengeli sonuç verir; maliyet artar.</span>
            </label>
          </div>
        )}
        {hasOsint && (
          <label className="mt-5 flex cursor-pointer items-start gap-2.5 rounded-xl bg-raised p-3 text-[13px]">
            <input type="checkbox" className="mt-0.5 size-4 accent-[var(--accent)]" checked={osint} onChange={(e) => setOsint(e.target.checked)} />
            <span>Bu kişiye bağlı <b>{osintFindings.data!.length} doğrulanmış OSINT bulgusunu</b> pakete ekle (kimlik doğrulaması değildir; önizlemede görünür).</span>
          </label>
        )}
        {needsConsent && (
          <label className="mt-5 flex cursor-pointer items-start gap-2.5 rounded-xl bg-raised p-3 text-[13px]">
            <input type="checkbox" className="mt-0.5 size-4 accent-[var(--accent)]" checked={consent} onChange={(e) => setConsent(e.target.checked)} />
            <span>Bu veri <b>bana ait</b> ya da ilgili kişinin <b>paylaşma rızası alınmış</b>tır. Analiz tanı ya da yargı değildir.</span>
          </label>
        )}
        <div className="mt-5 flex items-center gap-3">
          <Button variant="primary" onClick={calc} busy={busy && !plan} disabled={!ready}>Maliyeti hesapla</Button>
          {!ready && <span className="text-[12.5px] text-muted">{needsConsent && !consent ? 'Rıza onayı gerekli.' : 'Seçimleri tamamla.'}</span>}
        </div>
        {error && <div className="mt-4"><ErrorNote>{error}</ErrorNote></div>}
        {plan && <PlanCard plan={plan} busy={busy} onStart={start} />}
      </Panel>

      <h2 className="mb-3 mt-10 text-[17px] font-semibold tracking-tight">Geçmiş</h2>
      {history.loading ? <Spinner /> : (history.data?.length ?? 0) === 0 ? <p className="text-[13px] text-muted">Henüz analiz yok.</p> : (
        <Card className="divide-y divide-line">
          {history.data!.map((c) => {
            const st = STATUS_LABEL[c.status] ?? { text: c.status, tone: 'neutral' as const }
            const who = c.subject_ids.map((id) => people.find((p) => p.id === id)?.name ?? `#${id}`).join(' × ')
            return (
              <div key={c.id} className="flex items-center gap-3 px-4 py-3">
                <a href={href(`/konsey/${c.id}`)} className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 truncate text-[14px] font-medium">{who}<Badge tone={st.tone}>{st.text}</Badge>{c.running && <span className="pulse-soft size-2 rounded-full bg-accent" />}</div>
                  <div className="truncate text-[12.5px] text-muted">{c.kind === 'ask' ? `Soru: ${c.question}` : `${c.persona_ids.length} merceğin analizi`} · {dateTime(c.created_at)}</div>
                </a>
                <span className="text-[13px] tabular-nums text-muted">{usd(c.spent_usd ?? 0)}</span>
                <Button variant="ghost" size="sm" aria-label="Sil" onClick={async () => { if (await confirm({ title: 'Bu analiz silinsin mi?', message: 'Analizin tüm mercek çıktıları, moderatör özeti ve genel değerlendirmesi silinir.', confirmLabel: 'Analizi sil' })) { try { await api.councilDelete(c.id); toast('Silindi.'); history.setData((history.data ?? []).filter((x) => x.id !== c.id)) } catch (e) { toast(e instanceof Error ? e.message : 'Silinemedi', 'bad') } } }}><Trash2 className="size-4" /></Button>
              </div>
            )
          })}
        </Card>
      )}
    </>
  )
}

function PlanCard({ plan, busy, onStart }: { plan: CouncilPlan; busy: boolean; onStart: () => void }) {
  const e = plan.estimate
  const masks = Object.entries(plan.mask_report ?? {})
  return (
    <div className="mt-5 rounded-2xl border border-line bg-raised/60 p-4">
      <div className="flex flex-wrap items-end gap-x-8 gap-y-3">
        <div>
          <div className="text-[12px] text-muted">Tahmini maliyet</div>
          <div className="text-[26px] font-semibold leading-tight tabular-nums">{e.known ? `${usd(e.low_usd)} – ${usd(e.high_usd)}` : 'bilinmiyor'}</div>
        </div>
        <Mini label="Çağrı" value={num(e.calls)} />
        <Mini label="Paket" value={`${num(plan.pack_tokens)} token`} />
        <Mini label="Bütçe tavanı" value={usd(plan.cap_usd)} />
        <Mini label="Model" value={plan.model} />
        {plan.strong_model && plan.strong_model !== plan.model && <Mini label="Güçlü model (sentez/doğrulama)" value={plan.strong_model} />}
        {plan.depth === 'deep' && <Mini label="Mod" value="Derin" />}
      </div>
      {plan.coverage && plan.coverage.items > 0 && <p className="mt-2 text-[12.5px] text-muted">Kapsam: {num(plan.coverage.messages)} / {num(plan.coverage.total_messages)} mesaj, {plan.coverage.months_covered}/{plan.coverage.months_total} ay, {plan.coverage.items} kesit.</p>}
      {e.note && <p className="mt-2 text-[12px] text-muted">{e.note}</p>}
      {masks.length > 0 && <div className="mt-3 flex flex-wrap items-center gap-1.5 text-[12.5px]"><span className="text-muted">Maskelenen:</span>{masks.map(([k, v]) => <Badge key={k}>{k} × {v}</Badge>)}</div>}
      <div className="mt-3 flex flex-wrap gap-1.5">
        {plan.items.map((i) => <Badge key={i.id} tone="accent" className="font-normal">{i.id} · {i.title} ({i.messages})</Badge>)}
        {plan.skipped.length > 0 && <Badge tone="warn">bütçeye sığmayan {plan.skipped.length} kesit atlandı</Badge>}
      </div>
      <details className="group mt-3 text-[13px]">
        <summary className="flex cursor-pointer list-none items-center gap-1 text-accent"><ChevronDown className="size-4 transition group-open:rotate-180" /> Sağlayıcıya gidecek metni gör (maskelenmiş)</summary>
        <pre className="mt-2 max-h-96 overflow-auto whitespace-pre-wrap rounded-xl bg-surface p-3 font-mono text-[12px] leading-relaxed">{plan.preview}</pre>
      </details>
      <div className="mt-4 flex flex-wrap items-center gap-3">
        {plan.allowed ? (
          <Button variant="primary" onClick={onStart} busy={busy}><Sparkles className="size-4" /> Onayla ve başlat</Button>
        ) : <ErrorNote>{plan.reason}</ErrorNote>}
        {plan.allowed && <span className="text-[12px] text-muted">Onay 15 dakika geçerli ve tek kullanımlık; yukarıdaki metin ve maliyet için verilir.</span>}
      </div>
    </div>
  )
}

const Mini = ({ label, value }: { label: string; value: string }) => (
  <div><div className="text-[12px] text-muted">{label}</div><div className="text-[15px] font-semibold tabular-nums">{value}</div></div>
)

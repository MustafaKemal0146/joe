import { useConfirm } from '@/components/Confirm'
import { ChevronDown, Loader2, ShieldAlert, Square, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { Badge, Button, Card, ErrorNote, Field, inputCls, PageHeader, Panel, Spinner } from '@/components/ui'
import { api } from '@/lib/api'
import { cn } from '@/lib/cn'
import { useAsync, useData } from '@/lib/data'
import { dateTime, num } from '@/lib/fmt'
import { go, href } from '@/lib/router'
import type { OsintFinding, OsintList, OsintPlan } from '@/lib/types'

const KIND_LABEL: Record<string, string> = { sites: 'Kullanıcı adı · WhatsMyName', 'sites:sherlock': 'Kullanıcı adı · Sherlock', 'sites:maigret': 'Kullanıcı adı · Maigret', dir_username: 'Kullanıcı adı · açık dizinler', dir_full_name: 'Tam ad · açık dizinler' }

const REVIEW: Record<string, { text: string; tone: 'neutral' | 'ok' | 'bad' | 'warn' }> = {
  unreviewed: { text: 'doğrulanmadı', tone: 'neutral' }, confirmed: { text: 'doğrulandı', tone: 'ok' }, rejected: { text: 'reddedildi', tone: 'bad' }, unsure: { text: 'belirsiz', tone: 'warn' },
}

export default function OsintPage({ id }: { id?: string }) {
  return id ? <RunView id={id} /> : <Home />
}

function Home() {
  const { toast } = useData()
  const sites = useAsync(() => api.osintSites(), [])
  const runs = useAsync(() => api.osintRuns(), [])
  const [username, setUsername] = useState('')
  const [ack, setAck] = useState(false)
  const [busy, setBusy] = useState('')
  const [listId, setListId] = useState<OsintList['id']>('wmn')
  const [limit, setLimit] = useState<string>('')

  async function update(id: string) {
    setBusy(id)
    try { const r = await api.osintUpdateSites(id); toast(`${num(r.count)} site yüklendi${r.skipped ? ` (${num(r.skipped)} destek dışı kayıt atlandı)` : ''}.`); sites.setData(await api.osintSites()) } catch (e) { toast(e instanceof Error ? e.message : 'İndirilemedi', 'bad') } finally { setBusy('') }
  }
  async function start() {
    setBusy('start')
    try { const r = await api.osintStart(username.trim().replace(/^@/, ''), listId, limit === '' ? undefined : Number(limit)); go(`/osint/${r.id}`) } catch (e) { toast(e instanceof Error ? e.message : 'Başlatılamadı', 'bad') } finally { setBusy('') }
  }

  const lists = sites.data?.lists ?? []
  const cur = lists.find((l) => l.id === listId)
  // Seçili liste indirilmediyse ilk kullanılabilir listeye geç.
  useEffect(() => { if (lists.length && !lists.find((l) => l.id === listId)?.available) { const f = lists.find((l) => l.available); if (f) setListId(f.id) } }, [lists]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <>
      <PageHeader title="OSINT" sub="Bir kullanıcı adının herkese açık sitelerde var olup olmadığını denetler. Sonuçlar aday bulgudur; kimlik doğrulaması değildir." />
      <Card className="mb-4 flex gap-3 border-warn/40 p-4 text-[13px]">
        <ShieldAlert className="mt-0.5 size-5 shrink-0 text-warn" />
        <div className="text-muted"><b className="text-fg">Sınırlar.</b> Yalnızca herkese açık profil adreslerine istek atılır; giriş, hesap bilgisi ya da özel içerik kullanılmaz. Aynı kullanıcı adı farklı kişilere ait olabilir: bir eşleşmeyi doğrulamadan kişi dosyasına bağlama. Hata veren siteler "hesap yok" anlamına gelmez, ayrı gösterilir.</div>
      </Card>
      <Panel title="Site listeleri" hint="Üç kaynaktan biri ya da hepsi; liste senin isteğinle indirilir, uygulamayla dağıtılmaz.">
        <ul className="divide-y divide-line">
          {lists.map((l) => (
            <li key={l.id} className="flex flex-wrap items-center gap-3 py-2.5 text-[13.5px]">
              <div className="min-w-0 flex-1">
                <div className="font-medium">{l.name} <span className="font-normal text-muted">· {l.license.split(', ')[1]}</span></div>
                <div className="text-[12.5px] text-muted">{l.available ? `${num(l.count ?? 0)} site${l.skipped ? ` (${num(l.skipped)} destek dışı atlandı)` : ''} · ${dateTime(l.updated_at)}` : (l.reason ?? 'Yüklenmedi')}</div>
              </div>
              <Button size="sm" busy={busy === l.id} onClick={() => update(l.id)}>{l.available ? 'Güncelle' : 'Listeyi indir'}</Button>
            </li>
          ))}
        </ul>
        <p className="mt-2 text-[12.5px] text-muted">Kaynaklar: WhatsMyName (CC BY-SA 4.0), Sherlock (MIT), Maigret (MIT). Yalnızca GET ile denetlenebilen siteler kullanılır; POST, özel başlık, bot koruması ya da etkinleştirme gerektirenler ve NSFW siteler atlanır ve sayılır.</p>
      </Panel>
      <div className="h-4" />
      <Panel title="Yeni tarama">
        <div className="grid gap-4 md:grid-cols-[1fr_200px_160px]">
          <Field label="Kullanıcı adı"><input className={inputCls} value={username} onChange={(e) => setUsername(e.target.value)} placeholder="ornek_kullanici" maxLength={41} /></Field>
          <Field label="Liste">
            <select className={inputCls} value={listId} onChange={(e) => { setListId(e.target.value as OsintList['id']); setLimit('') }}>
              {lists.map((l) => <option key={l.id} value={l.id} disabled={!l.available}>{l.name}{l.available ? ` (${num(l.count ?? 0)})` : ' — indirilmedi'}</option>)}
            </select>
          </Field>
          <Field label="En çok site">
            <input className={inputCls} type="number" min={0} max={20000} value={limit} onChange={(e) => setLimit(e.target.value)} placeholder={cur?.default_limit ? String(cur.default_limit) : 'hepsi'} />
          </Field>
        </div>
        <p className="mt-2 text-[12.5px] text-muted">Sınır, popülerliğe göre en çok ziyaret edilen siteleri seçer; boş bırakırsan varsayılan ({cur?.default_limit ? `${num(cur.default_limit)} site` : 'hepsi'}) uygulanır, 0 hepsini tarar. Büyük listeler dakikalar sürer; istediğinde durdurabilirsin, eldeki bulgular korunur.</p>
        <label className="mt-4 flex cursor-pointer items-start gap-2.5 rounded-xl bg-raised p-3 text-[13px]">
          <input type="checkbox" className="mt-0.5 size-4 accent-[var(--accent)]" checked={ack} onChange={(e) => setAck(e.target.checked)} />
          <span>Yalnızca herkese açık profil adreslerine istek atılacağını ve eşleşmenin <b>kimlik doğrulaması olmadığını</b> anlıyorum.</span>
        </label>
        <div className="mt-4"><Button variant="primary" busy={busy === 'start'} disabled={!cur?.available || !ack || username.trim().length < 2} onClick={start}>Taramayı başlat</Button></div>
      </Panel>
      <div className="h-4" />
      <Directories />
      <h2 className="mb-3 mt-8 text-[17px] font-semibold tracking-tight">Geçmiş taramalar</h2>
      {(runs.data?.length ?? 0) === 0 ? <p className="text-[13px] text-muted">Henüz tarama yok.</p> : (
        <Card className="divide-y divide-line">
          {runs.data!.map((r) => (
            <a key={r.id} href={href(`/osint/${r.id}`)} className="flex items-center gap-3 px-4 py-3 hover:bg-raised/50">
              <div className="min-w-0 flex-1"><div className="text-[14px] font-medium">{r.kind === 'dir_full_name' ? r.username : `@${r.username}`}</div><div className="text-[12.5px] text-muted">{KIND_LABEL[r.kind] ?? r.kind} · {dateTime(r.started_at)}</div></div>
              <Badge tone={r.status === 'done' ? 'ok' : r.status === 'running' ? 'accent' : 'warn'}>{r.status === 'done' ? 'bitti' : r.status === 'running' ? 'sürüyor' : r.status === 'failed' ? 'hata' : 'iptal'}</Badge>
              <span className="text-[13px] tabular-nums text-muted">{r.found} bulgu</span>
            </a>
          ))}
        </Card>
      )}
    </>
  )
}

function Directories() {
  const { toast } = useData()
  const [q, setQ] = useState('')
  const [kind, setKind] = useState<'username' | 'full_name'>('username')
  const [ack, setAck] = useState(false)
  const [busy, setBusy] = useState(false)
  const [plan, setPlan] = useState<OsintPlan | null>(null)
  const ok = q.trim().length >= 2

  async function search() {
    setBusy(true)
    try { const r = await api.osintDirectories(q.trim(), kind); go(`/osint/${r.id}`) } catch (e) { toast(e instanceof Error ? e.message : 'Başlatılamadı', 'bad') } finally { setBusy(false) }
  }
  async function makePlan() {
    try { setPlan(await api.osintPlan(q.trim(), kind)) } catch (e) { toast(e instanceof Error ? e.message : 'Plan üretilemedi', 'bad') }
  }

  return (
    <Panel title="Açık dizinler ve elle araştırma" hint="GitHub, GitLab, Codeberg ve (tam ad için) Stack Overflow'un herkese açık arama uçları. Site listesi gerekmez.">
      <div className="grid gap-4 md:grid-cols-[180px_1fr]">
        <Field label="Arama türü">
          <select className={inputCls} value={kind} onChange={(e) => { setKind(e.target.value as 'username' | 'full_name'); setPlan(null) }}>
            <option value="username">Kullanıcı adı</option><option value="full_name">Tam ad</option>
          </select>
        </Field>
        <Field label={kind === 'username' ? 'Kullanıcı adı' : 'Tam ad'}>
          <input className={inputCls} value={q} onChange={(e) => { setQ(e.target.value); setPlan(null) }} placeholder={kind === 'username' ? 'ornek_kullanici' : 'Ad Soyad'} maxLength={120} />
        </Field>
      </div>
      <label className="mt-4 flex cursor-pointer items-start gap-2.5 rounded-xl bg-raised p-3 text-[13px]">
        <input type="checkbox" className="mt-0.5 size-4 accent-[var(--accent)]" checked={ack} onChange={(e) => setAck(e.target.checked)} />
        <span>Yalnızca herkese açık dizinlere sorgu gideceğini; ad ya da kullanıcı adı benzerliğinin <b>sahiplik kanıtı olmadığını</b> anlıyorum.</span>
      </label>
      <div className="mt-4 flex flex-wrap gap-2">
        <Button variant="primary" busy={busy} disabled={!ok || !ack} onClick={search}>Dizinlerde ara</Button>
        <Button disabled={!ok} onClick={makePlan}>Elle araştırma planı</Button>
      </div>
      {plan && (
        <div className="mt-4 space-y-3 text-[13px]">
          <p className="text-muted">{plan.notice} Bu bağlantılar yalnızca sen tıklayınca açılır; Joe bu adreslere istek atmaz.</p>
          {plan.direct.length > 0 && <div className="flex flex-wrap gap-2">{plan.direct.map((l) => <a key={l.url} className="rounded-full bg-raised px-3 py-1 text-accent" href={l.url} target="_blank" rel="noopener noreferrer nofollow">{l.label}</a>)}</div>}
          <ul className="divide-y divide-line rounded-xl border border-line">
            {plan.searches.map((l, i) => (
              <li key={i} className="flex items-center gap-3 px-3 py-2">
                <span className="w-24 shrink-0 text-muted">{l.engine}</span>
                <a className="min-w-0 flex-1 truncate text-accent" href={l.url} target="_blank" rel="noopener noreferrer nofollow">{l.query}</a>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Panel>
  )
}

function RunView({ id }: { id: string }) {
  const confirm = useConfirm()
  const { people, toast } = useData()
  const [data, setData] = useState<Awaited<ReturnType<typeof api.osintRun>> | null>(null)
  const [error, setError] = useState<string | null>(null)
  const load = useCallback(async () => { try { setData(await api.osintRun(id)); setError(null) } catch (e) { setError(e instanceof Error ? e.message : 'Okunamadı') } }, [id])
  useEffect(() => { void load() }, [load])
  const running = data?.run.status === 'running'
  useEffect(() => { if (!running) return; const t = setInterval(() => void load(), 1000); return () => clearInterval(t) }, [running, load])

  if (error && !data) return <ErrorNote>{error}</ErrorNote>
  if (!data) return <Spinner />
  const { run, findings } = data
  const found = findings.filter((f) => f.outcome === 'found')
  const failed = findings.filter((f) => f.outcome === 'failed')
  const pct = run.total ? Math.round((run.done / run.total) * 100) : 0

  async function review(f: OsintFinding, r: string) { try { await api.osintReview(f.id, r); await load() } catch (e) { toast(e instanceof Error ? e.message : 'Kaydedilemedi', 'bad') } }
  async function attach(f: OsintFinding, pid: number) { try { await api.osintAttach(f.id, pid); toast('Kişi dosyasına bağlandı.'); await load() } catch (e) { toast(e instanceof Error ? e.message : 'Bağlanamadı', 'bad') } }

  return (
    <>
      <a className="text-[13px] text-accent" href={href('/osint')}>← OSINT</a>
      <div className="mb-5 mt-1 flex flex-wrap items-center gap-3">
        <div className="flex-1"><h1 className="text-[26px] font-semibold tracking-tight">{run.kind === 'dir_full_name' ? run.username : `@${run.username}`}</h1><p className="text-[13px] text-muted">{KIND_LABEL[run.kind] ?? run.kind}</p></div>
        {running && <Button onClick={async () => { await api.osintCancel(id); await load() }}><Square className="size-4" /> Durdur</Button>}
        {!running && <Button variant="danger" onClick={async () => { if (await confirm({ title: 'Bu tarama silinsin mi?', message: 'Taramanın tüm bulguları ve doğrulama kayıtların silinir.', confirmLabel: 'Taramayı sil' })) { await api.osintDelete(id); go('/osint') } }}><Trash2 className="size-4" /> Sil</Button>}
      </div>
      <Card className="mb-5 p-4">
        <div className="mb-1.5 flex items-center justify-between text-[13px]"><span className="flex items-center gap-2">{running && <Loader2 className="spin size-4" />}{num(run.done)} / {num(run.total)} {run.kind === 'sites' ? 'site' : 'dizin'} denetlendi</span><span className="text-muted">{pct}%</span></div>
        <div className="h-1.5 overflow-hidden rounded-full bg-raised"><div className="h-full rounded-full bg-accent" style={{ width: `${pct}%`, transition: 'width 400ms' }} /></div>
        <p className="mt-2 text-[12.5px] text-muted">{run.found} aday bulgu · {run.failed} site denetlenemedi{run.status === 'canceled' ? ' · tarama durduruldu, eldeki bulgular korundu' : ''}{run.error ? ` · ${run.error}` : ''}</p>
      </Card>
      <h2 className="mb-3 text-[17px] font-semibold tracking-tight">Aday bulgular ({found.length})</h2>
      {found.length === 0 ? <p className="text-[13px] text-muted">{running ? 'Henüz bulgu yok…' : 'Hiçbir sitede eşleşme bulunamadı.'}</p> : (
        <div className="space-y-2">
          {found.map((f) => {
            const rv = REVIEW[f.review]!
            return (
              <Card key={f.id} className="p-4">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-[14.5px] font-semibold">{f.site}</span>
                  <Badge>{f.category}</Badge><Badge tone={rv.tone}>{rv.text}</Badge>
                  {f.person_id ? <a href={href(`/kisi/${f.person_id}`)}><Badge tone="accent">{people.find((p) => p.id === f.person_id)?.name ?? 'kişi'} dosyasında</Badge></a> : null}
                </div>
                {f.detail && <div className="mt-0.5 text-[12.5px] text-muted">{f.detail}</div>}
                <a className="mt-1 block break-all text-[12.5px] text-accent" href={f.url} target="_blank" rel="noopener noreferrer nofollow">{f.url}</a>
                <div className="mt-3 flex flex-wrap items-center gap-2">
                  <Button size="sm" variant={f.review === 'confirmed' ? 'primary' : 'default'} onClick={() => review(f, 'confirmed')}>Doğrula</Button>
                  <Button size="sm" variant={f.review === 'unsure' ? 'primary' : 'default'} onClick={() => review(f, 'unsure')}>Belirsiz</Button>
                  <Button size="sm" variant={f.review === 'rejected' ? 'primary' : 'default'} onClick={() => review(f, 'rejected')}>Reddet</Button>
                  {f.review === 'confirmed' && (
                    <select className={cn(inputCls, 'h-8 w-auto text-[13px]')} value={f.person_id ?? 0} onChange={(e) => { const v = Number(e.target.value); if (v) void attach(f, v) }} aria-label="Kişi dosyasına bağla">
                      <option value={0}>Kişi dosyasına bağla…</option>
                      {people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                    </select>
                  )}
                </div>
              </Card>
            )
          })}
        </div>
      )}
      {failed.length > 0 && (
        <details className="group mt-6 text-[13px]">
          <summary className="flex cursor-pointer list-none items-center gap-1 text-muted"><ChevronDown className="size-4 transition group-open:rotate-180" /> Denetlenemeyen siteler ({failed.length}) — bu, hesabın olmadığı anlamına gelmez</summary>
          <ul className="mt-2 space-y-1 rounded-xl bg-raised p-3">{failed.map((f) => <li key={f.id} className="flex justify-between gap-3"><span>{f.site}</span><span className="text-muted">{f.detail}</span></li>)}</ul>
        </details>
      )}
    </>
  )
}

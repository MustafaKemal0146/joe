import { CheckCircle2, ExternalLink, ShieldCheck } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Badge, Button, Card, ErrorNote, Field, inputCls, PageHeader, Panel, Spinner } from '@/components/ui'
import { api } from '@/lib/api'
import { cn } from '@/lib/cn'
import { useAsync, useData } from '@/lib/data'
import { dateTime, num, usd } from '@/lib/fmt'
import type { EmbedDiscovery, EmbedStatus } from '@/lib/types'

export default function SettingsPage() {
  const { reload, toast } = useData()
  const { data: s, error, loading, setData } = useAsync(() => api.settings(), [])
  const [key, setKey] = useState('')
  const [model, setModel] = useState('')
  const [budget, setBudget] = useState('0.25')
  const [strong, setStrong] = useState('')
  const [busy, setBusy] = useState(false)
  const [test, setTest] = useState<{ ok: boolean; text: string } | null>(null)

  useEffect(() => {
    if (s) { setModel(s.active_model); setBudget(String(s.budget_usd)); setStrong(s.strong_model ?? '') }
  }, [s?.active_model, s?.budget_usd, s?.strong_model]) // eslint-disable-line react-hooks/exhaustive-deps

  if (loading) return <Spinner />
  if (error || !s) return <ErrorNote>{error ?? 'Ayarlar okunamadı'}</ErrorNote>
  const active = s.providers.find((p) => p.active)!
  const models = s.models[active.id] ?? []

  async function save(u: Record<string, unknown>, ok: string) {
    setBusy(true)
    try { setData(await api.saveSettings(u)); await reload(); toast(ok) } catch (e) { toast(e instanceof Error ? e.message : 'Kaydedilemedi', 'bad') } finally { setBusy(false) }
  }

  return (
    <>
      <PageHeader title="Ayarlar" sub="Yapay zekâ sağlayıcısı, model ve bütçe. Metrikler ve harita bunlar olmadan da çalışır." />
      <div className="space-y-4">
        <Panel title="Sağlayıcı" hint="Konsey çağrıları bu sağlayıcıya gider. DeepSeek, önbellek indirimi sayesinde bu iş yükü için en ucuz seçenektir.">
          <div className="grid gap-2 sm:grid-cols-2">
            {s.providers.map((p) => (
              <button key={p.id} onClick={() => { setTest(null); void save({ active_provider: p.id, active_model: p.default_model || s.active_model }, `${p.name} etkin.`) }}
                className={cn('flex items-center gap-3 rounded-2xl border p-3.5 text-left transition', p.active ? 'border-accent bg-accent-soft' : 'border-line hover:bg-raised')}>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 text-[14px] font-semibold">{p.name}{p.id === 'deepseek' && <Badge tone="accent">önerilen</Badge>}</div>
                  <div className="truncate text-[12px] text-muted">{p.needs_key ? (p.has_key ? 'Anahtar kayıtlı' : 'Anahtar gerekli') : 'Anahtar gerekmez'}</div>
                </div>
                {p.has_key && <CheckCircle2 className="size-4 text-ok" />}
              </button>
            ))}
          </div>
        </Panel>

        <Panel title={`${active.name} bağlantısı`}>
          <div className="grid gap-4 md:grid-cols-2">
            {active.needs_key && (
              <Field label="API anahtarı" hint={active.has_key ? 'Bir anahtar kayıtlı. Değiştirmek için yenisini yaz.' : 'Anahtar bu bilgisayarda şifreli saklanır; arayüze geri gönderilmez.'}>
                <input className={inputCls} type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} placeholder={active.has_key ? '••••••••••••' : 'sk-…'} />
              </Field>
            )}
            <Field label="Model">
              {models.length > 0 ? (
                <select className={inputCls} value={model} onChange={(e) => setModel(e.target.value)}>
                  {!models.includes(model) && model && <option value={model}>{model}</option>}
                  {models.map((m) => <option key={m} value={m}>{m}{s.prices[m] ? ` · $${s.prices[m].in}/$${s.prices[m].out} (1M token)` : ''}</option>)}
                </select>
              ) : <input className={inputCls} value={model} onChange={(e) => setModel(e.target.value)} placeholder="model kimliği" />}
            </Field>
          </div>
          <div className="mt-4 flex flex-wrap items-center gap-2">
            <Button variant="primary" busy={busy} onClick={async () => {
              const u: Record<string, unknown> = { active_model: model }
              if (key.trim()) { u.provider_id = active.id; u.api_key = key.trim() }
              await save(u, 'Kaydedildi.'); setKey('')
            }}>Kaydet</Button>
            <Button busy={busy} disabled={!s.ready && !key} onClick={async () => {
              setBusy(true); setTest(null)
              try {
                const r = await api.testProvider()
                setTest(r.ok ? { ok: true, text: `Çalışıyor · ${r.ms} ms · ${r.tokens} token${r.cost_usd ? ` · ${usd(r.cost_usd)}` : ''}` } : { ok: false, text: r.error ?? 'Başarısız' })
              } catch (e) { setTest({ ok: false, text: e instanceof Error ? e.message : 'Başarısız' }) } finally { setBusy(false) }
            }}>Bağlantıyı sına</Button>
            {active.key_url && <a className="inline-flex items-center gap-1 text-[13px] text-accent" href={active.key_url} target="_blank" rel="noreferrer noopener">Anahtar al <ExternalLink className="size-3" /></a>}
          </div>
          {test && <p role="status" className={cn('mt-3 text-[13px]', test.ok ? 'text-ok' : 'text-bad')}>{test.text}</p>}
          <p className="mt-3 text-[12px] text-muted">"Sına" gerçek ama çok küçük bir istektir (birkaç token).</p>
        </Panel>

        <Panel title="Konsey bütçesi" hint="Tek bir konsey oturumunun üst sınırı. Aşılırsa iş durur ve o ana kadarki sonuçlar korunur.">
          <div className="flex items-end gap-3">
            <Field label="Üst sınır (USD)"><input className={`${inputCls} w-36`} inputMode="decimal" value={budget} onChange={(e) => setBudget(e.target.value)} /></Field>
            <Button busy={busy} disabled={!(Number(budget.replace(',', '.')) >= 0) || budget.trim() === ''} onClick={() => void save({ budget_usd: Number(budget.replace(',', '.')) }, 'Bütçe kaydedildi.')}>Kaydet</Button>
          </div>
        </Panel>

        <Panel title="Güçlü model" hint="Moderatör, genel değerlendirme ve kanıt doğrulaması bu modelle yapılır; mercekler etkin modelle çalışır. Boş bırakırsan uygun bir model otomatik seçilir; kapatmak için “-” yaz.">
          <div className="flex flex-wrap items-end gap-3">
            <Field label="Model kimliği"><input className={`${inputCls} w-64`} value={strong} onChange={(e) => setStrong(e.target.value)} placeholder="otomatik" /></Field>
            <Button busy={busy} onClick={() => void save({ strong_model: strong.trim() }, 'Güçlü model kaydedildi.')}>Kaydet</Button>
          </div>
          <p className="mt-3 text-[12.5px] text-muted">Şu an kullanılan: <b className="text-fg">{s.strong_model_effective || 'etkin modelin kendisi'}</b></p>
        </Panel>

        <Embedding />

        <Spending />

        <Card className="flex gap-3 p-4">
          <ShieldCheck className="mt-0.5 size-5 shrink-0 text-ok" />
          <div className="text-[13px] text-muted">
            <b className="text-fg">Gizlilik.</b> Mesajlar ve etkinlikler yalnızca bu bilgisayardaki veritabanında durur. Sağlayıcıya yalnızca konsey çalıştırdığında,
            önizlemesini gördüğün seçilmiş kesitler gider. Joe yalnızca <code>127.0.0.1</code> adresinde dinler.
          </div>
        </Card>
      </div>
    </>
  )
}

function Spending() {
  const { data, error } = useAsync(() => api.ledger(), [])
  if (error) return null
  if (!data) return null
  return (
    <Panel title="Harcama" hint="Yapay zekâ çağrılarının yerel defteri. Önbellekten dönen çağrılar para harcamaz.">
      {data.calls === 0 ? <p className="text-[13px] text-muted">Henüz yapay zekâ çağrısı yapılmadı: harcama $0.</p> : (
        <>
          <div className="flex flex-wrap gap-x-8 gap-y-2">
            <div><div className="text-[12px] text-muted">Toplam</div><div className="text-[22px] font-semibold tabular-nums">{usd(data.total_usd)}</div></div>
            <div><div className="text-[12px] text-muted">Çağrı</div><div className="text-[22px] font-semibold tabular-nums">{num(data.calls)}</div></div>
            <div><div className="text-[12px] text-muted">Önbellekten</div><div className="text-[22px] font-semibold tabular-nums">{num(data.cached_calls)}</div></div>
            <div><div className="text-[12px] text-muted">Token</div><div className="text-[22px] font-semibold tabular-nums">{num(data.prompt_tokens + data.completion_tokens)}</div></div>
          </div>
          <ul className="mt-4 divide-y divide-line text-[13px]">
            {data.sessions.map((s) => (
              <li key={s.session} className="flex items-center gap-3 py-2">
                <span className="min-w-0 flex-1 truncate font-medium">{s.session}</span>
                <span className="text-muted">{num(s.calls)} çağrı</span>
                <span className="w-16 text-right tabular-nums">{usd(s.usd)}</span>
                <span className="hidden w-36 text-right text-faint sm:block">{dateTime(s.last)}</span>
              </li>
            ))}
          </ul>
        </>
      )}
    </Panel>
  )
}

function Embedding() {
  const { toast } = useData()
  const settings = useAsync(() => api.settings(), [])
  const [status, setStatus] = useState<EmbedStatus | null>(null)
  const [url, setUrl] = useState('')
  const [model, setModel] = useState('')
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [found, setFound] = useState<EmbedDiscovery | null>(null)
  const [looking, setLooking] = useState(false)

  useEffect(() => { if (settings.data) { setUrl(settings.data.embed.url); setModel(settings.data.embed.model) } }, [settings.data])
  // Henüz ayarlı değilse, yerel Ollama'yı sessizce tara: uygun model varsa tek tıkla kurulum öner.
  useEffect(() => {
    if (!settings.data || settings.data.embed.url) return
    api.embedDiscover().then(setFound).catch(() => {})
  }, [settings.data])
  const refresh = async () => { try { setStatus(await api.embedStatus()) } catch { /* sunucu yok */ } }
  useEffect(() => { void refresh() }, [settings.data])
  useEffect(() => {
    if (!status?.running) return
    const t = setInterval(() => void refresh(), 1200)
    return () => clearInterval(t)
  }, [status?.running])

  async function save() {
    setBusy(true)
    try {
      const u: Record<string, unknown> = { embed_url: url, embed_model: model }
      if (key.trim()) u.embed_key = key.trim()
      settings.setData(await api.saveSettings(u)); setKey(''); toast('Gömme ayarı kaydedildi.')
    } catch (e) { toast(e instanceof Error ? e.message : 'Kaydedilemedi', 'bad') } finally { setBusy(false) }
  }
  async function look() {
    setLooking(true)
    try { setFound(await api.embedDiscover()) } catch (e) { toast(e instanceof Error ? e.message : 'Taranamadı', 'bad') } finally { setLooking(false) }
  }
  async function auto() {
    setBusy(true)
    try {
      const r = await api.embedAuto()
      toast(`${r.model} kuruldu (${r.dim} boyut)${r.indexing_started ? '; dizinleme başladı' : ''}.`)
      settings.setData(await api.settings()); setFound(null); await refresh()
    } catch (e) { toast(e instanceof Error ? e.message : 'Otomatik kurulum başarısız', 'bad') } finally { setBusy(false) }
  }
  const pct = status?.total ? Math.round(((status.done ?? 0) / status.total) * 100) : 0
  return (
    <Panel title="Anlamsal arama (gömme)" hint="Mesajların anlam vektörleri yerelde saklanır. OpenAI uyumlu bir /embeddings uç noktası gerekir: yerel bir bge-m3 sunucusu ya da ucuz bir API. Sohbet modeli olarak yerel model gerekmez.">
      <div className="grid gap-4 md:grid-cols-3">
        <Field label="Adres"><input className={inputCls} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="http://127.0.0.1:11434/v1" /></Field>
        <Field label="Model"><input className={inputCls} value={model} onChange={(e) => setModel(e.target.value)} placeholder="bge-m3" /></Field>
        <Field label="Anahtar (varsa)" hint={settings.data?.embed.has_key ? 'Bir anahtar kayıtlı.' : undefined}><input className={inputCls} type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} /></Field>
      </div>
      {found?.verified && found.recommended && (
        <div className="mb-4 flex flex-wrap items-center gap-3 rounded-xl border border-accent/40 bg-accent-soft p-3 text-[13px]">
          <div className="min-w-0 flex-1"><b>Ollama'da uygun model bulundu:</b> {found.recommended} ({found.dim} boyut{found.candidates[0]?.reason ? `, ${found.candidates[0].reason}` : ''}).</div>
          <Button size="sm" variant="primary" busy={busy} onClick={auto}>Otomatik kur ve dizinle</Button>
        </div>
      )}
      {found && !found.verified && <p className="mb-4 rounded-xl bg-raised p-3 text-[13px] text-muted">{found.hint ?? 'Uygun model bulunamadı.'}</p>}
      {found && found.candidates.length > 1 && (
        <div className="mb-4 flex flex-wrap gap-1.5 text-[12.5px]">
          <span className="text-muted">Bulunan modeller:</span>
          {found.candidates.map((c) => (
            <button key={c.name} onClick={() => { setUrl(found.openai_url); setModel(c.name) }} className="rounded-full bg-raised px-2.5 py-1 hover:bg-sunken" title={c.reason}>{c.name}</button>
          ))}
        </div>
      )}
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <Button busy={looking} onClick={look}>Ollama'yı otomatik bul</Button>
        <Button busy={busy} onClick={save} disabled={!url.trim() || !model.trim()}>Kaydet</Button>
        {status?.configured && !status.running && <Button variant="primary" onClick={async () => { try { await api.embedRun(); await refresh() } catch (e) { toast(e instanceof Error ? e.message : 'Başlatılamadı', 'bad') } }}>Dizinlemeyi başlat</Button>}
        {status?.running && <Button onClick={async () => { await api.embedCancel(); await refresh() }}>Durdur</Button>}
      </div>
      {status?.configured && (
        <label className="mt-4 flex cursor-pointer items-start gap-2.5 text-[13px]">
          <input type="checkbox" className="mt-0.5 size-4 accent-[var(--accent)]" checked={settings.data?.embed.auto ?? false}
            onChange={async (e) => { try { settings.setData(await api.saveSettings({ embed_auto: e.target.checked })) } catch (err) { toast(err instanceof Error ? err.message : 'Kaydedilemedi', 'bad') } }} />
          <span>Yeni bir kaynak içe aktarınca otomatik dizinle <span className="text-muted">(yalnızca gömme adresi yerel olduğunda çalışır; mesajlar uzak bir sunucuya kendiliğinden gitmez)</span></span>
        </label>
      )}
      {status?.configured && (
        <div className="mt-4">
          <div className="mb-1 flex justify-between text-[12.5px] text-muted"><span>{num(status.done ?? 0)} / {num(status.total ?? 0)} mesaj gömüldü</span><span>{pct}%</span></div>
          <div className="h-1.5 overflow-hidden rounded-full bg-raised"><div className="h-full rounded-full bg-accent" style={{ width: `${pct}%`, transition: 'width 400ms' }} /></div>
          {status.error && <p role="alert" className="mt-2 text-[12.5px] text-bad">{status.error}</p>}
          <p className="mt-2 text-[12px] text-muted">Kaldığı yerden devam eder; yalnızca yeni mesajlar gömülür.</p>
        </div>
      )}
    </Panel>
  )
}

import { Check, FileUp, Loader2, X } from 'lucide-react'
import { useRef, useState } from 'react'
import { Badge, Button, Card, inputCls } from '@/components/ui'
import { api } from '@/lib/api'
import { useAsync, useData } from '@/lib/data'
import { dateShort, num, platformName } from '@/lib/fmt'
import { go } from '@/lib/router'
import type { Contact, ImportResult, Person, Source } from '@/lib/types'

type Step = 'file' | 'owner' | 'who' | 'same' | 'consent' | 'kind' | 'declined'

const fold = (s: string) => s.toLocaleLowerCase('tr-TR').replace(/[ıİ]/g, 'i').normalize('NFD').replace(/[̀-ͯ]/g, '').replace(/[^a-z0-9]+/g, ' ').trim()

/** WhatsApp dosya adından ("WhatsApp Sohbeti - Ayşe.txt") karşı taraf tahmini. */
export const partnerFromFile = (name: string): string => {
  const m = name.replace(/\.[^.]+$/, '').match(/(?:sohbeti|chat with|with)\s*[-–]?\s*(.+)$/i) ?? name.replace(/\.[^.]+$/, '').match(/-\s*(.+)$/)
  return m?.[1]?.trim() ?? ''
}

/** İki ad aynı kişi olabilir mi? İlk sözcük eşleşmesi ya da kimlik/kullanıcı adı içermesi yeterlidir; karar kullanıcıdadır. */
export function maybeSame(name: string, p: Person): boolean {
  const a = fold(name).split(' ')[0] ?? ''
  if (a.length < 3) return false
  return [p.name, ...p.identities.map((i) => i.value)].some((v) => { const f = fold(v); return f.split(' ')[0] === a || f.includes(a) || a.includes(f.split(' ')[0] ?? '#') && f.length >= 3 })
}

/**
 * Sohbet içi kurulum sihirbazı: dosya bırak (ya da yol yaz) → özet → hesap sahibi → kimi analiz edeceksin
 * → kimlik belirsizse sor → rıza → tür. Her adım deterministiktir (model çağrısı yok, maliyet yok);
 * hiçbir kimlik onay olmadan birleştirilmez. Sihirbaz kapatılabilir ve yeniden açılabilir.
 */
export default function Onboarding({ onClose, initialFiles }: { onClose: () => void; initialFiles?: File[] }) {
  const { toast, reload, people } = useData()
  const [step, setStep] = useState<Step>('file')
  const [results, setResults] = useState<{ name: string; r?: ImportResult; error?: string }[]>([])
  const [source, setSource] = useState<Source | null>(null)
  const [guess, setGuess] = useState('')
  const [target, setTarget] = useState<Person | null>(null)
  const [pending, setPending] = useState<{ c: Contact; similar: Person[] } | null>(null)
  const [consent, setConsent] = useState<'self' | 'consented' | ''>('')
  const [busy, setBusy] = useState(false)
  const [owner, setOwner] = useState('')
  const [drag, setDrag] = useState(false)
  const [path, setPath] = useState('')
  const input = useRef<HTMLInputElement>(null)
  const started = useRef(false)
  const me = people.find((p) => p.is_self)

  /** .txt dosyaları ayrı sohbetlerdir (her biri ayrı içe aktarılır); .zip parçaları tek arşivin parçalarıdır. */
  async function ingest(files: File[]) {
    if (!files.length) return
    setBusy(true); setResults([])
    const groups: File[][] = [...files.filter((f) => !/\.zip$/i.test(f.name)).map((f) => [f])]
    const zips = files.filter((f) => /\.zip$/i.test(f.name))
    if (zips.length) groups.push(zips)
    await run(groups.map((g) => ({ name: g.map((f) => f.name).join(' + '), hint: g.length === 1 ? partnerFromFile(g[0]!.name) : '', go: () => api.importFiles(g) })))
  }
  async function ingestPath() {
    const paths = path.split('\n').map((p) => p.trim()).filter(Boolean)
    if (!paths.length) return
    setBusy(true); setResults([])
    await run([{ name: paths.join(' + '), hint: paths.length === 1 ? partnerFromFile(paths[0]!.split('/').pop() ?? '') : '', go: () => api.importPath(paths) }])
  }
  async function run(jobs: { name: string; hint: string; go: () => Promise<ImportResult> }[]) {
    const out: { name: string; r?: ImportResult; error?: string }[] = []
    let first: ImportResult | undefined
    let hint = ''
    for (const j of jobs) {
      try {
        const r = await j.go()
        out.push({ name: j.name, r })
        if (!first || (first.duplicate && !r.duplicate)) { first = r; hint = j.hint }
      } catch (e) {
        out.push({ name: j.name, error: e instanceof Error ? e.message : 'Dosya okunamadı. WhatsApp için .txt, Instagram için .zip bekliyorum.' })
      }
    }
    setResults(out)
    await reload()
    setBusy(false)
    if (first) { setSource(first.source); setOwner(first.source.owner_name); setGuess(hint); if (!first.duplicate || first.owner_needed) setStep(first.owner_needed || !first.source.owner_confirmed ? 'owner' : 'who'); else setStep('who') }
  }
  if (initialFiles?.length && !started.current) { started.current = true; void ingest(initialFiles) }

  async function confirmOwner() {
    if (!source || !owner.trim()) return
    setBusy(true)
    try {
      const r = await api.setOwner(source.id, owner.trim(), source.owner_handle)
      setSource(r.source); await reload()
      if (r.partners?.length) toast(`Karşı taraf için de kişi dosyası açtım: ${r.partners.map((p) => p.name).join(', ')}.`)
      setStep('who')
    }
    catch (e) { toast(e instanceof Error ? e.message : 'Kaydedilemedi', 'bad') } finally { setBusy(false) }
  }
  async function pick(c: Contact) {
    const existing = c.person_id ? people.find((p) => p.id === c.person_id) : undefined
    if (existing) { setTarget(existing); setStep('consent'); return }
    const similar = people.filter((p) => !p.is_self && maybeSame(c.name, p))
    // Belirsiz eşleşmede otomatik birleştirme yok: kullanıcıya sorulur.
    if (similar.length) { setPending({ c, similar }); setStep('same'); return }
    await create(c)
  }
  async function create(c: Contact) {
    setBusy(true)
    try { const p = await api.createPerson(c.name, [c.name]); await reload(); setTarget(p); setStep('consent') }
    catch (e) { toast(e instanceof Error ? e.message : 'Kişi dosyası açılamadı', 'bad') } finally { setBusy(false) }
  }
  async function merge(p: Person) {
    if (!pending) return
    setBusy(true)
    try { const u = await api.addIdentity(p.id, pending.c.name); await reload(); setTarget(u); setStep('consent') }
    catch (e) { toast(e instanceof Error ? e.message : 'Kimlik eklenemedi', 'bad') } finally { setBusy(false) }
  }
  async function saveConsent(v: 'self' | 'consented' | 'no') {
    if (v === 'no') { setStep('declined'); return }
    setConsent(v)
    if (target) { try { await api.addNote(target.id, `Rıza beyanı (kurulum sihirbazı): ${v === 'self' ? 'veri kullanıcının kendisine ait' : 'veri sahibinin rızasıyla alındı'}.`) } catch { /* not kaydı yardımcıdır; analiz yine rıza kutusunu ister */ } }
    setStep('kind')
  }

  const contacts = useAsync(() => (source && step === 'who' ? api.contacts(source.id) : Promise.resolve([] as Contact[])), [source?.id, step])
  const dms = (contacts.data ?? []).filter((c) => !c.is_group)
  const main = results.find((x) => x.r && !x.r.duplicate)?.r ?? results.find((x) => x.r)?.r
  const participants = main?.summary.participants ?? []
  const likelyMe = guess ? participants.find((p) => fold(p) !== fold(guess) && !fold(p).includes(fold(guess))) : undefined

  return (
    <Card className="mb-4 p-5">
      <div className="mb-3 flex items-center gap-2">
        <h2 className="flex-1 text-[16px] font-semibold tracking-tight">Kurulum sihirbazı</h2>
        <Badge tone="neutral">ücretsiz · model çağrısı yok</Badge>
        <button aria-label="Sihirbazı kapat" className="rounded-lg p-1.5 text-muted hover:bg-raised" onClick={onClose}><X className="size-4" /></button>
      </div>

      {step === 'file' && (
        <div className="space-y-3">
          <p className="text-[14px]">Merhaba, ben Joe. Önce elindeki veriyi bırak: WhatsApp sohbetleri (.txt, birden fazlasını birlikte bırakabilirsin) ya da Instagram dışa aktarımı (.zip; çok parçalıysa parçaları birlikte bırak).</p>
          <div role="button" tabIndex={0}
            onDragOver={(e) => { e.preventDefault(); setDrag(true) }} onDragLeave={() => setDrag(false)}
            onDrop={(e) => { e.preventDefault(); setDrag(false); void ingest([...e.dataTransfer.files]) }}
            onClick={() => input.current?.click()} onKeyDown={(e) => e.key === 'Enter' && input.current?.click()}
            className={`flex cursor-pointer flex-col items-center gap-2 rounded-2xl border-2 border-dashed p-8 text-center text-[13.5px] ${drag ? 'border-accent bg-accent-soft' : 'border-line text-muted'}`}>
            {busy ? <Loader2 className="spin size-6" /> : <FileUp className="size-6" />}
            {busy ? 'İçe aktarılıyor… büyük arşivlerde birkaç dakika sürebilir; sayfayı kapatma.' : 'Dosyaları buraya sürükle ya da seçmek için tıkla'}
          </div>
          <input ref={input} type="file" multiple hidden accept=".txt,.zip" onChange={(e) => void ingest([...(e.target.files ?? [])])} />
          <details className="text-[13px]">
            <summary className="cursor-pointer text-muted">Ya da bilgisayardaki dosya yolunu yaz (büyük arşivler için daha hızlı)</summary>
            <div className="mt-2 flex gap-2">
              <textarea className={`${inputCls} min-h-[44px] flex-1`} rows={1} placeholder="/home/kullanici/instagram-veri.zip" value={path} onChange={(e) => setPath(e.target.value)} aria-label="Dosya yolu" />
              <Button busy={busy} disabled={!path.trim()} onClick={ingestPath}>İçe aktar</Button>
            </div>
          </details>
          {me && <p className="text-[12.5px] text-muted">Zaten yüklü verin varsa <button className="text-accent" onClick={() => setStep('who')}>doğrudan kişi seçmeye geç</button>.</p>}
        </div>
      )}

      {results.length > 0 && step !== 'file' && step !== 'declined' && <ImportCards results={results} />}
      {results.length > 0 && step === 'file' && <ImportCards results={results} />}

      {step === 'owner' && source && (
        <div className="mt-3 space-y-3">
          <p className="text-[14px]">Bu dışa aktarımı kim yaptı, yani bu verideki "ben" kim?{guess && <> Dosya adından sohbetin <b>{guess}</b> ile olduğunu anladım{likelyMe ? <>; büyük olasılıkla sen <b>{likelyMe}</b>'sın.</> : '.'}</>}</p>
          {participants.length > 0 && <div className="flex flex-wrap gap-2">{participants.map((p) => <Button key={p} size="sm" variant={p === (likelyMe ?? owner) ? 'primary' : 'default'} onClick={() => setOwner(p)}>{p}</Button>)}</div>}
          <input className={inputCls} value={owner} onChange={(e) => setOwner(e.target.value)} placeholder="Sohbette görünen adın" />
          <p className="text-[12.5px] text-muted">{source.owner_name ? `Dosyadan tahminim "${source.owner_name}"; yanlışsa düzelt.` : 'Hesap sahibini dosyadan çıkaramadım.'} Yanlış seçim, kimin hangi mesajı yazdığını karıştırır.</p>
          <Button variant="primary" busy={busy} disabled={!owner.trim()} onClick={confirmOwner}>Bu benim</Button>
        </div>
      )}

      {step === 'who' && (
        <div className="mt-3 space-y-3">
          <p className="text-[14px]">Kimi anlamak istiyorsun? Bir sohbet karşısı seç; onun için kişi dosyası açarım. Başka bir adla tanımladığın biri olabilirse sana soracağım, kendiliğimden birleştirmem.</p>
          {contacts.loading ? <Loader2 className="spin size-5" /> : dms.length === 0 ? <p className="text-[13px] text-muted">Bire-bir sohbet bulamadım. Kaynaklar sayfasından kontrol edebilirsin.</p> : (
            <ul className="max-h-72 divide-y divide-line overflow-auto rounded-xl border border-line">
              {dms.slice(0, 30).map((c) => (
                <li key={c.conv_id} className="flex items-center gap-3 px-3 py-2 text-[13.5px]">
                  <span className="min-w-0 flex-1 truncate">{c.name}</span><span className="text-muted tabular-nums">{num(c.count)} mesaj</span>
                  <Button size="sm" busy={busy} onClick={() => void pick(c)}>Bunu analiz et</Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {step === 'same' && pending && (
        <div className="mt-3 space-y-3">
          <p className="text-[14px]">"<b>{pending.c.name}</b>" adı, mevcut bir dosyaya benziyor. Aynı kişi mi?</p>
          <ul className="space-y-2">
            {pending.similar.map((p) => (
              <li key={p.id} className="flex flex-wrap items-center gap-3 rounded-xl border border-line px-3 py-2 text-[13.5px]">
                <span className="min-w-0 flex-1"><b>{p.name}</b> <span className="text-muted">{p.identities.map((i) => i.value).join(', ')}</span></span>
                <Button size="sm" busy={busy} onClick={() => void merge(p)}>Evet, aynı kişi</Button>
              </li>
            ))}
          </ul>
          <Button busy={busy} onClick={() => void create(pending.c)}>Hayır, ayrı bir kişi dosyası aç</Button>
          <p className="text-[12.5px] text-muted">"Evet" dersen bu ad, seçtiğin dosyaya kimlik olarak eklenir; sonradan Kişiler sayfasından kaldırabilirsin.</p>
        </div>
      )}

      {step === 'consent' && target && (
        <div className="mt-3 space-y-3">
          <p className="text-[14px]"><b>{target.name}</b> hakkında analiz yapmadan önce: bu veri kime ait?</p>
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => void saveConsent('self')}>Kendi verim</Button>
            <Button onClick={() => void saveConsent('consented')}>Karşı tarafın rızasıyla</Button>
            <Button variant="danger" onClick={() => void saveConsent('no')}>İkisi de değil</Button>
          </div>
          <p className="text-[12.5px] text-muted">Cevabın kişi dosyasına not olarak kaydedilir. Analiz ekranı ayrıca onayını ister.</p>
        </div>
      )}

      {step === 'declined' && (
        <div className="mt-3 space-y-3">
          <p className="text-[14px]">Anlaşıldı: rıza ya da sahiplik yoksa analiz başlatmıyorum. Veri yerelde kalır; istersen Kaynaklar'dan silebilirsin.</p>
          <Button onClick={onClose}>Tamam</Button>
        </div>
      )}

      {step === 'kind' && target && (
        <div className="mt-3 space-y-3">
          <p className="flex items-center gap-2 text-[14px]"><Check className="size-4 text-ok" /> Hazır. Nasıl bir analiz istersin? ({consent === 'self' ? 'kendi verin' : 'rızalı veri'})</p>
          <div className="flex flex-wrap gap-2">
            <Button variant="primary" onClick={() => go(`/konsey?kisi=${target.id}`)}>{target.name} için tek kişilik analiz</Button>
            {me && me.id !== target.id && <Button onClick={() => go(`/konsey?kisi=${me.id}&ikinci=${target.id}&tur=pair`)}>{me.name} × {target.name} ilişki analizi</Button>}
            <Button onClick={() => go(`/kisi/${target.id}`)}>Önce metriklere bak (ücretsiz)</Button>
          </div>
          <p className="text-[12.5px] text-muted">Konsey sayfasında önce maliyeti ve modele gidecek metni görürsün; onaylamadan hiçbir şey gönderilmez.</p>
        </div>
      )}
    </Card>
  )
}

function ImportCards({ results }: { results: { name: string; r?: ImportResult; error?: string }[] }) {
  return (
    <ul className="mt-3 space-y-2">
      {results.map((x, i) => (
        <li key={i} className={`rounded-xl border p-3 text-[13px] ${x.error ? 'border-bad/40' : 'border-line'}`}>
          <div className="flex flex-wrap items-center gap-2">
            <span className="min-w-0 flex-1 truncate font-medium">{x.name}</span>
            {x.r && <Badge tone={x.r.duplicate ? 'warn' : 'ok'}>{x.r.duplicate ? 'daha önce yüklenmiş' : 'alındı'}</Badge>}
            {x.r && <Badge>{platformName(x.r.source.platform)}</Badge>}
          </div>
          {x.error && <p className="mt-1 text-bad">{x.error}</p>}
          {x.r && (
            <p className="mt-1 text-muted">
              {num(x.r.summary.conversations)} sohbet · {num(x.r.summary.messages)} mesaj{x.r.summary.first && !x.r.summary.first.startsWith('0001') ? ` · ${dateShort(x.r.summary.first)} – ${dateShort(x.r.summary.last)}` : ''}
              {x.r.duplicate && ' · aynı veriyi ikinci kez eklemedim'}
            </p>
          )}
          {x.r && x.r.summary.warnings.length > 0 && <ul className="mt-1 list-disc pl-5 text-warn">{x.r.summary.warnings.slice(0, 4).map((w, j) => <li key={j}>{w}</li>)}</ul>}
        </li>
      ))}
    </ul>
  )
}

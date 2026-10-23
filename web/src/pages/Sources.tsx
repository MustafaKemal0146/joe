import { useConfirm } from '@/components/Confirm'
import { AlertTriangle, ChevronDown, Trash2, UserPlus } from 'lucide-react'
import { useState } from 'react'
import { Importer } from '@/components/Importer'
import { Badge, Button, Card, Chip, ErrorNote, inputCls, PageHeader, PlatformBadge, Spinner } from '@/components/ui'
import { api } from '@/lib/api'
import { useAsync, useData } from '@/lib/data'
import { dateShort, num } from '@/lib/fmt'
import { href } from '@/lib/router'
import type { Contact, Source } from '@/lib/types'

export default function Sources() {
  const { sources, loaded } = useData()
  return (
    <>
      <PageHeader title="Kaynaklar" sub="İçe aktarılan dışa aktarımlar. Silince bağlı mesaj ve etkinlikler de silinir; kişi dosyaların kalır." />
      <Importer compact={sources.length > 0} />
      {!loaded ? <Spinner /> : sources.length === 0 ? null : (
        <div className="mt-6 space-y-4">
          {sources.map((s) => <SourceCard key={s.id} s={s} />)}
        </div>
      )}
    </>
  )
}

function SourceCard({ s }: { s: Source }) {
  const { reload, toast } = useData()
  const [open, setOpen] = useState(!s.owner_confirmed)
  const [busy, setBusy] = useState(false)
  const confirm = useConfirm()
  const events = Object.entries(s.stats).filter(([k]) => k.startsWith('event:'))
  const eventTotal = events.reduce((n, [, v]) => n + v, 0)

  async function remove() {
    if (!await confirm({ title: `"${s.label}" kaynağı silinsin mi?`, message: 'Kaynağa bağlı tüm mesajlar ve etkinlik kayıtları bu bilgisayardan silinir; arama dizini de temizlenir.\n\nBu işlem geri alınamaz.', confirmLabel: 'Kaynağı ve verisini sil' })) return
    setBusy(true)
    try { await api.deleteSource(s.id); await reload(); toast('Kaynak silindi.') } catch (e) { toast(e instanceof Error ? e.message : 'Silinemedi', 'bad') } finally { setBusy(false) }
  }

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center gap-3 p-4">
        <PlatformBadge platform={s.platform} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[15px] font-semibold">{s.owner_name || s.owner_handle || 'Hesap sahibi belirsiz'}{s.owner_handle && s.owner_name && <span className="ml-1.5 font-normal text-muted">@{s.owner_handle}</span>}</div>
          <div className="truncate text-[12.5px] text-muted">{s.label} · {dateShort(s.imported_at)}</div>
        </div>
        <div className="flex gap-4 text-[13px] text-muted tabular-nums">
          <span><b className="text-fg">{num(s.stats.conversations ?? 0)}</b> sohbet</span>
          <span><b className="text-fg">{num(s.stats.messages ?? 0)}</b> mesaj</span>
          {eventTotal > 0 && <span><b className="text-fg">{num(eventTotal)}</b> etkinlik</span>}
        </div>
        <Button variant="ghost" size="sm" onClick={() => setOpen(!open)} aria-expanded={open}>
          Ayrıntı <ChevronDown className={`size-4 transition ${open ? 'rotate-180' : ''}`} />
        </Button>
        <Button variant="danger" size="sm" onClick={remove} busy={busy} aria-label="Kaynağı sil"><Trash2 className="size-4" /></Button>
      </div>
      {!s.owner_confirmed && <OwnerPicker s={s} />}
      {open && (
        <div className="border-t border-line bg-raised/40 p-4">
          {s.warnings?.length > 0 && (
            <div className="mb-4 space-y-1.5">
              {s.warnings.map((w, i) => (
                <div key={i} className="flex gap-2 text-[12.5px] text-warn"><AlertTriangle className="mt-0.5 size-3.5 shrink-0" />{w}</div>
              ))}
            </div>
          )}
          {events.length > 0 && (
            <div className="mb-4 flex flex-wrap gap-1.5">
              {events.sort((a, b) => b[1] - a[1]).map(([k, v]) => <Badge key={k}>{EVENT_LABEL[k.slice(6)] ?? k.slice(6)} · {num(v)}</Badge>)}
            </div>
          )}
          <Contacts source={s} />
        </div>
      )}
    </Card>
  )
}

const EVENT_LABEL: Record<string, string> = {
  following: 'takip edilen', follower: 'takipçi', unfollowed: 'takipten çıkılan', follow_request: 'bekleyen istek',
  close_friend: 'yakın arkadaş', blocked: 'engellenen', restricted: 'kısıtlanan', like_post: 'beğenilen gönderi',
  like_comment: 'beğenilen yorum', comment: 'yorum', saved: 'kaydedilen', story_like: 'hikâye beğenisi',
  story_interact: 'hikâye etkileşimi', story_view: 'hikâye izleme', post_view: 'gönderi görüntüleme', video_view: 'video izleme', suggestion_view: 'önerilen hesap görüntüleme', search_account: 'hesap araması', search_keyword: 'kelime araması', login: 'giriş',
  post: 'gönderi', story: 'hikâye', reel: 'reels', profile_change: 'profil değişimi', interest: 'ilgi alanı', media_meta: 'fotoğraf EXIF',
}

function OwnerPicker({ s }: { s: Source }) {
  const { reload, toast } = useData()
  const [name, setName] = useState(s.owner_name)
  const [handle, setHandle] = useState(s.owner_handle)
  const [busy, setBusy] = useState(false)
  async function save(n: string, h: string) {
    setBusy(true)
    try { await api.setOwner(s.id, n, h); await reload(); toast('Hesap sahibi kaydedildi.') } catch (e) { toast(e instanceof Error ? e.message : 'Kaydedilemedi', 'bad') } finally { setBusy(false) }
  }
  return (
    <div className="border-t border-line bg-warn/8 p-4">
      <div className="mb-1 text-[14px] font-semibold">Bu dışa aktarımı kim yaptı?</div>
      <p className="mb-3 text-[13px] text-muted">
        {s.owner_name ? `Tahminim "${s.owner_name}". Yanlışsa düzelt.` : 'Hesap sahibi dosyadan çıkarılamadı.'} Sahip, "ben" tarafını ve hangi etkinliklerin kime ait olduğunu belirler.
      </p>
      {s.candidates?.length > 0 && (
        <div className="mb-3 flex flex-wrap gap-1.5">
          {s.candidates.map((c) => (
            <button key={c} onClick={() => setName(c)} className={`rounded-full border px-3 py-1 text-[13px] ${name === c ? 'border-accent bg-accent-soft text-accent' : 'border-line bg-surface hover:bg-raised'}`}>{c}</button>
          ))}
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <input className={`${inputCls} max-w-xs`} value={name} onChange={(e) => setName(e.target.value)} placeholder="Görünen ad" aria-label="Görünen ad" />
        <input className={`${inputCls} max-w-[200px]`} value={handle} onChange={(e) => setHandle(e.target.value)} placeholder="@kullanıcı_adı (isteğe bağlı)" aria-label="Kullanıcı adı" />
        <Button variant="primary" busy={busy} disabled={!name.trim() && !handle.trim()} onClick={() => save(name, handle)}>Doğrula</Button>
      </div>
    </div>
  )
}

function Contacts({ source }: { source: Source }) {
  const { reload, toast } = useData()
  const { data, error, loading, setData } = useAsync(() => api.contacts(source.id), [source.id, source.owner_name, source.owner_handle])
  const [busy, setBusy] = useState<number | null>(null)
  if (loading) return <Spinner />
  if (error) return <ErrorNote>{error}</ErrorNote>
  const contacts = data ?? []
  if (contacts.length === 0) return <p className="text-[13px] text-muted">Sohbet yok.</p>

  async function open(c: Contact) {
    setBusy(c.conv_id)
    try {
      const p = await api.createPerson(c.name, [c.name])
      await reload()
      setData((await api.contacts(source.id)))
      toast(`${p.name} için kişi dosyası açıldı.`)
    } catch (e) { toast(e instanceof Error ? e.message : 'Açılamadı', 'bad') } finally { setBusy(null) }
  }

  return (
    <div>
      <div className="mb-2 text-[13px] font-medium">Sohbet karşıları <span className="font-normal text-muted">({contacts.length})</span></div>
      <ul className="max-h-80 divide-y divide-line overflow-y-auto rounded-xl border border-line bg-surface">
        {contacts.map((c) => (
          <li key={c.conv_id} className="flex items-center gap-3 px-3 py-2">
            <div className="min-w-0 flex-1">
              <div className="truncate text-[13.5px] font-medium">{c.name} {c.is_group && <Badge className="ml-1">grup</Badge>}</div>
              <div className="text-[12px] text-muted">{dateShort(c.first)} – {dateShort(c.last)}</div>
            </div>
            <Chip>{num(c.count)} mesaj</Chip>
            <a className="text-[13px] text-accent" href={href(`/sohbet/${c.conv_id}`)}>Oku</a>
            {!c.is_group && (c.person_id ? (
              <a href={href(`/kisi/${c.person_id}`)}><Badge tone="ok">kişi dosyası</Badge></a>
            ) : (
              <Button size="sm" busy={busy === c.conv_id} onClick={() => open(c)}><UserPlus className="size-3.5" /> Dosya aç</Button>
            ))}
          </li>
        ))}
      </ul>
    </div>
  )
}

import { Check, ChevronDown, ChevronUp, MessagesSquare, Sparkles, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Bars } from '@/components/charts'
import { Avatar, Badge, Button, Card, Empty, ErrorNote, inputCls, PageHeader, Panel, Spinner, Stat } from '@/components/ui'
import { api } from '@/lib/api'
import { cn } from '@/lib/cn'
import { useAsync, useData } from '@/lib/data'
import { dateShort, duration, num, pct } from '@/lib/fmt'
import { HIGHLIGHT_TR, LABEL_ORDER, LABEL_TR } from '@/lib/labels'
import { go, href } from '@/lib/router'
import type { GuideRow, Highlight } from '@/lib/types'

type Sort = 'total' | 'last' | 'affection' | 'conflict' | 'night'
const SORTS: { id: Sort; label: string }[] = [
  { id: 'total', label: 'Mesaj sayısı' }, { id: 'last', label: 'Son yazışma' }, { id: 'affection', label: 'Yakınlık ifadesi' },
  { id: 'conflict', label: 'Çatışma ifadesi' }, { id: 'night', label: 'Gece payı' },
]
const KIND: Record<string, string> = {
  dm: 'mesaj', like_post: 'gönderi beğenisi', like_comment: 'yorum beğenisi', comment: 'yorum', saved: 'kaydetme', story_like: 'hikâye beğenisi',
  story_interact: 'hikâye etkileşimi', story_view: 'hikâye izleme', post_view: 'gönderi görüntüleme', video_view: 'video izleme', suggestion_view: 'önerilen hesap görüntüleme', search_account: 'arama',
}
const DAYS = ['Pzt', 'Sal', 'Çar', 'Per', 'Cum', 'Cmt', 'Paz']

export default function GuidePage() {
  const { people } = useData()
  const me = people.find((p) => p.is_self) ?? people[0]
  const [who, setWho] = useState(0)
  const personId = who || me?.id || 0
  const [tab, setTab] = useState<'sohbetler' | 'meta'>('sohbetler')

  if (people.length === 0) return <><PageHeader title="Sohbet rehberi" /><Empty icon={<MessagesSquare className="size-8" />} title="Önce bir kaynak yükle">Rehber, bir kişinin tüm bire-bir sohbetlerini listeler.</Empty></>
  return (
    <>
      <PageHeader title="Sohbet rehberi" sub="Kimlerle, ne kadar ve nasıl yazışıyorsun: tüm bire-bir sohbetlerin ölçümleri, yakınlık ve çatışma ifadeleri, ilişki etiketlerin. Yapay zekâ kullanmaz, para harcamaz." />
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <div className="inline-flex rounded-full bg-raised p-0.5 text-[13px]" role="tablist">
          {([['sohbetler', 'Sohbetler'], ['meta', 'Meta bağlar']] as const).map(([id, label]) => (
            <button key={id} role="tab" aria-selected={tab === id} onClick={() => setTab(id)} className={cn('rounded-full px-4 py-1.5', tab === id ? 'bg-surface font-medium shadow-sm' : 'text-muted')}>{label}</button>
          ))}
        </div>
        {people.length > 1 && (
          <select className="h-9 rounded-xl border border-line bg-surface px-3 text-[13.5px]" value={personId} onChange={(e) => setWho(Number(e.target.value))} aria-label="Kişi">
            {people.map((p) => <option key={p.id} value={p.id}>{p.name}{p.is_self ? ' (ben)' : ''}</option>)}
          </select>
        )}
      </div>
      {tab === 'sohbetler' ? <Chats key={personId} personId={personId} /> : <Meta key={personId} personId={personId} />}
    </>
  )
}

function Chats({ personId }: { personId: number }) {
  const { toast, reload, people } = useData()
  const guide = useAsync(() => api.guide(personId), [personId])
  const [q, setQ] = useState('')
  const [filter, setFilter] = useState<string>('') // '' tümü, '_none' etiketsiz, '_suggested' önerisi olanlar, ya da etiket
  const [sort, setSort] = useState<Sort>('total')
  const [shown, setShown] = useState(60)
  const [open, setOpen] = useState<number | null>(null)
  const g = guide.data

  const rows = useMemo(() => {
    if (!g) return []
    const nq = q.trim().toLocaleLowerCase('tr-TR')
    const keyOf = (r: GuideRow): number => {
      switch (sort) {
        case 'last': return new Date(r.last).getTime()
        case 'affection': return r.affection.subject + r.affection.partner
        case 'conflict': return r.conflict.subject + r.conflict.partner
        case 'night': return r.night_ratio
        default: return r.total
      }
    }
    return g.rows
      .filter((r) => (!nq || r.name.toLocaleLowerCase('tr-TR').includes(nq)) &&
        (filter === '' || (filter === '_none' ? !r.label : filter === '_suggested' ? !!r.suggestion : r.label === filter)))
      .sort((a, b) => keyOf(b) - keyOf(a))
  }, [g, q, filter, sort])

  async function setLabel(r: GuideRow, label: string, dismiss = false) {
    try {
      await api.setContactLabel(r.key, label, dismiss)
      guide.setData(await api.guide(personId))
    } catch (e) { toast(e instanceof Error ? e.message : 'Kaydedilemedi', 'bad') }
  }
  async function toCouncil(r: GuideRow) {
    try {
      const shared = r.key.includes('#') // aynı ad birden çok sohbette ortak: analiz yalnızca bu sohbetle sınırlanır
      const norm = r.key.split('#')[0]
      let other = people.find((p) => p.identities.some((i) => i.norm === norm))
      if (!other) { other = await api.createPerson(shared ? `${r.name} (sohbet ${r.conv_id})` : r.name, [r.name]); await reload() }
      go(`/konsey?kisi=${personId}&ikinci=${other.id}&tur=pair${shared ? `&sohbet=${r.conv_id}` : ''}`)
    } catch (e) { toast(e instanceof Error ? e.message : 'Açılamadı', 'bad') }
  }

  if (guide.loading) return <Spinner label="Sohbetler hesaplanıyor…" />
  if (guide.error) return <ErrorNote>{guide.error}</ErrorNote>
  if (!g || g.rows.length === 0) return <Empty icon={<MessagesSquare className="size-8" />} title="Bu kişinin bire-bir sohbeti yok">Kişi dosyasındaki kimliklerin sohbetlerdeki adla eşleştiğinden emin ol.</Empty>

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Bire-bir sohbet" value={num(g.rows.length)} />
        <Stat label="Etiketli" value={num(g.rows.length - g.unlabeled)} sub={`${num(g.unlabeled)} etiketsiz`} />
        <Stat label="Etiket önerisi" value={num(g.suggested)} sub="onayını bekliyor" />
        <Stat label="Toplam mesaj" value={num(g.rows.reduce((n, r) => n + r.total, 0))} />
      </div>
      <Card className="p-4">
        <div className="flex flex-wrap gap-1.5 text-[12.5px]">
          {[['', `Tümü (${g.rows.length})`], ['_none', `Etiketsiz (${g.unlabeled})`], ['_suggested', `Önerisi olan (${g.suggested})`], ...LABEL_ORDER.filter((l) => g.label_counts[l]).map((l) => [l, `${LABEL_TR[l]} (${g.label_counts[l]})`])].map(([id, label]) => (
            <button key={id} onClick={() => { setFilter(id!); setShown(60) }} className={cn('rounded-full px-3 py-1', filter === id ? 'bg-accent text-accent-fg' : 'bg-raised text-muted hover:text-fg')}>{label}</button>
          ))}
        </div>
        <div className="mt-3 flex flex-wrap gap-3">
          <input className={cn(inputCls, 'max-w-xs')} placeholder="Ada göre ara" value={q} onChange={(e) => { setQ(e.target.value); setShown(60) }} aria-label="Ara" />
          <select className="h-10 rounded-xl border border-line bg-surface px-3 text-[13.5px]" value={sort} onChange={(e) => setSort(e.target.value as Sort)} aria-label="Sırala">
            {SORTS.map((s) => <option key={s.id} value={s.id}>Sırala: {s.label}</option>)}
          </select>
        </div>
        <p className="mt-3 text-[12px] text-muted">Etiketleri sen koyarsın ya da önerileri onaylarsın; Joe kimseyi kendi başına etiketlemez. "Yakınlık" ve "çatışma" yalnızca ilgili ifadelerin geçtiği mesaj sayısıdır (sen / karşı taraf); şaka, alıntı ya da olumsuzlama ayrımı yapılmaz, bir puan ya da hüküm değildir.</p>
      </Card>

      <Card className="divide-y divide-line">
        {rows.slice(0, shown).map((r) => (
          <div key={r.conv_id} className="p-4">
            <div className="flex flex-wrap items-center gap-3">
              <Avatar name={r.name} size={40} />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate text-[15px] font-semibold">{r.name}</span>
                  {r.label && <Badge tone="accent">{LABEL_TR[r.label]}</Badge>}
                  {r.mutual_follow_hint && <Badge tone="ok">karşılıklı takip?</Badge>}
                  {r.close_friend_hint && <Badge tone="ok">yakın arkadaş listesinde?</Badge>}
                </div>
                <div className="text-[12.5px] text-muted">
                  {num(r.total)} mesaj · {dateShort(r.first)} – {dateShort(r.last)} · sana ait {pct(r.messages.subject / r.total)} · gece {pct(r.night_ratio)} · başlatma sende {pct(r.starts.subject / Math.max(1, r.starts.subject + r.starts.partner))}
                  {r.latency.partner.n >= 5 && <> · onun medyan yanıtı {duration(r.latency.partner.median)}</>}
                </div>
                <div className="mt-1 flex flex-wrap gap-1.5 text-[11.5px]">
                  <span className="rounded-full bg-raised px-2 py-0.5 text-muted">yakınlık ifadesi {r.affection.subject} / {r.affection.partner}</span>
                  <span className="rounded-full bg-raised px-2 py-0.5 text-muted">çatışma ifadesi {r.conflict.subject} / {r.conflict.partner}</span>
                  {r.unsent.subject + r.unsent.partner > 0 && <span className="rounded-full bg-raised px-2 py-0.5 text-muted">geri alınan {r.unsent.subject} / {r.unsent.partner}</span>}
                  {r.longest_silence_days >= 14 && <span className="rounded-full bg-raised px-2 py-0.5 text-muted">en uzun sessizlik {Math.round(r.longest_silence_days)} gün</span>}
                </div>
              </div>
              <select className="h-9 rounded-xl border border-line bg-surface px-2 text-[13px]" value={r.label} onChange={(e) => void setLabel(r, e.target.value)} aria-label={`${r.name} ilişki etiketi`}>
                <option value="">Etiketsiz</option>
                {LABEL_ORDER.map((l) => <option key={l} value={l}>{LABEL_TR[l]}</option>)}
              </select>
            </div>
            {r.suggestion && (
              <div className="mt-3 flex flex-wrap items-center gap-2 rounded-xl bg-accent-soft px-3 py-2 text-[12.5px]">
                <span className="min-w-0 flex-1"><b>Öneri: {LABEL_TR[r.suggestion.label]}</b> <span className="text-muted">({r.suggestion.confidence} güven) — {r.suggestion.why.join('; ')}</span></span>
                <Button size="sm" variant="primary" onClick={() => void setLabel(r, r.suggestion!.label)}><Check className="size-3.5" /> Onayla</Button>
                <Button size="sm" onClick={() => void setLabel(r, '', true)}><X className="size-3.5" /> Reddet</Button>
              </div>
            )}
            <div className="mt-3 flex flex-wrap gap-2">
              <Button size="sm" onClick={() => setOpen(open === r.conv_id ? null : r.conv_id)}>{open === r.conv_id ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />} Önemli anlar</Button>
              <a href={href(`/sohbet/${r.conv_id}?kisi=${personId}`)}><Button size="sm">Sohbeti oku</Button></a>
              <Button size="sm" title={r.key.includes('#') ? 'Bu ad birden çok (silinmiş/devre dışı) hesapta ortak; analiz yalnızca bu sohbetle sınırlanır.' : undefined} onClick={() => void toCouncil(r)}><Sparkles className="size-3.5" /> Konseye götür</Button>
            </div>
            {open === r.conv_id && <Highlights convId={r.conv_id} personId={personId} />}
          </div>
        ))}
        {rows.length === 0 && <p className="p-6 text-center text-[13px] text-muted">Bu filtreyle eşleşen sohbet yok.</p>}
      </Card>
      {rows.length > shown && <div className="text-center"><Button onClick={() => setShown(shown + 60)}>Daha fazla göster ({num(rows.length - shown)})</Button></div>}
    </div>
  )
}

export function Highlights({ convId, personId }: { convId: number; personId: number }) {
  const hl = useAsync(() => api.highlights(convId, personId), [convId, personId])
  if (hl.loading) return <div className="mt-3"><Spinner /></div>
  if (hl.error) return <div className="mt-3"><ErrorNote>{hl.error}</ErrorNote></div>
  const items: Highlight[] = hl.data?.highlights ?? []
  if (items.length === 0) return <p className="mt-3 text-[13px] text-muted">Öne çıkan bir an bulunamadı.</p>
  return (
    <ul className="mt-3 divide-y divide-line rounded-xl border border-line text-[13px]">
      {items.map((h, i) => (
        <li key={i}>
          <a href={href(`/sohbet/${convId}?kisi=${personId}&mesaj=${h.message_id}`)} className="flex items-start gap-3 px-3 py-2 hover:bg-raised/60">
            <span className="w-24 shrink-0 pt-0.5 text-[11.5px] text-muted tabular-nums">{dateShort(h.ts)}</span>
            <span className="min-w-0 flex-1"><b className="font-medium">{h.title}</b>{h.detail && <span className="block text-[12px] text-muted">{h.detail}</span>}</span>
            <Badge tone={h.kind === 'conflict' || h.kind === 'rupture' ? 'warn' : h.kind === 'affection' ? 'accent' : 'neutral'}>{HIGHLIGHT_TR[h.kind] ?? h.kind}</Badge>
          </a>
        </li>
      ))}
    </ul>
  )
}

function Meta({ personId }: { personId: number }) {
  const meta = useAsync(() => api.meta(personId, 40), [personId])
  const m = meta.data
  if (meta.loading) return <Spinner label="Meta veri hesaplanıyor…" />
  if (meta.error) return <ErrorNote>{meta.error}</ErrorNote>
  if (!m || (m.ties.length === 0 && m.events === 0)) return <Empty icon={<MessagesSquare className="size-8" />} title="Meta veri bulunamadı">Bu bölüm, Instagram hesap etkinliğinden (hikâye izleme, beğeni, yorum, kaydetme, arama, takip) çıkar; kişinin Instagram verisi yüklü olmalı.</Empty>
  const total = m.hours.reduce((a, b) => a + b, 0)
  const night = m.hours.slice(0, 6).reduce((a, b) => a + b, 0)
  return (
    <div className="space-y-4">
      <Card className="p-4 text-[13px] text-muted">Bu bölüm <b className="text-fg">mesaj içeriğine hiç bakmadan</b>, yalnızca hesap etkinliğinden çıkar: kimin hikâyelerine bakıyorsun, kimi beğeniyorsun, kime yorum yazıyorsun, kimi arıyorsun, takip dengen nasıl, hangi saatlerde aktifsin.</Card>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Takip ettiğin" value={num(m.follow.following)} sub={`${num(m.follow.not_following_back)} seni takip etmiyor`} />
        <Stat label="Takipçin" value={num(m.follow.followers)} sub={`${num(m.follow.fans_not_followed)} kişiyi sen takip etmiyorsun`} />
        <Stat label="Karşılıklı takip" value={num(m.follow.mutual)} />
        <Stat label="Etkinlik gece payı" value={pct(total ? night / total : 0)} sub="00:00 – 06:00" />
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <Panel title="Etkinlik saatleri" hint="Hikâye izleme, beğeni, yorum, kaydetme, arama ve girişlerin saat dağılımı."><Bars values={m.hours} labels={m.hours.map((_, h) => String(h))} /></Panel>
        <Panel title="Haftanın günleri"><Bars values={m.weekday} labels={DAYS} /></Panel>
      </div>
      <Panel title="En çok ilgi gösterdiğin hesaplar" hint="İzleme, beğeni, yorum, kaydetme ve arama ağırlıklı toplamı. 'Sohbet' sütunu ad benzerliğinden kurulur ve kesin değildir.">
        <ul className="divide-y divide-line text-[13px]">
          {m.ties.map((t) => (
            <li key={t.handle} className="flex flex-wrap items-center gap-3 py-2.5">
              <span className="w-40 shrink-0 truncate font-medium">@{t.handle}</span>
              <span className="flex min-w-0 flex-1 flex-wrap gap-1">{Object.entries(t.counts).sort((a, b) => b[1] - a[1]).map(([k, v]) => <span key={k} className="rounded-full bg-raised px-2 py-0.5 text-[11.5px] text-muted">{KIND[k] ?? k} {num(v)}</span>)}</span>
              {t.dm_name ? (
                <a href={href(`/sohbet/${t.dm_conv}?kisi=${personId}`)} className="text-accent" title={`Ad benzerliği %${Math.round((t.dm_score ?? 0) * 100)}`}>sohbet: {t.dm_name} ({num(t.dm_total ?? 0)})</a>
              ) : <span className="text-warn" title="Görünen ad ile kullanıcı adı benzerliğinden bir sohbet bulunamadı">mesajlaşma yok?</span>}
            </li>
          ))}
        </ul>
      </Panel>
    </div>
  )
}

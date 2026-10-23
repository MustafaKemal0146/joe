import { useConfirm } from '@/components/Confirm'
import { Download, History, Sparkles, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Area, Bars, BarList, Heatmap, Split } from '@/components/charts'
import EgoNetwork from '@/components/EgoNetwork'
import PastCouncils from '@/components/PastCouncils'
import AskTab from './AskTab'
import { Avatar, Badge, Button, Card, Chip, Empty, ErrorNote, Field, inputCls, Panel, PlatformBadge, Spinner, Stat, Tabs } from '@/components/ui'
import { api } from '@/lib/api'
import { useAsync, useData } from '@/lib/data'
import { date, dateShort, duration, hourLabel, month, num, pct, platformName } from '@/lib/fmt'
import { go, href } from '@/lib/router'
import type { Activity, Partner, Person, Profile, TimelineItem, Topic } from '@/lib/types'

type Tab = 'ozet' | 'ritim' | 'konular' | 'sohbetler' | 'gruplar' | 'instagram' | 'osint' | 'cizelge' | 'cevre' | 'sor' | 'konseyler' | 'kimlik'
const TABS: { id: Tab; label: string }[] = [
  { id: 'ozet', label: 'Özet' }, { id: 'cevre', label: 'Çevre' }, { id: 'sor', label: 'Sor' }, { id: 'konseyler', label: 'Geçmiş konseyler' }, { id: 'ritim', label: 'Ritim' }, { id: 'konular', label: 'Konular' }, { id: 'sohbetler', label: 'Sohbetler' }, { id: 'gruplar', label: 'Gruplar' },
  { id: 'instagram', label: 'Instagram' }, { id: 'osint', label: 'OSINT' }, { id: 'cizelge', label: 'Zaman çizelgesi' }, { id: 'kimlik', label: 'Kimlikler' },
]

export default function PersonPage({ id }: { id: number }) {
  const { people, loaded } = useData()
  const [tab, setTab] = useState<Tab>('ozet')
  const person = people.find((p) => p.id === id)
  const profile = useAsync(() => api.profile(id), [id, person?.identities.map((i) => i.id).join(',')])
  if (!loaded) return <Spinner />
  if (!person) return <Empty title="Kişi bulunamadı"><a className="text-accent" href={href('/kisiler')}>Kişilere dön</a></Empty>
  const p = profile.data

  return (
    <>
      <div className="mb-6 flex flex-wrap items-center gap-4">
        <Avatar name={person.name} size={64} />
        <div className="min-w-0 flex-1">
          <h1 className="flex items-center gap-2 text-[28px] font-semibold leading-tight tracking-tight">{person.name}{person.is_self && <Badge tone="accent">ben</Badge>}</h1>
          <p className="text-[14px] text-muted">
            {p && p.messages > 0 ? `${num(p.messages)} mesaj · ${dateShort(p.first)} – ${dateShort(p.last)}` : p ? 'Bu kişiyle eşleşen mesaj yok' : 'Yükleniyor…'}
          </p>
        </div>
        <a href={href(`/konsey?kisi=${person.id}`)}><Button size="sm" variant="primary"><Sparkles className="size-4" /> Konsey'e sor</Button></a>
        <Button size="sm" onClick={() => setTab('konseyler')}><History className="size-4" /> Geçmiş konseyleri gör</Button>
        <a href={`/api/people/${person.id}/report.html?print=1`} target="_blank" rel="noopener noreferrer"><Button size="sm"><Download className="size-4" /> PDF</Button></a>
        <a href={`/api/people/${person.id}/report.md`} download title="Varsa son iki konsey oturumunu da içerir"><Button size="sm"><Download className="size-4" /> Markdown</Button></a>
        <a href={`/api/people/${person.id}/report.md?konsey=0`} download className="text-[12.5px] text-muted underline hover:text-fg" title="Yapay zekâ yorumu olmadan yalnızca metrikler">konseysiz</a>
      </div>
      <div className="mb-6 overflow-x-auto"><Tabs tabs={TABS} value={tab} onChange={setTab} /></div>

      {tab === 'konseyler' ? <PastCouncils personId={person.id} /> : tab === 'cevre' ? <EgoNetwork personId={person.id} /> : tab === 'sor' ? <AskTab person={person} /> : tab === 'kimlik' ? <Identities person={person} /> : profile.loading ? <Spinner label="Metrikler hesaplanıyor…" /> : profile.error ? <ErrorNote>{profile.error}</ErrorNote> : p && (
        <>
          {tab === 'ozet' && <><RiskBanner id={id} /><Overview p={p} /></>}
          {tab === 'ritim' && <Rhythm p={p} />}
          {tab === 'konular' && <TopicsTab id={id} />}
          {tab === 'sohbetler' && <Chats p={p} />}
          {tab === 'gruplar' && <Groups p={p} />}
          {tab === 'osint' && <PersonOsint id={id} />}
          {tab === 'instagram' && <Instagram a={p.activity} />}
          {tab === 'cizelge' && <Timeline id={id} />}
        </>
      )}
    </>
  )
}

function RiskBanner({ id }: { id: number }) {
  const { data } = useAsync(() => api.risk(id), [id])
  if (!data || data.concern + data.acute === 0) return null
  return (
    <div role="note" className="mb-4 rounded-2xl border border-warn/40 bg-warn/8 p-4 text-[13px]">
      <div className="mb-1 text-[14px] font-semibold text-warn">
        {data.concern + data.acute} mesajda kendine zarar ya da şiddet içeren ifade geçiyor
      </div>
      <p className="text-muted">{data.note}</p>
      <div className="mt-2 flex flex-wrap gap-2">
        {data.messages.slice(0, 5).map((m) => (
          <a key={m.id} className="rounded-full bg-surface px-2.5 py-1 text-accent" href={href(`/sohbet/${m.conv_id}?mesaj=${m.id}`)}>{dateShort(m.ts)} · bağlamı oku</a>
        ))}
      </div>
    </div>
  )
}

function Notes({ p }: { p: Profile }) {
  if (!p.notes?.length) return null
  return <div className="mb-4 space-y-2">{p.notes.map((n, i) => <div key={i} className="rounded-xl border border-warn/30 bg-warn/8 px-4 py-2.5 text-[13px] text-warn">{n}</div>)}</div>
}

/** Bir bakışta: yalnızca ölçülen değerlerden kurulan düz anlatım; yorum ya da etiket içermez. */
function Glance({ p }: { p: Profile }) {
  const top = [...(p.partners ?? [])].sort((a, b) => b.total - a.total)[0]
  const peak = p.hours.indexOf(Math.max(...p.hours))
  const months = p.monthly ?? []
  const busiest = months.length ? months.reduce((a, b) => (b.count > a.count ? b : a)) : null
  const span = Math.max(1, Math.round((new Date(p.last).getTime() - new Date(p.first).getTime()) / 86400000))
  const lines: string[] = []
  lines.push(`${dateShort(p.first)} ile ${dateShort(p.last)} arasında (${num(span)} gün) ${num(p.messages)} mesaj; ${num(p.active_days)} gün aktif (%${Math.round((p.active_days / span) * 100)}).`)
  if (top) lines.push(`En yoğun sohbet ${top.name} (${platformName(top.platform)}): ${num(top.total)} mesaj, ${num(top.sessions)} oturum. Kişinin payı ${pct(top.subject_share)}.`)
  if ((p.partners?.length ?? 0) > 1) lines.push(`Toplam ${num(p.partners!.length)} sohbette yer alıyor.`)
  lines.push(`En çok ${hourLabel(peak)} civarında yazıyor${p.night_ratio >= 0.05 ? `; mesajların ${pct(p.night_ratio)}'i gece yarısı ile 06:00 arasında` : ''}.`)
  if (busiest) lines.push(`En yoğun ay ${month(busiest.month)} (${num(busiest.count)} mesaj).`)
  if (p.latency.n >= 5) lines.push(p.latency.median < 60 ? `Yanıtların yarısından fazlası 1 dakikanın altında (${num(p.latency.n)} örnek; kayıt dakika çözünürlüğünde olabilir).` : `Medyan yanıt süresi ${duration(p.latency.median)} (${num(p.latency.n)} örnek).`)
  return (
    <Panel title="Bir bakışta" hint="Yalnızca ölçülen değerlerden derlenir; yorum içermez.">
      <ul className="space-y-1.5 text-[13.5px] leading-relaxed">{lines.map((l, i) => <li key={i} className="flex gap-2"><span className="text-faint">•</span><span>{l}</span></li>)}</ul>
    </Panel>
  )
}

function Overview({ p }: { p: Profile }) {
  const l = p.language
  const monthly = (p.monthly ?? []).map((m) => ({ month: m.month, value: m.count }))
  return (
    <div className="space-y-4">
      <Notes p={p} />
      <Glance p={p} />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Mesaj" value={num(p.messages)} />
        <Stat label="Aktif gün" value={num(p.active_days)} />
        <Stat label="Medyan yanıt" value={p.latency.n >= 5 ? duration(p.latency.median) : '—'} sub={p.latency.n >= 5 ? `%90'ı ${duration(p.latency.p90)} içinde` : 'yeterli örnek yok'} />
        <Stat label="Gece payı" value={pct(p.night_ratio)} sub="00:00 – 06:00" />
      </div>
      <Panel title="Zaman içinde mesaj hacmi"><Area points={monthly} /></Panel>
      {(p.periods?.length ?? 0) > 0 && (
        <Panel title="Dikkat çeken dönemler" hint="Mesajlaşmanın kişinin olağan düzeyinden belirgin saptığı aylar.">
          <ul className="divide-y divide-line">
            {p.periods!.map((per, i) => (
              <li key={i} className="flex items-center gap-3 py-2.5 text-[13.5px]">
                <Badge tone={per.kind === 'surge' ? 'warn' : 'neutral'}>{per.kind === 'surge' ? 'yoğun' : 'sessiz'}</Badge>
                <span className="font-medium">{month(per.from)}{per.from !== per.to && ` – ${month(per.to)}`}</span>
                <span className="ml-auto text-muted tabular-nums">{num(per.count)} mesaj · olağan ≈ {num(per.baseline)}/ay</span>
              </li>
            ))}
          </ul>
        </Panel>
      )}
      {l.text_messages > 0 && (
        <div className="grid gap-4 md:grid-cols-2">
          <Panel title="Yazış biçimi">
            <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-[13px]">
              <Dl k="Ortalama sözcük" v={l.avg_words.toFixed(1).replace('.', ',')} />
              <Dl k="Soru oranı" v={pct(l.question_ratio)} />
              <Dl k="Ünlem oranı" v={pct(l.exclaim_ratio)} />
              <Dl k="Büyük harfle yazılan" v={pct(l.caps_ratio, 1)} />
              <Dl k="Üç nokta" v={pct(l.ellipsis_ratio)} />
              <Dl k="Mesaj başına emoji" v={l.emoji_per_msg.toFixed(2).replace('.', ',')} />
              <Dl k="Söz dağarcığı çeşitliliği" v={pct(l.richness)} />
              <Dl k="Bağlantı paylaşma" v={pct(l.link_ratio)} />
            </dl>
            {(l.kinds?.length ?? 0) > 0 && <div className="mt-4 flex flex-wrap gap-1.5">{l.kinds!.map((k) => <Chip key={k.key}>{k.key} · {num(k.count)}</Chip>)}</div>}
          </Panel>
          <Panel title="Sık kullanılanlar">
            {(l.top_emoji?.length ?? 0) > 0 && <div className="mb-4 flex flex-wrap gap-2 text-[22px]">{l.top_emoji!.map((e) => <span key={e.key} title={`${e.count} kez`}>{e.key}</span>)}</div>}
            <div className="flex flex-wrap gap-1.5">{(l.top_words ?? []).slice(0, 24).map((w) => <Chip key={w.key}>{w.key} <span className="text-faint">{w.count}</span></Chip>)}</div>
          </Panel>
        </div>
      )}
    </div>
  )
}

const Dl = ({ k, v }: { k: string; v: string }) => (
  <div><dt className="text-muted">{k}</dt><dd className="text-[16px] font-semibold tabular-nums">{v}</dd></div>
)

function Rhythm({ p }: { p: Profile }) {
  if (p.messages === 0) return <Empty title="Ritim için mesaj yok" />
  const DAYS = ['Pzt', 'Sal', 'Çar', 'Per', 'Cum', 'Cmt', 'Paz']
  const sleep = p.sleep_known ? `${hourLabel(p.sleep_start)} – ${hourLabel((p.sleep_start + 6) % 24)}` : null
  return (
    <div className="space-y-4">
      <Notes p={p} />
      <Panel title="Haftanın günü × saat" hint="Mesajların ne zaman yazıldığı (yerel saat).">
        <Heatmap heat={p.heat} />
      </Panel>
      <div className="grid gap-4 md:grid-cols-2">
        <Panel title="Saate göre"><Bars values={p.hours} labels={Array.from({ length: 24 }, (_, h) => String(h))} highlight={[0, 1, 2, 3, 4, 5]} /></Panel>
        <Panel title="Güne göre"><Bars values={p.weekday} labels={DAYS} /></Panel>
      </div>
      <Panel title="Uyku penceresi tahmini" hint="Etkinliğin en düşük olduğu 6 saatlik aralık. Mesajlardan çıkarılan bir tahmindir, kesin bilgi değildir.">
        {sleep ? (
          <div className="flex flex-wrap items-baseline gap-x-6 gap-y-1">
            <span className="text-[28px] font-semibold tabular-nums">{sleep}</span>
            <span className="text-[13px] text-muted">Gece (00–06) mesaj payı {pct(p.night_ratio)}</span>
          </div>
        ) : <p className="text-[13px] text-muted">Tahmin için en az 100 mesaj gerekir.</p>}
      </Panel>
    </div>
  )
}

function Chats({ p }: { p: Profile }) {
  const partners = p.partners ?? []
  if (partners.length === 0) return <Empty title="Bire-bir sohbet bulunamadı">Bu kişiyle iki kişilik bir sohbet yoksa karşılıklılık metrikleri hesaplanamaz. Gruplar bu analize girmez.</Empty>
  return (
    <div className="space-y-4">
      <Card className="flex flex-wrap items-center gap-3 p-4 text-[13px]"><span className="min-w-0 flex-1 text-muted">Burada yalnızca en yoğun {partners.length} sohbet var. <b className="text-fg">Tüm bire-bir sohbetler</b>, ilişki etiketleri, yakınlık/çatışma ifadeleri ve önemli anlar için Sohbet rehberine bak.</span><a href={href('/rehber')}><Button size="sm" variant="primary">Sohbet rehberini aç</Button></a></Card>
      {partners.map((pt) => <PartnerCard key={pt.conv_id} pt={pt} subject={p.subject} />)}
    </div>
  )
}

function PartnerCard({ pt, subject }: { pt: Partner; subject: string }) {
  const [open, setOpen] = useState(false)
  const monthly = pt.monthly.map((m) => ({ month: m.month, value: m.subject + m.partner }))
  return (
    <Card className="p-5">
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Avatar name={pt.name} size={36} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 text-[15px] font-semibold">{pt.name} <PlatformBadge platform={pt.platform} /></div>
          <div className="text-[12.5px] text-muted">{num(pt.total)} mesaj · {num(pt.sessions)} konuşma · {dateShort(pt.first)} – {dateShort(pt.last)}</div>
        </div>
        <a href={href(`/sohbet/${pt.conv_id}`)}><Button size="sm">Sohbeti oku</Button></a>
        <Button size="sm" variant="ghost" onClick={() => setOpen(!open)}>{open ? 'Gizle' : 'Zaman içinde'}</Button>
      </div>
      <div className="mb-2 flex justify-between text-[12px] font-medium"><span className="text-accent">{subject}</span><span className="text-ig">{pt.name}</span></div>
      <div className="grid gap-3 md:grid-cols-2">
        <Split label="mesaj" left={pt.messages.subject} right={pt.messages.partner} leftName={subject} rightName={pt.name} />
        <Split label="konuşmayı başlatma" left={pt.starts.subject} right={pt.starts.partner} leftName={subject} rightName={pt.name} />
        <Split label="konuşmayı bitirme" left={pt.ends.subject} right={pt.ends.partner} leftName={subject} rightName={pt.name} />
        <Split label="yanıt yazmadan peş peşe mesaj" left={pt.double_texts.subject} right={pt.double_texts.partner} leftName={subject} rightName={pt.name} />
        <Split label="medyan yanıt süresi" left={pt.latency.subject.median} right={pt.latency.partner.median} leftName={subject} rightName={pt.name} format={duration} />
        <Split label="ort. sözcük / mesaj" left={pt.avg_words.subject} right={pt.avg_words.partner} leftName={subject} rightName={pt.name} format={(n) => n.toFixed(1).replace('.', ',')} />
        {(pt.unsent.subject + pt.unsent.partner > 0) && <Split label="geri alınan mesaj" left={pt.unsent.subject} right={pt.unsent.partner} leftName={subject} rightName={pt.name} />}
      </div>
      {pt.longest_silence_days >= 7 && <p className="mt-4 text-[13px] text-muted">En uzun sessizlik: <b className="text-fg">{Math.round(pt.longest_silence_days)} gün</b> ({date(pt.silence_from)} sonrası).</p>}
      {open && <div className="mt-4"><Area points={monthly} /></div>}
    </Card>
  )
}

function TopicList({ topics }: { topics: Topic[] }) {
  return (
    <div className="space-y-3">
      {topics.map((t, i) => (
        <div key={i} className="rounded-xl bg-raised p-3.5">
          <div className="mb-1.5 flex flex-wrap items-center gap-1.5">
            {t.keywords.map((k) => <Chip key={k}>{k}</Chip>)}
            <span className="ml-auto text-[12.5px] text-muted">{num(t.size)} mesaj · {pct(t.share)}</span>
          </div>
          <a className="block text-[13px] text-muted hover:text-fg" href={href(`/sohbet/${t.rep.conv_id}?mesaj=${t.rep.id}`)}>“{t.rep.text}” <span className="text-faint">· {dateShort(t.rep.ts)}</span></a>
        </div>
      ))}
    </div>
  )
}

function TopicsTab({ id }: { id: number }) {
  const { data, error, loading } = useAsync(() => api.topics(id), [id])
  if (loading) return <Spinner label="Konular çıkarılıyor…" />
  if (error) return (
    <Empty title="Konu analizi için gömme dizini gerekli">
      {error} <a className="text-accent" href={href('/ayarlar')}>Ayarlar'a git →</a>
    </Empty>
  )
  if (!data) return null
  const embedded = data.coverage.embedded ?? data.coverage.Embedded ?? 0
  const total = data.coverage.total ?? data.coverage.Total ?? 0
  return (
    <div className="space-y-4">
      <p className="text-[12.5px] text-muted">{num(embedded)} / {num(total)} mesaj gömülü. Konular, anlamca yakın mesajların kümelenmesiyle bulunur; yapay zekâ çağrısı yapılmaz.</p>
      <Panel title="Genel konular"><TopicList topics={data.overall} /></Panel>
      {data.periods.map((per) => (
        <Panel key={per.from + per.kind} title={`${per.kind === 'surge' ? 'Yoğun' : 'Sessiz'} dönem: ${month(per.from)}${per.from !== per.to ? ` – ${month(per.to)}` : ''}`} hint={`${num(per.messages)} mesaj`}>
          <TopicList topics={per.topics} />
        </Panel>
      ))}
    </div>
  )
}

function PersonOsint({ id }: { id: number }) {
  const { data, loading } = useAsync(() => api.personOsint(id), [id])
  if (loading) return <Spinner />
  if ((data?.length ?? 0) === 0) return <Empty title="Bu kişiye bağlı OSINT bulgusu yok">OSINT sayfasında bir taramada bulduğun profili <b>doğrulayıp</b> bu kişiye bağlayabilirsin. Doğrulanmayan bulgular kişi dosyasına girmez. <a className="text-accent" href={href('/osint')}>OSINT'e git →</a></Empty>
  return (
    <Panel title="Doğrulanmış OSINT bulguları" hint="Senin onayladığın aday profiller. Eşleşme kimlik doğrulaması değildir; konseye ancak sen isteyince eklenir.">
      <ul className="divide-y divide-line">
        {data!.map((f) => (
          <li key={f.id} className="py-2.5 text-[13.5px]">
            <div className="flex items-center gap-2"><b>{f.site}</b><Badge>{f.category}</Badge><span className="ml-auto text-[12px] text-faint">{dateShort(f.found_at)}</span></div>
            <a className="break-all text-[12.5px] text-accent" href={f.url} target="_blank" rel="noopener noreferrer nofollow">{f.url}</a>
          </li>
        ))}
      </ul>
    </Panel>
  )
}

function Groups({ p }: { p: Profile }) {
  const groups = p.groups ?? []
  if (groups.length === 0) return <Empty title="Grup sohbeti bulunamadı">Bu kişinin katıldığı bir grup sohbeti yoksa burada bir şey görünmez. Grup metrikleri bire-bir sohbetlerle karıştırılmaz.</Empty>
  return (
    <div className="space-y-4">
      {groups.map((g) => (
        <Card key={g.conv_id} className="p-5">
          <div className="mb-4 flex flex-wrap items-center gap-3">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 text-[15px] font-semibold">{g.title} <PlatformBadge platform={g.platform} /></div>
              <div className="text-[12.5px] text-muted">{g.members} üye · {num(g.total)} mesaj · {dateShort(g.first)} – {dateShort(g.last)}</div>
            </div>
            <a href={href(`/sohbet/${g.conv_id}`)}><Button size="sm">Sohbeti oku</Button></a>
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            <div>
              <div className="mb-1 text-[12.5px] text-muted">{p.subject} grubun <b className="text-fg">{g.rank}. en aktif üyesi</b> · mesajların {pct(g.subject_share)}'i</div>
              <Split label="mesaj" left={g.subject_msgs} right={g.total - g.subject_msgs} leftName={p.subject} rightName="diğerleri" />
              <div className="mt-4 text-[12px] font-semibold uppercase tracking-wide text-muted">En çok yazanlar</div>
              <div className="mt-2"><BarList items={g.top ?? []} /></div>
            </div>
            <div className="space-y-4">
              <div><div className="mb-2 text-[12px] font-semibold uppercase tracking-wide text-muted">Hemen sonra yazdığı kişiler (yanıt gibi)</div><BarList items={g.replies_to ?? []} empty="Belirgin bir örüntü yok" /></div>
              <div><div className="mb-2 text-[12px] font-semibold uppercase tracking-wide text-muted">Ondan hemen sonra yazanlar</div><BarList items={g.replied_by ?? []} empty="Belirgin bir örüntü yok" /></div>
            </div>
          </div>
          <div className="mt-4"><Bars values={g.hours} labels={Array.from({ length: 24 }, (_, h) => String(h))} height={56} /></div>
        </Card>
      ))}
    </div>
  )
}

function Instagram({ a }: { a?: Activity }) {
  if (!a) return <Empty title="Bu kişinin Instagram dışa aktarımı yok">Etkinlik verisi (takip, beğeni, arama, giriş) yalnızca kişinin kendi hesabından alınan dışa aktarımda bulunur. Kişi dosyasının kimliği, kaynağın hesap sahibiyle eşleşmeli.</Empty>
  const f = a.follow
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Takipçi" value={num(f.followers)} />
        <Stat label="Takip edilen" value={num(f.following)} />
        <Stat label="Karşılıklı" value={num(f.mutual)} sub={`${num(f.not_following_back)} kişi takip edip takip edilmiyor`} />
        <Stat label="Takipten çıkılan" value={num(f.unfollowed)} />
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <Panel title="En çok arananlar" hint="Hesap araması geçmişi; tekrar sayısı ve ilk/son tarih.">
          <BarList items={a.searched.map((s) => ({ key: s.key, count: s.count, sub: s.count > 1 ? `${dateShort(s.first)} → ${dateShort(s.last)}` : dateShort(s.first) }))} empty="Arama kaydı yok" />
        </Panel>
        <Panel title="En çok beğenilenler"><BarList items={a.liked} empty="Beğeni kaydı yok" /></Panel>
        <Panel title="Yorum yapılanlar"><BarList items={a.commented} empty="Yorum kaydı yok" /></Panel>
        <Panel title="Hikâyesiyle etkileşilenler"><BarList items={a.story} empty="Hikâye etkileşimi yok" /></Panel>
      </div>
      {(a.posts.posts + a.posts.stories + a.posts.reels > 0) && (
        <Panel title="Kendi paylaşımları" hint={`${num(a.posts.posts)} gönderi · ${num(a.posts.stories)} hikâye · ${num(a.posts.reels)} reels · ${dateShort(a.posts.first)} – ${dateShort(a.posts.last)}`}>
          <Area points={(a.posts.monthly ?? []).map((m) => ({ month: m.month, value: m.count }))} />
          <div className="mt-4"><Bars values={a.posts.hours} labels={Array.from({ length: 24 }, (_, h) => String(h))} height={60} /></div>
        </Panel>
      )}
      {a.media.with_meta > 0 && (
        <Panel title="Fotoğraf metadata'sı (EXIF)" hint={`${num(a.media.with_meta)} fotoğrafta EXIF var · ${num(a.media.with_gps)} tanesinde konum · ${dateShort(a.media.first)} – ${dateShort(a.media.last)}`}>
          <div className="grid gap-4 md:grid-cols-2">
            <div><div className="mb-2 text-[12px] font-semibold uppercase tracking-wide text-muted">Cihazlar</div><BarList items={a.media.devices ?? []} empty="Cihaz bilgisi yok" /></div>
            <div>
              <div className="mb-2 text-[12px] font-semibold uppercase tracking-wide text-muted">Konumlar (≈1 km kümeler)</div>
              {(a.media.places?.length ?? 0) === 0 ? <p className="text-[13px] text-muted">Konumlu fotoğraf yok.</p> : (
                <ul className="space-y-1.5 text-[13px]">
                  {a.media.places!.map((p, i) => (
                    <li key={i} className="flex items-center justify-between gap-3">
                      <span className="font-mono text-[12.5px]">{p.lat.toFixed(3)}, {p.lon.toFixed(3)}</span>
                      <span className="text-muted">{num(p.count)} fotoğraf · {dateShort(p.first)}{p.first !== p.last ? ` – ${dateShort(p.last)}` : ''}</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
          <p className="mt-3 text-[12px] text-muted">Konum verisi hassastır: yalnızca bu bilgisayarda gösterilir, kanıt paketine ve yapay zekâya gönderilmez.</p>
        </Panel>
      )}
      {(a.profile_changes?.length ?? 0) > 0 && (
        <Panel title="Profil değişiklikleri" hint="Kullanıcı adı, e-posta, telefon ya da biyografi değişimleri.">
          <ul className="divide-y divide-line">
            {a.profile_changes!.map((c, i) => (
              <li key={i} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5 text-[13.5px]">
                <Badge>{c.what}</Badge>
                <span className="text-muted">{c.from || '—'} → <b className="text-fg">{c.to || '—'}</b></span>
                <span className="ml-auto text-[12px] text-faint">{dateShort(c.ts)}</span>
              </li>
            ))}
          </ul>
        </Panel>
      )}
      {(a.interests?.length ?? 0) > 0 && (
        <Panel title="Instagram'ın atadığı ilgi alanları"><div className="flex flex-wrap gap-1.5">{a.interests!.map((k) => <Chip key={k}>{k}</Chip>)}</div></Panel>
      )}
      {a.keywords.length > 0 && <Panel title="Aranan sözcükler"><div className="flex flex-wrap gap-1.5">{a.keywords.map((k) => <Chip key={k.key}>{k.key} <span className="text-faint">{k.count}</span></Chip>)}</div></Panel>}
      {a.follow_monthly.length > 1 && (
        <Panel title="Takip hareketi (aylık)" hint="Yeni takip edilen, yeni takipçi ve takipten çıkılan.">
          <Bars values={a.follow_monthly.map((m) => m.following + m.followers)} labels={a.follow_monthly.map((m) => m.month.slice(2))} height={100} />
        </Panel>
      )}
      <div className="grid gap-4 md:grid-cols-2">
        <Panel title="Hesap">
          <dl className="grid grid-cols-2 gap-3 text-[13px]">
            <Dl k="Yakın arkadaş" v={num(f.close_friends)} /><Dl k="Engellenen" v={num(f.blocked)} />
            <Dl k="Kısıtlanan" v={num(f.restricted)} /><Dl k="Bekleyen istek" v={num(f.pending)} />
            <Dl k="Takip edip edilmeyen" v={num(f.not_following_back)} /><Dl k="Takip edilip etmeyen" v={num(f.fans)} />
          </dl>
        </Panel>
        {a.logins.total > 0 && (
          <Panel title="Giriş etkinliği" hint={`${num(a.logins.total)} giriş · ${num(a.logins.distinct_ips)} farklı IP · ${dateShort(a.logins.first)} – ${dateShort(a.logins.last)}`}>
            <BarList items={a.logins.devices} empty="Cihaz bilgisi yok" />
            <div className="mt-4"><Bars values={a.logins.hours} labels={Array.from({ length: 24 }, (_, h) => String(h))} height={60} /></div>
          </Panel>
        )}
      </div>
    </div>
  )
}

const KIND: Record<string, { label: string; tone: 'neutral' | 'accent' | 'ok' | 'warn' | 'bad' | 'ig' }> = {
  contact: { label: 'temas', tone: 'accent' }, silence: { label: 'sessizlik', tone: 'warn' }, follow: { label: 'takip', tone: 'ok' },
  unfollow: { label: 'kopuş', tone: 'bad' }, milestone: { label: 'kilometre taşı', tone: 'accent' }, peak: { label: 'yoğun gün', tone: 'warn' },
  night: { label: 'gece', tone: 'neutral' }, run: { label: 'tek taraflı', tone: 'warn' }, trend: { label: 'yoğunluk', tone: 'accent' }, retract: { label: 'silinen', tone: 'neutral' },
  keyword: { label: 'anahtar sözcük', tone: 'ig' }, profile: { label: 'profil', tone: 'accent' }, interest: { label: 'ilgi', tone: 'ig' }, search: { label: 'arama', tone: 'ig' }, period: { label: 'dönem', tone: 'neutral' },
}

function Timeline({ id }: { id: number }) {
  const { data, error, loading } = useAsync(() => api.timeline(id), [id])
  const [filter, setFilter] = useState<string | null>(null)
  const [important, setImportant] = useState(true)
  if (loading) return <Spinner />
  if (error) return <ErrorNote>{error}</ErrorNote>
  const all = data ?? []
  const items: TimelineItem[] = all.filter((i) => (!filter || i.kind === filter) && (!important || filter || i.weight >= 3 || ['milestone', 'peak', 'keyword', 'night'].includes(i.kind)))
  if ((data ?? []).length === 0) return <Empty title="Zaman çizelgesi boş">Mesaj ya da etkinlik verisi olan bir kişi dosyasında önemli anlar burada toplanır.</Empty>
  const kinds = [...new Set((data ?? []).map((i) => i.kind))]
  let last = ''
  return (
    <div>
      <div className="mb-4 flex flex-wrap gap-1.5">
        <button onClick={() => { setFilter(null); setImportant(true) }} className={`rounded-full px-3 py-1 text-[13px] ${!filter && important ? 'bg-accent text-accent-fg' : 'bg-raised text-muted'}`}>Önemli anlar</button>
        <button onClick={() => { setFilter(null); setImportant(false) }} className={`rounded-full px-3 py-1 text-[13px] ${!filter && !important ? 'bg-accent text-accent-fg' : 'bg-raised text-muted'}`}>Tümü ({all.length})</button>
        {kinds.map((k) => <button key={k} onClick={() => setFilter(filter === k ? null : k)} className={`rounded-full px-3 py-1 text-[13px] ${filter === k ? 'bg-accent text-accent-fg' : 'bg-raised text-muted'}`}>{KIND[k]?.label ?? k}</button>)}
      </div>
      {items.some((i) => i.kind === 'keyword') && <p className="mb-3 text-[12px] text-muted">“Anahtar sözcük” satırları yalnızca sözcük eşleşmesidir; ifadenin anlamı (şaka, alıntı, olumsuzlama) yorumlanmaz.</p>}
      <ol className="relative ml-2 border-l border-line pl-6">
        {items.map((it, i) => {
          const m = it.ts.slice(0, 7)
          const head = m !== last
          last = m
          const k = KIND[it.kind] ?? { label: it.kind, tone: 'neutral' as const }
          return (
            <li key={i} className="relative pb-4">
              {head && <div className="mb-2 mt-2 text-[12px] font-semibold uppercase tracking-wider text-faint">{month(m)}</div>}
              <span className="absolute -left-[31px] mt-1.5 size-2.5 rounded-full border-2 border-surface bg-accent" />
              <div className="flex flex-wrap items-center gap-2 text-[13.5px]">
                <Badge tone={k.tone}>{k.label}</Badge>
                <span className="font-medium">{it.title}</span>
                <span className="text-[12px] text-faint">{dateShort(it.ts)}</span>
              </div>
              {it.detail && <div className="mt-0.5 text-[12.5px] text-muted">{it.detail}</div>}
            </li>
          )
        })}
      </ol>
    </div>
  )
}

function Identities({ person }: { person: Person }) {
  const confirm = useConfirm()
  const { reload, toast } = useData()
  const [value, setValue] = useState('')
  const [name, setName] = useState(person.name)
  const [note, setNote] = useState(person.note)
  const [busy, setBusy] = useState(false)

  async function act(fn: () => Promise<unknown>, ok?: string) {
    setBusy(true)
    try { await fn(); await reload(); if (ok) toast(ok) } catch (e) { toast(e instanceof Error ? e.message : 'İşlem başarısız', 'bad') } finally { setBusy(false) }
  }

  return (
    <div className="space-y-4">
      <Panel title="Kimlikler" hint="Bu kişinin farklı kaynaklarda görünen adları ve kullanıcı adları. Eşleşme büyük/küçük harf, boşluk ve noktalamadan bağımsızdır.">
        <div className="mb-4 flex flex-wrap gap-2">
          {person.identities.map((i) => <Chip key={i.id} onRemove={person.identities.length > 1 ? () => void act(() => api.removeIdentity(person.id, i.id)) : undefined}>{i.value}</Chip>)}
        </div>
        <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); if (value.trim()) void act(() => api.addIdentity(person.id, value.trim()), 'Kimlik eklendi.').then(() => setValue('')) }}>
          <input className={`${inputCls} max-w-xs`} placeholder="Ad ya da @kullanıcı_adı ekle" value={value} onChange={(e) => setValue(e.target.value)} aria-label="Yeni kimlik" />
          <Button type="submit" busy={busy} disabled={!value.trim()}>Ekle</Button>
        </form>
      </Panel>
      <NotesPanel personId={person.id} />
      <Panel title="Bilgiler">
        <div className="grid gap-4 md:grid-cols-2">
          <Field label="Ad"><input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <Field label="Not"><input className={inputCls} value={note} onChange={(e) => setNote(e.target.value)} placeholder="İsteğe bağlı" /></Field>
        </div>
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <Button variant="primary" busy={busy} disabled={name === person.name && note === person.note} onClick={() => void act(() => api.patchPerson(person.id, { name, note }), 'Kaydedildi.')}>Kaydet</Button>
          <Button busy={busy} onClick={() => void act(() => api.patchPerson(person.id, { is_self: !person.is_self }), person.is_self ? '"Ben" işareti kaldırıldı.' : 'Bu kişi artık "ben".')}>
            {person.is_self ? '"Ben" işaretini kaldır' : 'Bu kişi benim'}
          </Button>
          <Button variant="danger" className="ml-auto" busy={busy} onClick={async () => { if (await confirm({ title: `"${person.name}" dosyası silinsin mi?`, message: 'Notlar ve OSINT bağları gider. Kaynak veriler (mesajlar) silinmez.', confirmLabel: 'Dosyayı sil' })) void act(async () => { await api.deletePerson(person.id); go('/kisiler') }, 'Kişi dosyası silindi.') }}>
            <Trash2 className="size-4" /> Dosyayı sil
          </Button>
        </div>
      </Panel>
    </div>
  )
}

function NotesPanel({ personId }: { personId: number }) {
  const { toast } = useData()
  const notes = useAsync(() => api.notes(personId), [personId])
  const [text, setText] = useState('')
  return (
    <Panel title="Notlar" hint="Senin onayladığın, kaynağı belli notlar. Ham mesaj metni burada tutulmaz.">
      <ul className="mb-3 space-y-2">
        {(notes.data ?? []).map((n) => (
          <li key={n.id} className="flex items-start gap-3 rounded-xl bg-raised px-3 py-2 text-[13px]">
            <div className="min-w-0 flex-1"><div>{n.text}</div><div className="text-[11.5px] text-faint">{n.source === 'user' ? 'sen yazdın' : n.source} · {dateShort(n.created_at)}</div></div>
            <button aria-label="Notu sil" className="text-faint hover:text-bad" onClick={async () => { try { await api.deleteNote(n.id); notes.setData((notes.data ?? []).filter((x) => x.id !== n.id)) } catch (e) { toast(e instanceof Error ? e.message : 'Silinemedi', 'bad') } }}><Trash2 className="size-3.5" /></button>
          </li>
        ))}
        {(notes.data?.length ?? 0) === 0 && <li className="text-[13px] text-muted">Henüz not yok.</li>}
      </ul>
      <form className="flex gap-2" onSubmit={async (e) => { e.preventDefault(); if (!text.trim()) return; try { const n = await api.addNote(personId, text.trim()); notes.setData([...(notes.data ?? []), n]); setText('') } catch (err) { toast(err instanceof Error ? err.message : 'Eklenemedi', 'bad') } }}>
        <input className={inputCls} value={text} onChange={(e) => setText(e.target.value)} placeholder="Not ekle…" maxLength={1000} aria-label="Yeni not" />
        <Button type="submit" disabled={!text.trim()}>Ekle</Button>
      </form>
    </Panel>
  )
}

import { Plus, Users } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Avatar, Badge, Button, Card, Empty, inputCls, PageHeader } from '@/components/ui'
import { api } from '@/lib/api'
import { useData } from '@/lib/data'
import { dateShort, duration, hourLabel, num, pct, platformName } from '@/lib/fmt'
import { go, href } from '@/lib/router'
import type { PersonOverview } from '@/lib/types'

export default function People() {
  const { people, reload, toast, sources } = useData()
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [ov, setOv] = useState<Record<number, PersonOverview>>({})
  useEffect(() => {
    api.peopleOverview().then((l) => setOv(Object.fromEntries(l.map((o) => [o.id, o])))).catch(() => {})
  }, [people])

  async function create(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim()) return
    setBusy(true)
    try {
      const p = await api.createPerson(name.trim())
      await reload()
      setName('')
      go(`/kisi/${p.id}`)
    } catch (err) { toast(err instanceof Error ? err.message : 'Oluşturulamadı', 'bad') } finally { setBusy(false) }
  }

  return (
    <>
      <PageHeader title="Kişiler" sub="Her kişi dosyası, farklı kaynaklardaki ad ve kullanıcı adlarını tek kimlikte toplar." />
      <form onSubmit={create} className="mb-6 flex gap-2">
        <input className={`${inputCls} max-w-sm`} placeholder="Yeni kişi adı" value={name} onChange={(e) => setName(e.target.value)} aria-label="Yeni kişi adı" />
        <Button type="submit" variant="primary" busy={busy} disabled={!name.trim()}><Plus className="size-4" /> Kişi ekle</Button>
      </form>
      {people.length === 0 ? (
        <Empty icon={<Users className="size-8" />} title="Henüz kişi dosyası yok"
          action={sources.length > 0 ? <a href={href('/kaynaklar')}><Button>Kaynaklardan kişi aç</Button></a> : undefined}>
          Bir kaynak içe aktardığında hesap sahibi için otomatik dosya açılır. Sohbet karşıları için Kaynaklar sayfasından "Dosya aç" de.
        </Empty>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {people.map((p) => (
            <a key={p.id} href={href(`/kisi/${p.id}`)}>
              <Card className="h-full p-4 transition hover:border-accent/50">
                <div className="flex items-center gap-3">
                  <Avatar name={p.name} size={44} />
                  <div className="min-w-0">
                    <div className="flex items-center gap-2 truncate text-[15px] font-semibold">{p.name}{p.is_self && <Badge tone="accent">ben</Badge>}</div>
                    <div className="text-[12.5px] text-muted">{p.identities.length} kimlik</div>
                  </div>
                </div>
                {ov[p.id] && ov[p.id].messages > 0 && <Glance o={ov[p.id]} />}
                <div className="mt-3 flex flex-wrap gap-1">
                  {p.identities.slice(0, 4).map((i) => <span key={i.id} className="rounded-full bg-raised px-2 py-0.5 text-[12px] text-muted">{i.value}</span>)}
                </div>
              </Card>
            </a>
          ))}
        </div>
      )}
    </>
  )
}

function Glance({ o }: { o: PersonOverview }) {
  return (
    <div className="mt-3 space-y-1.5 border-t border-line pt-3 text-[12.5px] text-muted">
      <div><b className="text-ink tabular-nums">{num(o.messages)}</b> mesaj · {num(o.active_days)} aktif gün · {o.platforms.map(platformName).join(' + ')}</div>
      <div>{dateShort(o.first)} – {dateShort(o.last)}</div>
      {o.top_partner && <div>En yoğun sohbet: <span className="text-ink">{o.top_partner}</span> ({pct(o.top_share ?? 0)}) · {o.conversations} sohbet</div>}
      <div>
        En aktif saat {hourLabel(o.peak_hour)}
        {o.reply_median_sec >= 60 && <> · medyan yanıt {duration(o.reply_median_sec)}</>}
        {o.night_ratio >= 0.05 && <> · gece payı {pct(o.night_ratio)}</>}
        {o.notes > 0 && <> · {o.notes} not</>}
      </div>
    </div>
  )
}

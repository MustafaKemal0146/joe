import { EyeOff } from 'lucide-react'
import { useMemo, useState } from 'react'
import { GraphView } from '@/components/Graph'
import { Avatar, Badge, Button, Card, Empty, ErrorNote, Spinner } from '@/components/ui'
import { api } from '@/lib/api'
import { useAsync, useData } from '@/lib/data'
import { num } from '@/lib/fmt'
import { href } from '@/lib/router'
import type { EgoNeighbor, Graph } from '@/lib/types'

export const KIND: Record<string, string> = {
  dm: 'mesaj', group: 'grup mesajı', reaction: 'tepki', like_post: 'gönderi beğenisi', like_comment: 'yorum beğenisi', comment: 'yorum',
  saved: 'kaydedilen', story_like: 'hikâye beğenisi', story_interact: 'hikâye etkileşimi', story_view: 'hikâye izleme', post_view: 'gönderi görüntüleme', video_view: 'video izleme', suggestion_view: 'önerilen hesap görüntüleme', search_account: 'arama',
  group_reply: 'grup içi yanıt', following: 'takip', follower: 'takipçi', close_friend: 'yakın arkadaş',
}

/** Bir kişinin çevresi: doğrudan bağlantıları, aralarındaki bağlar ve yakın/geniş çevre ayrımı. */
export default function EgoNetwork({ personId }: { personId: number }) {
  const { reload, toast } = useData()
  const [max, setMax] = useState(40)
  const [bump, setBump] = useState(0)
  const ego = useAsync(() => api.network(personId, max), [personId, max, bump])
  const [sel, setSel] = useState<string | null>(null)
  const e = ego.data

  const graph: Graph | null = useMemo(() => e && {
    nodes: [{ ...e.center, is_owner: true, strength: Math.max(1, ...e.neighbors.map((n) => n.strength)) }, ...e.neighbors.map((n) => ({ id: n.id, label: n.label, person_id: n.person_id, is_owner: false, strength: n.strength }))],
    edges: e.edges, total_nodes: e.total_neighbors + 1,
  }, [e])
  const byId = useMemo(() => new Map((e?.neighbors ?? []).map((n) => [n.id, n])), [e])
  const chosen = sel ? byId.get(sel) : undefined
  const near = (e?.neighbors ?? []).filter((n) => n.circle === 'yakin')
  const wide = (e?.neighbors ?? []).filter((n) => n.circle === 'genis')

  async function hide(n: EgoNeighbor) {
    try { await api.graphHide(n.id, n.label); toast(`${n.label} haritadan gizlendi (Gizlenenler'den geri getirebilirsin).`); setSel(null); setBump((b) => b + 1) } catch (err) { toast(err instanceof Error ? err.message : 'Gizlenemedi', 'bad') }
  }
  async function open(n: EgoNeighbor) {
    try { const p = await api.createPerson(n.label, [n.label]); await reload(); toast(`${p.name} için dosya açıldı.`); setBump((b) => b + 1) } catch (err) { toast(err instanceof Error ? err.message : 'Açılamadı', 'bad') }
  }

  if (ego.loading && !e) return <Spinner label="Çevre kuruluyor…" />
  if (ego.error) return <ErrorNote>{ego.error}</ErrorNote>
  if (!e || !graph || e.neighbors.length === 0) {
    return <Empty title="Bu kişinin çevresi çıkarılamadı">Bu kişiyle eşleşen mesaj ya da etkinlik yok. Kişi dosyasındaki kimliklerin (ad, kullanıcı adı) verideki adlarla eşleştiğinden emin ol.</Empty>
  }

  const row = (n: EgoNeighbor) => (
    <li key={n.id}>
      <button onClick={() => setSel(n.id === sel ? null : n.id)} className={`flex w-full items-center gap-3 rounded-xl px-3 py-2 text-left hover:bg-raised ${n.id === sel ? 'bg-raised' : ''}`}>
        <Avatar name={n.label} size={32} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13.5px] font-medium">{n.label}</div>
          <div className="mt-0.5 flex flex-wrap gap-1">{n.relations.slice(0, 4).map((r) => <span key={r} className="rounded-full bg-sunken px-2 py-0.5 text-[11.5px] text-muted">{r}</span>)}</div>
        </div>
      </button>
    </li>
  )

  return (
    <div className="grid gap-4 lg:grid-cols-[1fr_320px]">
      <Card className="relative overflow-hidden">
        <div className="aspect-[640/420] w-full"><GraphView graph={graph} selected={sel} onSelect={setSel} /></div>
        <div className="flex items-center justify-between gap-3 border-t border-line px-4 py-2.5 text-[12.5px] text-muted">
          <span>{num(e.neighbors.length)} / {num(e.total_neighbors)} bağlantı · {num(near.length)} yakın çevre · çizgi kalınlığı etkileşim gücü</span>
          {e.total_neighbors > max && <button className="text-accent" onClick={() => setMax(Math.min(150, max + 40))}>Daha fazla göster</button>}
        </div>
      </Card>
      <div className="space-y-4">
        {chosen && (
          <Card className="p-4">
            <div className="flex items-center gap-3">
              <Avatar name={chosen.label} size={40} />
              <div className="min-w-0 flex-1">
                <div className="truncate text-[15px] font-semibold">{chosen.label}</div>
                <div className="flex gap-1.5"><Badge tone={chosen.circle === 'yakin' ? 'accent' : 'neutral'}>{chosen.circle === 'yakin' ? 'yakın çevre' : 'geniş çevre'}</Badge>{chosen.person_id ? <Badge tone="ok">kişi dosyası</Badge> : <Badge>dosyası yok</Badge>}</div>
              </div>
            </div>
            <div className="mt-3 flex flex-wrap gap-1">{Object.entries(chosen.breakdown).sort((a, b) => b[1] - a[1]).map(([k, v]) => <span key={k} className="rounded-full bg-raised px-2 py-0.5 text-[11.5px] text-muted">{KIND[k] ?? k} {num(v)}</span>)}</div>
            <div className="mt-3 flex flex-wrap gap-2">
              {chosen.person_id ? <a href={href(`/kisi/${chosen.person_id}`)}><Button size="sm">Kişi dosyasını aç</Button></a> : <Button size="sm" onClick={() => void open(chosen)}>Kişi dosyası aç</Button>}
              <Button size="sm" onClick={() => void hide(chosen)}><EyeOff className="size-3.5" /> Haritadan gizle</Button>
            </div>
          </Card>
        )}
        <Card className="p-2">
          <div className="px-3 pb-1 pt-2 text-[12px] font-semibold uppercase tracking-wide text-muted">Yakın çevre ({near.length})</div>
          {near.length === 0 ? <p className="px-3 pb-2 text-[12.5px] text-muted">Yakın arkadaş, karşılıklı takip ya da yoğun mesajlaşma bulunamadı.</p> : <ul>{near.map(row)}</ul>}
          <div className="px-3 pb-1 pt-3 text-[12px] font-semibold uppercase tracking-wide text-muted">Geniş çevre ({wide.length})</div>
          <ul className="max-h-80 overflow-auto">{wide.map(row)}</ul>
        </Card>
        <p className="px-1 text-[12px] text-muted">
          {e.own_data ? 'Takip, yakın arkadaş ve beğeni bilgisi bu kişinin kendi hesap verisinden gelir.' : 'Bu kişinin kendi hesap verisi (takip, yakın arkadaş) yüklenmemiş; bağlar yalnızca mesaj ve başkalarının verisinden çıkarılmıştır.'} Yakın/geniş çevre ayrımı sezgiseldir (yakın arkadaş, karşılıklı takip ya da 50+ mesaj); bir ilişki yargısı değildir.
        </p>
      </div>
    </div>
  )
}

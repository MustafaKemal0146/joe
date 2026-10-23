import { useConfirm } from '@/components/Confirm'
import { EyeOff, Link2, Network, Trash2, Undo2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import EgoNetwork, { KIND } from '@/components/EgoNetwork'
import { GraphView } from '@/components/Graph'
import { Avatar, Badge, Button, Card, Empty, ErrorNote, PageHeader, Spinner } from '@/components/ui'
import { api } from '@/lib/api'
import { useAsync, useData } from '@/lib/data'
import { num } from '@/lib/fmt'
import { href } from '@/lib/router'

export default function MapPage() {
  const { people, reload, toast, sources } = useData()
  const confirm = useConfirm()
  const [max, setMax] = useState(40)
  const [bump, setBump] = useState(0)
  const graph = useAsync(() => api.graph(max), [max, people.length, sources.length, bump])
  const sugg = useAsync(() => api.suggestions(), [people.length, sources.length])
  const [sel, setSel] = useState<string | null>(null)
  const [mode, setMode] = useState<'genel' | 'kisi'>('genel')
  const [who, setWho] = useState(0)
  const [busy, setBusy] = useState<string | null>(null)
  const g = graph.data
  const byId = useMemo(() => new Map((g?.nodes ?? []).map((n) => [n.id, n])), [g])
  const node = sel ? byId.get(sel) : undefined
  const edges = useMemo(() => (g && sel ? g.edges.filter((e) => e.a === sel || e.b === sel).sort((a, b) => b.strength - a.strength) : []), [g, sel])

  const me = people.find((p) => p.is_self)
  const personId = who || me?.id || people[0]?.id || 0

  async function hide(id: string, label: string) {
    setBusy('hide' + id)
    try { await api.graphHide(id, label); setSel(null); setBump((b) => b + 1); toast(`${label} haritadan gizlendi. Veri silinmedi; "Gizlenenler"den geri getirebilirsin.`) } catch (e) { toast(e instanceof Error ? e.message : 'Gizlenemedi', 'bad') } finally { setBusy(null) }
  }
  async function unhide(id: string) {
    try { await api.graphUnhide(id); setBump((b) => b + 1) } catch (e) { toast(e instanceof Error ? e.message : 'Geri getirilemedi', 'bad') }
  }
  async function removePerson(pid: number, name: string) {
    if (!await confirm({ title: `"${name}" dosyası silinsin mi?`, message: 'Notlar ve OSINT bağları gider; mesaj verisi kaynakta kalır ve düğüm haritadan gizlenir.', confirmLabel: 'Dosyayı sil' })) return
    try { await api.deletePerson(pid); await reload(); setSel(null); setBump((b) => b + 1); toast(`${name} dosyası silindi.`) } catch (e) { toast(e instanceof Error ? e.message : 'Silinemedi', 'bad') }
  }

  async function link(name: string, handle: string) {
    setBusy(name + handle)
    try {
      await api.createPerson(name, [handle])
      await reload()
      toast(`${name} ile @${handle} aynı kişi olarak birleştirildi.`)
    } catch (e) { toast(e instanceof Error ? e.message : 'Birleştirilemedi', 'bad') } finally { setBusy(null) }
  }

  return (
    <>
      <PageHeader title="İlişki haritası" sub="Mesajlar, tepkiler, beğeniler, yorumlar, hikâye etkileşimleri, aramalar ve takip ilişkileri tek ağda." />
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <div className="inline-flex rounded-full bg-raised p-0.5 text-[13px]" role="tablist" aria-label="Harita türü">
          {([['genel', 'Genel'], ['kisi', 'Kişi bazlı']] as const).map(([id, label]) => (
            <button key={id} role="tab" aria-selected={mode === id} onClick={() => { setMode(id); setSel(null) }} className={`rounded-full px-4 py-1.5 ${mode === id ? 'bg-surface font-medium shadow-sm' : 'text-muted'}`}>{label}</button>
          ))}
        </div>
        {mode === 'kisi' && people.length > 0 && (
          <select className="h-9 rounded-xl border border-line bg-surface px-3 text-[13.5px]" value={personId} onChange={(e) => setWho(Number(e.target.value))} aria-label="Kişi">
            {people.map((p) => <option key={p.id} value={p.id}>{p.name}{p.is_self ? ' (ben)' : ''}</option>)}
          </select>
        )}
      </div>
      {mode === 'kisi' ? (people.length === 0 ? <Empty icon={<Network className="size-8" />} title="Önce bir kişi dosyası aç">Kişi bazlı harita, seçtiğin kişinin çevresini gösterir.</Empty> : <EgoNetwork key={personId + ':' + bump} personId={personId} />) : graph.loading ? <Spinner label="Harita kuruluyor…" /> : graph.error ? <ErrorNote>{graph.error}</ErrorNote> : !g || g.nodes.length < 2 ? (
        <Empty icon={<Network className="size-8" />} title="Harita için yeterli veri yok">Bir kaynak içe aktarıp hesap sahibini doğruladığında burada sahibin çevresi görünür.</Empty>
      ) : (
        <div className="grid gap-4 lg:grid-cols-[1fr_300px]">
          <Card className="relative overflow-hidden">
            <div className="aspect-[640/420] w-full"><GraphView graph={g} selected={sel} onSelect={setSel} /></div>
            <div className="flex items-center justify-between gap-3 border-t border-line px-4 py-2.5 text-[12.5px] text-muted">
              <span>{num(g.nodes.length)} / {num(g.total_nodes)} düğüm · çizgi kalınlığı etkileşim gücü</span>
              {g.total_nodes > max && <button className="text-accent" onClick={() => setMax(Math.min(200, max + 40))}>Daha fazla göster</button>}
            </div>
          </Card>
          <div className="space-y-4">
            <Card className="p-4">
              {node ? (
                <>
                  <div className="flex items-center gap-3">
                    <Avatar name={node.label} size={40} />
                    <div className="min-w-0">
                      <div className="truncate text-[15px] font-semibold">{node.label}</div>
                      <div className="flex gap-1.5">{node.is_owner && <Badge tone="accent">hesap sahibi</Badge>}{node.person_id ? <Badge tone="ok">kişi dosyası</Badge> : <Badge>dosyası yok</Badge>}</div>
                    </div>
                  </div>
                  {!node.is_owner && (
                    <div className="mt-3 flex gap-2">
                      <Button size="sm" className="flex-1" busy={busy === 'hide' + node.id} onClick={() => void hide(node.id, node.label)}><EyeOff className="size-3.5" /> Haritadan gizle</Button>
                      {node.person_id ? <Button size="sm" variant="danger" aria-label="Kişi dosyasını sil" title="Kişi dosyasını sil" onClick={() => void removePerson(node.person_id!, node.label)}><Trash2 className="size-3.5" /></Button> : null}
                    </div>
                  )}
                  {node.person_id ? <a href={href(`/kisi/${node.person_id}`)} className="mt-3 block"><Button size="sm" className="w-full">Kişi dosyasını aç</Button></a> : (
                    <Button size="sm" className="mt-3 w-full" busy={busy === node.id} onClick={async () => { setBusy(node.id); try { const p = await api.createPerson(node.label, [node.label]); await reload(); toast(`${p.name} için dosya açıldı.`) } catch (e) { toast(e instanceof Error ? e.message : 'Açılamadı', 'bad') } finally { setBusy(null) } }}>Kişi dosyası aç</Button>
                  )}
                  <div className="mt-4 space-y-3">
                    {edges.map((e) => {
                      const other = byId.get(e.a === sel ? e.b : e.a)
                      return (
                        <div key={e.a + e.b}>
                          <div className="text-[13px] font-medium">{other?.label}</div>
                          <div className="mt-1 flex flex-wrap gap-1">
                            {Object.entries(e.breakdown).sort((a, b) => b[1] - a[1]).map(([k, v]) => <span key={k} className="rounded-full bg-raised px-2 py-0.5 text-[11.5px] text-muted">{KIND[k] ?? k} {num(v)}</span>)}
                          </div>
                        </div>
                      )
                    })}
                  </div>
                </>
              ) : <p className="text-[13px] text-muted">Bir düğüme tıkla: kimlerle, hangi kanaldan, ne kadar etkileştiğini göreceksin.</p>}
            </Card>
            {(sugg.data?.length ?? 0) > 0 && (
              <Card className="p-4">
                <div className="mb-1 text-[14px] font-semibold">Aynı kişi olabilir</div>
                <p className="mb-3 text-[12.5px] text-muted">Görünen ad ile kullanıcı adı benzerliği. Birleştirmek senin onayına bağlıdır.</p>
                <ul className="space-y-2">
                  {sugg.data!.slice(0, 6).map((s) => (
                    <li key={s.name + s.handle} className="flex items-center gap-2 text-[13px]">
                      <div className="min-w-0 flex-1"><div className="truncate font-medium">{s.name}</div><div className="truncate text-muted">@{s.handle} · %{Math.round(s.score * 100)}</div></div>
                      <Button size="sm" busy={busy === s.name + s.handle} onClick={() => link(s.name, s.handle)}><Link2 className="size-3.5" /> Birleştir</Button>
                    </li>
                  ))}
                </ul>
              </Card>
            )}
          </div>
        </div>
      )}
      {mode === 'genel' && (g?.hidden?.length ?? 0) > 0 && (
        <details className="mt-4 rounded-2xl border border-line p-4 text-[13px]">
          <summary className="cursor-pointer text-muted">Gizlenenler ({g!.hidden!.length}) — veri silinmedi, yalnızca haritada görünmüyor</summary>
          <ul className="mt-3 space-y-1.5">
            {g!.hidden!.map((h) => (
              <li key={h.id} className="flex items-center gap-3">
                <span className="min-w-0 flex-1 truncate">{h.label}</span>
                <Button size="sm" onClick={() => void unhide(h.id)}><Undo2 className="size-3.5" /> Geri getir</Button>
              </li>
            ))}
          </ul>
        </details>
      )}
    </>
  )
}

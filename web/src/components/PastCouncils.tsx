import { RotateCcw, Sparkles, Trash2 } from 'lucide-react'
import { useMemo } from 'react'
import { useConfirm } from '@/components/Confirm'
import { Badge, Button, Card, Empty, ErrorNote, Spinner } from '@/components/ui'
import { api } from '@/lib/api'
import { useAsync, useData } from '@/lib/data'
import { dateTime, usd } from '@/lib/fmt'
import { href } from '@/lib/router'
import { STATUS_LABEL } from '@/pages/CouncilPage'

const KIND: Record<string, string> = { single: 'Kişi analizi', pair: 'İki kişilik analiz', ask: 'Soru' }

/** Bir kişinin geçmiş konsey oturumları: yeniden aç, devam ettir ya da sil. */
export default function PastCouncils({ personId }: { personId: number }) {
  const { people, toast } = useData()
  const confirm = useConfirm()
  const list = useAsync(() => api.councils(), [personId])
  const name = useMemo(() => new Map(people.map((p) => [p.id, p.name])), [people])
  const rows = useMemo(() => (list.data ?? []).filter((c) => c.subject_ids.includes(personId)), [list.data, personId])

  async function remove(id: string) {
    if (!await confirm({ title: 'Bu analiz silinsin mi?', message: 'Analizin tüm mercek çıktıları, moderatör özeti ve genel değerlendirmesi silinir.', confirmLabel: 'Analizi sil' })) return
    try { await api.councilDelete(id); toast('Silindi.'); list.setData((list.data ?? []).filter((x) => x.id !== id)) } catch (e) { toast(e instanceof Error ? e.message : 'Silinemedi', 'bad') }
  }
  async function resume(id: string) {
    try { await api.councilResume(id); toast('Devam ediyor.'); list.setData(await api.councils()) } catch (e) { toast(e instanceof Error ? e.message : 'Başlatılamadı', 'bad') }
  }

  if (list.loading) return <Spinner label="Geçmiş konseyler okunuyor…" />
  if (list.error) return <ErrorNote>{list.error}</ErrorNote>
  const total = rows.reduce((n, c) => n + (c.spent_usd ?? 0), 0)
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3 text-[13px] text-muted">
        <span className="flex-1">{rows.length} geçmiş analiz · toplam harcama {usd(total)}</span>
        <a href={href(`/konsey?kisi=${personId}`)}><Button size="sm" variant="primary"><Sparkles className="size-4" /> Yeni analiz</Button></a>
      </div>
      {rows.length === 0 ? (
        <Empty title="Bu kişi için geçmiş konsey yok">Yeni bir analiz başlattığında burada listelenir.</Empty>
      ) : (
        <Card className="divide-y divide-line">
          {rows.map((c) => {
            const st = STATUS_LABEL[c.status] ?? { text: c.status, tone: 'neutral' as const }
            const who = c.subject_ids.map((id) => name.get(id) ?? '—').join(' × ')
            return (
              <div key={c.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                <a href={href(`/konsey/${c.id}`)} className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-[14px] font-medium">{KIND[c.kind] ?? c.kind}{c.kind === 'pair' ? `: ${who}` : ''}</span>
                    <Badge tone={st.tone}>{st.text}</Badge>
                  </div>
                  <div className="truncate text-[12.5px] text-muted">
                    {c.kind === 'ask' ? `Soru: ${(c.question ?? '').split('\n\nYeni soru: ').pop()}` : `${c.persona_ids.length} merceğin analizi`} · {dateTime(c.created_at)} · {c.model} · {usd(c.spent_usd ?? 0)}
                  </div>
                </a>
                {['interrupted', 'partial', 'failed', 'canceled'].includes(c.status) && c.kind !== 'ask' && (
                  <Button size="sm" onClick={() => void resume(c.id)}><RotateCcw className="size-3.5" /> Devam et</Button>
                )}
                <a href={href(`/konsey/${c.id}`)}><Button size="sm">Aç</Button></a>
                <Button size="sm" variant="ghost" aria-label="Sil" onClick={() => void remove(c.id)}><Trash2 className="size-4" /></Button>
              </div>
            )
          })}
        </Card>
      )}
    </div>
  )
}

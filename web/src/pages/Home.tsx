import { ArrowRight, KeyRound, X } from 'lucide-react'
import { useConfirm } from '@/components/Confirm'
import { Importer } from '@/components/Importer'
import { Avatar, Badge, Button, Card, PageHeader, PlatformBadge, Spinner, Stat } from '@/components/ui'
import { api } from '@/lib/api'
import { useData } from '@/lib/data'
import { dateShort, num } from '@/lib/fmt'
import { href } from '@/lib/router'

export default function Home() {
  const { sources, people, state, loaded, reload, toast } = useData()
  const confirm = useConfirm()
  if (!loaded) return <Spinner />

  async function removeSource(id: number, name: string, messages: number) {
    if (!await confirm({ title: `"${name}" kaynağı silinsin mi?`, message: `${num(messages)} mesaj ve tüm etkinlik kayıtları bu bilgisayardan silinir; arama dizini de temizlenir.\n\nBu işlem geri alınamaz.`, confirmLabel: 'Kaynağı ve verisini sil' })) return
    try { await api.deleteSource(id); await reload(); toast('Kaynak ve verisi silindi.') } catch (e) { toast(e instanceof Error ? e.message : 'Silinemedi', 'bad') }
  }
  async function removePerson(id: number, name: string, self: boolean) {
    if (!await confirm({ title: `"${name}" dosyası silinsin mi?`, message: `${self ? 'Bu SENİN dosyan; silinirse "ben" tarafı belirsizleşir.\n\n' : ''}Notlar ve OSINT bağları gider; mesaj verisi kaynakta kalır ve düğüm haritadan gizlenir.`, confirmLabel: 'Dosyayı sil' })) return
    try { await api.deletePerson(id); await reload(); toast(`${name} dosyası silindi.`) } catch (e) { toast(e instanceof Error ? e.message : 'Silinemedi', 'bad') }
  }

  if (sources.length === 0) {
    return (
      <div className="mx-auto max-w-2xl pt-6">
        <div className="mb-8 text-center">
          <h1 className="text-[40px] font-semibold leading-[1.1] tracking-tight">Bir kişiyi, verisinden tanı.</h1>
          <p className="mx-auto mt-3 max-w-lg text-[16px] text-muted">
            Instagram ve WhatsApp dışa aktarımlarından ritim, ilişki ve davranış izleri çıkar. Önce hepsi bu bilgisayarda,
            yapay zekâ harcaması olmadan hesaplanır.
          </p>
        </div>
        <Importer />
      </div>
    )
  }

  const totalMsgs = sources.reduce((n, s) => n + (s.stats.messages ?? 0), 0)
  const totalEvents = sources.reduce((n, s) => n + Object.entries(s.stats).filter(([k]) => k.startsWith('event:')).reduce((a, [, v]) => a + v, 0), 0)

  return (
    <>
      <PageHeader title="Genel bakış" sub="Eldeki veri ve hazır olan kişi dosyaları." />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Kaynak" value={num(sources.length)} />
        <Stat label="Kişi dosyası" value={num(people.length)} />
        <Stat label="Mesaj" value={num(totalMsgs)} />
        <Stat label="Etkinlik kaydı" value={num(totalEvents)} sub="takip, beğeni, arama, giriş…" />
      </div>

      {state && !state.provider_ready && (
        <Card className="mt-4 flex items-center gap-4 p-4">
          <div className="grid size-10 place-items-center rounded-xl bg-warn/14 text-warn"><KeyRound className="size-5" /></div>
          <div className="flex-1">
            <div className="text-[14px] font-semibold">Konsey için bir yapay zekâ anahtarı gerekir</div>
            <div className="text-[13px] text-muted">Metrikler anahtarsız çalışır. DeepSeek önerilir; anahtar şifreli saklanır.</div>
          </div>
          <a href={href('/ayarlar')}><Button size="sm">Ayarlara git</Button></a>
        </Card>
      )}

      <section className="mt-8">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-[17px] font-semibold tracking-tight">Kişiler</h2>
          <a href={href('/kisiler')} className="flex items-center gap-1 text-[13px] text-accent">Tümü <ArrowRight className="size-3.5" /></a>
        </div>
        {people.length === 0 ? (
          <p className="text-[13px] text-muted">Henüz kişi dosyası yok. <a className="text-accent" href={href('/kaynaklar')}>Kaynaklar</a> sayfasından bir sohbet karşısı için dosya aç.</p>
        ) : (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {people.slice(0, 6).map((p) => (
              <div key={p.id} className="group relative">
                <a href={href(`/kisi/${p.id}`)}>
                  <Card className="flex items-center gap-3 p-4 transition hover:border-accent/50">
                    <Avatar name={p.name} />
                    <div className="min-w-0">
                      <div className="flex items-center gap-2 truncate text-[14px] font-semibold">{p.name}{p.is_self && <Badge tone="accent">ben</Badge>}</div>
                      <div className="truncate text-[12.5px] text-muted">{p.identities.length} kimlik</div>
                    </div>
                  </Card>
                </a>
                <button aria-label={`${p.name} dosyasını sil`} title="Kişi dosyasını sil" onClick={() => void removePerson(p.id, p.name, p.is_self)}
                  className="absolute right-2 top-2 grid size-7 place-items-center rounded-full text-bad opacity-0 transition hover:bg-bad/10 focus:opacity-100 group-hover:opacity-100"><X className="size-4" /></button>
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="mt-8">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-[17px] font-semibold tracking-tight">Kaynaklar</h2>
          <a href={href('/kaynaklar')} className="flex items-center gap-1 text-[13px] text-accent">Yönet <ArrowRight className="size-3.5" /></a>
        </div>
        <Card className="divide-y divide-line">
          {sources.map((s) => (
            <div key={s.id} className="flex items-center gap-3 px-4 py-3">
              <PlatformBadge platform={s.platform} />
              <div className="min-w-0 flex-1">
                <div className="truncate text-[14px] font-medium">{s.owner_name || s.owner_handle || s.label}</div>
                <div className="truncate text-[12.5px] text-muted">{s.label} · {dateShort(s.imported_at)}</div>
              </div>
              <div className="text-right text-[13px] text-muted tabular-nums">{num(s.stats.messages ?? 0)} mesaj</div>
              <button aria-label="Kaynağı sil" title="Kaynağı ve verisini sil" onClick={() => void removeSource(s.id, s.owner_name || s.owner_handle || s.label, s.stats.messages ?? 0)}
                className="grid size-7 shrink-0 place-items-center rounded-full text-bad hover:bg-bad/10"><X className="size-4" /></button>
            </div>
          ))}
        </Card>
      </section>
    </>
  )
}

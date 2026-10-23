import { GitCompareArrows } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Ring, Split } from '@/components/charts'
import { Avatar, Badge, Card, Chip, Empty, ErrorNote, Meter, PageHeader, Panel, PlatformBadge, Spinner } from '@/components/ui'
import { api } from '@/lib/api'
import { useAsync, useData } from '@/lib/data'
import { duration, num } from '@/lib/fmt'
import { go } from '@/lib/router'

export default function ComparePage({ query }: { query: URLSearchParams }) {
  const { people } = useData()
  const [a, setA] = useState(Number(query.get('a')) || 0)
  const [b, setB] = useState(Number(query.get('b')) || 0)

  useEffect(() => {
    if (!a && people.length > 0) setA((people.find((p) => p.is_self) ?? people[0]!).id)
  }, [people, a])
  useEffect(() => {
    if (a && !b && people.length > 1) setB((people.find((p) => p.id !== a) ?? people[0]!).id)
  }, [people, a, b])
  useEffect(() => { if (a && b) go(`/karsilastir?a=${a}&b=${b}`) }, [a, b])

  const res = useAsync(() => (a && b && a !== b ? api.compare(a, b) : Promise.resolve(null)), [a, b])

  if (people.length < 2) return (
    <>
      <PageHeader title="Karşılaştır" sub="İki kişinin ritim, tempo, ifade ve çevre benzerliği ile aralarındaki sohbetin dengesi." />
      <Empty icon={<GitCompareArrows className="size-8" />} title="Karşılaştırmak için en az iki kişi gerekir">Sen ve bir başkası ya da iki kişi: kişi dosyalarını açtıktan sonra buraya dön.</Empty>
    </>
  )
  const pa = people.find((p) => p.id === a)
  const pb = people.find((p) => p.id === b)
  const c = res.data

  return (
    <>
      <PageHeader title="Karşılaştır" sub="Ben ve o, o ve başkası, ya da herhangi iki kişi." />
      <div className="mb-6 grid items-center gap-3 sm:grid-cols-[1fr_auto_1fr]">
        <PersonSelect value={a} onChange={setA} exclude={b} />
        <div className="hidden text-faint sm:block"><GitCompareArrows className="size-5" /></div>
        <PersonSelect value={b} onChange={setB} exclude={a} />
      </div>
      {res.loading ? <Spinner label="Hesaplanıyor…" /> : res.error ? <ErrorNote>{res.error}</ErrorNote> : c && pa && pb && (
        <div className="space-y-4">
          <Card className="flex flex-wrap items-center gap-6 p-6">
            <Ring value={c.overall} label="Benzerlik ve denge göstergesi" />
            <div className="min-w-0 flex-1">
              <div className="mb-2 flex items-center gap-2"><Avatar name={pa.name} size={28} /><span className="font-semibold">{pa.name}</span><span className="text-faint">×</span><Avatar name={pb.name} size={28} /><span className="font-semibold">{pb.name}</span></div>
              <h2 className="text-[17px] font-semibold tracking-tight">Benzerlik ve denge göstergesi</h2>
              <p className="mt-1 text-[13px] text-muted">{(c.dimensions?.length ?? 0)} boyutun ortalaması. {c.caveat}</p>
            </div>
          </Card>
          {(c.dimensions?.length ?? 0) > 0 && (
            <Panel title="Boyutlar">
              <ul className="space-y-4">
                {c.dimensions!.map((d) => (
                  <li key={d.key}>
                    <div className="mb-1 flex items-baseline justify-between gap-3">
                      <span className="text-[13.5px] font-medium">{d.label}</span>
                      <span className="text-[13px] font-semibold tabular-nums">{Math.round(d.score * 100)}</span>
                    </div>
                    <Meter value={d.score} tone={d.score > 0.66 ? 'ok' : d.score > 0.33 ? 'accent' : 'warn'} />
                    <div className="mt-1 text-[12.5px] text-muted">{d.detail}</div>
                  </li>
                ))}
              </ul>
            </Panel>
          )}
          {c.direct && (
            <Panel title="Aralarındaki sohbet" hint={`${num(c.direct.total)} mesaj · ${c.direct.platforms.join(', ')}`} action={<PlatformBadge platform={c.direct.platform} />}>
              <div className="mb-2 flex justify-between text-[12px] font-medium"><span className="text-accent">{pa.name}</span><span className="text-ig">{pb.name}</span></div>
              <div className="grid gap-3 md:grid-cols-2">
                <Split label="mesaj" left={c.direct.messages.subject} right={c.direct.messages.partner} leftName={pa.name} rightName={pb.name} />
                <Split label="konuşmayı başlatma" left={c.direct.starts.subject} right={c.direct.starts.partner} leftName={pa.name} rightName={pb.name} />
                <Split label="medyan yanıt süresi" left={c.direct.latency.subject.median} right={c.direct.latency.partner.median} leftName={pa.name} rightName={pb.name} format={duration} />
                <Split label="peş peşe mesaj" left={c.direct.double_texts.subject} right={c.direct.double_texts.partner} leftName={pa.name} rightName={pb.name} />
              </div>
            </Panel>
          )}
          {((c.shared_words?.length ?? 0) > 0 || (c.shared_people?.length ?? 0) > 0) && (
            <div className="grid gap-4 md:grid-cols-2">
              {(c.shared_words?.length ?? 0) > 0 && <Panel title="Ortak sık sözcükler"><div className="flex flex-wrap gap-1.5">{c.shared_words.map((w) => <Chip key={w}>{w}</Chip>)}</div></Panel>}
              {(c.shared_people?.length ?? 0) > 0 && <Panel title="Ortak çevre" hint="Her ikisinin de etkileştiği kişi ve hesaplar."><div className="flex flex-wrap gap-1.5">{c.shared_people.slice(0, 30).map((w) => <Chip key={w}>{w}</Chip>)}</div></Panel>}
            </div>
          )}
          {(c.missing?.length ?? 0) > 0 && (
            <Card className="p-4">
              <div className="mb-1 flex items-center gap-2 text-[14px] font-semibold">Hesaplanamayanlar <Badge tone="warn">eksik veri</Badge></div>
              <ul className="list-disc space-y-1 pl-5 text-[13px] text-muted">{c.missing.map((m, i) => <li key={i}>{m}</li>)}</ul>
            </Card>
          )}
        </div>
      )}
    </>
  )
}

function PersonSelect({ value, onChange, exclude }: { value: number; onChange: (n: number) => void; exclude: number }) {
  const { people } = useData()
  return (
    <select value={value} onChange={(e) => onChange(Number(e.target.value))} aria-label="Kişi seç"
      className="h-11 w-full rounded-2xl border border-line bg-raised px-3 text-[14px] font-medium outline-none focus:border-accent">
      {people.map((p) => <option key={p.id} value={p.id} disabled={p.id === exclude}>{p.name}{p.is_self ? ' (ben)' : ''}</option>)}
    </select>
  )
}

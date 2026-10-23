import { Search as SearchIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Badge, Card, Empty, ErrorNote, inputCls, PageHeader, Spinner, Tabs } from '@/components/ui'
import { api } from '@/lib/api'
import { useAsync } from '@/lib/data'
import { dateTime } from '@/lib/fmt'
import { go, href } from '@/lib/router'

function Snippet({ text }: { text: string }) {
  const parts = text.split(/(«[^»]*»)/g)
  return <>{parts.map((p, i) => p.startsWith('«') ? <mark key={i} className="rounded bg-accent-soft px-0.5 text-fg">{p.slice(1, -1)}</mark> : <span key={i}>{p}</span>)}</>
}

export default function SearchPage({ query }: { query: URLSearchParams }) {
  const initial = query.get('q') ?? ''
  const [q, setQ] = useState(initial)
  const [applied, setApplied] = useState(initial)
  useEffect(() => { const t = setTimeout(() => setApplied(q.trim()), 300); return () => clearTimeout(t) }, [q])
  useEffect(() => { go(applied ? `/ara?q=${encodeURIComponent(applied)}` : '/ara') }, [applied])
  const status = useAsync(() => api.embedStatus(), [])
  const semantic = !!status.data?.configured && (status.data.done ?? 0) > 0
  const [mode, setMode] = useState<'text' | 'semantic' | 'hybrid'>('text')
  const res = useAsync(() => (applied ? api.search(applied, mode) : Promise.resolve([])), [applied, mode])

  return (
    <>
      <PageHeader title="Arama" sub="Tüm kaynaklardaki mesajlarda arama. Sözcük araması Türkçe karakterlerden bağımsızdır (ş = s); anlam araması, gömme dizini kurulunca aynı anlama gelen farklı sözcükleri de bulur." />
      {semantic && (
        <div className="mb-3"><Tabs tabs={[{ id: 'text', label: 'Sözcük' }, { id: 'semantic', label: 'Anlam' }, { id: 'hybrid', label: 'Karma' }]} value={mode} onChange={setMode} /></div>
      )}
      <div className="relative mb-6">
        <SearchIcon className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-faint" />
        <input autoFocus className={`${inputCls} h-12 pl-10 text-[15px]`} placeholder="Mesajlarda ara…" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Arama" />
      </div>
      {res.loading && applied ? <Spinner /> : res.error ? <ErrorNote>{res.error}</ErrorNote> : !applied ? (
        <Empty icon={<SearchIcon className="size-8" />} title="Bir şey yaz">Bir sözcük ya da ifade ara; sonuçtan sohbete geçip bağlamını okuyabilirsin.</Empty>
      ) : (res.data?.length ?? 0) === 0 ? <Empty title="Sonuç yok">"{applied}" için eşleşen mesaj bulunamadı.</Empty> : (
        <div className="space-y-2">
          {res.data!.map((h) => (
            <a key={h.id} href={href(`/sohbet/${h.conv_id}?mesaj=${h.id}`)} className="block">
              <Card className="p-4 transition hover:border-accent/50">
                <div className="mb-1 flex items-center gap-2 text-[12.5px] text-muted"><Badge>{h.title}</Badge><span>{h.sender}</span>{h.score ? <span className="text-faint">yakınlık {h.score.toFixed(2).replace(".", ",")}</span> : null}<span className="ml-auto">{dateTime(h.ts)}</span></div>
                <p className="text-[14px]"><Snippet text={h.snippet} /></p>
              </Card>
            </a>
          ))}
        </div>
      )}
    </>
  )
}

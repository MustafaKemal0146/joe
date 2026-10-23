import { Command, Database, MessageCircle, MessagesSquare, Radar, GitCompareArrows, LayoutDashboard, Menu, Moon, Network, Search, Settings as Cog, Sparkles, Sun, Users, WifiOff, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { cn } from '@/lib/cn'
import { api } from '@/lib/api'
import { DataProvider, useData } from '@/lib/data'
import { go, href, useRoute } from '@/lib/router'
import { Avatar } from '@/components/ui'
import Home from '@/pages/Home'
import Sources from '@/pages/Sources'
import People from '@/pages/People'
import PersonPage from '@/pages/PersonPage'
import MapPage from '@/pages/MapPage'
import ComparePage from '@/pages/ComparePage'
import SearchPage from '@/pages/SearchPage'
import ConversationPage from '@/pages/ConversationPage'
import SettingsPage from '@/pages/SettingsPage'
import { ConfirmProvider, useConfirm } from '@/components/Confirm'
import JoeDock from '@/components/JoeDock'
import GuidePage from '@/pages/GuidePage'
import JoePage from '@/pages/JoePage'
import OsintPage from '@/pages/OsintPage'
import CouncilPage from '@/pages/CouncilPage'
import CouncilSessionPage from '@/pages/CouncilSessionPage'

type Theme = 'light' | 'dark' | 'system'

function useTheme() {
  const [theme, setTheme] = useState<Theme>(() => {
    try { return (localStorage.getItem('joe-theme') as Theme) || 'system' } catch { return 'system' }
  })
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const apply = () => document.documentElement.classList.toggle('dark', theme === 'dark' || (theme === 'system' && mq.matches))
    apply()
    mq.addEventListener('change', apply)
    try { localStorage.setItem('joe-theme', theme) } catch { /* özel pencere */ }
    return () => mq.removeEventListener('change', apply)
  }, [theme])
  return [theme, setTheme] as const
}

const NAV = [
  { group: 'Çalışma alanı', items: [
    { to: '/', label: 'Genel bakış', icon: LayoutDashboard },
    { to: '/joe', label: 'Joe ile konuş', icon: MessageCircle },
    { to: '/kisiler', label: 'Kişiler', icon: Users },
    { to: '/harita', label: 'İlişki haritası', icon: Network },
    { to: '/rehber', label: 'Sohbet rehberi', icon: MessagesSquare },
    { to: '/karsilastir', label: 'Karşılaştır', icon: GitCompareArrows },
  ] },
  { group: 'Veri', items: [
    { to: '/kaynaklar', label: 'Kaynaklar', icon: Database },
    { to: '/ara', label: 'Arama', icon: Search },
    { to: '/osint', label: 'OSINT', icon: Radar },
  ] },
]

function Sidebar({ route, onNavigate }: { route: string; onNavigate: () => void }) {
  const { people, sources, state, reload, toast } = useData()
  const confirm = useConfirm()
  const active = (to: string) => (to === '/' ? route === '/' : route === to || route.startsWith(to + '/') || (to === '/kisiler' && route.startsWith('/kisi/')))
  return (
    <nav className="flex h-full flex-col gap-5 px-3 py-4" aria-label="Ana menü">
      <a href={href('/')} onClick={onNavigate} className="px-2.5 text-[28px] font-bold leading-none tracking-[-0.045em]">
        joe
      </a>
      {NAV.map((g) => (
        <div key={g.group}>
          <div className="mb-1 px-2 text-[11px] font-medium uppercase tracking-wider text-faint">{g.group}</div>
          {g.items.map((it) => (
            <a key={it.to} href={href(it.to)} onClick={onNavigate} aria-current={active(it.to) ? 'page' : undefined}
              className={cn('flex items-center gap-2.5 rounded-xl px-2.5 py-2 text-[13.5px] font-medium transition', active(it.to) ? 'bg-surface text-fg shadow-sm' : 'text-muted hover:bg-surface/60 hover:text-fg')}>
              <it.icon className="size-4" />
              {it.label}
              {it.to === '/kisiler' && people.length > 0 && <span className="ml-auto text-[12px] text-faint">{people.length}</span>}
              {it.to === '/kaynaklar' && (state?.needs_owner.length ?? 0) > 0 && <span className="ml-auto size-2 rounded-full bg-warn" title="Sahibi doğrulanmamış kaynak var" />}
            </a>
          ))}
        </div>
      ))}
      <div>
        <div className="mb-1 px-2 text-[11px] font-medium uppercase tracking-wider text-faint">Konsey</div>
        <a href={href('/konsey')} onClick={onNavigate} aria-current={route.startsWith('/konsey') ? 'page' : undefined}
          className={cn('flex items-center gap-2.5 rounded-xl px-2.5 py-2 text-[13.5px] font-medium transition', route.startsWith('/konsey') ? 'bg-surface text-fg shadow-sm' : 'text-muted hover:bg-surface/60 hover:text-fg')}>
          <Sparkles className="size-4" /> 13 psikolog
        </a>
      </div>
      {people.length > 0 && sources.length > 0 && (
        <div className="min-h-0 flex-1 overflow-y-auto">
          <div className="mb-1 px-2 text-[11px] font-medium uppercase tracking-wider text-faint">Son kişiler</div>
          {people.slice(0, 6).map((p) => (
            <div key={p.id} className="group flex items-center rounded-xl hover:bg-surface/60">
              <a href={href(`/kisi/${p.id}`)} onClick={onNavigate} className="flex min-w-0 flex-1 items-center gap-2.5 px-2.5 py-1.5 text-[13px] text-muted group-hover:text-fg">
                <Avatar name={p.name} size={22} /> <span className="truncate">{p.name}</span>
              </a>
              <button
                aria-label={`${p.name} kişi dosyasını sil`}
                title="Kişi dosyasını sil"
                onClick={async () => {
                  const warn = p.is_self ? ' Bu "ben" dosyası; silersen karşılaştırmalar ve konsey için yeniden açman gerekir.' : ''
                  if (!await confirm({ title: `"${p.name}" dosyası silinsin mi?`, message: `Kaynak veriler (mesajlar) silinmez.${warn}`, confirmLabel: 'Dosyayı sil' })) return
                  try {
                    await api.deletePerson(p.id)
                    await reload()
                    toast('Kişi dosyası silindi.')
                    if (window.location.hash.startsWith(`#/kisi/${p.id}`)) go('/kisiler')
                  } catch (e) {
                    toast(e instanceof Error ? e.message : 'Silinemedi', 'bad')
                  }
                }}
                className="mr-1 grid size-6 shrink-0 place-items-center rounded-full text-bad/70 hover:bg-bad/15 hover:text-bad focus-visible:opacity-100 md:opacity-0 md:group-hover:opacity-100"
              >
                <X className="size-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}
      <div className="mt-auto">
        <a href={href('/ayarlar')} onClick={onNavigate} className={cn('flex items-center gap-2.5 rounded-xl px-2.5 py-2 text-[13.5px] font-medium transition', route === '/ayarlar' ? 'bg-surface text-fg shadow-sm' : 'text-muted hover:bg-surface/60 hover:text-fg')}>
          <Cog className="size-4" /> Ayarlar
        </a>
      </div>
    </nav>
  )
}

function Palette({ onClose }: { onClose: () => void }) {
  const { people } = useData()
  const [q, setQ] = useState('')
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => ref.current?.focus(), [])
  const items = useMemo(() => {
    const pages = [
      { label: 'Genel bakış', to: '/' }, { label: 'Joe ile konuş', to: '/joe' }, { label: 'Kişiler', to: '/kisiler' }, { label: 'İlişki haritası', to: '/harita' },
      { label: 'Karşılaştır', to: '/karsilastir' }, { label: 'Konsey', to: '/konsey' }, { label: 'Kaynaklar', to: '/kaynaklar' }, { label: 'Arama', to: '/ara' }, { label: 'OSINT', to: '/osint' }, { label: 'Ayarlar', to: '/ayarlar' },
    ]
    const ppl = people.map((p) => ({ label: p.name, to: `/kisi/${p.id}`, person: true }))
    const needle = q.toLocaleLowerCase('tr-TR')
    return [...pages, ...ppl].filter((i) => i.label.toLocaleLowerCase('tr-TR').includes(needle)).slice(0, 9)
  }, [q, people])
  const [i, setI] = useState(0)
  useEffect(() => setI(0), [q])
  const run = (to: string) => { go(to); onClose() }
  return (
    <div className="fixed inset-0 z-50 grid place-items-start justify-center bg-black/30 px-4 pt-[14vh] backdrop-blur-sm" onClick={onClose} role="dialog" aria-modal aria-label="Komut paleti">
      <div className="glass w-full max-w-lg overflow-hidden rounded-2xl border border-line shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <input ref={ref} value={q} onChange={(e) => setQ(e.target.value)} placeholder="Sayfa ya da kişi ara…"
          className="h-12 w-full border-b border-line bg-transparent px-4 text-[15px] outline-none"
          onKeyDown={(e) => {
            if (e.key === 'Escape') onClose()
            if (e.key === 'ArrowDown') { e.preventDefault(); setI((x) => Math.min(items.length - 1, x + 1)) }
            if (e.key === 'ArrowUp') { e.preventDefault(); setI((x) => Math.max(0, x - 1)) }
            if (e.key === 'Enter' && items[i]) run(items[i]!.to)
          }} />
        <ul className="max-h-72 overflow-y-auto p-1.5">
          {items.map((it, idx) => (
            <li key={it.to}>
              <button onClick={() => run(it.to)} onMouseEnter={() => setI(idx)} className={cn('flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-left text-[14px]', idx === i && 'bg-accent-soft')}>
                {'person' in it ? <Avatar name={it.label} size={20} /> : <Command className="size-4 text-faint" />}
                {it.label}
              </button>
            </li>
          ))}
          {items.length === 0 && <li className="px-3 py-4 text-[13px] text-muted">Sonuç yok</li>}
        </ul>
      </div>
    </div>
  )
}

function Shell() {
  const route = useRoute()
  const { toasts, dismiss, offline, loaded, state } = useData()
  const [theme, setTheme] = useTheme()
  const [menu, setMenu] = useState(false)
  const [palette, setPalette] = useState(false)
  const main = useRef<HTMLElement>(null)

  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette((p) => !p) }
    }
    window.addEventListener('keydown', on)
    return () => window.removeEventListener('keydown', on)
  }, [])
  useEffect(() => { main.current?.scrollTo({ top: 0 }) }, [route.path])

  const seg = route.segments
  let page: React.ReactNode
  switch (seg[0]) {
    case undefined: page = <Home />; break
    case 'kaynaklar': page = <Sources />; break
    case 'kisiler': page = <People />; break
    case 'kisi': page = <PersonPage id={Number(seg[1])} key={seg[1]} />; break
    case 'harita': page = <MapPage />; break
    case 'rehber': page = <GuidePage />; break
    case 'karsilastir': page = <ComparePage query={route.query} />; break
    case 'ara': page = <SearchPage query={route.query} />; break
    case 'sohbet': page = <ConversationPage id={Number(seg[1])} focus={Number(route.query.get('mesaj')) || 0} person={Number(route.query.get('kisi')) || 0} key={seg[1]} />; break
    case 'konsey': page = seg[1] ? <CouncilSessionPage id={seg[1]} key={seg[1]} /> : <CouncilPage query={route.query} />; break
    case 'osint': page = <OsintPage id={seg[1]} />; break
    case 'joe': page = <JoePage id={seg[1]} />; break
    case 'ayarlar': page = <SettingsPage />; break
    default: page = <div className="py-24 text-center text-muted">Sayfa bulunamadı.</div>
  }

  return (
    <div className="flex h-dvh">
      <aside className="hidden w-60 shrink-0 md:block"><Sidebar route={route.path} onNavigate={() => {}} /></aside>
      {menu && (
        <div className="fixed inset-0 z-40 md:hidden" onClick={() => setMenu(false)}>
          <div className="absolute inset-0 bg-black/30" />
          <aside className="absolute inset-y-0 left-0 w-64 bg-bg" onClick={(e) => e.stopPropagation()}><Sidebar route={route.path} onNavigate={() => setMenu(false)} /></aside>
        </div>
      )}
      <div className="flex min-w-0 flex-1 flex-col p-0 md:p-3 md:pl-0">
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-surface md:rounded-[22px] md:border md:border-line md:shadow-[0_1px_0_rgb(0_0_0/0.03),0_12px_40px_-12px_rgb(0_0_0/0.18)]">
          <header className="glass flex h-12 shrink-0 items-center gap-2 border-b border-line px-3 md:px-5">
            <button className="grid size-8 place-items-center rounded-full text-muted hover:bg-raised md:hidden" onClick={() => setMenu(true)} aria-label="Menüyü aç">{menu ? <X className="size-4" /> : <Menu className="size-4" />}</button>
            <button onClick={() => setPalette(true)} className="flex h-8 flex-1 items-center gap-2 rounded-full bg-raised px-3 text-left text-[13px] text-faint hover:bg-sunken md:max-w-xs">
              <Search className="size-3.5" /> Ara… <kbd className="ml-auto hidden rounded border border-line px-1.5 text-[11px] sm:block">Ctrl K</kbd>
            </button>
            <div className="ml-auto flex items-center gap-1">
              <button onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')} className="grid size-8 place-items-center rounded-full text-muted hover:bg-raised" aria-label="Temayı değiştir">
                {theme === 'dark' ? <Sun className="size-4" /> : <Moon className="size-4" />}
              </button>
            </div>
          </header>
          <main ref={main} className="min-h-0 flex-1 overflow-y-auto">
            <div className="rise mx-auto max-w-[1080px] px-4 py-6 md:px-8 md:py-8" key={route.path}>
              {offline && loaded ? (
                <div className="flex flex-col items-center py-24 text-center">
                  <WifiOff className="mb-3 size-8 text-faint" />
                  <p className="text-[15px] font-semibold">Joe sunucusuna ulaşılamıyor</p>
                  <p className="mt-1 text-[13px] text-muted">Uygulamayı terminalde <code className="rounded bg-raised px-1.5 py-0.5">joe</code> komutuyla başlat, sonra sayfayı yenile.</p>
                </div>
              ) : page}
            </div>
          </main>
          <footer className="flex h-8 shrink-0 items-center gap-3 border-t border-line px-4 text-[12px] text-muted">
            <span className="flex items-center gap-1.5"><i className={cn('size-1.5 rounded-full', offline ? 'bg-bad' : 'bg-ok')} />{offline ? 'Bağlantı yok' : 'Yerel'}</span>
            {state && <span className="truncate">{state.provider_ready ? `${state.provider} · ${state.model}` : 'Yapay zekâ sağlayıcısı bağlı değil'}</span>}
          </footer>
        </div>
      </div>
      {palette && <Palette onClose={() => setPalette(false)} />}
      {seg[0] !== 'joe' && !offline && <JoeDock />}
      <div className="pointer-events-none fixed bottom-28 right-4 z-50 flex flex-col gap-2" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={cn('rise pointer-events-auto flex max-w-sm items-start gap-3 rounded-2xl border px-4 py-3 text-[13px] shadow-xl glass', t.tone === 'bad' ? 'border-bad/40' : 'border-line')}>
            <span className={cn('mt-1 size-2 shrink-0 rounded-full', t.tone === 'bad' ? 'bg-bad' : 'bg-ok')} />
            <span className="flex-1">{t.text}</span>
            <button onClick={() => dismiss(t.id)} aria-label="Kapat" className="text-faint hover:text-fg"><X className="size-3.5" /></button>
          </div>
        ))}
      </div>
    </div>
  )
}

export default function App() {
  return <DataProvider><ConfirmProvider><Shell /></ConfirmProvider></DataProvider>
}

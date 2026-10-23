import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api } from './api'
import type { AppState, Person, Source } from './types'

interface Toast { id: number; text: string; tone: 'ok' | 'bad' }

interface Ctx {
  state: AppState | null
  people: Person[]
  sources: Source[]
  loaded: boolean
  offline: boolean
  reload: () => Promise<void>
  toast: (text: string, tone?: 'ok' | 'bad') => void
  toasts: Toast[]
  dismiss: (id: number) => void
}

const DataCtx = createContext<Ctx | null>(null)

export function DataProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AppState | null>(null)
  const [people, setPeople] = useState<Person[]>([])
  const [sources, setSources] = useState<Source[]>([])
  const [loaded, setLoaded] = useState(false)
  const [offline, setOffline] = useState(false)
  const [toasts, setToasts] = useState<Toast[]>([])
  const seq = useRef(0)

  const reload = useCallback(async () => {
    try {
      const [s, p, src] = await Promise.all([api.state(), api.people(), api.sources()])
      setState(s)
      setPeople(p)
      setSources(src)
      setOffline(false)
    } catch {
      setOffline(true)
    } finally {
      setLoaded(true)
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  const dismiss = useCallback((id: number) => setToasts((t) => t.filter((x) => x.id !== id)), [])
  const toast = useCallback(
    (text: string, tone: 'ok' | 'bad' = 'ok') => {
      const id = ++seq.current
      setToasts((t) => [...t, { id, text, tone }])
      setTimeout(() => dismiss(id), tone === 'bad' ? 7000 : 3500)
    },
    [dismiss],
  )

  const value = useMemo(
    () => ({ state, people, sources, loaded, offline, reload, toast, toasts, dismiss }),
    [state, people, sources, loaded, offline, reload, toast, toasts, dismiss],
  )
  return <DataCtx.Provider value={value}>{children}</DataCtx.Provider>
}

export function useData(): Ctx {
  const c = useContext(DataCtx)
  if (!c) throw new Error('DataProvider eksik')
  return c
}

/** Bir API çağrısını yükler; bağımlılıklar değişince yeniden çeker. */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    let live = true
    setLoading(true)
    setError(null)
    fn().then(
      (d) => {
        if (live) {
          setData(d)
          setLoading(false)
        }
      },
      (e: unknown) => {
        if (live) {
          setError(e instanceof Error ? e.message : String(e))
          setLoading(false)
        }
      },
    )
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)
  return { data, error, loading, setData }
}

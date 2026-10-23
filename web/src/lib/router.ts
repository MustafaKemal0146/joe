import { useEffect, useState } from 'react'

export interface Route { path: string; segments: string[]; query: URLSearchParams }

function parse(): Route {
  const raw = window.location.hash.replace(/^#/, '') || '/'
  const [path, q = ''] = raw.split('?')
  const p = path || '/'
  return { path: p, segments: p.split('/').filter(Boolean), query: new URLSearchParams(q) }
}

export function useRoute(): Route {
  const [route, setRoute] = useState(parse)
  useEffect(() => {
    const on = () => setRoute(parse())
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  return route
}

export const href = (path: string) => `#${path}`
export const go = (path: string) => {
  window.location.hash = path
}

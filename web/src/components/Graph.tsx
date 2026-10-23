import { useMemo, useState } from 'react'
import { layout } from '@/lib/force'
import { hue } from '@/lib/fmt'
import type { Graph as G } from '@/lib/types'

const W = 640
const H = 420

export function GraphView({ graph, selected, onSelect }: { graph: G; selected: string | null; onSelect: (id: string | null) => void }) {
  const [hover, setHover] = useState<string | null>(null)
  const pos = useMemo(() => {
    const maxS = Math.max(1, ...graph.nodes.map((n) => n.strength))
    const nodes = graph.nodes.map((n) => ({ id: n.id, r: n.is_owner ? 22 : 7 + 14 * Math.sqrt(n.strength / maxS) }))
    const l = layout(nodes, graph.edges.map((e) => ({ a: e.a, b: e.b, w: e.strength })), W, H)
    return new Map(l.map((p) => [p.id, p]))
  }, [graph])
  const byId = useMemo(() => new Map(graph.nodes.map((n) => [n.id, n])), [graph])
  const maxE = Math.max(1, ...graph.edges.map((e) => e.strength))
  const focus = hover ?? selected
  const near = new Set<string>()
  if (focus) {
    near.add(focus)
    for (const e of graph.edges) {
      if (e.a === focus) near.add(e.b)
      if (e.b === focus) near.add(e.a)
    }
  }

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="h-full w-full" role="img" aria-label="İlişki haritası" onClick={() => onSelect(null)}>
      {graph.edges.map((e) => {
        const a = pos.get(e.a)
        const b = pos.get(e.b)
        if (!a || !b) return null
        const dim = focus && !(e.a === focus || e.b === focus)
        return (
          <line key={e.a + e.b} x1={a.x} y1={a.y} x2={b.x} y2={b.y} stroke="var(--accent)"
            strokeOpacity={dim ? 0.05 : 0.18 + 0.5 * (e.strength / maxE)} strokeWidth={0.8 + 4 * (e.strength / maxE)} strokeLinecap="round" />
        )
      })}
      {graph.nodes.map((n) => {
        const p = pos.get(n.id)
        if (!p) return null
        const dim = focus && !near.has(n.id)
        const h = hue(n.label)
        const isSel = selected === n.id
        return (
          <g key={n.id} transform={`translate(${p.x},${p.y})`} opacity={dim ? 0.25 : 1} style={{ cursor: 'pointer', transition: 'opacity 150ms' }}
            onMouseEnter={() => setHover(n.id)} onMouseLeave={() => setHover(null)}
            onClick={(ev) => { ev.stopPropagation(); onSelect(isSel ? null : n.id) }}
            tabIndex={0} role="button" aria-label={`${n.label}${n.is_owner ? ' (hesap sahibi)' : ''}`}
            onKeyDown={(ev) => { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); onSelect(isSel ? null : n.id) } }}>
            <circle r={p.r + (isSel ? 4 : 0)} fill={n.is_owner ? 'var(--accent)' : `hsl(${h} 65% 52%)`} stroke={isSel ? 'var(--fg)' : 'var(--surface)'} strokeWidth={isSel ? 2.5 : 2} />
            {(p.r > 14 || focus === n.id) && (
              <text y={p.r + 14} textAnchor="middle" fontSize="11.5" fontWeight={n.is_owner ? 600 : 500} fill="var(--fg)" style={{ paintOrder: 'stroke', stroke: 'var(--surface)', strokeWidth: 3 }}>
                {byId.get(n.id)!.label.length > 22 ? byId.get(n.id)!.label.slice(0, 21) + '…' : byId.get(n.id)!.label}
              </text>
            )}
          </g>
        )
      })}
    </svg>
  )
}

/** Çok küçük bir kuvvet yönelimli yerleşim: ilişki haritası için, bağımlılıksız. */
export interface FNode { id: string; x: number; y: number; vx: number; vy: number; r: number; fixed?: boolean }
export interface FEdge { a: string; b: string; w: number }

/** Deterministik ilk dizilim (aynı veri her seferinde aynı haritayı verir). */
export function layout(nodes: { id: string; r: number }[], edges: FEdge[], width: number, height: number, iterations = 320): FNode[] {
  const n = nodes.length
  const cx = width / 2
  const cy = height / 2
  const out: FNode[] = nodes.map((nd, i) => {
    const angle = (i / Math.max(1, n)) * Math.PI * 2
    const rad = i === 0 ? 0 : Math.min(width, height) * 0.32
    return { id: nd.id, r: nd.r, x: cx + Math.cos(angle) * rad, y: cy + Math.sin(angle) * rad, vx: 0, vy: 0 }
  })
  const idx = new Map(out.map((o, i) => [o.id, i]))
  const wmax = Math.max(1, ...edges.map((e) => e.w))
  for (let it = 0; it < iterations; it++) {
    const cool = 1 - it / iterations
    for (let i = 0; i < n; i++) {
      for (let j = i + 1; j < n; j++) {
        const a = out[i]!
        const b = out[j]!
        let dx = a.x - b.x
        let dy = a.y - b.y
        let d2 = dx * dx + dy * dy
        if (d2 < 1) {
          dx = (i - j) * 0.5
          dy = 0.5
          d2 = dx * dx + dy * dy
        }
        const d = Math.sqrt(d2)
        const rep = (4200 + (a.r + b.r) * 80) / d2
        const fx = (dx / d) * rep
        const fy = (dy / d) * rep
        a.vx += fx; a.vy += fy; b.vx -= fx; b.vy -= fy
      }
    }
    for (const e of edges) {
      const a = out[idx.get(e.a) ?? -1]
      const b = out[idx.get(e.b) ?? -1]
      if (!a || !b) continue
      const dx = b.x - a.x
      const dy = b.y - a.y
      const d = Math.sqrt(dx * dx + dy * dy) || 1
      const target = 170 - 100 * (e.w / wmax)
      const k = 0.02 * (0.4 + e.w / wmax)
      const f = (d - target) * k
      a.vx += (dx / d) * f; a.vy += (dy / d) * f
      b.vx -= (dx / d) * f; b.vy -= (dy / d) * f
    }
    for (const o of out) {
      o.vx += (cx - o.x) * 0.004
      o.vy += (cy - o.y) * 0.004
      o.x += Math.max(-30, Math.min(30, o.vx)) * cool
      o.y += Math.max(-30, Math.min(30, o.vy)) * cool
      o.vx *= 0.6
      o.vy *= 0.6
      o.x = Math.max(o.r + 8, Math.min(width - o.r - 8, o.x))
      o.y = Math.max(o.r + 8, Math.min(height - o.r - 8, o.y))
    }
  }
  return out
}

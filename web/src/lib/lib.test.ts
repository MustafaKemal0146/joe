import { describe, expect, it } from 'vitest'
import { duration, hue, initials, month, num, usd } from './fmt'
import { layout } from './force'

describe('fmt', () => {
  it('süreyi okunur yazar', () => {
    expect(duration(45)).toBe('45 sn')
    expect(duration(600)).toBe('10 dk')
    expect(duration(7200)).toBe('2,0 sa')
    expect(duration(86400 * 3)).toBe('3,0 gün')
    expect(duration(0)).toBe('—')
  })
  it('ayı Türkçe yazar', () => {
    expect(month('2026-03')).toBe('Mart 2026')
    expect(month('bozuk')).toBe('bozuk')
  })
  it('baş harfleri Türkçe büyütür', () => {
    expect(initials('ışık yılmaz')).toBe('IY')
    expect(initials('')).toBe('?')
  })
  it('sayıyı Türkçe biçimler', () => {
    expect(num(1234567)).toBe('1.234.567')
  })
  it('para ve ton', () => {
    expect(usd(0.004)).toBe('<$0,01')
    expect(usd(1.5)).toBe('$1,50')
    expect(hue('Ayşe')).toBe(hue('Ayşe'))
  })
})

describe('force layout', () => {
  it('deterministik ve sınırlar içinde kalır', () => {
    const nodes = [{ id: 'a', r: 20 }, { id: 'b', r: 12 }, { id: 'c', r: 12 }]
    const edges = [{ a: 'a', b: 'b', w: 3 }, { a: 'a', b: 'c', w: 1 }]
    const l1 = layout(nodes, edges, 800, 500)
    const l2 = layout(nodes, edges, 800, 500)
    expect(l1).toEqual(l2)
    for (const n of l1) {
      expect(n.x).toBeGreaterThanOrEqual(0)
      expect(n.x).toBeLessThanOrEqual(800)
      expect(n.y).toBeGreaterThanOrEqual(0)
      expect(n.y).toBeLessThanOrEqual(500)
    }
  })
  it('güçlü bağlı düğümler birbirine daha yakın olur', () => {
    const nodes = [{ id: 'a', r: 16 }, { id: 'b', r: 16 }, { id: 'c', r: 16 }]
    const l = layout(nodes, [{ a: 'a', b: 'b', w: 10 }, { a: 'a', b: 'c', w: 1 }], 800, 500)
    const d = (p: string, q: string) => {
      const x = l.find((n) => n.id === p)!
      const y = l.find((n) => n.id === q)!
      return Math.hypot(x.x - y.x, x.y - y.y)
    }
    expect(d('a', 'b')).toBeLessThan(d('a', 'c'))
  })
})

const nf = new Intl.NumberFormat('tr-TR')
export const num = (n: number) => nf.format(Math.round(n))

export const pct = (v: number, digits = 0) => `%${(v * 100).toFixed(digits)}`

const df = new Intl.DateTimeFormat('tr-TR', { day: 'numeric', month: 'long', year: 'numeric' })
const dfShort = new Intl.DateTimeFormat('tr-TR', { day: 'numeric', month: 'short', year: 'numeric' })
const mf = new Intl.DateTimeFormat('tr-TR', { month: 'long', year: 'numeric' })
const tf = new Intl.DateTimeFormat('tr-TR', { hour: '2-digit', minute: '2-digit' })

const valid = (iso?: string) => !!iso && !iso.startsWith('0001')

export const date = (iso?: string) => (valid(iso) ? df.format(new Date(iso!)) : '—')
export const dateShort = (iso?: string) => (valid(iso) ? dfShort.format(new Date(iso!)) : '—')
export const dateTime = (iso?: string) => (valid(iso) ? `${dfShort.format(new Date(iso!))} ${tf.format(new Date(iso!))}` : '—')

/** "2026-03" → "Mart 2026" */
export const month = (m: string) => {
  const [y, mo] = m.split('-').map(Number)
  if (!y || !mo) return m
  return mf.format(new Date(y, mo - 1, 1))
}

/** Saniyeyi okunur süreye çevirir. */
export function duration(sec: number): string {
  if (!sec || sec < 0) return '—'
  if (sec < 90) return `${Math.round(sec)} sn`
  if (sec < 5400) return `${Math.round(sec / 60)} dk`
  if (sec < 86400 * 2) return `${(sec / 3600).toFixed(1).replace('.', ',')} sa`
  return `${(sec / 86400).toFixed(1).replace('.', ',')} gün`
}

export const hourLabel = (h: number) => `${String(h).padStart(2, '0')}:00`

export const usd = (v: number) => (v < 0.01 && v > 0 ? '<$0,01' : `$${v.toFixed(2).replace('.', ',')}`)

export const initials = (name: string) =>
  name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]!.toLocaleUpperCase('tr-TR'))
    .join('') || '?'

export const platformName = (p: string) => (p === 'instagram' ? 'Instagram' : p === 'whatsapp' ? 'WhatsApp' : p)

/** Ad'a göre sabit bir ton (avatar rengi). */
export function hue(name: string): number {
  let h = 0
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) % 360
  return h
}

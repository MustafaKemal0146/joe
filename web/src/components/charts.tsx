import { useState } from 'react'
import { hourLabel, month as monthLabel, num } from '@/lib/fmt'

const DAYS = ['Pzt', 'Sal', 'Çar', 'Per', 'Cum', 'Cmt', 'Paz']

/** Haftanın günü × saat ısı haritası. */
export function Heatmap({ heat }: { heat: number[][] }) {
  const [tip, setTip] = useState<string | null>(null)
  const max = Math.max(1, ...heat.flat())
  return (
    <div>
      <div className="grid gap-[3px]" style={{ gridTemplateColumns: '28px repeat(24, minmax(0, 1fr))' }}>
        <div />
        {Array.from({ length: 24 }, (_, h) => (
          <div key={h} className="text-center text-[10px] text-faint">{h % 3 === 0 ? h : ''}</div>
        ))}
        {heat.map((row, d) => (
          <Row key={d} day={DAYS[d]!} row={row} max={max} onTip={setTip} dayIndex={d} />
        ))}
      </div>
      <div className="mt-3 flex items-center justify-between text-[12px] text-muted">
        <span aria-live="polite">{tip ?? 'Bir hücrenin üstüne gel'}</span>
        <span className="flex items-center gap-1.5">az
          {[0.08, 0.3, 0.55, 0.8, 1].map((o) => <i key={o} className="size-3 rounded-[3px] bg-accent" style={{ opacity: o }} />)}
          çok</span>
      </div>
    </div>
  )
}

function Row({ day, row, max, onTip, dayIndex }: { day: string; row: number[]; max: number; onTip: (s: string | null) => void; dayIndex: number }) {
  return (
    <>
      <div className="flex items-center text-[11px] text-faint">{day}</div>
      {row.map((v, h) => (
        <div
          key={h}
          onMouseEnter={() => onTip(`${DAYS[dayIndex]} ${hourLabel(h)} — ${num(v)} mesaj`)}
          onMouseLeave={() => onTip(null)}
          className="aspect-square rounded-[4px] bg-accent"
          style={{ opacity: v === 0 ? 0.05 : 0.12 + 0.88 * (v / max) }}
        />
      ))}
    </>
  )
}

/** Basit dikey çubuklar (saat dağılımı, hafta günü). */
export function Bars({ values, labels, highlight, height = 120 }: { values: number[]; labels: string[]; highlight?: number[]; height?: number }) {
  const max = Math.max(1, ...values)
  return (
    <div>
      <div className="flex items-end gap-[3px]" style={{ height }}>
        {values.map((v, i) => (
          <div key={i} className="group relative flex-1" style={{ height: '100%' }}>
            <div
              title={`${labels[i]}: ${num(v)}`}
              className={`absolute bottom-0 w-full rounded-t-[4px] ${highlight?.includes(i) ? 'bg-warn' : 'bg-accent'}`}
              style={{ height: `${Math.max(2, (v / max) * 100)}%`, opacity: v === 0 ? 0.15 : 1 }}
            />
          </div>
        ))}
      </div>
      <div className="mt-1.5 flex gap-[3px]">
        {labels.map((l, i) => (
          <div key={i} className="flex-1 text-center text-[10px] text-faint">{labels.length > 12 ? (i % 3 === 0 ? l : '') : l}</div>
        ))}
      </div>
    </div>
  )
}

/** Aylık alan grafiği. */
export function Area({ points, height = 120 }: { points: { month: string; value: number }[]; height?: number }) {
  const [hover, setHover] = useState<number | null>(null)
  if (points.length < 2) return <p className="text-[13px] text-muted">Zaman serisi için en az iki ay veri gerekir.</p>
  const w = 600
  const max = Math.max(1, ...points.map((p) => p.value))
  const x = (i: number) => (i / (points.length - 1)) * w
  const y = (v: number) => height - 4 - (v / max) * (height - 12)
  const line = points.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p.value).toFixed(1)}`).join(' ')
  const area = `${line} L${w},${height} L0,${height} Z`
  const h = hover ?? points.length - 1
  return (
    <div>
      <svg viewBox={`0 0 ${w} ${height}`} className="w-full" preserveAspectRatio="none" style={{ height }}
        onMouseLeave={() => setHover(null)}
        onMouseMove={(e) => {
          const r = e.currentTarget.getBoundingClientRect()
          setHover(Math.max(0, Math.min(points.length - 1, Math.round(((e.clientX - r.left) / r.width) * (points.length - 1)))))
        }}>
        <defs>
          <linearGradient id="areaGrad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.35" />
            <stop offset="100%" stopColor="var(--accent)" stopOpacity="0" />
          </linearGradient>
        </defs>
        <path d={area} fill="url(#areaGrad)" />
        <path d={line} fill="none" stroke="var(--accent)" strokeWidth="2" vectorEffect="non-scaling-stroke" />
        <line x1={x(h)} x2={x(h)} y1="0" y2={height} stroke="var(--accent)" strokeOpacity="0.4" vectorEffect="non-scaling-stroke" />
      </svg>
      <div className="mt-1 flex justify-between text-[12px] text-muted">
        <span>{monthLabel(points[0]!.month)}</span>
        <span className="font-medium text-fg">{monthLabel(points[h]!.month)} · {num(points[h]!.value)}</span>
        <span>{monthLabel(points[points.length - 1]!.month)}</span>
      </div>
    </div>
  )
}

/** İki tarafı karşılaştıran yatay çubuk: sol = özne, sağ = karşı taraf. */
export function Split({ label, left, right, leftName, rightName, format = num }: {
  label: string; left: number; right: number; leftName: string; rightName: string; format?: (n: number) => string
}) {
  const total = left + right
  const l = total === 0 ? 0.5 : left / total
  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between text-[12.5px]">
        <span className="font-medium tabular-nums">{format(left)}</span>
        <span className="text-muted">{label}</span>
        <span className="font-medium tabular-nums">{format(right)}</span>
      </div>
      <div className="flex h-2 overflow-hidden rounded-full bg-raised" role="img" aria-label={`${label}: ${leftName} ${format(left)}, ${rightName} ${format(right)}`}>
        <div className="bg-accent" style={{ width: `${l * 100}%`, transition: 'width 500ms cubic-bezier(.2,0,0,1)' }} />
        <div className="bg-ig" style={{ width: `${(1 - l) * 100}%` }} />
      </div>
    </div>
  )
}

/** Sıralı yatay çubuk listesi (en sık aranan hesaplar, sözcükler...). */
export function BarList({ items, empty = 'Veri yok' }: { items: { key: string; count: number; sub?: string }[]; empty?: string }) {
  if (!items.length) return <p className="text-[13px] text-muted">{empty}</p>
  const max = Math.max(1, ...items.map((i) => i.count))
  return (
    <ul className="space-y-2">
      {items.map((i) => (
        <li key={i.key}>
          <div className="mb-0.5 flex justify-between gap-3 text-[13px]">
            <span className="truncate font-medium">{i.key}</span>
            <span className="shrink-0 text-muted tabular-nums">{num(i.count)}{i.sub && <span className="ml-2 text-faint">{i.sub}</span>}</span>
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-raised">
            <div className="h-full rounded-full bg-accent" style={{ width: `${(i.count / max) * 100}%` }} />
          </div>
        </li>
      ))}
    </ul>
  )
}

/** Dairesel gösterge (0–1). */
export function Ring({ value, size = 132, label }: { value: number; size?: number; label: string }) {
  const r = (size - 14) / 2
  const c = 2 * Math.PI * r
  return (
    <div className="relative grid place-items-center" style={{ width: size, height: size }} role="img" aria-label={`${label}: yüzde ${Math.round(value * 100)}`}>
      <svg width={size} height={size} className="-rotate-90">
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--raised)" strokeWidth="9" />
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--accent)" strokeWidth="9" strokeLinecap="round"
          strokeDasharray={c} strokeDashoffset={c * (1 - value)} style={{ transition: 'stroke-dashoffset 900ms cubic-bezier(.2,0,0,1)' }} />
      </svg>
      <div className="absolute text-center">
        <div className="text-[30px] font-semibold leading-none tabular-nums">{Math.round(value * 100)}</div>
        <div className="mt-0.5 text-[11px] text-muted">/ 100</div>
      </div>
    </div>
  )
}

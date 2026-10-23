import { Loader2, X } from 'lucide-react'
import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { hue, initials, platformName } from '@/lib/fmt'

export function Button({
  variant = 'default', size = 'md', busy, className, children, ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'default' | 'primary' | 'ghost' | 'danger'; size?: 'sm' | 'md'; busy?: boolean }) {
  const v = {
    default: 'bg-raised hover:bg-sunken border border-line text-fg',
    primary: 'bg-accent text-accent-fg hover:brightness-110 border border-transparent',
    ghost: 'hover:bg-raised text-muted hover:text-fg border border-transparent',
    danger: 'bg-transparent text-bad hover:bg-bad/10 border border-bad/30',
  }[variant]
  const s = size === 'sm' ? 'h-8 px-3 text-[13px]' : 'h-9 px-4 text-sm'
  return (
    <button
      {...rest}
      disabled={rest.disabled || busy}
      className={cn('inline-flex items-center justify-center gap-2 rounded-full font-medium transition disabled:opacity-50 disabled:cursor-not-allowed', v, s, className)}
    >
      {busy && <Loader2 className="spin size-4" />}
      {children}
    </button>
  )
}

export function Card({ className, children, ...rest }: { className?: string; children: ReactNode } & React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div {...rest} className={cn('rounded-[18px] border border-line bg-surface', className)}>
      {children}
    </div>
  )
}

export function Panel({ title, hint, action, children, className }: { title: string; hint?: string; action?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <Card className={cn('p-5', className)}>
      <div className="mb-4 flex items-start justify-between gap-3">
        <div>
          <h3 className="text-[15px] font-semibold tracking-tight">{title}</h3>
          {hint && <p className="mt-0.5 text-[13px] text-muted">{hint}</p>}
        </div>
        {action}
      </div>
      {children}
    </Card>
  )
}

export function Stat({ label, value, sub }: { label: string; value: ReactNode; sub?: ReactNode }) {
  return (
    <Card className="p-4">
      <div className="text-[12px] font-medium uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-1 text-[26px] font-semibold leading-none tracking-tight tabular-nums">{value}</div>
      {sub && <div className="mt-1.5 text-[12px] text-muted">{sub}</div>}
    </Card>
  )
}

export function Badge({ children, tone = 'neutral', className }: { children: ReactNode; tone?: 'neutral' | 'accent' | 'ok' | 'warn' | 'bad' | 'ig' | 'wa'; className?: string }) {
  const t = {
    neutral: 'bg-raised text-muted',
    accent: 'bg-accent-soft text-accent',
    ok: 'bg-ok/12 text-ok',
    warn: 'bg-warn/14 text-warn',
    bad: 'bg-bad/12 text-bad',
    ig: 'bg-ig/12 text-ig',
    wa: 'bg-wa/12 text-wa',
  }[tone]
  return <span className={cn('inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[12px] font-medium', t, className)}>{children}</span>
}

export const PlatformBadge = ({ platform }: { platform: string }) => (
  <Badge tone={platform === 'instagram' ? 'ig' : platform === 'whatsapp' ? 'wa' : 'neutral'}>{platformName(platform)}</Badge>
)

export function Avatar({ name, size = 40 }: { name: string; size?: number }) {
  const h = hue(name)
  return (
    <div
      aria-hidden
      className="grid shrink-0 place-items-center rounded-full font-semibold text-white"
      style={{ width: size, height: size, fontSize: size * 0.38, background: `linear-gradient(135deg, hsl(${h} 70% 55%), hsl(${(h + 40) % 360} 70% 42%))` }}
    >
      {initials(name)}
    </div>
  )
}

export function Empty({ icon, title, children, action }: { icon?: ReactNode; title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center rounded-[18px] border border-dashed border-line px-6 py-14 text-center">
      {icon && <div className="mb-3 text-faint">{icon}</div>}
      <div className="text-[15px] font-semibold">{title}</div>
      {children && <p className="mt-1 max-w-md text-[13px] text-muted">{children}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex items-center justify-center gap-2 py-16 text-muted">
      <Loader2 className="spin size-4" />
      {label && <span className="text-[13px]">{label}</span>}
    </div>
  )
}

export function ErrorNote({ children }: { children: ReactNode }) {
  return <div role="alert" className="rounded-xl border border-bad/30 bg-bad/8 px-4 py-3 text-[13px] text-bad">{children}</div>
}

export function Tabs<T extends string>({ tabs, value, onChange }: { tabs: { id: T; label: string }[]; value: T; onChange: (t: T) => void }) {
  return (
    <div role="tablist" className="inline-flex gap-1 rounded-full bg-raised p-1">
      {tabs.map((t) => (
        <button
          key={t.id}
          role="tab"
          aria-selected={value === t.id}
          onClick={() => onChange(t.id)}
          className={cn('rounded-full px-3.5 py-1.5 text-[13px] font-medium transition', value === t.id ? 'bg-surface text-fg shadow-sm' : 'text-muted hover:text-fg')}
        >
          {t.label}
        </button>
      ))}
    </div>
  )
}

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[13px] font-medium">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-[12px] text-muted">{hint}</span>}
    </label>
  )
}

export const inputCls =
  'h-10 w-full rounded-xl border border-line bg-raised px-3 text-sm outline-none transition placeholder:text-faint focus:border-accent focus:bg-surface'

export function Chip({ children, onRemove }: { children: ReactNode; onRemove?: () => void }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-raised px-2.5 py-1 text-[12.5px]">
      {children}
      {onRemove && (
        <button aria-label="Kaldır" onClick={onRemove} className="text-faint hover:text-bad">
          <X className="size-3" />
        </button>
      )}
    </span>
  )
}

export function PageHeader({ title, sub, action }: { title: string; sub?: ReactNode; action?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="text-[28px] font-semibold leading-tight tracking-tight">{title}</h1>
        {sub && <p className="mt-1 text-[14px] text-muted">{sub}</p>}
      </div>
      {action}
    </div>
  )
}

export function Meter({ value, tone = 'accent' }: { value: number; tone?: 'accent' | 'ok' | 'warn' }) {
  const v = Math.max(0, Math.min(1, value))
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-raised">
      <div className={cn('h-full rounded-full', tone === 'accent' ? 'bg-accent' : tone === 'ok' ? 'bg-ok' : 'bg-warn')} style={{ width: `${v * 100}%`, transition: 'width 600ms cubic-bezier(.2,0,0,1)' }} />
    </div>
  )
}

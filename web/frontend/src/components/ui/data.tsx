import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

/* ------------------------------------------------------------------ page */

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string
  description?: string
  actions?: React.ReactNode
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3 mb-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="text-sm text-muted-foreground mt-0.5">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  )
}

/* ------------------------------------------------------------------ stat */

export function Stat({
  label,
  value,
  hint,
  icon: Icon,
  tone = 'default',
}: {
  label: string
  value: React.ReactNode
  hint?: string
  icon?: LucideIcon
  tone?: 'default' | 'success' | 'danger' | 'info'
}) {
  const iconTone = {
    default: 'bg-subtle text-muted-foreground',
    success: 'bg-success-soft text-success',
    danger: 'bg-danger-soft text-danger',
    info: 'bg-info-soft text-info',
  }[tone]

  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {label}
          </p>
          <p className="tabular mt-2 text-2xl font-semibold leading-none">{value}</p>
          {hint && <p className="text-xs text-muted-foreground mt-1.5 truncate">{hint}</p>}
        </div>
        {Icon && (
          <span className={cn('shrink-0 rounded-md p-2', iconTone)} aria-hidden>
            <Icon className="h-4 w-4" />
          </span>
        )}
      </div>
    </div>
  )
}

/* ----------------------------------------------------------------- table */

export function Table({ children, className }: { children: React.ReactNode; className?: string }) {
  // The wrapper scrolls, not the page: a wide table must never make the whole
  // layout scroll sideways on a narrow screen.
  return (
    <div className="overflow-x-auto">
      <table className={cn('w-full text-sm border-collapse', className)}>{children}</table>
    </div>
  )
}

export function Th({
  children,
  className,
  align = 'left',
}: {
  children?: React.ReactNode
  className?: string
  align?: 'left' | 'right' | 'center'
}) {
  return (
    <th
      scope="col"
      className={cn(
        'sticky top-0 z-10 bg-subtle px-3 py-2 text-xs font-medium uppercase tracking-wide text-muted-foreground border-b border-border',
        align === 'right' && 'text-right',
        align === 'center' && 'text-center',
        align === 'left' && 'text-left',
        className,
      )}
    >
      {children}
    </th>
  )
}

export function Td({
  children,
  className,
  align = 'left',
  mono = false,
}: {
  children?: React.ReactNode
  className?: string
  align?: 'left' | 'right' | 'center'
  mono?: boolean
}) {
  return (
    <td
      className={cn(
        'px-3 py-2.5 border-b border-border align-middle',
        align === 'right' && 'text-right',
        align === 'center' && 'text-center',
        mono && 'font-mono text-xs',
        className,
      )}
    >
      {children}
    </td>
  )
}

export function Tr({
  children,
  className,
  onClick,
}: {
  children: React.ReactNode
  className?: string
  onClick?: () => void
}) {
  return (
    <tr
      onClick={onClick}
      className={cn(
        'transition-colors hover:bg-subtle',
        onClick && 'cursor-pointer',
        className,
      )}
    >
      {children}
    </tr>
  )
}

/* ----------------------------------------------------------- empty state */

export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
}: {
  icon?: LucideIcon
  title: string
  description?: string
  action?: React.ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center text-center px-6 py-12">
      {Icon && (
        <span className="rounded-full bg-subtle p-3 mb-3" aria-hidden>
          <Icon className="h-6 w-6 text-muted-foreground" />
        </span>
      )}
      <p className="font-medium">{title}</p>
      {description && (
        <p className="text-sm text-muted-foreground mt-1 max-w-sm">{description}</p>
      )}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

/* ------------------------------------------------------------- skeleton */

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn('animate-pulse rounded bg-subtle', className)} />
}

/** Placeholder that reserves the table's height, so loading does not shift it. */
export function TableSkeleton({ rows = 4, cols = 4 }: { rows?: number; cols?: number }) {
  return (
    <div className="p-3 space-y-2" aria-hidden>
      {Array.from({ length: rows }).map((_, r) => (
        <div key={r} className="flex gap-3">
          {Array.from({ length: cols }).map((_, c) => (
            <Skeleton key={c} className={cn('h-5 flex-1', c === 0 && 'max-w-[36%]')} />
          ))}
        </div>
      ))}
    </div>
  )
}

/* ------------------------------------------------------------- progress */

export function Progress({ value, label }: { value: number; label?: string }) {
  const pct = Math.max(0, Math.min(100, value))
  return (
    <div>
      <div className="flex items-center justify-between text-xs mb-1">
        <span className="text-muted-foreground">{label ?? 'Progress'}</span>
        <span className="tabular font-medium">{pct.toFixed(1)}%</span>
      </div>
      <div
        className="h-2 w-full overflow-hidden rounded-full bg-subtle"
        role="progressbar"
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
      >
        <div
          className="h-full rounded-full bg-primary transition-[width] duration-300"
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  )
}

/* ---------------------------------------------------------------- utils */

export function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function formatDateTime(value?: string | null): string {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString(undefined, {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  })
}

/** Compact duration between two timestamps, for "how long did this take". */
export function formatDuration(from?: string | null, to?: string | null): string {
  if (!from) return '—'
  const start = new Date(from).getTime()
  const end = to ? new Date(to).getTime() : Date.now()
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return '—'
  const s = Math.floor((end - start) / 1000)
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
}

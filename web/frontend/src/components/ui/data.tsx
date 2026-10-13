import { CircleAlert, CircleCheck, Info, TriangleAlert, type LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

/* ------------------------------------------------------------------ page */

/**
 * Every page sits in the same column, so the title does not jump sideways when
 * moving between a wide table and a narrow form. A form page narrows its body,
 * not the column: `narrow` keeps a readable line length under a header that
 * stays put.
 */
export function Page({
  title,
  description,
  actions,
  narrow = false,
  children,
}: {
  title: string
  description?: string
  actions?: React.ReactNode
  narrow?: boolean
  children: React.ReactNode
}) {
  return (
    <div className="mx-auto w-full max-w-6xl">
      <PageHeader title={title} description={description} actions={actions} />
      <div className={cn('space-y-4', narrow && 'max-w-3xl')}>{children}</div>
    </div>
  )
}

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
    <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  )
}

/* --------------------------------------------------------------- section */

/** The one card every page is built from: a titled header over a body. */
export function Section({
  title,
  description,
  aside,
  flush = false,
  className,
  children,
}: {
  title?: React.ReactNode
  description?: React.ReactNode
  aside?: React.ReactNode
  /** No body padding, for a table or list that runs edge to edge. */
  flush?: boolean
  className?: string
  children: React.ReactNode
}) {
  return (
    <section className={cn('rounded-lg border border-border bg-card', className)}>
      {(title || aside) && (
        <header className="flex min-h-12 flex-wrap items-center justify-between gap-x-3 gap-y-2 border-b border-border px-4 py-2.5">
          <div className="min-w-0">
            {title && <h2 className="text-sm font-semibold">{title}</h2>}
            {description && <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>}
          </div>
          {aside && <div className="flex flex-wrap items-center gap-2">{aside}</div>}
        </header>
      )}
      <div className={cn(!flush && 'p-4')}>{children}</div>
    </section>
  )
}

/* ----------------------------------------------------------------- alert */

const alertTone = {
  danger: { box: 'bg-danger-soft text-danger', icon: CircleAlert },
  warning: { box: 'bg-warning-soft text-warning', icon: TriangleAlert },
  success: { box: 'bg-success-soft text-success', icon: CircleCheck },
  info: { box: 'bg-info-soft text-info', icon: Info },
}

/** Inline message. The icon carries the tone too, so it survives greyscale. */
export function Alert({
  tone = 'danger',
  className,
  children,
}: {
  tone?: keyof typeof alertTone
  className?: string
  children: React.ReactNode
}) {
  const { box, icon: Icon } = alertTone[tone]
  return (
    <div
      // Only failures interrupt a screen reader; the rest wait their turn.
      role={tone === 'danger' ? 'alert' : 'status'}
      className={cn('flex items-start gap-2 rounded-md px-3 py-2 text-sm', box, className)}
    >
      <Icon className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}

/* ------------------------------------------------------------------ chip */

/** A toggle in a small closed set: status filters, presets. */
export function Chip({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        'inline-flex h-8 cursor-pointer items-center rounded-full border px-3 text-xs font-medium transition-colors',
        active
          ? 'border-primary bg-primary-soft text-primary'
          : 'border-border bg-card text-muted-foreground hover:border-border-strong hover:text-foreground'
      )}
    >
      {children}
    </button>
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
        className
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
        className
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
      className={cn('transition-colors hover:bg-subtle', onClick && 'cursor-pointer', className)}
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
      {description && <p className="text-sm text-muted-foreground mt-1 max-w-sm">{description}</p>}
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

/** Compact duration between two timestamps, for "how long did this take". */

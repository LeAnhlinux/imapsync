import { cn } from '@/lib/utils'

type Tone = 'success' | 'warning' | 'danger' | 'info' | 'neutral'

const toneClass: Record<Tone, string> = {
  success: 'bg-success-soft text-success',
  warning: 'bg-warning-soft text-warning',
  danger: 'bg-danger-soft text-danger',
  info: 'bg-info-soft text-info',
  neutral: 'bg-neutral-soft text-neutral',
}

export function Badge({
  tone = 'neutral',
  dot = false,
  pulse = false,
  className,
  children,
}: {
  tone?: Tone
  dot?: boolean
  pulse?: boolean
  className?: string
  children: React.ReactNode
}) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap',
        toneClass[tone],
        className,
      )}
    >
      {dot && (
        <span
          aria-hidden
          className={cn('h-1.5 w-1.5 rounded-full bg-current', pulse && 'animate-pulse-dot')}
        />
      )}
      {children}
    </span>
  )
}

/** Every status a SyncJob can be in, mapped to a tone and a human label. */
const jobStatus: Record<string, { tone: Tone; label: string }> = {
  running: { tone: 'info', label: 'Running' },
  completed: { tone: 'success', label: 'Completed' },
  failed: { tone: 'danger', label: 'Failed' },
  aborted: { tone: 'warning', label: 'Aborted' },
  interrupted: { tone: 'warning', label: 'Interrupted' },
  pending: { tone: 'neutral', label: 'Pending' },
}

// The label carries the meaning; the colour only reinforces it, so the badge
// still reads correctly in greyscale or to a colour-blind user.
export function StatusBadge({ status, className }: { status: string; className?: string }) {
  const s = jobStatus[status] ?? { tone: 'neutral' as Tone, label: status }
  return (
    <Badge tone={s.tone} dot pulse={status === 'running'} className={className}>
      {s.label}
    </Badge>
  )
}

import { useId, type ReactNode } from 'react'
import { useDialog } from '@/lib/dialog'
import { cn } from '@/lib/utils'

// Dialog is the modal every confirmation and viewer in this app uses.
//
// It exists because those were hand-rolled in each page and each one was
// missing the same things: Escape did nothing, focus never entered the dialog
// and never came back, and Tab walked out into the page behind the overlay —
// where the controls are visually covered but still clickable. Putting it in
// one place means a new dialog gets that behaviour for free rather than
// forgetting it again.
export function Dialog({
  title,
  onClose,
  children,
  footer,
  size = 'sm',
  labelledBy,
}: {
  title?: ReactNode
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  size?: 'sm' | 'lg'
  labelledBy?: string
}) {
  const ref = useDialog(true, onClose)
  const headingId = useId()

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <button className="absolute inset-0 bg-black/60" onClick={onClose} aria-label="Cancel" />
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy ?? (title ? headingId : undefined)}
        className={cn(
          'relative w-full rounded-lg border border-border bg-card shadow-xl',
          size === 'lg' ? 'flex max-h-[85vh] max-w-4xl flex-col' : 'max-w-sm p-5'
        )}
      >
        {title && size === 'sm' && (
          <h2 id={headingId} className="text-sm font-semibold">
            {title}
          </h2>
        )}
        {children}
        {footer && <div className="mt-4 flex justify-end gap-2">{footer}</div>}
      </div>
    </div>
  )
}

import { useEffect, useRef } from 'react'

// useDialog gives a modal the three behaviours it needs to be usable without a
// mouse, and which none of the dialogs here had:
//
//   - Escape closes it. Every other app does this, so people try it.
//   - Focus moves into the dialog on open and back to the trigger on close.
//     Without that, a keyboard or screen-reader user is left at the top of the
//     page behind the overlay with no idea anything happened.
//   - Tab cycles inside the dialog. This was not a focus *trap* — it was the
//     opposite: Tab walked straight out into the page behind the overlay, where
//     the controls are visually covered but still reachable.
//
// Returns the ref to put on the dialog element.
export function useDialog(open: boolean, onClose: () => void) {
  const ref = useRef<HTMLDivElement>(null)
  const restoreTo = useRef<HTMLElement | null>(null)

  useEffect(() => {
    if (!open) return
    restoreTo.current = document.activeElement as HTMLElement | null

    const node = ref.current
    const focusable = () =>
      Array.from(
        node?.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
        ) ?? []
      ).filter((el) => el.offsetParent !== null)

    // Prefer the first control that is not the close button, so the dialog
    // does not open with "dismiss" selected.
    const first = focusable()
    ;(first.find((el) => el.getAttribute('aria-label') !== 'Close') ?? first[0])?.focus()

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onClose()
        return
      }
      if (e.key !== 'Tab') return
      const items = focusable()
      if (items.length === 0) return
      const firstItem = items[0]
      const lastItem = items[items.length - 1]
      if (e.shiftKey && document.activeElement === firstItem) {
        e.preventDefault()
        lastItem.focus()
      } else if (!e.shiftKey && document.activeElement === lastItem) {
        e.preventDefault()
        firstItem.focus()
      }
    }

    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      restoreTo.current?.focus()
    }
  }, [open, onClose])

  return ref
}

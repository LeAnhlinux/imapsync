import { forwardRef, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'
import { controlClass } from './control'

export function Field({
  label,
  hint,
  error,
  htmlFor,
  optional = false,
  className,
  children,
}: {
  label: string
  hint?: React.ReactNode
  error?: string
  htmlFor?: string
  optional?: boolean
  className?: string
  children: React.ReactNode
}) {
  return (
    <div className={className}>
      <label className="mb-1.5 block text-sm font-medium" htmlFor={htmlFor}>
        {label}
        {optional && <span className="font-normal text-muted-foreground"> (optional)</span>}
      </label>
      {children}
      {error ? (
        <p role="alert" className="mt-1.5 text-xs text-danger">
          {error}
        </p>
      ) : (
        hint && <p className="mt-1.5 text-xs text-muted-foreground">{hint}</p>
      )}
    </div>
  )
}

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(
  ({ className, ...props }, ref) => (
    <select ref={ref} className={cn(controlClass, 'h-10 cursor-pointer', className)} {...props} />
  )
)
Select.displayName = 'Select'

export const Textarea = forwardRef<
  HTMLTextAreaElement,
  TextareaHTMLAttributes<HTMLTextAreaElement>
>(({ className, ...props }, ref) => (
  <textarea ref={ref} className={cn(controlClass, 'py-2 font-mono', className)} {...props} />
))
Textarea.displayName = 'Textarea'

/** A checkbox with its label and, optionally, a line saying what it does. */
export function Checkbox({
  label,
  hint,
  className,
  ...props
}: Omit<React.InputHTMLAttributes<HTMLInputElement>, 'type'> & {
  label: React.ReactNode
  hint?: React.ReactNode
}) {
  return (
    <label
      className={cn(
        'flex items-start gap-2.5 text-sm',
        props.disabled ? 'cursor-not-allowed' : 'cursor-pointer',
        className
      )}
    >
      <input
        type="checkbox"
        className="mt-0.5 h-4 w-4 shrink-0 cursor-[inherit] accent-primary"
        {...props}
      />
      <span className={cn(props.disabled && 'text-muted-foreground')}>
        {label}
        {hint && <span className="block text-xs text-muted-foreground">{hint}</span>}
      </span>
    </label>
  )
}

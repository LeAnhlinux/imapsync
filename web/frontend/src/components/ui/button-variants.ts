import { cva } from 'class-variance-authority'

// Kept out of button.tsx so that file exports only components: a module that
// mixes the two cannot be hot-replaced, which is what react-refresh flags.
export const buttonVariants = cva(
  'inline-flex cursor-pointer items-center justify-center whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary/90',
        // The action the screen exists for. One per screen, at most.
        accent: 'bg-brand-accent text-brand-accent-foreground shadow-sm hover:bg-brand-accent/90',
        destructive: 'bg-destructive text-destructive-foreground hover:bg-destructive/90',
        outline: 'border border-border bg-card hover:bg-accent hover:text-accent-foreground',
        // A destructive action that is one of several in a row: it reads as
        // dangerous without a solid red block on every list item.
        danger: 'border border-border bg-card text-danger hover:border-danger/40 hover:bg-danger-soft',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
        ghost: 'hover:bg-accent hover:text-accent-foreground',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-10 px-4 py-2',
        sm: 'h-9 rounded-md px-3',
        lg: 'h-11 rounded-md px-8',
        icon: 'h-10 w-10',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  }
)

/** The quiet "see more" link in a section header, sized to be tappable. */
export const sectionLink =
  '-mr-2 inline-flex h-8 items-center gap-1 rounded-md px-2 text-xs font-medium text-primary transition-colors hover:bg-primary-soft'

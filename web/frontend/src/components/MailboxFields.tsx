import { Input } from '@/components/ui/input'
import { Checkbox, Field } from '@/components/ui/form'

export type MailboxValue = { host: string; user: string; password: string; ssl: boolean }

/**
 * One side of a sync: host, username, password, TLS. New Sync and Profiles
 * both ask for exactly this, twice, and used to each draw their own.
 */
export function MailboxFields({
  which,
  value,
  onChange,
  passwordPlaceholder,
  passwordRequired = true,
  disabled = false,
}: {
  which: 1 | 2
  value: MailboxValue
  onChange: (patch: Partial<MailboxValue>) => void
  passwordPlaceholder?: string
  passwordRequired?: boolean
  /** Read-only, when a saved profile supplies the values. */
  disabled?: boolean
}) {
  const id = (name: string) => `${name}${which}`
  return (
    <fieldset className="min-w-0 rounded-lg border border-border bg-subtle/50 p-4">
      <legend className="-ml-1 px-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {which === 1 ? 'Source — copied from' : 'Destination — copied to'}
      </legend>
      <div className="space-y-3">
        <Field label="Host" htmlFor={id('host')}>
          <Input
            id={id('host')}
            value={value.host}
            onChange={(e) => onChange({ host: e.target.value })}
            placeholder={which === 1 ? 'imap.gmail.com' : 'mail.example.com'}
            required
            disabled={disabled}
            autoCapitalize="none"
            spellCheck={false}
          />
        </Field>
        <Field label="Username" htmlFor={id('user')}>
          <Input
            id={id('user')}
            value={value.user}
            onChange={(e) => onChange({ user: e.target.value })}
            placeholder="me@example.com"
            required
            disabled={disabled}
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
          />
        </Field>
        <Field label="Password" htmlFor={id('password')}>
          <Input
            id={id('password')}
            type="password"
            value={value.password}
            onChange={(e) => onChange({ password: e.target.value })}
            placeholder={passwordPlaceholder}
            required={passwordRequired}
            disabled={disabled}
            autoComplete="new-password"
          />
        </Field>
        <Checkbox
          label="Use SSL/TLS"
          checked={value.ssl}
          disabled={disabled}
          onChange={(e) => onChange({ ssl: e.target.checked })}
        />
      </div>
    </fieldset>
  )
}

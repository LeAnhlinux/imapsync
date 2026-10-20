import { useEffect, useState } from 'react'
import { apiKeyApi, webhookApi, type ApiKey, type Webhook } from '@/lib/api'
import { Check, Copy, KeyRound, Plus, Webhook as WebhookIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Alert, EmptyState, Page, Section, Table, Td, Th, Tr } from '@/components/ui/data'
import { Checkbox, Field } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { formatDateTime } from '@/lib/format'

const code = 'rounded bg-subtle px-1 py-0.5 font-mono text-[0.92em] text-foreground'

function errorText(err: unknown, fallback: string) {
  return (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || fallback
}

export function ApiKeysPage() {
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [scopes, setScopes] = useState<string[]>([])
  const [hooks, setHooks] = useState<Webhook[]>([])
  const [events, setEvents] = useState<string[]>([])

  const [name, setName] = useState('')
  const [chosen, setChosen] = useState<string[]>(['sync:read', 'sync:write'])
  const [rateLimit, setRateLimit] = useState(60)
  const [expiresIn, setExpiresIn] = useState(0)
  const [newKey, setNewKey] = useState<string | null>(null)
  const [keyError, setKeyError] = useState('')
  // Editing changes what an existing key may do without issuing a new one, so
  // the integration holding it does not have to be redeployed.
  const [editing, setEditing] = useState<{
    id: number
    name: string
    scopes: string[]
    rateLimit: number
  } | null>(null)
  const [editError, setEditError] = useState('')

  const [hookUrl, setHookUrl] = useState('')
  const [hookEvents, setHookEvents] = useState<string[]>(['*'])
  const [newSecret, setNewSecret] = useState<string | null>(null)
  const [hookError, setHookError] = useState('')
  const [hookResult, setHookResult] = useState<string | null>(null)

  // Revoking a key or deleting a webhook used to be fire-and-forget: a failure
  // left the row exactly as it was, with nothing said, so the user believed an
  // integration had been cut off when it had not.
  const [actionError, setActionError] = useState('')
  const [copied, setCopied] = useState<'' | 'done' | 'failed'>('')
  // Deleting a key breaks whatever integration uses it, and there is no undo.
  // Every other destructive action in this app confirms first; these two sat
  // next to "Revoke" and fired on the first click.
  const [confirmDelete, setConfirmDelete] = useState<{
    kind: 'key' | 'webhook'
    id: number
    name: string
  } | null>(null)
  const act = async (fn: () => Promise<unknown>, whatFailed: string) => {
    setActionError('')
    try {
      await fn()
      reload()
    } catch (err: unknown) {
      setActionError(
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail || whatFailed
      )
    }
  }

  const reload = () => {
    apiKeyApi
      .list()
      .then((r) => setKeys(r.data))
      .catch(() => {})
    webhookApi
      .list()
      .then((r) => setHooks(r.data))
      .catch(() => {})
  }

  useEffect(() => {
    apiKeyApi
      .scopes()
      .then((r) => setScopes(r.data))
      .catch(() => {})
    webhookApi
      .events()
      .then((r) => setEvents(r.data))
      .catch(() => {})
    reload()
  }, [])

  const toggle = (list: string[], set: (v: string[]) => void, value: string) =>
    set(list.includes(value) ? list.filter((v) => v !== value) : [...list, value])

  const createKey = async (e: React.FormEvent) => {
    e.preventDefault()
    setKeyError('')
    setNewKey(null)
    try {
      const r = await apiKeyApi.create({
        name,
        scopes: chosen,
        rate_limit_per_minute: rateLimit,
        expires_in_days: expiresIn,
      })
      setNewKey(r.data.key)
      setName('')
      reload()
    } catch (err) {
      setKeyError(errorText(err, 'Could not create the key'))
    }
  }

  const saveKey = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing) return
    setEditError('')
    try {
      await apiKeyApi.update(editing.id, {
        name: editing.name,
        scopes: editing.scopes,
        rate_limit_per_minute: editing.rateLimit,
      })
      setEditing(null)
      reload()
    } catch (err) {
      setEditError(errorText(err, 'Could not save the key'))
    }
  }

  const createHook = async (e: React.FormEvent) => {
    e.preventDefault()
    setHookError('')
    setNewSecret(null)
    try {
      const r = await webhookApi.create({ url: hookUrl, events: hookEvents })
      setNewSecret(r.data.secret)
      setHookUrl('')
      reload()
    } catch (err) {
      setHookError(errorText(err, 'Could not create the webhook'))
    }
  }

  const testHook = async (id: number) => {
    setHookResult(null)
    try {
      const r = await webhookApi.test(id)
      setHookResult(
        r.data.status === 'ok'
          ? `Receiver answered HTTP ${r.data.http_status}`
          : `Failed: ${r.data.detail}`
      )
    } catch (err) {
      setHookResult(errorText(err, 'Test failed'))
    }
    reload()
  }

  return (
    <Page title="API Access" description="Keys for programs, and webhooks for job events.">
      {actionError && <Alert>{actionError}</Alert>}

      {/* ------------------------------------------------------- API keys */}
      <Section
        title="API keys"
        description={
          <>
            A key lets a program call this API without a password. Send it as{' '}
            <code className={code}>Authorization: Bearer &lt;key&gt;</code> or{' '}
            <code className={code}>X-API-Key: &lt;key&gt;</code>.
          </>
        }
      >
        <div className="space-y-4">
          {newKey && (
            <Alert tone="warning">
              <p className="font-medium">Copy this key now — it is never shown again.</p>
              <code className="mt-2 block break-all rounded bg-card p-2 font-mono text-xs text-foreground">
                {newKey}
              </code>
              <Button
                size="sm"
                variant="outline"
                className="mt-2 text-foreground"
                onClick={async () => {
                  // The page says the key is never shown again, so a copy that
                  // silently failed cost the user the key. The rejection was
                  // also unhandled.
                  try {
                    await navigator.clipboard.writeText(newKey)
                    setCopied('done')
                  } catch {
                    setCopied('failed')
                  }
                }}
              >
                {copied === 'done' ? (
                  <Check className="mr-1.5 h-3.5 w-3.5" />
                ) : (
                  <Copy className="mr-1.5 h-3.5 w-3.5" />
                )}
                {copied === 'done' ? 'Copied' : 'Copy'}
              </Button>
              {copied === 'failed' && (
                <p role="alert" className="mt-2 text-xs text-danger">
                  Could not reach the clipboard — select the key above and copy it by hand.
                </p>
              )}
            </Alert>
          )}

          <form onSubmit={createKey} className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-3">
              <Field label="Name" htmlFor="key-name">
                <Input
                  id="key-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="telegram-bot"
                  required
                />
              </Field>
              <Field label="Rate limit" htmlFor="key-rate" hint="Requests per minute.">
                <Input
                  id="key-rate"
                  type="number"
                  inputMode="numeric"
                  min={1}
                  max={10000}
                  value={rateLimit}
                  onChange={(e) => setRateLimit(Number(e.target.value))}
                />
              </Field>
              <Field label="Expires in" htmlFor="key-expires" hint="Days. 0 means never.">
                <Input
                  id="key-expires"
                  type="number"
                  inputMode="numeric"
                  min={0}
                  value={expiresIn}
                  onChange={(e) => setExpiresIn(Number(e.target.value))}
                />
              </Field>
            </div>

            <fieldset>
              <legend className="mb-2 text-sm font-medium">Scopes</legend>
              <div className="flex flex-wrap gap-x-5 gap-y-2.5">
                {scopes.map((s) => (
                  <Checkbox
                    key={s}
                    label={<code className="font-mono text-xs">{s}</code>}
                    checked={chosen.includes(s)}
                    onChange={() => toggle(chosen, setChosen, s)}
                  />
                ))}
              </div>
              {chosen.includes('admin') && (
                <Alert tone="warning" className="mt-3">
                  The admin scope also lets the key issue more keys and read every setting.
                </Alert>
              )}
            </fieldset>

            {keyError && <Alert>{keyError}</Alert>}
            <Button type="submit">
              <Plus className="mr-1.5 h-4 w-4" />
              Create key
            </Button>
          </form>
        </div>
      </Section>

      <Section flush title="Your keys">
        {keys.length === 0 ? (
          <EmptyState
            icon={KeyRound}
            title="No keys yet"
            description="Create one above to let a script or bot call the API."
          />
        ) : (
          <Table>
            <thead>
              <tr>
                <Th>Name</Th>
                <Th>Prefix</Th>
                <Th>Scopes</Th>
                <Th align="right">Limit</Th>
                <Th>Last used</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <Tr key={k.id}>
                  <Td className="whitespace-nowrap font-medium">
                    {k.name}
                    {k.revoked && (
                      <Badge tone="danger" className="ml-2">
                        Revoked
                      </Badge>
                    )}
                  </Td>
                  <Td mono className="whitespace-nowrap">
                    {k.prefix}…
                  </Td>
                  <Td mono>{k.scopes}</Td>
                  <Td align="right" className="tabular whitespace-nowrap text-muted-foreground">
                    {k.rate_limit_per_minute}/min
                  </Td>
                  <Td className="whitespace-nowrap text-muted-foreground">
                    {k.last_used_at ? formatDateTime(k.last_used_at) : 'Never'}
                  </Td>
                  <Td align="right">
                    <div className="flex justify-end gap-2">
                      {!k.revoked && (
                        <>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => {
                              setEditError('')
                              setEditing({
                                id: k.id,
                                name: k.name,
                                scopes: k.scopes.split(',').filter(Boolean),
                                rateLimit: k.rate_limit_per_minute,
                              })
                            }}
                          >
                            Edit
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() =>
                              act(() => apiKeyApi.revoke(k.id), 'Could not revoke that key.')
                            }
                          >
                            Revoke
                          </Button>
                        </>
                      )}
                      <Button
                        size="sm"
                        variant="danger"
                        onClick={() => setConfirmDelete({ kind: 'key', id: k.id, name: k.name })}
                      >
                        Delete
                      </Button>
                    </div>
                  </Td>
                </Tr>
              ))}
            </tbody>
          </Table>
        )}
      </Section>

      {/* ------------------------------------------------------- webhooks */}
      <Section
        title="Webhooks"
        description={
          <>
            Called with a JSON body when a job finishes, so nothing has to poll. Each delivery
            carries an <code className={code}>X-Imapsync-Signature</code> header — an HMAC-SHA256
            of the body, keyed with the signing secret.
          </>
        }
      >
        <div className="space-y-4">
          {newSecret && (
            <Alert tone="warning">
              <p className="font-medium">
                Signing secret — shown once. Your receiver needs it to verify deliveries.
              </p>
              <code className="mt-2 block break-all rounded bg-card p-2 font-mono text-xs text-foreground">
                {newSecret}
              </code>
            </Alert>
          )}

          <form onSubmit={createHook} className="space-y-4">
            <Field label="Endpoint URL" htmlFor="hook-url" className="max-w-xl">
              <Input
                id="hook-url"
                type="url"
                value={hookUrl}
                onChange={(e) => setHookUrl(e.target.value)}
                placeholder="https://example.com/hooks/imapsync"
                required
              />
            </Field>
            <fieldset>
              <legend className="mb-2 text-sm font-medium">Events</legend>
              <div className="flex flex-wrap gap-x-5 gap-y-2.5">
                <Checkbox
                  label={<code className="font-mono text-xs">* (all)</code>}
                  checked={hookEvents.includes('*')}
                  onChange={() => toggle(hookEvents, setHookEvents, '*')}
                />
                {events.map((e) => (
                  <Checkbox
                    key={e}
                    label={<code className="font-mono text-xs">{e}</code>}
                    checked={hookEvents.includes(e)}
                    onChange={() => toggle(hookEvents, setHookEvents, e)}
                  />
                ))}
              </div>
            </fieldset>
            {hookError && <Alert>{hookError}</Alert>}
            <Button type="submit">
              <Plus className="mr-1.5 h-4 w-4" />
              Add webhook
            </Button>
          </form>
        </div>
      </Section>

      <Section flush title="Your webhooks">
        {hooks.length === 0 ? (
          <EmptyState
            icon={WebhookIcon}
            title="No webhooks yet"
            description="Add an endpoint above and it is called whenever a job finishes."
          />
        ) : (
          <Table>
            <thead>
              <tr>
                <Th>URL</Th>
                <Th>Events</Th>
                <Th>Last call</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {hooks.map((h) => (
                <Tr key={h.id}>
                  <Td className="min-w-56 break-all">
                    {h.url}
                    {!h.is_active && (
                      <Badge tone="neutral" className="ml-2">
                        Disabled
                      </Badge>
                    )}
                  </Td>
                  <Td mono>{h.events}</Td>
                  <Td className="whitespace-nowrap text-muted-foreground">
                    {h.last_called_at
                      ? `${h.last_status} · ${formatDateTime(h.last_called_at)}`
                      : 'Never'}
                  </Td>
                  <Td align="right">
                    <div className="flex justify-end gap-2">
                      <Button size="sm" variant="outline" onClick={() => testHook(h.id)}>
                        Test
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          act(
                            () => webhookApi.update(h.id, { is_active: !h.is_active }),
                            'Could not change that webhook.'
                          )
                        }
                      >
                        {h.is_active ? 'Disable' : 'Enable'}
                      </Button>
                      <Button
                        size="sm"
                        variant="danger"
                        onClick={() => setConfirmDelete({ kind: 'webhook', id: h.id, name: h.url })}
                      >
                        Delete
                      </Button>
                    </div>
                  </Td>
                </Tr>
              ))}
            </tbody>
          </Table>
        )}
        {hookResult && (
          <p role="status" className="border-t border-border px-4 py-3 text-sm text-muted-foreground">
            {hookResult}
          </p>
        )}
      </Section>

      {editing && (
        <Dialog
          title="Edit API key"
          onClose={() => setEditing(null)}
          footer={
            <>
              <Button variant="outline" onClick={() => setEditing(null)}>
                Cancel
              </Button>
              <Button type="submit" form="edit-key">
                Save
              </Button>
            </>
          }
        >
          <form id="edit-key" onSubmit={saveKey} className="mt-3 space-y-4">
            <p className="text-sm text-muted-foreground">
              The key itself stays the same — whatever uses it picks up the new permissions on its
              next request.
            </p>
            <div className="grid gap-4">
              <Field label="Name" htmlFor="edit-key-name">
                <Input
                  id="edit-key-name"
                  value={editing.name}
                  onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                  required
                />
              </Field>
              <Field label="Rate limit" htmlFor="edit-key-rate" hint="Requests per minute.">
                <Input
                  id="edit-key-rate"
                  type="number"
                  inputMode="numeric"
                  min={1}
                  max={10000}
                  value={editing.rateLimit}
                  onChange={(e) => setEditing({ ...editing, rateLimit: Number(e.target.value) })}
                />
              </Field>
            </div>
            <fieldset>
              <legend className="mb-2 text-sm font-medium">Scopes</legend>
              <div className="flex flex-wrap gap-x-5 gap-y-2.5">
                {scopes.map((s) => (
                  <Checkbox
                    key={s}
                    label={<code className="font-mono text-xs">{s}</code>}
                    checked={editing.scopes.includes(s)}
                    onChange={() =>
                      toggle(editing.scopes, (v) => setEditing({ ...editing, scopes: v }), s)
                    }
                  />
                ))}
              </div>
              {editing.scopes.includes('admin') && (
                <Alert tone="warning" className="mt-3">
                  The admin scope also lets the key issue more keys and read every setting.
                </Alert>
              )}
            </fieldset>
            {editError && <Alert>{editError}</Alert>}
          </form>
        </Dialog>
      )}

      {confirmDelete && (
        <Dialog
          title={confirmDelete.kind === 'key' ? 'Delete this API key?' : 'Delete this webhook?'}
          onClose={() => setConfirmDelete(null)}
          footer={
            <>
              <Button variant="outline" onClick={() => setConfirmDelete(null)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                onClick={() => {
                  const c = confirmDelete
                  setConfirmDelete(null)
                  act(
                    () => (c.kind === 'key' ? apiKeyApi.delete(c.id) : webhookApi.delete(c.id)),
                    c.kind === 'key'
                      ? 'Could not delete that key.'
                      : 'Could not delete that webhook.'
                  )
                }}
              >
                Delete
              </Button>
            </>
          }
        >
          <p className="mt-1.5 text-sm text-muted-foreground">
            <span className="font-medium text-foreground">{confirmDelete.name}</span>
            {confirmDelete.kind === 'key'
              ? ' stops working immediately, and anything using it starts getting 401s. Revoke instead if you only want to disable it — a revoked key stays in the list.'
              : ' will no longer receive job events. This cannot be undone.'}
          </p>
        </Dialog>
      )}
    </Page>
  )
}

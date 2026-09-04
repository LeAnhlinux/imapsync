import { useEffect, useState } from 'react'
import { apiKeyApi, webhookApi, type ApiKey, type Webhook } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { PageHeader } from '@/components/ui/data'
import { Input } from '@/components/ui/input'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'

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
    <div className="mx-auto max-w-4xl space-y-6">
      <PageHeader
        title="API Access"
        description="Keys for programs, and webhooks for job events."
      />

      {actionError && (
        <div role="alert" className="mb-4 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger">
          {actionError}
        </div>
      )}

      {/* ------------------------------------------------------- API keys */}
      <Card>
        <CardHeader>
          <CardTitle>API Keys</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            A key lets a program call this API without a password. Send it as{' '}
            <code className="text-xs bg-muted px-1 py-0.5 rounded">
              Authorization: Bearer &lt;key&gt;
            </code>{' '}
            or <code className="text-xs bg-muted px-1 py-0.5 rounded">X-API-Key: &lt;key&gt;</code>.
          </p>

          {newKey && (
            <div className="border border-amber-500 rounded-md p-3 space-y-2">
              <p className="text-sm font-medium text-amber-700 dark:text-amber-500">
                Copy this key now — it is never shown again.
              </p>
              <code className="block text-xs bg-muted rounded p-2 break-all">{newKey}</code>
              <Button
                size="sm"
                variant="outline"
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
                {copied === 'done' ? 'Copied' : 'Copy'}
              </Button>
              {copied === 'failed' && (
                <p role="alert" className="text-xs text-danger">
                  Could not reach the clipboard — select the key above and copy it by hand.
                </p>
              )}
            </div>
          )}

          <form onSubmit={createKey} className="space-y-4 border-t border-border pt-4">
            <div className="grid grid-cols-3 gap-4">
              <div>
                <label htmlFor="key-name" className="text-sm font-medium">
                  Name
                </label>
                <Input
                  id="key-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="telegram-bot"
                  required
                />
              </div>
              <div>
                <label htmlFor="key-rate" className="text-sm font-medium">
                  Rate limit (req/min)
                </label>
                <Input
                  id="key-rate"
                  type="number"
                  min={1}
                  max={10000}
                  value={rateLimit}
                  onChange={(e) => setRateLimit(Number(e.target.value))}
                />
              </div>
              <div>
                <label htmlFor="key-expires" className="text-sm font-medium">
                  Expires in (days)
                </label>
                <Input
                  id="key-expires"
                  type="number"
                  min={0}
                  value={expiresIn}
                  onChange={(e) => setExpiresIn(Number(e.target.value))}
                  placeholder="0 = never"
                />
              </div>
            </div>

            <div>
              <span className="text-sm font-medium">Scopes</span>
              <div className="flex flex-wrap gap-3 mt-1">
                {scopes.map((s) => (
                  <label key={s} className="flex items-center gap-1.5 text-sm">
                    <input
                      type="checkbox"
                      checked={chosen.includes(s)}
                      onChange={() => toggle(chosen, setChosen, s)}
                    />
                    <code className="text-xs">{s}</code>
                  </label>
                ))}
              </div>
              {chosen.includes('admin') && (
                <p className="text-sm text-amber-700 dark:text-amber-500 mt-1">
                  The admin scope also lets the key issue more keys and read every setting.
                </p>
              )}
            </div>

            <Button type="submit">Create Key</Button>
            {keyError && <p className="text-sm text-red-600 dark:text-red-500">{keyError}</p>}
          </form>

          <div className="border-t border-border pt-4 overflow-x-auto">
            {keys.length === 0 ? (
              <p className="text-sm text-muted-foreground">No keys yet.</p>
            ) : (
              <table className="w-full text-sm">
                <thead className="text-muted-foreground">
                  <tr className="text-left">
                    <th className="py-1 pr-3">Name</th>
                    <th className="py-1 pr-3">Prefix</th>
                    <th className="py-1 pr-3">Scopes</th>
                    <th className="py-1 pr-3">Limit</th>
                    <th className="py-1 pr-3">Last used</th>
                    <th className="py-1"></th>
                  </tr>
                </thead>
                <tbody>
                  {keys.map((k) => (
                    <tr key={k.id} className="border-t border-border">
                      <td className="py-2 pr-3">
                        {k.name}
                        {k.revoked && (
                          <span className="ml-2 text-xs text-red-600 dark:text-red-500">
                            revoked
                          </span>
                        )}
                      </td>
                      <td className="py-2 pr-3">
                        <code className="text-xs">{k.prefix}…</code>
                      </td>
                      <td className="py-2 pr-3">
                        <code className="text-xs">{k.scopes}</code>
                      </td>
                      <td className="py-2 pr-3">{k.rate_limit_per_minute}/min</td>
                      <td className="py-2 pr-3 text-muted-foreground">
                        {k.last_used_at ? new Date(k.last_used_at).toLocaleString() : 'never'}
                      </td>
                      <td className="py-2 text-right whitespace-nowrap">
                        {!k.revoked && (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() =>
                              act(() => apiKeyApi.revoke(k.id), 'Could not revoke that key.')
                            }
                          >
                            Revoke
                          </Button>
                        )}
                        <Button
                          size="sm"
                          variant="outline"
                          className="ml-2"
                          onClick={() =>
                            setConfirmDelete({
                              kind: 'key',
                              id: k.id,
                              name: k.name,
                            })
                          }
                        >
                          Delete
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </CardContent>
      </Card>

      {/* ------------------------------------------------------- webhooks */}
      <Card>
        <CardHeader>
          <CardTitle>Webhooks</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            Called with a JSON body when a job finishes, so nothing has to poll. Each delivery
            carries an{' '}
            <code className="text-xs bg-muted px-1 py-0.5 rounded">X-Imapsync-Signature</code>{' '}
            header — an HMAC-SHA256 of the body, keyed with the secret below.
          </p>

          {newSecret && (
            <div className="border border-amber-500 rounded-md p-3 space-y-2">
              <p className="text-sm font-medium text-amber-700 dark:text-amber-500">
                Signing secret — shown once. Your receiver needs it to verify deliveries.
              </p>
              <code className="block text-xs bg-muted rounded p-2 break-all">{newSecret}</code>
            </div>
          )}

          <form onSubmit={createHook} className="space-y-4 border-t border-border pt-4">
            <div>
              <label htmlFor="hook-url" className="text-sm font-medium">
                Endpoint URL
              </label>
              <Input
                id="hook-url"
                value={hookUrl}
                onChange={(e) => setHookUrl(e.target.value)}
                placeholder="https://example.com/hooks/imapsync"
                required
              />
            </div>
            <div>
              <span className="text-sm font-medium">Events</span>
              <div className="flex flex-wrap gap-3 mt-1">
                <label className="flex items-center gap-1.5 text-sm">
                  <input
                    type="checkbox"
                    checked={hookEvents.includes('*')}
                    onChange={() => toggle(hookEvents, setHookEvents, '*')}
                  />
                  <code className="text-xs">* (all)</code>
                </label>
                {events.map((e) => (
                  <label key={e} className="flex items-center gap-1.5 text-sm">
                    <input
                      type="checkbox"
                      checked={hookEvents.includes(e)}
                      onChange={() => toggle(hookEvents, setHookEvents, e)}
                    />
                    <code className="text-xs">{e}</code>
                  </label>
                ))}
              </div>
            </div>
            <Button type="submit">Add Webhook</Button>
            {hookError && <p className="text-sm text-red-600 dark:text-red-500">{hookError}</p>}
          </form>

          <div className="border-t border-border pt-4 overflow-x-auto">
            {hooks.length === 0 ? (
              <p className="text-sm text-muted-foreground">No webhooks yet.</p>
            ) : (
              <table className="w-full text-sm">
                <thead className="text-muted-foreground">
                  <tr className="text-left">
                    <th className="py-1 pr-3">URL</th>
                    <th className="py-1 pr-3">Events</th>
                    <th className="py-1 pr-3">Last call</th>
                    <th className="py-1"></th>
                  </tr>
                </thead>
                <tbody>
                  {hooks.map((h) => (
                    <tr key={h.id} className="border-t border-border">
                      <td className="py-2 pr-3 break-all">
                        {h.url}
                        {!h.is_active && (
                          <span className="ml-2 text-xs text-muted-foreground">disabled</span>
                        )}
                      </td>
                      <td className="py-2 pr-3">
                        <code className="text-xs">{h.events}</code>
                      </td>
                      <td className="py-2 pr-3 text-muted-foreground">
                        {h.last_called_at
                          ? `${h.last_status} · ${new Date(h.last_called_at).toLocaleString()}`
                          : 'never'}
                      </td>
                      <td className="py-2 text-right whitespace-nowrap">
                        <Button size="sm" variant="outline" onClick={() => testHook(h.id)}>
                          Test
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          className="ml-2"
                          onClick={() =>
                            act(
                              () =>
                                webhookApi.update(h.id, {
                                  is_active: !h.is_active,
                                }),
                              'Could not change that webhook.'
                            )
                          }
                        >
                          {h.is_active ? 'Disable' : 'Enable'}
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          className="ml-2"
                          onClick={() =>
                            setConfirmDelete({
                              kind: 'webhook',
                              id: h.id,
                              name: h.url,
                            })
                          }
                        >
                          Delete
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
          {hookResult && <p className="text-sm text-muted-foreground">{hookResult}</p>}
        </CardContent>
      </Card>

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
    </div>
  )
}

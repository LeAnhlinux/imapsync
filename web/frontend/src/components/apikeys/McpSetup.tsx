import { useEffect, useState } from 'react'
import { Check, Copy, Download } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { buttonVariants } from '@/components/ui/button-variants'
import { Alert, Section } from '@/components/ui/data'
import { cn } from '@/lib/utils'

// The MCP connector lets Claude drive this site through the API. It runs on
// the user's machine, so the page offers it as a download — built by deploy.sh
// into /downloads/mcp/ with a manifest listing what is there.

interface McpFile {
  name: string
  os: string
  arch: string
  size: number
  sha256: string
}

interface McpManifest {
  version: string
  files: McpFile[]
}

const platformLabel: Record<string, string> = {
  'darwin-arm64': 'macOS · Apple Silicon',
  'darwin-amd64': 'macOS · Intel',
  'windows-amd64': 'Windows',
  'linux-amd64': 'Linux',
}

// A guess at which file this visitor wants. A browser does not reliably say
// whether a Mac is Apple Silicon, and every Mac sold since 2021 is, so that is
// the default; the Intel build stays one click away.
function guessPlatform(): string {
  const ua = navigator.userAgent
  if (/Windows/i.test(ua)) return 'windows-amd64'
  if (/Mac OS X|Macintosh/i.test(ua)) return 'darwin-arm64'
  if (/Linux/i.test(ua) && !/Android/i.test(ua)) return 'linux-amd64'
  return 'darwin-arm64'
}

function CodeBlock({ text }: { text: string }) {
  const [state, setState] = useState<'' | 'done' | 'failed'>('')
  return (
    <div className="relative">
      <pre className="overflow-x-auto rounded-md bg-subtle p-3 pr-20 font-mono text-xs leading-relaxed text-foreground">
        {text}
      </pre>
      <Button
        size="sm"
        variant="outline"
        className="absolute right-2 top-2 h-7 px-2 text-xs"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(text)
            setState('done')
          } catch {
            setState('failed')
          }
        }}
      >
        {state === 'done' ? <Check className="mr-1 h-3 w-3" /> : <Copy className="mr-1 h-3 w-3" />}
        {state === 'done' ? 'Copied' : state === 'failed' ? 'Select it' : 'Copy'}
      </Button>
    </div>
  )
}

function Step({ n, title, children }: { n: number; title: string; children: React.ReactNode }) {
  return (
    <li className="flex gap-3">
      <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary-soft text-xs font-semibold text-primary">
        {n}
      </span>
      <div className="min-w-0 flex-1 space-y-2">
        <p className="text-sm font-medium">{title}</p>
        {children}
      </div>
    </li>
  )
}

export function McpSetup({ newKey }: { newKey: string | null }) {
  const [manifest, setManifest] = useState<McpManifest | null>(null)
  const [unavailable, setUnavailable] = useState(false)
  const [platform, setPlatform] = useState(guessPlatform)

  useEffect(() => {
    fetch('/downloads/mcp/manifest.json', { cache: 'no-cache' })
      .then((r) => (r.ok ? r.json() : Promise.reject()))
      .then((m: McpManifest) => {
        if (!Array.isArray(m.files) || m.files.length === 0) throw new Error()
        setManifest(m)
      })
      // In development the SPA fallback answers this with index.html, which
      // fails to parse — the same as a server deployed without downloads.
      .catch(() => setUnavailable(true))
  }, [])

  const file = manifest?.files.find((f) => `${f.os}-${f.arch}` === platform) ?? manifest?.files[0]
  const isWindows = file?.os === 'windows'
  const path = isWindows ? `C:\\path\\to\\${file?.name}` : `~/Downloads/${file?.name}`
  const key = newKey ?? 'isk_your_key'
  const site = window.location.origin

  const prepare =
    file?.os === 'darwin'
      ? `chmod +x ${path}\nxattr -d com.apple.quarantine ${path}`
      : file?.os === 'linux'
        ? `chmod +x ${path}`
        : ''
  // cmd and PowerShell do not continue a line on a backslash, so Windows gets
  // the command on one line.
  const addArgs = [
    'claude mcp add imapsync --scope user',
    `-e IMAPSYNC_URL=${site}`,
    `-e IMAPSYNC_API_KEY=${key}`,
    `-- ${isWindows ? `"${path}"` : path}`,
  ]
  const addCommand = isWindows ? addArgs.join(' ') : addArgs.join(' \\\n  ')
  const desktopConfig = JSON.stringify(
    {
      mcpServers: {
        imapsync: {
          command: isWindows ? `C:\\path\\to\\${file?.name}` : `/full/path/to/${file?.name}`,
          env: { IMAPSYNC_URL: site, IMAPSYNC_API_KEY: key },
        },
      },
    },
    null,
    2
  )

  return (
    <Section
      title="Use with Claude (MCP)"
      description="Let Claude start, watch and explain syncs for you — in plain words, through this API."
    >
      {unavailable ? (
        <p className="text-sm text-muted-foreground">
          The connector downloads are not on this server yet. They are added by the next deploy.
        </p>
      ) : !manifest || !file ? (
        <p className="text-sm text-muted-foreground">Loading…</p>
      ) : (
        <div className="space-y-5">
          <ol className="space-y-5">
            <Step n={1} title={`Download the connector · v${manifest.version}`}>
              <div className="flex flex-wrap gap-2">
                {manifest.files.map((f) => {
                  const id = `${f.os}-${f.arch}`
                  return (
                    <a
                      key={f.name}
                      href={`/downloads/mcp/${f.name}`}
                      download
                      onClick={() => setPlatform(id)}
                      className={cn(
                        buttonVariants({
                          variant: id === platform ? 'default' : 'outline',
                          size: 'sm',
                        })
                      )}
                    >
                      <Download className="mr-1.5 h-3.5 w-3.5" />
                      {platformLabel[id] ?? id}
                      <span className="ml-1.5 opacity-70">{(f.size / 1048576).toFixed(1)} MB</span>
                    </a>
                  )
                })}
              </div>
              <details className="text-xs text-muted-foreground">
                <summary className="cursor-pointer">SHA-256 checksums</summary>
                <ul className="mt-1.5 space-y-0.5 font-mono">
                  {manifest.files.map((f) => (
                    <li key={f.name} className="break-all">
                      {f.sha256} {f.name}
                    </li>
                  ))}
                </ul>
              </details>
            </Step>

            <Step n={2} title="Have an API key">
              {newKey ? (
                <p className="text-sm text-muted-foreground">
                  The commands below already carry the key you just created.
                </p>
              ) : (
                <p className="text-sm text-muted-foreground">
                  Create one above. For migrations, tick{' '}
                  <code className="font-mono text-xs">profiles:read</code>,{' '}
                  <code className="font-mono text-xs">profiles:write</code>,{' '}
                  <code className="font-mono text-xs">sync:read</code> and{' '}
                  <code className="font-mono text-xs">sync:write</code>. Then replace{' '}
                  <code className="font-mono text-xs">isk_your_key</code> below with it.
                </p>
              )}
            </Step>

            <Step n={3} title="Add it to Claude Code">
              {prepare && (
                <>
                  <p className="text-sm text-muted-foreground">
                    Allow it to run
                    {file.os === 'darwin'
                      ? ' — macOS blocks downloaded programs until told otherwise'
                      : ''}
                    :
                  </p>
                  <CodeBlock text={prepare} />
                </>
              )}
              <CodeBlock text={addCommand} />
              <p className="text-sm text-muted-foreground">
                {isWindows
                  ? 'Use the full path to where you saved the file. '
                  : 'Moved the file? Use its new path. '}
                Then start a new Claude Code session and ask, for example,{' '}
                <span className="italic">“copy mail from a@old.com to b@new.com”</span>.
              </p>
              <details className="text-sm">
                <summary className="cursor-pointer text-muted-foreground">
                  Using Claude Desktop instead
                </summary>
                <p className="mb-2 mt-2 text-muted-foreground">
                  Settings → Developer → Edit Config, add this to{' '}
                  <code className="font-mono text-xs">claude_desktop_config.json</code>, then
                  restart Claude Desktop:
                </p>
                <CodeBlock text={desktopConfig} />
              </details>
            </Step>
          </ol>
          {newKey && (
            <Alert tone="warning">
              These commands contain your key. Do not paste them anywhere shared.
            </Alert>
          )}
        </div>
      )}
    </Section>
  )
}

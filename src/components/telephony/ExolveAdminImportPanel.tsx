import { useState } from 'react'
import { api } from '../../lib/api'
import { useAuth } from '../../store/auth'
import { Button } from '../ui/Button'

const SAMPLE = `{
  "managers": [
    {
      "email": "manager@example.com",
      "sip_login": "88314XXXXXXXXXX",
      "sip_password": "********",
      "numbers": ["79001234567", "79007654321"]
    }
  ]
}`

type ImportResult = {
  ok?: boolean
  results?: Array<{
    email?: string
    user_id: string
    sip_login: string
    imported: number
    errors?: string[]
  }>
}

export function ExolveAdminImportPanel() {
  const { user } = useAuth()
  const [raw, setRaw] = useState(SAMPLE)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState<ImportResult | null>(null)

  if (user?.role !== 'admin') return null

  const submit = async () => {
    setBusy(true)
    setError('')
    setResult(null)
    try {
      let body: unknown
      try {
        body = JSON.parse(raw)
      } catch {
        throw new Error('Некорректный JSON')
      }
      const res = await api<ImportResult>('/telephony/exolve/admin/import', {
        method: 'POST',
        body: JSON.stringify(body),
      })
      setResult(res)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Импорт не удался')
    } finally {
      setBusy(false)
    }
  }

  return (
    <details className="b24-glass-panel group">
      <summary className="cursor-pointer list-none px-4 py-3 text-[13px] font-semibold text-b24-text-dark flex items-center justify-between select-none [&::-webkit-details-marker]:hidden">
        <span>Импорт</span>
        <span className="text-[12px] font-normal text-b24-text-muted group-open:hidden">админ</span>
      </summary>
      <div className="px-4 pb-4 flex flex-col gap-3 border-t border-black/6 pt-3">
        <textarea
          value={raw}
          onChange={(e) => setRaw(e.target.value)}
          rows={8}
          spellCheck={false}
          className="w-full rounded-b24-sm border border-black/10 bg-white px-3 py-2 font-mono text-[12px] text-b24-text-dark outline-none focus:border-b24-cyan"
        />
        {error ? <div className="text-[12px] text-red-600">{error}</div> : null}
        {result?.results?.length ? (
          <ul className="text-[12px] text-b24-text-dark flex flex-col gap-1">
            {result.results.map((r, i) => (
              <li key={`${r.user_id}-${i}`}>
                {r.email || r.user_id}: {r.imported}
                {r.errors?.length ? (
                  <span className="text-red-600"> — {r.errors.slice(0, 2).join('; ')}</span>
                ) : null}
              </li>
            ))}
          </ul>
        ) : null}
        <Button
          variant="green"
          className="rounded-b24-sm self-start h-9"
          disabled={busy}
          onClick={() => void submit()}
        >
          Импорт
        </Button>
      </div>
    </details>
  )
}

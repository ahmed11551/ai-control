import { useCallback, useEffect, useState } from 'react'
import { api } from '../../lib/api'
import { Button } from '../ui/Button'

type InboundNumber = {
  number: string
  on_softphone: boolean
  redirect_to?: string
  source?: string
}

type InboundStatus = {
  configured: boolean
  api_key: boolean
  sip_login: string
  numbers: InboundNumber[]
  hint?: string
}

export function ExolveInboundPanel({ softphoneReady: _softphoneReady }: { softphoneReady: boolean }) {
  const [status, setStatus] = useState<InboundStatus | null>(null)
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)
  const [manual, setManual] = useState('')

  const load = useCallback(async () => {
    setError('')
    try {
      const data = await api<InboundStatus>('/telephony/exolve/inbound')
      setStatus(data)
    } catch (e) {
      setStatus(null)
      setError(e instanceof Error ? e.message : 'Ошибка')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const enable = async (number: string) => {
    setBusy(true)
    setError('')
    setMsg('')
    try {
      await api('/telephony/exolve/inbound', {
        method: 'POST',
        body: JSON.stringify({ number }),
      })
      setMsg('Готово')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Ошибка')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded-b24-sm border border-black/8 bg-white/60 p-3 flex flex-col gap-2.5">
      <div className="flex items-center gap-2 text-[13px]">
        <span className="font-semibold text-b24-text-dark">Входящие</span>
        {status?.sip_login ? (
          <span className="font-mono text-[12px] text-b24-text-muted ml-auto">{status.sip_login}</span>
        ) : null}
      </div>

      {error ? <div className="text-[12px] text-red-600">{error}</div> : null}
      {msg ? <div className="text-[12px] text-emerald-700">{msg}</div> : null}

      {status?.numbers?.length ? (
        <ul className="flex flex-col gap-1.5">
          {status.numbers.map((n) => (
            <li key={n.number} className="flex items-center justify-between gap-2">
              <span className="font-mono text-[13px] text-b24-text-dark">{n.number}</span>
              <Button
                variant={n.on_softphone ? 'filter' : 'green'}
                className="rounded-b24-sm h-8 text-[12px]"
                disabled={busy || n.on_softphone}
                onClick={() => void enable(n.number)}
              >
                {n.on_softphone ? 'Ок' : 'На softphone'}
              </Button>
            </li>
          ))}
        </ul>
      ) : null}

      {status?.api_key ? (
        <div className="flex flex-wrap gap-2">
          <input
            value={manual}
            onChange={(e) => setManual(e.target.value)}
            placeholder="DID"
            inputMode="tel"
            className="h-9 flex-1 min-w-[120px] rounded-b24-sm border border-black/10 px-2.5 font-mono text-[13px] outline-none focus:border-b24-cyan"
          />
          <Button
            variant="green"
            className="rounded-b24-sm h-9"
            disabled={busy || !manual.trim()}
            onClick={() => void enable(manual)}
          >
            На softphone
          </Button>
        </div>
      ) : null}
    </div>
  )
}

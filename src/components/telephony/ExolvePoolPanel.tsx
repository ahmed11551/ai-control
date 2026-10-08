import { useCallback, useEffect, useState } from 'react'
import { api } from '../../lib/api'
import { Button } from '../ui/Button'

type PoolNumber = {
  id: string
  number: string
  forwarding_ok: boolean
  last_used_at?: string
  cli_lease_until?: string
}

type PoolStatus = {
  mode: 'pool' | 'fallback'
  sip_login: string
  carousel_auth?: string
  numbers: PoolNumber[]
  pool_count: number
  forwarding_ok: number
  leased_cli?: string
  api_key: boolean
  hint?: string
}

export function ExolvePoolPanel({
  softphoneReady: _softphoneReady,
  activeCli,
}: {
  softphoneReady: boolean
  activeCli?: string
}) {
  const [status, setStatus] = useState<PoolStatus | null>(null)
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)
  const [carouselAuth, setCarouselAuth] = useState('883140776920488')

  const load = useCallback(async () => {
    setError('')
    try {
      const data = await api<PoolStatus>('/telephony/exolve/pool')
      setStatus(data)
      if (data.carousel_auth) setCarouselAuth(data.carousel_auth)
    } catch (e) {
      setStatus(null)
      setError(e instanceof Error ? e.message : 'Не удалось загрузить пул')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load, activeCli])

  const provision = async () => {
    setBusy(true)
    setError('')
    setMsg('')
    try {
      const res = await api<{ message?: string; ok?: boolean; errors?: string[] }>(
        '/telephony/exolve/pool/provision',
        { method: 'POST', body: '{}' },
      )
      setMsg(res.message || 'Готово')
      if (res.errors?.length) setError(res.errors.slice(0, 3).join('; '))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Ошибка')
    } finally {
      setBusy(false)
    }
  }

  const syncFromExolve = async () => {
    setBusy(true)
    setError('')
    setMsg('')
    try {
      const res = await api<{ message?: string; hint?: string; errors?: string[] }>(
        '/telephony/exolve/pool/sync',
        {
          method: 'POST',
          body: JSON.stringify({ carousel_auth: carouselAuth.trim() }),
        },
      )
      setMsg(res.message || 'Синхронизировано')
      if (res.hint && (res.message || '').includes(' 0 ')) setError(res.hint)
      if (res.errors?.length) setError(res.errors.slice(0, 3).join('; '))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Ошибка')
    } finally {
      setBusy(false)
    }
  }

  const isPool = status?.mode === 'pool'
  const preview = (status?.numbers ?? []).slice(0, 6)
  const cli = activeCli || status?.leased_cli

  return (
    <div className="rounded-b24-sm border border-black/8 bg-white/60 p-3 flex flex-col gap-2.5">
      <div className="flex flex-wrap items-center gap-2 text-[13px]">
        <span className="font-semibold text-b24-text-dark">Пул</span>
        {status?.sip_login ? (
          <span className="font-mono text-[12px] text-b24-text-muted">{status.sip_login}</span>
        ) : null}
        <span className="text-[12px] text-b24-text-muted ml-auto">
          {status?.pool_count ?? 0}
          {status && status.pool_count > 0 ? ` · CF ${status.forwarding_ok}/${status.pool_count}` : null}
          {cli ? ` · ${cli}` : null}
        </span>
      </div>

      {error ? <div className="text-[12px] text-red-600">{error}</div> : null}
      {msg ? <div className="text-[12px] text-emerald-700">{msg}</div> : null}

      <div className="flex flex-wrap gap-2 items-center">
        <input
          value={carouselAuth}
          onChange={(e) => setCarouselAuth(e.target.value)}
          placeholder="Карусель"
          className="h-9 flex-1 min-w-[140px] rounded-b24-sm border border-black/10 px-2.5 font-mono text-[13px] text-b24-text-dark outline-none focus:border-b24-cyan"
        />
        <Button
          variant="green"
          className="rounded-b24-sm h-9"
          disabled={busy || !status?.api_key || !carouselAuth.trim()}
          onClick={() => void syncFromExolve()}
        >
          Sync
        </Button>
        {isPool || (status?.pool_count ?? 0) > 0 ? (
          <Button
            variant="filter"
            className="rounded-b24-sm h-9"
            disabled={busy || !status?.api_key || (status?.pool_count ?? 0) === 0}
            onClick={() => void provision()}
          >
            Входящие
          </Button>
        ) : null}
      </div>

      {preview.length ? (
        <ul className="flex flex-col gap-1 max-h-[140px] overflow-auto b24-scrollbar">
          {preview.map((n) => (
            <li
              key={n.id}
              className="flex items-center justify-between gap-2 px-2 py-1 text-[12px] rounded-b24-sm hover:bg-black/[0.03]"
            >
              <span className="font-mono text-b24-text-dark">{n.number}</span>
              <span className="text-b24-text-muted">
                {n.cli_lease_until ? 'звонок' : n.forwarding_ok ? 'ok' : '—'}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

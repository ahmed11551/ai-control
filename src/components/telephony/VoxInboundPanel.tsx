import { useCallback, useEffect, useState } from 'react'
import { api } from '../../lib/api'
import { Button } from '../ui/Button'

type InboundStatus = {
  configured: boolean
  vox_user: string
  vox_username: string
  steps: string[]
  hint?: string
}

export function VoxInboundPanel({ softphoneReady }: { softphoneReady: boolean }) {
  const [status, setStatus] = useState<InboundStatus | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setError('')
    try {
      const data = await api<InboundStatus>('/telephony/vox/inbound')
      setStatus(data)
    } catch (e) {
      setStatus(null)
      setError(e instanceof Error ? e.message : 'Ошибка')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  return (
    <div className="rounded-b24-sm border border-black/8 bg-white/60 p-3 flex flex-col gap-2.5">
      <div className="flex flex-wrap items-center gap-2 text-[13px]">
        <span className="font-semibold text-b24-text-dark">Vox</span>
        <span
          className={[
            'h-6 px-2 rounded-b24-pill text-[11px] font-medium inline-flex items-center',
            softphoneReady ? 'bg-emerald-50 text-emerald-700' : 'bg-[#eef1f4] text-b24-text-muted',
          ].join(' ')}
        >
          {softphoneReady ? 'на линии' : 'офлайн'}
        </span>
        {status?.vox_user ? (
          <span className="font-mono text-[12px] text-b24-text-muted ml-auto">{status.vox_user}</span>
        ) : null}
      </div>

      {error ? <div className="text-[12px] text-red-600">{error}</div> : null}

      <div className="flex flex-wrap gap-2">
        <Button variant="filter" className="rounded-b24-sm h-8 text-[12px]" onClick={() => void load()}>
          Обновить
        </Button>
        <a
          className="inline-flex items-center h-8 px-3 rounded-b24-sm text-[12px] border border-black/10 text-b24-text-dark hover:bg-black/5"
          href="https://manage.voximplant.com/numbers"
          target="_blank"
          rel="noreferrer"
        >
          Numbers
        </a>
      </div>
    </div>
  )
}

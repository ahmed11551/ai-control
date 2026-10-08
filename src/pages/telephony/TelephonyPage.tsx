import { useEffect, useState } from 'react'
import { SoftphonePanel } from '../../components/telephony/SoftphonePanel'
import { api } from '../../lib/api'
import { useTelephony } from '../../store/telephony'
import { TELEPHONY_ENABLED } from '../../telephony/config'

type TelCall = {
  id: string
  direction: string
  from_number: string
  to_number: string
  duration_sec: number
  disposition: string
  started_at: string
  user_name: string
  entity_type?: string
}

type TelData = {
  calls: TelCall[]
  live: unknown[]
  ami: boolean
  withInner: number
}

function formatWhen(raw: string) {
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function formatDur(sec: number) {
  if (!sec) return '—'
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

function dirLabel(v: string) {
  const s = v.toLowerCase()
  if (s.includes('in') || s === 'inbound') return 'Входящий'
  if (s.includes('out') || s === 'outbound') return 'Исходящий'
  return v || '—'
}

function dispLabel(v: string) {
  switch (v.toUpperCase()) {
    case 'ANSWERED':
      return 'Отвечен'
    case 'NO ANSWER':
    case 'NOANSWER':
    case 'NO_REACH':
      return 'Нет ответа'
    case 'BUSY':
      return 'Занято'
    case 'FAILED':
      return 'Сбой'
    default:
      return v || '—'
  }
}

export function TelephonyPage() {
  const { history, liveDealId } = useTelephony()
  const [data, setData] = useState<TelData | null>(null)
  const [error, setError] = useState('')

  const load = () =>
    api<TelData>('/telephony')
      .then(setData)
      .catch((e: Error) => setError(e.message))

  useEffect(() => {
    void load()
  }, [history, liveDealId])

  return (
    <div className="flex flex-col gap-4 pt-1 pb-6 min-h-[calc(100%-2rem)]">
      <div className="min-w-0">
        <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">Телефония</h1>
        <p className="mt-1.5 text-[13px] text-white/65">Softphone и свой журнал звонков</p>
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,420px)_minmax(0,1fr)] items-start">
        <div className="flex flex-col gap-3 min-w-0 max-w-[420px] w-full xl:max-w-none">
          {TELEPHONY_ENABLED ? (
            <SoftphonePanel />
          ) : (
            <div className="b24-glass-panel p-4 text-[14px] text-b24-text-muted">Softphone выключен</div>
          )}
        </div>

        <div className="flex flex-col gap-3 min-w-0">
          {error ? <p className="text-[13px] text-orange-200">{error}</p> : null}

          <div className="flex flex-wrap items-center gap-2">
            <div className="text-[15px] font-semibold text-white">Журнал</div>
            <div className="text-[12px] text-white/70 ml-auto tabular-nums">
              {data ? `${data.calls.length}` : ''}
            </div>
          </div>

          <div className="b24-glass-panel overflow-hidden min-h-[480px]">
            <div className="overflow-x-auto b24-scrollbar">
              <table className="w-full min-w-[720px] text-left text-[13px]">
                <thead className="bg-b24-panel-header text-[12px] text-b24-text-muted">
                  <tr>
                    <th className="px-3 py-2.5 font-medium">Когда</th>
                    <th className="px-3 py-2.5 font-medium">Напр.</th>
                    <th className="px-3 py-2.5 font-medium">Откуда</th>
                    <th className="px-3 py-2.5 font-medium">Куда</th>
                    <th className="px-3 py-2.5 font-medium">Длит.</th>
                    <th className="px-3 py-2.5 font-medium">Статус</th>
                    <th className="px-3 py-2.5 font-medium">Кто</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-b24-border text-b24-text-dark">
                  {(data?.calls ?? []).length === 0 ? (
                    <tr>
                      <td colSpan={7} className="px-3 py-12 text-center text-b24-text-muted">
                        Пусто
                      </td>
                    </tr>
                  ) : (
                    (data?.calls ?? []).map((c) => (
                      <tr key={c.id} className="hover:bg-[#f7fafc] transition-colors">
                        <td className="px-3 py-2.5 whitespace-nowrap">{formatWhen(c.started_at)}</td>
                        <td className="px-3 py-2.5">{dirLabel(c.direction)}</td>
                        <td className="px-3 py-2.5 tabular-nums font-mono text-[12px]">
                          {c.from_number || '—'}
                        </td>
                        <td className="px-3 py-2.5 tabular-nums font-mono text-[12px]">
                          {c.to_number || '—'}
                        </td>
                        <td className="px-3 py-2.5 tabular-nums">{formatDur(Number(c.duration_sec) || 0)}</td>
                        <td className="px-3 py-2.5">{dispLabel(c.disposition)}</td>
                        <td className="px-3 py-2.5">{c.user_name || '—'}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

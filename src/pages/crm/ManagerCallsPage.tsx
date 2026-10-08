import { useEffect, useMemo, useRef, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { api } from '../../lib/api'
import { isManagerRole } from '../../lib/roles'
import { useAuth } from '../../store/auth'
import { useCrmLeadDrawer } from '../../store/crmLeadDrawer'
import { useTelephony } from '../../store/telephony'

type CallKind = 'missed' | 'outgoing' | 'incoming' | 'other'
type Filter = 'all' | 'missed' | 'outgoing'

type ManagerCall = {
  id: string
  kind: CallKind
  duration_sec: number
  started_at: string
  tail: string
  lead_id: string
  review_status?: string
}

const filters: { id: Filter; label: string }[] = [
  { id: 'all', label: 'Все' },
  { id: 'missed', label: 'Пропущенные' },
  { id: 'outgoing', label: 'Исходящие' },
]

function formatWhen(raw: string) {
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatDur(sec: number) {
  const n = Number(sec) || 0
  const m = Math.floor(n / 60)
  const s = n % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

function kindLabel(kind: CallKind) {
  if (kind === 'missed') return 'Пропущенный'
  if (kind === 'outgoing') return 'Исходящий'
  if (kind === 'incoming') return 'Входящий'
  return 'Звонок'
}

function reviewLabel(status?: string) {
  if (status === 'flagged') return 'Нарушение'
  if (status === 'clean') return 'Проверен'
  if (status === 'pending') return 'Ждёт проверку'
  if (status === 'skipped') return 'Без записи'
  return ''
}

function kindClass(kind: CallKind) {
  if (kind === 'missed') return 'bg-rose-50 text-rose-700 ring-rose-200'
  if (kind === 'outgoing') return 'bg-sky-50 text-sky-800 ring-sky-200'
  return 'bg-slate-100 text-slate-600 ring-slate-200'
}

export function ManagerCallsPage() {
  const { user } = useAuth()
  const { openLead } = useCrmLeadDrawer()
  const { history } = useTelephony()
  const [calls, setCalls] = useState<ManagerCall[]>([])
  const [filter, setFilter] = useState<Filter>('all')
  const [selected, setSelected] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [opening, setOpening] = useState('')
  const openingRef = useRef('')

  const manager = isManagerRole(user?.role)
  const historyMark = history[0]?.id ?? ''

  useEffect(() => {
    if (!manager) return
    let cancelled = false
    setLoading(true)
    api<{ calls?: ManagerCall[] }>('/calls/mine')
      .then((res) => {
        if (cancelled) return
        setCalls(Array.isArray(res.calls) ? res.calls : [])
        setError('')
      })
      .catch((e: Error) => {
        if (cancelled) return
        setError(e.message || 'Не удалось загрузить звонки')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [historyMark, manager])

  const visible = useMemo(() => {
    if (filter === 'all') return calls
    return calls.filter((c) => c.kind === filter)
  }, [calls, filter])

  if (!manager) {
    return <Navigate to="/crm" replace />
  }

  const openCard = async (call: ManagerCall) => {
    if (openingRef.current) return
    openingRef.current = call.id
    setOpening(call.id)
    setSelected(call.id)
    setError('')
    try {
      let leadId = String(call.lead_id || '').trim()
      if (!leadId) {
        const res = await api<{ lead_id?: string }>('/calls/open-card', {
          method: 'POST',
          body: JSON.stringify({ call_id: call.id }),
        })
        leadId = String(res.lead_id || '').trim()
        if (leadId) {
          setCalls((prev) => prev.map((row) => (row.id === call.id ? { ...row, lead_id: leadId } : row)))
        }
      }
      if (!leadId) throw new Error('Не удалось открыть карточку')
      openLead(leadId)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось открыть карточку')
    } finally {
      openingRef.current = ''
      setOpening('')
    }
  }

  return (
    <div className="flex flex-col gap-4 pt-1 pb-4 min-h-[calc(100%-2rem)]">
      <div>
        <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">Звонки</h1>
        <p className="mt-1.5 text-[13px] text-white/65">Свои звонки · карточка открывается справа</p>
      </div>

      {error ? <p className="text-[13px] text-orange-200">{error}</p> : null}

      <div className="b24-glass-panel overflow-hidden flex flex-col min-h-[480px]">
        <div className="px-4 py-3 border-b border-black/6 bg-white/80 flex flex-wrap items-center gap-2">
          {filters.map((item) => {
            const active = filter === item.id
            const count =
              item.id === 'all' ? calls.length : calls.filter((c) => c.kind === item.id).length
            return (
              <button
                key={item.id}
                type="button"
                onClick={() => setFilter(item.id)}
                className={[
                  'h-10 px-3.5 rounded-[10px] text-[13px] font-medium border inline-flex items-center gap-1.5 transition',
                  active
                    ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep'
                    : 'bg-white border-black/8 text-b24-text hover:bg-[#f8fafb]',
                ].join(' ')}
              >
                {item.label}
                <span className="tabular-nums text-[12px] opacity-70">{count}</span>
              </button>
            )
          })}
        </div>

        <div className="overflow-x-auto b24-scrollbar flex-1">
          <table className="w-full min-w-[640px] text-left text-[13px]">
            <thead className="bg-b24-panel-header text-[12px] text-b24-text-muted">
              <tr>
                <th className="px-4 py-2.5 font-medium">Время</th>
                <th className="px-3 py-2.5 font-medium">Длительность</th>
                <th className="px-3 py-2.5 font-medium">Номер</th>
                <th className="px-3 py-2.5 font-medium">Проверка</th>
                <th className="px-3 py-2.5 font-medium" />
              </tr>
            </thead>
            <tbody className="divide-y divide-b24-border text-b24-text-dark">
              {loading && calls.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-12 text-center text-b24-text-muted">
                    Загрузка…
                  </td>
                </tr>
              ) : visible.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-12 text-center text-b24-text-muted">
                    Звонков нет
                  </td>
                </tr>
              ) : (
                visible.map((call) => {
                  const active = selected === call.id
                  const busy = opening === call.id
                  return (
                    <tr
                      key={call.id}
                      className={active ? 'bg-[#e8f8fe]' : 'hover:bg-[#f7fafc] transition-colors'}
                    >
                      <td className="px-4 py-3 whitespace-nowrap">
                        <div className="font-medium">{formatWhen(call.started_at)}</div>
                        <span
                          className={`mt-1 inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium ring-1 ${kindClass(call.kind)}`}
                        >
                          {kindLabel(call.kind)}
                        </span>
                      </td>
                      <td className="px-3 py-3 tabular-nums">{formatDur(call.duration_sec)}</td>
                      <td className="px-3 py-3 tabular-nums font-mono text-[13px]">
                        {call.tail ? `··${call.tail}` : '—'}
                      </td>
                      <td className="px-3 py-3 text-[12px]">
                        {reviewLabel(call.review_status) ? (
                          <span
                            className={
                              call.review_status === 'flagged'
                                ? 'text-rose-700'
                                : 'text-b24-text-muted'
                            }
                          >
                            {reviewLabel(call.review_status)}
                          </span>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="px-3 py-3 text-right">
                        <button
                          type="button"
                          disabled={Boolean(opening)}
                          onClick={() => void openCard(call)}
                          className="h-9 px-3 rounded-[10px] text-[13px] font-medium border transition bg-white border-black/8 text-b24-text hover:bg-[#f8fafb] disabled:opacity-60"
                        >
                          {busy ? 'Открываем…' : 'Открыть карточку'}
                        </button>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

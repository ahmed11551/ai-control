import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { PageHeader, PageShell } from '../../components/layout/PageHeader'
import { api } from '../../lib/api'
import { useAuth } from '../../store/auth'

type Preset = 'day' | 'week' | 'month' | 'custom'

type Group = {
  id: string
  name: string
  arrived: number
  in_work: number
  success: number
  rejected: number
  to_logist: number
}

type Analytics = {
  period: { preset: string; from: string; to: string }
  leads: { arrived: number; in_work: number; success: number; rejected: number; to_logist: number }
  by_manager: Group[]
  by_department: Group[]
  by_source: Group[]
  calls: {
    count: number
    total_duration_sec: number
    avg_duration_sec: number
    connected: number
    connect_rate: number
    flagged: number
    by_disposition: { name: string; count: number }[]
  }
  funnel: { key: string; name: string; count: number; share: number }[]
  incidents: { total: number; open: number; by_type: { name: string; count: number }[] }
}

type Meta = {
  departments?: { id: string; name: string }[]
  managers?: { id: string; full_name: string; department_id: string }[]
  locked_department_id?: string
  locked_user_id?: string
}

const presets: { id: Preset; label: string }[] = [
  { id: 'day', label: 'Сегодня' },
  { id: 'week', label: 'Неделя' },
  { id: 'month', label: 'Месяц' },
  { id: 'custom', label: 'Период' },
]

const leadCards: { key: keyof Analytics['leads']; label: string }[] = [
  { key: 'arrived', label: 'Пришло' },
  { key: 'in_work', label: 'В работе' },
  { key: 'success', label: 'Успешно' },
  { key: 'rejected', label: 'Отказ' },
  { key: 'to_logist', label: 'У логиста' },
]

function n(value: number | undefined) {
  return new Intl.NumberFormat('ru-RU').format(Number(value) || 0)
}

function pct(value: number | undefined) {
  return `${Math.round((Number(value) || 0) * 1000) / 10}%`
}

function dur(sec: number) {
  const s = Math.max(0, Math.round(Number(sec) || 0))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const r = s % 60
  if (h > 0) return `${h} ч ${m} мин`
  if (m > 0) return `${m} мин ${r} с`
  return `${r} с`
}

function periodLabel(period?: Analytics['period']) {
  if (!period?.from) return ''
  const fmt = (raw: string) => {
    const d = new Date(raw)
    if (Number.isNaN(d.getTime())) return raw
    return d.toLocaleDateString('ru-RU')
  }
  const end = new Date(period.to)
  if (!Number.isNaN(end.getTime())) end.setDate(end.getDate() - 1)
  const endLabel = Number.isNaN(end.getTime()) ? fmt(period.to) : end.toLocaleDateString('ru-RU')
  return `${fmt(period.from)} — ${endLabel}`
}

export function AnalyticsPage() {
  const { user } = useAuth()
  const role = String(user?.role || '').toLowerCase()
  const canFilter = role === 'admin' || role === 'rop'
  const [preset, setPreset] = useState<Preset>('month')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [departmentId, setDepartmentId] = useState('')
  const [managerId, setManagerId] = useState('')
  const [group, setGroup] = useState<'manager' | 'department' | 'source'>('manager')
  const [meta, setMeta] = useState<Meta>({})
  const [data, setData] = useState<Analytics | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!canFilter) return
    api<Meta>('/analytics/meta')
      .then((res) => {
        setMeta(res)
        if (res.locked_department_id) setDepartmentId(res.locked_department_id)
      })
      .catch(() => setMeta({}))
  }, [canFilter])

  useEffect(() => {
    const params = new URLSearchParams({ preset })
    if (preset === 'custom') {
      if (!from || !to) return
      params.set('from', from)
      params.set('to', to)
    }
    if (departmentId) params.set('department_id', departmentId)
    if (managerId) params.set('manager_id', managerId)
    setLoading(true)
    setError('')
    api<Analytics>(`/analytics?${params.toString()}`)
      .then(setData)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [preset, from, to, departmentId, managerId])

  const managers = useMemo(() => {
    const list = meta.managers || []
    if (!departmentId) return list
    return list.filter((m) => m.department_id === departmentId)
  }, [meta.managers, departmentId])

  const rows = group === 'department' ? data?.by_department : group === 'source' ? data?.by_source : data?.by_manager

  return (
    <PageShell>
      <PageHeader
        title="Аналитика"
        subtitle="Лиды воронки «Адаптация», звонки и инциденты. Период считается по Москве."
      />

      <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm flex flex-wrap items-end gap-3">
        <div className="flex flex-wrap gap-2">
          {presets.map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={() => setPreset(item.id)}
              className={[
                'h-9 px-3 rounded-[10px] text-[13px] font-medium border',
                preset === item.id
                  ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep'
                  : 'bg-white border-black/10 text-b24-text hover:bg-[#f8fafb]',
              ].join(' ')}
            >
              {item.label}
            </button>
          ))}
        </div>
        {preset === 'custom' ? (
          <>
            <label className="text-[12px] text-b24-text-muted">
              С
              <input
                type="date"
                value={from}
                onChange={(e) => setFrom(e.target.value)}
                className="mt-1 block h-9 rounded-[10px] border border-black/10 px-2 text-[13px] text-b24-text-dark"
              />
            </label>
            <label className="text-[12px] text-b24-text-muted">
              По
              <input
                type="date"
                value={to}
                onChange={(e) => setTo(e.target.value)}
                className="mt-1 block h-9 rounded-[10px] border border-black/10 px-2 text-[13px] text-b24-text-dark"
              />
            </label>
          </>
        ) : null}
        {canFilter ? (
          <>
            {role === 'admin' ? (
              <label className="text-[12px] text-b24-text-muted">
                Отдел
                <select
                  value={departmentId}
                  onChange={(e) => {
                    setDepartmentId(e.target.value)
                    setManagerId('')
                  }}
                  className="mt-1 block h-9 min-w-[180px] rounded-[10px] border border-black/10 px-2 text-[13px] text-b24-text-dark"
                >
                  <option value="">Все отделы</option>
                  {(meta.departments || []).map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.name}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}
            <label className="text-[12px] text-b24-text-muted">
              Менеджер
              <select
                value={managerId}
                onChange={(e) => setManagerId(e.target.value)}
                className="mt-1 block h-9 min-w-[180px] rounded-[10px] border border-black/10 px-2 text-[13px] text-b24-text-dark"
              >
                <option value="">Все</option>
                {managers.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.full_name}
                  </option>
                ))}
              </select>
            </label>
          </>
        ) : null}
        <div className="ml-auto text-[13px] text-b24-text-muted">{periodLabel(data?.period)}</div>
      </section>

      {error ? <p className="text-[13px] text-rose-700">{error}</p> : null}

      <div className="grid grid-cols-2 lg:grid-cols-5 gap-3">
        {leadCards.map((card) => (
          <div key={card.key} className="rounded-[14px] bg-white px-4 py-4 text-b24-text-dark shadow-sm">
            <div className="text-[12px] text-b24-text-muted">{card.label}</div>
            <div className="mt-2 text-[26px] font-semibold tabular-nums">
              {loading && !data ? '…' : n(data?.leads[card.key])}
            </div>
          </div>
        ))}
      </div>
      <p className="text-[12px] text-white/70 -mt-1">
        Карточки считаются по сделкам «Адаптации», созданным в выбранном периоде. «У логиста» — текущая передача, без успешных и отказов.
      </p>

      <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-[16px] font-semibold">Группировка</h2>
          <div className="flex gap-2">
            {(
              [
                ['manager', 'Менеджер'],
                ['department', 'Отдел'],
                ['source', 'Источник'],
              ] as const
            ).map(([id, label]) => (
              <button
                key={id}
                type="button"
                onClick={() => setGroup(id)}
                className={[
                  'h-8 px-3 rounded-[10px] text-[12px] border',
                  group === id ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep' : 'border-black/10',
                ].join(' ')}
              >
                {label}
              </button>
            ))}
          </div>
        </div>
        <div className="mt-3 overflow-x-auto">
          <table className="w-full min-w-[640px] text-left text-[13px]">
            <thead className="text-[12px] text-b24-text-muted">
              <tr>
                <th className="py-2 font-medium"> </th>
                <th className="py-2 font-medium">Пришло</th>
                <th className="py-2 font-medium">В работе</th>
                <th className="py-2 font-medium">Успешно</th>
                <th className="py-2 font-medium">Отказ</th>
                <th className="py-2 font-medium">Логист</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-black/5">
              {(rows || []).length === 0 ? (
                <tr>
                  <td colSpan={6} className="py-6 text-b24-text-muted">
                    За период нет сделок
                  </td>
                </tr>
              ) : (
                (rows || []).map((row) => (
                  <tr key={`${row.id}-${row.name}`}>
                    <td className="py-2 pr-3 font-medium">{row.name || '—'}</td>
                    <td className="py-2 tabular-nums">{n(row.arrived)}</td>
                    <td className="py-2 tabular-nums">{n(row.in_work)}</td>
                    <td className="py-2 tabular-nums">{n(row.success)}</td>
                    <td className="py-2 tabular-nums">{n(row.rejected)}</td>
                    <td className="py-2 tabular-nums">{n(row.to_logist)}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
        <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
          <h2 className="text-[16px] font-semibold">Звонки</h2>
          <div className="mt-3 grid grid-cols-2 gap-3 text-[13px]">
            <div>
              <div className="text-b24-text-muted">Количество</div>
              <div className="text-[22px] font-semibold tabular-nums">{n(data?.calls.count)}</div>
            </div>
            <div>
              <div className="text-b24-text-muted">Дозвон</div>
              <div className="text-[22px] font-semibold tabular-nums">{pct(data?.calls.connect_rate)}</div>
            </div>
            <div>
              <div className="text-b24-text-muted">Средняя длительность</div>
              <div className="text-[18px] font-semibold">{dur(data?.calls.avg_duration_sec || 0)}</div>
            </div>
            <div>
              <div className="text-b24-text-muted">Суммарно</div>
              <div className="text-[18px] font-semibold">{dur(data?.calls.total_duration_sec || 0)}</div>
            </div>
          </div>
          <div className="mt-3 text-[13px]">
            С запрещённой фразой: <span className="font-semibold">{n(data?.calls.flagged)}</span>
          </div>
          <ul className="mt-3 flex flex-col gap-1 text-[13px]">
            {(data?.calls.by_disposition || []).map((row) => (
              <li key={row.name} className="flex justify-between gap-3">
                <span>{row.name}</span>
                <span className="tabular-nums">{n(row.count)}</span>
              </li>
            ))}
          </ul>
        </section>

        <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
          <h2 className="text-[16px] font-semibold">Воронка</h2>
          <p className="mt-1 text-[12px] text-b24-text-muted">Доля сделок периода, которые сейчас на стадии.</p>
          <ul className="mt-3 flex flex-col gap-2">
            {(data?.funnel || []).map((stage) => (
              <li key={stage.key || stage.name}>
                <div className="flex justify-between text-[13px]">
                  <span>{stage.name}</span>
                  <span className="tabular-nums">
                    {n(stage.count)} · {pct(stage.share)}
                  </span>
                </div>
                <div className="mt-1 h-1.5 rounded-full bg-black/5">
                  <div
                    className="h-1.5 rounded-full bg-b24-cyan"
                    style={{ width: `${Math.min(100, Math.round((Number(stage.share) || 0) * 100))}%` }}
                  />
                </div>
              </li>
            ))}
          </ul>
        </section>
      </div>

      <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-[16px] font-semibold">Инциденты безопасности</h2>
          {role === 'admin' || role === 'rop' ? (
            <Link to="/security" className="text-[13px] text-b24-cyan-deep">
              Открыть список
            </Link>
          ) : null}
        </div>
        <div className="mt-2 text-[13px]">
          Открытых {n(data?.incidents.open)} · всего за период {n(data?.incidents.total)}
        </div>
        <ul className="mt-2 flex flex-wrap gap-2">
          {(data?.incidents.by_type || []).map((row) => (
            <li key={row.name} className="rounded-full bg-[#f4f7f8] px-3 py-1 text-[12px]">
              {incidentTypeLabel(row.name)} · {n(row.count)}
            </li>
          ))}
        </ul>
      </section>
    </PageShell>
  )
}

export function incidentTypeLabel(type: string) {
  switch (type) {
    case 'message_blocked':
      return 'Сообщение'
    case 'call_phrase':
      return 'Звонок'
    case 'sensitive_denied':
      return 'Документ, отказ'
    case 'sensitive_view':
      return 'Документ, просмотр'
    case 'mass_view':
      return 'Массовый просмотр'
    case 'restriction_lifted':
      return 'Ограничение снято'
    default:
      return type || '—'
  }
}

import {
  Ban,
  CheckCircle2,
  Phone,
  PhoneCall,
  PhoneMissed,
  PhoneOff,
  Search,
  Settings,
  UserCheck,
  Users,
} from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { AutodialScheduleModal } from '../../components/telephony/AutodialScheduleModal'
import { PeriodFilter, type PeriodDays } from '../../components/ui/PeriodFilter'
import { api } from '../../lib/api'
import { canSeePhones, maskPhone } from '../../lib/roles'
import { useAuth } from '../../store/auth'
import { AUTODIAL_ENABLED } from '../../telephony/config'

type HistoryItem = {
  id: string
  deal_id: string
  status: string
  call_id?: string
  cli?: string
  error?: string
  started_at: string
  finished_at?: string
  accepted_at?: string
  accepted_by?: string
  title?: string
  contact?: string
  phone?: string
  operator?: string
  duration_sec?: number
  talk_sec?: number
}

type Stats = {
  total: number
  active: number
  connected: number
  accepted: number
  miss: number
  dialed: number
}

type TabId = 'all' | 'active' | 'connected' | 'accepted' | 'miss'

type HistoryResp = {
  items: HistoryItem[]
  total: number
  stats: Stats
  tab: string
}

function formatWhen(raw?: string) {
  if (!raw) return '—'
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return '—'
  const now = new Date()
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  const time = d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
  if (sameDay) return `Сегодня, ${time}`
  return d.toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatDur(sec?: number) {
  const n = Math.max(0, Math.floor(sec || 0))
  const m = Math.floor(n / 60)
  const s = n % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

function formatPhone(raw: string, canSee: boolean) {
  const digits = raw.replace(/\D/g, '')
  if (!digits) return '—'
  const shown = canSee ? digits : maskPhone(digits)
  if (shown.length === 11 && shown.startsWith('7')) {
    return `+7 ${shown.slice(1, 4)} ${shown.slice(4, 7)}-${shown.slice(7, 9)}-${shown.slice(9)}`
  }
  return canSee ? raw : shown
}

function causeFromError(error?: string) {
  const e = String(error || '')
  const m = e.match(/cause=([A-Z0-9_]+)/i)
  return m?.[1] || ''
}

type StatusMeta = {
  label: string
  result: string
  tone: string
  iconBg: string
  icon: ReactNode
  rowAccent: string
}

function statusMeta(item: HistoryItem): StatusMeta {
  const st = String(item.status || '').toLowerCase()
  const cause = causeFromError(item.error)
  const talk = item.talk_sec || item.duration_sec || 0
  const talkLabel = talk > 0 ? `Разговор ${formatDur(talk)}` : ''

  if (!item.finished_at && ['started', 'dialing', 'ringing'].includes(st)) {
    return {
      label: 'Звонок…',
      result: 'Идёт дозвон',
      tone: 'bg-[#e8f4fc] text-[#0b6e99] ring-[#b7dff2]',
      iconBg: 'bg-[#d5eef9] text-[#0b6e99]',
      icon: <PhoneCall size={14} strokeWidth={2.4} />,
      rowAccent: 'border-l-[#3aa6d8]',
    }
  }
  if (st === 'connected' && item.accepted_by) {
    return {
      label: 'Принят',
      result: talkLabel || 'Оператор на линии',
      tone: 'bg-emerald-50 text-emerald-800 ring-emerald-200',
      iconBg: 'bg-emerald-100 text-emerald-700',
      icon: <CheckCircle2 size={14} strokeWidth={2.4} />,
      rowAccent: 'border-l-emerald-500',
    }
  }
  if (st === 'connected') {
    return {
      label: 'Разговор',
      result: talkLabel || 'Кандидат ответил',
      tone: 'bg-emerald-50 text-emerald-800 ring-emerald-200',
      iconBg: 'bg-emerald-100 text-emerald-700',
      icon: <Phone size={14} strokeWidth={2.4} />,
      rowAccent: 'border-l-emerald-400',
    }
  }
  if (st === 'no_answer') {
    return {
      label: 'Нет ответа',
      result: cause === 'ALLOTTED_TIMEOUT' ? 'Таймаут ожидания' : 'Перезвонить',
      tone: 'bg-rose-50 text-rose-700 ring-rose-200',
      iconBg: 'bg-rose-100 text-rose-600',
      icon: <PhoneMissed size={14} strokeWidth={2.4} />,
      rowAccent: 'border-l-rose-400',
    }
  }
  if (st === 'rejected' || st === 'canceled') {
    return {
      label: 'Отказ',
      result: cause || 'Сброс / отмена',
      tone: 'bg-slate-100 text-slate-700 ring-slate-200',
      iconBg: 'bg-slate-200 text-slate-600',
      icon: <PhoneOff size={14} strokeWidth={2.4} />,
      rowAccent: 'border-l-slate-400',
    }
  }
  if (st === 'failed' || st === 'error') {
    const unallocated = /UNALLOCATED/i.test(cause) || /UNALLOCATED/i.test(item.error || '')
    return {
      label: unallocated ? 'Недоступен' : 'Ошибка',
      result: unallocated ? 'Номер не существует' : cause || item.error || 'Сбой дозвона',
      tone: 'bg-amber-50 text-amber-900 ring-amber-200',
      iconBg: 'bg-amber-100 text-amber-800',
      icon: <Ban size={14} strokeWidth={2.4} />,
      rowAccent: 'border-l-amber-400',
    }
  }
  return {
    label: item.status || '—',
    result: cause || '—',
    tone: 'bg-slate-100 text-slate-600 ring-slate-200',
    iconBg: 'bg-slate-200 text-slate-600',
    icon: <Phone size={14} strokeWidth={2.4} />,
    rowAccent: 'border-l-slate-300',
  }
}

function initial(name: string) {
  const t = name.trim()
  return (t.slice(0, 1) || '?').toUpperCase()
}

export function AutodialHistoryPage() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const [tab, setTab] = useState<TabId>('all')
  const [days, setDays] = useState<PeriodDays>(1)
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')
  const [data, setData] = useState<HistoryResp | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [scheduleOpen, setScheduleOpen] = useState(false)

  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(query.trim()), 280)
    return () => window.clearTimeout(t)
  }, [query])

  useEffect(() => {
    if (!AUTODIAL_ENABLED) {
      setLoading(false)
      return
    }
    let alive = true
    setLoading(true)
    setError('')
    const qs = new URLSearchParams({ tab, limit: '100' })
    if (days > 0) qs.set('days', String(days))
    if (debounced) qs.set('q', debounced)
    void api<HistoryResp>(`/telephony/autodial/history?${qs}`)
      .then((r) => {
        if (alive) setData(r)
      })
      .catch((e: Error) => {
        if (alive) setError(e.message)
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [tab, days, debounced])

  if (!AUTODIAL_ENABLED) {
    return (
      <div className="flex flex-col gap-4 pt-1 pb-4">
        <div className="b24-glass-panel p-6 max-w-xl">
          <div className="text-[18px] font-semibold text-b24-text-dark">Скорозвон приостановлен</div>
          <p className="mt-2 text-[14px] text-b24-text-muted">
            Функция временно выключена. Код сохранён — включится, когда снимем паузу.
          </p>
          <button
            type="button"
            className="mt-4 h-10 rounded-[10px] px-4 text-[13px] font-medium bg-white border border-black/10"
            onClick={() => navigate('/crm')}
          >
            В CRM
          </button>
        </div>
      </div>
    )
  }

  const stats = data?.stats
  const canSee = canSeePhones(user)

  const tabs = useMemo(
    () => [
      { id: 'all' as const, label: 'Все', count: stats?.total ?? 0 },
      { id: 'active' as const, label: 'В работе', count: stats?.active ?? 0 },
      { id: 'connected' as const, label: 'Разговор', count: stats?.connected ?? 0 },
      { id: 'accepted' as const, label: 'Принято', count: stats?.accepted ?? 0 },
      { id: 'miss' as const, label: 'Недозвон', count: stats?.miss ?? 0 },
    ],
    [stats],
  )

  const cards = [
    { label: 'Всего', value: stats?.total ?? 0, hint: 'попыток скорозвона', icon: Phone },
    { label: 'Прозвонено', value: stats?.dialed ?? 0, hint: 'с попыткой дозвона', icon: PhoneCall },
    { label: 'Разговор', value: stats?.connected ?? 0, hint: 'кандидат ответил', icon: Users },
    { label: 'Принято', value: stats?.accepted ?? 0, hint: 'оператор забрал', icon: UserCheck },
  ]

  return (
    <div className="flex flex-col gap-3 pt-1 pb-4 min-h-[calc(100%-2rem)]">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">Скорозвон</h1>
          <p className="mt-1.5 text-[13px] text-white/65">
            Холодная база · Только звонки · Операторы
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => setScheduleOpen(true)}
            className="inline-flex h-10 items-center gap-2 rounded-[10px] border border-white/15 bg-white/12 px-3 text-[13px] font-medium text-white/90 backdrop-blur-sm transition hover:bg-white/18"
          >
            <Settings size={15} strokeWidth={2} />
            Настройки
          </button>
          <PeriodFilter value={days} onChange={setDays} tone="dark" />
        </div>
      </div>

      <div className="grid grid-cols-2 gap-2.5 lg:grid-cols-4">
        {cards.map((card) => (
          <div
            key={card.label}
            className="rounded-[14px] border border-white/10 bg-white/12 px-4 py-3 backdrop-blur-sm"
          >
            <div className="flex items-center justify-between gap-2">
              <div className="text-[11px] tracking-wide text-white/55 uppercase">{card.label}</div>
              <card.icon size={14} className="text-white/40" strokeWidth={2} />
            </div>
            <div className="mt-1 text-[26px] font-semibold leading-none text-white tabular-nums">
              {card.value.toLocaleString('ru-RU')}
            </div>
            <div className="mt-1 text-[11px] text-white/45">{card.hint}</div>
          </div>
        ))}
      </div>

      <div className="b24-glass-panel flex min-h-[480px] flex-col overflow-hidden">
        <div className="flex flex-col gap-3 border-b border-black/6 bg-white/80 px-4 py-3">
          <div className="flex flex-wrap items-center gap-2">
            <div className="inline-flex overflow-hidden rounded-[10px] border border-black/8 bg-white">
              {tabs.map((t) => {
                const active = tab === t.id
                return (
                  <button
                    key={t.id}
                    type="button"
                    onClick={() => setTab(t.id)}
                    className={[
                      'h-10 px-3 text-[13px] font-medium transition',
                      active ? 'bg-b24-text-dark text-white' : 'text-b24-text hover:bg-[#f5f7f9]',
                    ].join(' ')}
                  >
                    {t.label}
                    <span className={active ? 'ml-1.5 text-white/70' : 'ml-1.5 text-b24-text-muted'}>
                      {t.count.toLocaleString('ru-RU')}
                    </span>
                  </button>
                )
              })}
            </div>

            <div className="relative min-w-[200px] flex-1">
              <Search
                size={16}
                className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-b24-text-muted"
              />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Поиск по номеру…"
                className="h-10 w-full rounded-[10px] border border-black/8 bg-white pr-3 pl-9 text-[13px] text-b24-text outline-none focus:border-b24-cyan/40"
              />
            </div>
          </div>
        </div>

        {error ? (
          <div className="px-4 py-6 text-[13px] text-rose-600">{error}</div>
        ) : loading && !data ? (
          <div className="px-4 py-10 text-center text-[13px] text-b24-text-muted">Загрузка…</div>
        ) : !data?.items.length ? (
          <div className="flex flex-col items-center gap-2 px-4 py-16 text-center">
            <PhoneMissed size={28} className="text-b24-text-muted/50" />
            <div className="text-[15px] font-medium text-b24-text">Пока пусто</div>
            <div className="text-[13px] text-b24-text-muted">
              За выбранный период скорозвонов нет
            </div>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[820px] border-collapse text-left">
              <thead>
                <tr className="border-b border-black/6 bg-[#f7f9fb] text-[11px] font-semibold tracking-wide text-b24-text-muted uppercase">
                  <th className="w-12 px-4 py-3 font-semibold">№</th>
                  <th className="px-4 py-3 font-semibold">Номер</th>
                  <th className="px-4 py-3 font-semibold">Статус</th>
                  <th className="px-4 py-3 font-semibold">Результат</th>
                  <th className="px-4 py-3 font-semibold">Оператор</th>
                  <th className="px-4 py-3 font-semibold">Время</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((item, idx) => {
                  const meta = statusMeta(item)
                  const name = (item.contact || item.title || 'Кандидат').trim()
                  const op = (item.operator || '').trim()
                  const isLive = !item.finished_at && ['started', 'dialing', 'ringing', 'connected'].includes(
                    String(item.status || '').toLowerCase(),
                  )
                  return (
                    <tr
                      key={item.id}
                      className={[
                        'group cursor-pointer border-b border-black/5 border-l-[3px] transition',
                        meta.rowAccent,
                        isLive ? 'bg-[#f3faff]' : 'bg-white hover:bg-[#eef7fc]',
                      ].join(' ')}
                      onClick={() => navigate(`/crm?open=${item.deal_id}`)}
                    >
                      <td className="px-4 py-3.5 text-[13px] font-medium tabular-nums text-b24-text-muted">
                        {idx + 1}
                      </td>
                      <td className="px-4 py-3.5">
                        <div className="flex items-center gap-3">
                          <span
                            className={[
                              'flex size-9 shrink-0 items-center justify-center rounded-[10px]',
                              meta.iconBg,
                            ].join(' ')}
                          >
                            {meta.icon}
                          </span>
                          <div className="min-w-0">
                            <div className="font-mono text-[14px] font-semibold tracking-tight text-b24-text-dark">
                              {formatPhone(item.phone || '', canSee)}
                            </div>
                            <div className="mt-0.5 max-w-[200px] truncate text-[12px] text-b24-text-muted">
                              {name}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3.5">
                        <span
                          className={[
                            'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[12px] font-semibold ring-1 ring-inset',
                            meta.tone,
                          ].join(' ')}
                        >
                          {meta.label}
                        </span>
                      </td>
                      <td className="px-4 py-3.5">
                        <div className="text-[13px] font-medium text-b24-text-dark">{meta.result}</div>
                      </td>
                      <td className="px-4 py-3.5">
                        {op ? (
                          <div className="inline-flex items-center gap-2.5">
                            <span className="flex size-8 items-center justify-center rounded-full bg-gradient-to-br from-[#d9f0fb] to-[#c5e4f5] text-[12px] font-bold text-[#14607e] shadow-[inset_0_1px_0_rgba(255,255,255,0.7)]">
                              {initial(op)}
                            </span>
                            <div className="min-w-0">
                              <div className="truncate text-[13px] font-semibold text-b24-text-dark">
                                {op.split(' ')[0]}
                              </div>
                              <div className="text-[11px] text-b24-text-muted">оператор</div>
                            </div>
                          </div>
                        ) : (
                          <span className="text-[13px] text-b24-text-muted">—</span>
                        )}
                      </td>
                      <td className="px-4 py-3.5 whitespace-nowrap">
                        <div className="text-[13px] font-medium text-b24-text-dark">
                          {formatWhen(item.started_at)}
                        </div>
                        {item.finished_at ? (
                          <div className="mt-0.5 text-[11px] text-b24-text-muted">
                            до {formatWhen(item.finished_at).replace('Сегодня, ', '')}
                          </div>
                        ) : isLive ? (
                          <div className="mt-0.5 text-[11px] font-medium text-sky-600">сейчас</div>
                        ) : null}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <AutodialScheduleModal open={scheduleOpen} onClose={() => setScheduleOpen(false)} />
    </div>
  )
}

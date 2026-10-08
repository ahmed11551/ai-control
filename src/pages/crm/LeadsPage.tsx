import { Building2, Phone, Plus, Search, Trash2, UserRound, Users } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { LeadFormModal } from '../../components/crm/LeadFormModal'
import { Button } from '../../components/ui/Button'
import { ListPager } from '../../components/ui/ListPager'
import { PeriodFilter, type PeriodDays } from '../../components/ui/PeriodFilter'
import { api } from '../../lib/api'
import {
  type CrmRow,
  leadStatusLabel,
  sourceLabel,
} from '../../lib/crm'
import { type Page, qs } from '../../lib/page'
import { isManagerRole } from '../../lib/roles'
import { useAuth } from '../../store/auth'
import { useCrmLeadDrawer } from '../../store/crmLeadDrawer'
import { useOpenId } from './useOpenId'

function formatPhone(raw: string) {
  const d = raw.replace(/\D/g, '')
  if (d.length === 11 && (d[0] === '7' || d[0] === '8')) {
    const n = '7' + d.slice(1)
    return `+${n[0]} ${n.slice(1, 4)} ${n.slice(4, 7)}-${n.slice(7, 9)}-${n.slice(9, 11)}`
  }
  if (d.length === 10) {
    return `+7 ${d.slice(0, 3)} ${d.slice(3, 6)}-${d.slice(6, 8)}-${d.slice(8, 10)}`
  }
  return raw || '—'
}

function statusTone(status: string) {
  switch (status) {
    case 'new':
      return 'bg-sky-50 text-sky-700 ring-sky-200'
    case 'in_progress':
      return 'bg-amber-50 text-amber-800 ring-amber-200'
    case 'qualified':
      return 'bg-emerald-50 text-emerald-800 ring-emerald-200'
    case 'rejected':
      return 'bg-rose-50 text-rose-700 ring-rose-200'
    default:
      return 'bg-slate-100 text-slate-600 ring-slate-200'
  }
}

function relativeCreated(value: unknown) {
  const raw = String(value ?? '')
  if (!raw) return '—'
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return '—'
  const diff = Date.now() - date.getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'только что'
  if (mins < 60) return `${mins} мин назад`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours} ч назад`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days} дн назад`
  return date.toLocaleDateString('ru-RU', { day: '2-digit', month: 'short' })
}

function initial(row: CrmRow) {
  const t = String(row.contact || row.title || '?')
  return t.slice(0, 1).toUpperCase()
}

export function LeadsPage() {
  const { user } = useAuth()
  const { openLead, closeLead, leadId: drawerLeadId, revision, setEditHandler } = useCrmLeadDrawer()
  const isAdmin = String(user?.role || '').toLowerCase() === 'admin'
  const isManager = isManagerRole(user?.role)
  const isRop = String(user?.role || '').toLowerCase() === 'rop'
  const canCreateLeads = !isManager
  const [openId, setOpenId] = useOpenId()
  const [rows, setRows] = useState<CrmRow[]>([])
  const [total, setTotal] = useState(0)
  const [limit, setLimit] = useState(20)
  const [offset, setOffset] = useState(0)
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')
  const [mine, setMine] = useState(false)
  const [queue, setQueue] = useState(false)
  const [statusFilter, setStatusFilter] = useState('')
  const [period, setPeriod] = useState<PeriodDays>(0)
  const [sort, setSort] = useState('created_at')
  const [dir, setDir] = useState<'ASC' | 'DESC'>('DESC')
  const [error, setError] = useState('')
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<CrmRow | null>(null)
  const [pending, setPending] = useState(false)
  const [formError, setFormError] = useState('')
  const [loading, setLoading] = useState(false)
  const [stats, setStats] = useState({ all: 0, new: 0, in_progress: 0, qualified: 0 })

  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(query.trim()), 280)
    return () => window.clearTimeout(t)
  }, [query])

  useEffect(() => {
    setOffset(0)
  }, [debounced, mine, queue, statusFilter, sort, dir, limit, period])

  const load = useCallback(async (opts?: { quiet?: boolean }) => {
    if (!opts?.quiet) setLoading(true)
    try {
      const data = await api<Page<CrmRow>>(
        `/leads${qs({
          q: debounced,
          limit,
          offset,
          sort,
          dir,
          mine: mine ? 1 : undefined,
          queue: queue ? 1 : undefined,
          status: statusFilter || undefined,
          days: period || undefined,
        })}`,
      )
      setRows(data.items ?? [])
      setTotal(data.total ?? 0)
    } finally {
      if (!opts?.quiet) setLoading(false)
    }
  }, [debounced, limit, offset, sort, dir, mine, queue, statusFilter, period])

  const loadStats = useCallback(async () => {
    try {
      const base = { mine: mine ? 1 : undefined, queue: queue ? 1 : undefined, days: period || undefined }
      const [all, neu, work, qual] = await Promise.all([
        api<Page<CrmRow>>(`/leads${qs({ limit: 1, offset: 0, ...base })}`),
        api<Page<CrmRow>>(`/leads${qs({ limit: 1, offset: 0, status: 'new', ...base })}`),
        api<Page<CrmRow>>(`/leads${qs({ limit: 1, offset: 0, status: 'in_progress', ...base })}`),
        api<Page<CrmRow>>(`/leads${qs({ limit: 1, offset: 0, status: 'qualified', ...base })}`),
      ])
      setStats({
        all: all.total ?? 0,
        new: neu.total ?? 0,
        in_progress: work.total ?? 0,
        qualified: qual.total ?? 0,
      })
    } catch {
      /* ignore */
    }
  }, [mine, queue, period])

  useEffect(() => {
    load().catch((e: Error) => setError(e.message))
  }, [load])

  useEffect(() => {
    void loadStats()
  }, [loadStats, rows.length])

  useEffect(() => {
    if (revision === 0) return
    void load({ quiet: true })
    void loadStats()
  }, [revision, load, loadStats])

  const openCreate = () => {
    setEditing(null)
    setFormError('')
    setFormOpen(true)
  }

  const openEdit = (row: CrmRow) => {
    setEditing(row)
    setFormError('')
    setFormOpen(true)
  }

  useEffect(() => {
    setEditHandler((id) => {
      const row = rows.find((r) => String(r.id) === id)
      openEdit(row ?? { id })
    })
    return () => setEditHandler(null)
  }, [rows, setEditHandler])

  // URL ?open=leadId ↔ глобальная карточка (не размонтируется при уходе в Найм)
  useEffect(() => {
    if (openId) {
      if (drawerLeadId !== openId) {
        const row = rows.find((r) => String(r.id) === openId)
        openLead(openId, row ?? null)
      }
      return
    }
    // URL очищен локально — карточку не трогаем, если уже ушли в /crm (конвертация)
    if (drawerLeadId && window.location.pathname.startsWith('/crm/leads')) {
      closeLead()
    }
  }, [openId, drawerLeadId, rows, openLead, closeLead])

  const submit = async (values: Record<string, string>) => {
    setPending(true)
    setFormError('')
    try {
      if (editing) {
        await api(`/leads/${editing.id}`, {
          method: 'PATCH',
          body: JSON.stringify({
            title: values.title,
            city: values.city,
            candidate: values.candidate,
            card_note: values.card_note || '',
          }),
        })
        setFormOpen(false)
        setEditing(null)
        await load()
        await loadStats()
      } else {
        const res = await api<{ id: string }>('/leads', { method: 'POST', body: JSON.stringify(values) })
        setFormOpen(false)
        await load()
        await loadStats()
        if (res.id) setOpenId(res.id)
      }
    } catch (e) {
      setFormError(e instanceof Error ? e.message : 'Ошибка сохранения')
    } finally {
      setPending(false)
    }
  }

  const removeLead = async (id: string, title: string, e?: React.MouseEvent) => {
    e?.preventDefault()
    e?.stopPropagation()
    if (!window.confirm(`Удалить заявку «${title || 'без названия'}»?`)) return
    try {
      await api(`/leads/${id}`, { method: 'DELETE' })
      if (openId === id) setOpenId(null)
      if (drawerLeadId === id) closeLead()
      await load()
      await loadStats()
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Не удалось удалить'
      setError(msg)
      window.alert(msg)
    }
  }

  const formInitial = useMemo((): Record<string, string> => {
    if (!editing) return {}
    return {
      title: String(editing.title ?? ''),
      candidate: String(editing.contact ?? ''),
      phone: String(editing.contact_phone ?? ''),
      contact_phone: String(editing.contact_phone ?? ''),
      city: String(editing.city ?? ''),
      card_note: String(editing.card_note ?? ''),
    }
  }, [editing])

  const tabs = [
    { id: '', label: 'Все', count: stats.all },
    { id: 'new', label: 'Новые', count: stats.new },
    { id: 'in_progress', label: 'В работе', count: stats.in_progress },
    { id: 'qualified', label: 'Квалиф.', count: stats.qualified },
  ]

  return (
    <div className="flex flex-col gap-4 pt-1 pb-4 min-h-[calc(100%-2rem)]">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">Лиды</h1>
          <p className="mt-1.5 text-[13px] text-white/65">
            {isManager
              ? `Ваши лиды · ${total.toLocaleString('ru-RU')}`
              : `Входящие заявки · ${total.toLocaleString('ru-RU')} в списке`}
          </p>
        </div>
        {canCreateLeads ? (
          <Button
            type="button"
            variant="green"
            className="h-10 rounded-[10px] px-4 shadow-[0_8px_24px_rgba(47,192,110,0.35)]"
            onClick={openCreate}
            leftIcon={<Plus size={16} strokeWidth={2.5} />}
          >
            Новый лид
          </Button>
        ) : null}
      </div>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-2.5">
        {[
          { label: 'Всего', value: stats.all, hint: 'активных' },
          { label: 'Новые', value: stats.new, hint: 'ждут разбора' },
          { label: 'В работе', value: stats.in_progress, hint: 'на связи' },
          { label: 'Квалиф.', value: stats.qualified, hint: 'готовы к сделке' },
        ].map((card) => (
          <div
            key={card.label}
            className="rounded-[14px] bg-white/12 border border-white/10 px-4 py-3 backdrop-blur-sm"
          >
            <div className="text-[11px] uppercase tracking-wide text-white/55">{card.label}</div>
            <div className="mt-1 text-[26px] font-semibold text-white tabular-nums leading-none">
              {card.value.toLocaleString('ru-RU')}
            </div>
            <div className="mt-1 text-[11px] text-white/45">{card.hint}</div>
          </div>
        ))}
      </div>

      <div className="b24-glass-panel overflow-hidden flex flex-col min-h-[480px]">
        <div className="px-4 py-3 border-b border-black/6 bg-white/80 flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex-1 min-w-[220px] relative">
              <Search
                size={16}
                className="absolute left-3 top-1/2 -translate-y-1/2 text-b24-text-muted pointer-events-none"
              />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Поиск по имени, телефону, компании…"
                className="h-10 w-full rounded-[10px] border border-black/8 bg-[#f5f7f9] pl-9 pr-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan focus:bg-white"
              />
            </div>
            <button
              type="button"
              onClick={() => setMine((v) => !v)}
              className={[
                'h-10 px-3.5 rounded-[10px] text-[13px] font-medium border inline-flex items-center gap-1.5 transition',
                mine
                  ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep'
                  : 'bg-white border-black/8 text-b24-text hover:bg-[#f8fafb]',
              ].join(' ')}
            >
              <UserRound size={15} />
              Мои
            </button>
            {isRop || String(user?.role || '').toLowerCase() === 'admin' ? (
              <button
                type="button"
                onClick={() => {
                  setQueue((v) => !v)
                  if (!queue) setMine(false)
                }}
                className={[
                  'h-10 px-3.5 rounded-[10px] text-[13px] font-medium border inline-flex items-center gap-1.5 transition',
                  queue
                    ? 'bg-amber-50 border-amber-300 text-amber-900'
                    : 'bg-white border-black/8 text-b24-text hover:bg-[#f8fafb]',
                ].join(' ')}
              >
                На раздачу
              </button>
            ) : null}
            <PeriodFilter value={period} onChange={setPeriod} />
            <button
              type="button"
              onClick={() => {
                setSort('created_at')
                setDir((d) => (d === 'DESC' ? 'ASC' : 'DESC'))
              }}
              className="h-10 px-3 rounded-[10px] text-[13px] border border-black/8 bg-white text-b24-text hover:bg-[#f8fafb]"
            >
              По дате {dir === 'DESC' ? '↓' : '↑'}
            </button>
          </div>

          <div className="flex flex-wrap gap-1.5">
            {tabs.map((tab) => {
              const active = statusFilter === tab.id
              return (
                <button
                  key={tab.id || 'all'}
                  type="button"
                  onClick={() => setStatusFilter(tab.id)}
                  className={[
                    'h-8 px-3 rounded-full text-[12px] font-medium inline-flex items-center gap-1.5 transition',
                    active
                      ? 'bg-b24-text-dark text-white'
                      : 'bg-[#eef1f4] text-b24-text hover:bg-[#e4e8ec]',
                  ].join(' ')}
                >
                  {tab.label}
                  <span
                    className={[
                      'tabular-nums text-[11px] px-1.5 py-0.5 rounded-full',
                      active ? 'bg-white/20' : 'bg-white text-b24-text-muted',
                    ].join(' ')}
                  >
                    {tab.count}
                  </span>
                </button>
              )
            })}
          </div>
        </div>

        {error ? <p className="px-4 py-2 text-[13px] text-red-500">{error}</p> : null}

        <div className={`flex-1 overflow-x-auto b24-scrollbar ${loading ? 'opacity-60' : ''}`}>
          {rows.length === 0 && loading ? (
            <div className="flex flex-col items-center justify-center py-20 px-6 text-center text-[14px] text-b24-text-muted">
              Загрузка…
            </div>
          ) : rows.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 px-6 text-center">
              <div className="size-14 rounded-2xl bg-[#e8f8fe] text-b24-cyan-deep inline-flex items-center justify-center mb-3">
                <Users size={26} />
              </div>
              <div className="text-[16px] font-semibold text-b24-text-dark">
                {debounced || mine || queue || statusFilter ? 'Ничего не найдено' : 'Пока нет лидов'}
              </div>
              <p className="mt-1 text-[13px] text-b24-text-muted max-w-sm">
                {debounced || mine || queue || statusFilter
                  ? 'Попробуйте сбросить фильтры или изменить запрос.'
                  : isManager
                    ? 'РОП назначит вам лиды — они появятся здесь.'
                    : 'Создайте первый лид — с телефоном сразу можно звонить и писать в MAX.'}
              </p>
              {canCreateLeads && !debounced && !mine && !queue && !statusFilter ? (
                <Button
                  type="button"
                  variant="green"
                  className="mt-4 rounded-[10px]"
                  onClick={openCreate}
                  leftIcon={<Plus size={16} />}
                >
                  Создать лид
                </Button>
              ) : null}
            </div>
          ) : (
            <table className="w-full min-w-[920px] text-left">
              <thead className="sticky top-0 z-10 bg-[#f8f9fb] text-[11px] uppercase tracking-wide text-b24-text-muted">
                <tr>
                  <th className="px-4 py-3 font-semibold">Лид</th>
                  <th className="px-3 py-3 font-semibold">Телефон</th>
                  <th className="px-3 py-3 font-semibold">Статус</th>
                  <th className="px-3 py-3 font-semibold">Источник</th>
                  <th className="px-3 py-3 font-semibold text-right">Создан</th>
                  <th className="w-12 px-2 py-3" />
                </tr>
              </thead>
              <tbody className="divide-y divide-black/[0.05]">
                {rows.map((row) => {
                  const id = String(row.id)
                  const phone = String(row.contact_phone ?? '')
                  const status = String(row.status ?? '')
                  const title = String(row.title || 'Без названия')
                  return (
                    <tr
                      key={id}
                      className="group cursor-pointer hover:bg-[#f3fafd] transition-colors"
                      onClick={() => setOpenId(id)}
                    >
                      <td className="px-4 py-3.5">
                        <div className="flex items-center gap-3 min-w-0">
                          <div className="size-10 rounded-full bg-[linear-gradient(135deg,#2fc6f6,#1ea8d4)] text-white text-[14px] font-semibold inline-flex items-center justify-center shrink-0 shadow-[0_4px_12px_rgba(47,198,246,0.35)]">
                            {initial(row)}
                          </div>
                          <div className="min-w-0">
                            <div className="text-[14px] font-semibold text-b24-text-dark truncate group-hover:text-b24-cyan-deep">
                              {title}
                            </div>
                            <div className="mt-0.5 flex items-center gap-2 text-[12px] text-b24-text-muted min-w-0">
                              {row.contact ? (
                                <span className="inline-flex items-center gap-1 truncate">
                                  <UserRound size={12} />
                                  {String(row.contact)}
                                </span>
                              ) : null}
                              {row.company ? (
                                <span className="inline-flex items-center gap-1 truncate">
                                  <Building2 size={12} />
                                  {String(row.company)}
                                </span>
                              ) : null}
                              {!row.contact && !row.company ? <span>Без контакта</span> : null}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td className="px-3 py-3.5">
                        {phone ? (
                          <span className="inline-flex items-center gap-1.5 text-[13px] font-medium text-b24-text-dark tabular-nums">
                            <Phone size={13} className="text-b24-green shrink-0" />
                            {formatPhone(phone)}
                          </span>
                        ) : (
                          <span className="text-[13px] text-b24-text-muted">—</span>
                        )}
                      </td>
                      <td className="px-3 py-3.5">
                        <span
                          className={[
                            'inline-flex h-7 items-center rounded-full px-2.5 text-[12px] font-medium ring-1 ring-inset',
                            statusTone(status),
                          ].join(' ')}
                        >
                          {leadStatusLabel(status)}
                        </span>
                      </td>
                      <td className="px-3 py-3.5">
                        <span className="text-[13px] text-b24-text">
                          {sourceLabel(String(row.source ?? ''))}
                        </span>
                      </td>
                      <td className="px-3 py-3.5 text-right text-[12px] text-b24-text-muted whitespace-nowrap">
                        {relativeCreated(row.created_at)}
                      </td>
                      <td className="px-2 py-3.5" onClick={(e) => e.stopPropagation()}>
                        {isAdmin ? (
                          <button
                            type="button"
                            title="Удалить"
                            className="size-8 rounded-full text-b24-text-muted opacity-0 group-hover:opacity-100 hover:bg-red-50 hover:text-red-600 inline-flex items-center justify-center transition"
                            onClick={(e) => void removeLead(id, title, e)}
                          >
                            <Trash2 size={15} />
                          </button>
                        ) : null}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>

        <ListPager
          total={total}
          limit={limit}
          offset={offset}
          onOffset={setOffset}
          onLimit={(next) => {
            setLimit(next)
            setOffset(0)
          }}
        />
      </div>

      <LeadFormModal
        open={formOpen}
        editing={Boolean(editing)}
        pending={pending}
        error={formError}
        initial={formInitial}
        onClose={() => setFormOpen(false)}
        onSubmit={(values) => void submit(values)}
      />
    </div>
  )
}

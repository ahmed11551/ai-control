import {
  ArrowLeft,
  Hash,
  Link2,
  MessageCircle,
  Phone,
  RefreshCw,
  Search,
  ShieldCheck,
  Unlink,
  UserRound,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { EmployeeTelephonyDrawer } from '../../components/employees/EmployeeTelephonyDrawer'
import { PageHeader, PageShell } from '../../components/layout/PageHeader'
import { Button } from '../../components/ui/Button'
import { ListPager } from '../../components/ui/ListPager'
import { api } from '../../lib/api'
import { roleLabel } from '../../lib/roles'
import { useAuth } from '../../store/auth'

type EmpUser = {
  id: string
  email: string
  login?: string
  full_name: string
  role: string
  extension?: string
  phone_extension?: string
  work_phone?: string
  department?: string
  department_id?: string
  is_active?: boolean
}

type MaxAccount = {
  id: string
  profile_id?: string
  name?: string
  phone?: string
  is_active?: boolean
  threads?: number
  send_down?: boolean
  cold_limit?: boolean
}

type MaxAvail = {
  profile_id: string
  line_id?: string
  name: string
  phone?: string
  owner_id?: string
  owner?: string
  authorized?: boolean
  app_status?: string
  working?: boolean
}

type ViolationSummary = {
  restricted: boolean
  open: number
  confirmed: number
  reason?: string
  recent?: { id: string; summary: string; status: string; event_type: string }[]
}

type EmpDetail = {
  user: EmpUser
  max_accounts: MaxAccount[]
  available: MaxAvail[]
  can_bind_max?: boolean
  can_edit_department?: boolean
  departments?: { id: string; name: string }[]
  wappi?: boolean
  wappi_error?: string
}

type AvailFilter = 'all' | 'free' | 'busy'
type WorkFilter = 'all' | 'working' | 'offline'

const PAGE_SIZE = 20

function initial(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (!parts.length) return '?'
  if (parts.length === 1) return parts[0].slice(0, 1).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

function digits(raw: string) {
  return raw.replace(/\D/g, '')
}

function formatPhone(raw: string) {
  const d = digits(raw)
  if (d.length === 11 && (d[0] === '7' || d[0] === '8')) {
    const n = '7' + d.slice(1)
    return `+${n[0]} ${n.slice(1, 4)} ${n.slice(4, 7)}-${n.slice(7, 9)}-${n.slice(9, 11)}`
  }
  return raw || '—'
}

function roleTone(role: string) {
  switch (String(role).toLowerCase()) {
    case 'admin':
      return 'bg-[#efe8ff] text-[#5b3cc4] ring-[#d4c8f5]'
    case 'rop':
      return 'bg-[#e8f4ff] text-[#1a6fb5] ring-[#b8d9f0]'
    case 'manager':
    case 'sales':
      return 'bg-[#e8f8ef] text-[#1a7a45] ring-[#b8e0c8]'
    default:
      return 'bg-[#f0f2f5] text-[#5a6570] ring-[#d8dde3]'
  }
}

function isBusy(item: MaxAvail) {
  return Boolean(item.owner_id || item.owner)
}

function isWorking(item: MaxAvail) {
  if (typeof item.working === 'boolean') return item.working
  return item.authorized !== false && (item.app_status || 'open') === 'open'
}

export function EmployeePage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { user: me } = useAuth()
  const [data, setData] = useState<EmpDetail | null>(null)
  const [violations, setViolations] = useState<ViolationSummary | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState('')
  const [telephonyOpen, setTelephonyOpen] = useState(false)
  const [deptSaving, setDeptSaving] = useState(false)
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')
  const [filter, setFilter] = useState<AvailFilter>('free')
  const [workFilter, setWorkFilter] = useState<WorkFilter>('working')
  const [offset, setOffset] = useState(0)
  const [limit, setLimit] = useState(PAGE_SIZE)
  const [checkPhone, setCheckPhone] = useState('79804771910')
  const [checkBusy, setCheckBusy] = useState(false)
  const [checkResult, setCheckResult] = useState<{
    on_max?: boolean
    phone?: string
    detail?: string
    error?: string
  } | null>(null)

  const canTelephony =
    String(me?.role || '').toLowerCase() === 'admin' ||
    String(me?.role || '').toLowerCase() === 'rop'

  const load = useCallback(() => {
    if (!id) return Promise.resolve()
    setLoading(true)
    setError('')
    return api<EmpDetail>(`/employees/${id}`)
      .then(setData)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [id])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (!id || !canTelephony) {
      setViolations(null)
      return
    }
    api<ViolationSummary>(`/security/summary?user_id=${encodeURIComponent(id)}`)
      .then(setViolations)
      .catch(() => setViolations(null))
  }, [id, canTelephony])

  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(query.trim().toLowerCase()), 220)
    return () => window.clearTimeout(t)
  }, [query])

  useEffect(() => {
    setOffset(0)
  }, [debounced, filter, workFilter, id])

  const bind = async (item: MaxAvail) => {
    if (!id || !data?.can_bind_max) return
    if (!isWorking(item)) {
      if (!window.confirm('В Wappi профиль офлайн / не авторизован. Всё равно привязать?')) return
    }
    if (isBusy(item) && !window.confirm(`Аккаунт сейчас у ${item.owner || 'другого'}. Перепривязать?`)) {
      return
    }
    const key = item.line_id || item.profile_id
    setPending(key)
    setError('')
    try {
      await api(`/employees/${id}/max`, {
        method: 'POST',
        body: JSON.stringify({
          profile_id: item.profile_id,
          line_id: item.line_id || '',
          name: item.name,
          phone: item.phone || '',
        }),
      })
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось привязать')
    } finally {
      setPending('')
    }
  }

  const unbind = async (lineId: string) => {
    if (!id || !data?.can_bind_max) return
    if (!window.confirm('Отвязать аккаунт MAX от сотрудника?')) return
    setPending(lineId)
    setError('')
    try {
      await api(`/employees/${id}/max/${lineId}`, { method: 'DELETE' })
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось отвязать')
    } finally {
      setPending('')
    }
  }

  const checkMaxNumber = async () => {
    if (!id || !data?.can_bind_max) return
    const phone = digits(checkPhone)
    if (phone.length < 10) {
      setCheckResult({ error: 'Введите номер телефона' })
      return
    }
    setCheckBusy(true)
    setCheckResult(null)
    try {
      const res = await api<{ on_max?: boolean; phone?: string; detail?: string }>(
        `/employees/${id}/max/check`,
        { method: 'POST', body: JSON.stringify({ phone }) },
      )
      setCheckResult({
        on_max: Boolean(res.on_max),
        phone: res.phone || phone,
        detail: res.detail || '',
      })
    } catch (e) {
      setCheckResult({ error: e instanceof Error ? e.message : 'не удалось проверить' })
    } finally {
      setCheckBusy(false)
    }
  }

  const changeDepartment = async (departmentId: string) => {
    if (!id || !data?.can_edit_department || !u) return
    if ((u.department_id || '') === departmentId) return
    setDeptSaving(true)
    setError('')
    try {
      const res = await api<{ department?: string; department_id?: string }>(`/employees/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ department_id: departmentId }),
      })
      setData((d) =>
        d
          ? {
              ...d,
              user: {
                ...d.user,
                department_id: res.department_id || '',
                department: res.department || '',
              },
            }
          : d,
      )
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось сменить отдел')
    } finally {
      setDeptSaving(false)
    }
  }

  const u = data?.user
  const bound = data?.max_accounts ?? []
  const available = data?.available ?? []

  const counts = useMemo(() => {
    let free = 0
    let busy = 0
    let working = 0
    let offline = 0
    for (const item of available) {
      if (isBusy(item)) busy += 1
      else free += 1
      if (isWorking(item)) working += 1
      else offline += 1
    }
    return { all: available.length, free, busy, working, offline }
  }, [available])

  const filtered = useMemo(() => {
    const q = debounced
    const qDigits = digits(q)
    return available.filter((item) => {
      const busy = isBusy(item)
      const working = isWorking(item)
      if (filter === 'free' && busy) return false
      if (filter === 'busy' && !busy) return false
      if (workFilter === 'working' && !working) return false
      if (workFilter === 'offline' && working) return false
      if (!q) return true
      const hay = [item.name, item.phone, item.profile_id, item.owner].join(' ').toLowerCase()
      if (hay.includes(q)) return true
      if (qDigits && digits(`${item.phone || ''}${item.name || ''}`).includes(qDigits)) return true
      return false
    })
  }, [available, debounced, filter, workFilter])

  const pageItems = useMemo(() => {
    if (limit <= 0) return filtered
    return filtered.slice(offset, offset + limit)
  }, [filtered, limit, offset])

  const filterTabs: { id: AvailFilter; label: string; count: number }[] = [
    { id: 'free', label: 'Свободные', count: counts.free },
    { id: 'busy', label: 'Занятые', count: counts.busy },
    { id: 'all', label: 'Все', count: counts.all },
  ]

  const workTabs: { id: WorkFilter; label: string; count: number }[] = [
    { id: 'working', label: 'Рабочие', count: counts.working },
    { id: 'offline', label: 'Офлайн', count: counts.offline },
    { id: 'all', label: 'Все', count: counts.all },
  ]

  return (
    <PageShell>
      <PageHeader
        title={u?.full_name || 'Сотрудник'}
        subtitle={
          u
            ? [roleLabel(u.role), u.department || null, u.email].filter(Boolean).join(' · ')
            : 'Карточка и привязка MAX'
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              variant="glass"
              className="h-10 rounded-[10px] px-3"
              onClick={() => navigate('/employees')}
              leftIcon={<ArrowLeft size={16} />}
            >
              К списку
            </Button>
            <Button
              type="button"
              variant="glass"
              className="h-10 rounded-[10px] px-3"
              onClick={() => void load()}
              leftIcon={<RefreshCw size={15} className={loading ? 'animate-spin' : ''} />}
            >
              Обновить
            </Button>
            {canTelephony && u ? (
              <Button
                type="button"
                variant="green"
                className="h-10 rounded-[10px] px-4"
                onClick={() => setTelephonyOpen(true)}
                leftIcon={<Phone size={15} />}
              >
                Телефония
              </Button>
            ) : null}
          </div>
        }
      />

      {error ? (
        <div className="rounded-[12px] border border-red-200 bg-red-50 px-4 py-3 text-[13px] text-red-700">
          {error}
        </div>
      ) : null}

      {loading && !data ? (
        <div className="b24-glass-panel px-5 py-10 text-center text-[14px] text-b24-text-muted">
          Загрузка…
        </div>
      ) : null}

      {u ? (
        <div className="grid grid-cols-1 lg:grid-cols-[300px_1fr] gap-4 items-start">
          <section className="b24-glass-panel p-5 flex flex-col gap-4 self-start">
            <div className="flex items-center gap-3">
              <div className="size-14 rounded-full bg-[linear-gradient(135deg,#2fc6f6,#1ea8d4)] text-white text-[18px] font-semibold inline-flex items-center justify-center shrink-0 shadow-[0_4px_12px_rgba(47,198,246,0.35)]">
                {initial(u.full_name)}
              </div>
              <div className="min-w-0">
                <div className="text-[16px] font-semibold text-b24-text-dark truncate">{u.full_name}</div>
                <div className="mt-1 flex items-center gap-1 text-[12px] text-b24-text-muted truncate">
                  <UserRound size={12} />
                  {u.login || u.email || '—'}
                  {u.login && u.email ? ` · ${u.email}` : ''}
                </div>
              </div>
            </div>

            <div className="flex flex-wrap gap-2">
              <span
                className={[
                  'inline-flex h-7 items-center rounded-full px-2.5 text-[12px] font-medium ring-1 ring-inset',
                  roleTone(u.role),
                ].join(' ')}
              >
                {roleLabel(u.role)}
              </span>
              <span
                className={[
                  'inline-flex h-7 items-center rounded-full px-2.5 text-[12px] font-medium ring-1 ring-inset',
                  u.is_active !== false
                    ? 'bg-[#e8f8ef] text-[#1a7a45] ring-[#b8e0c8]'
                    : 'bg-[#f5f0f0] text-[#9a5050] ring-[#e0c8c8]',
                ].join(' ')}
              >
                {u.is_active !== false ? 'Активен' : 'Неактивен'}
              </span>
              {violations?.restricted ? (
                <span className="inline-flex h-7 items-center rounded-full px-2.5 text-[12px] font-medium ring-1 ring-inset bg-amber-50 text-amber-900 ring-amber-200">
                  Сессия ограничена
                </span>
              ) : null}
            </div>
            {violations ? (
              <div className="rounded-[12px] bg-[#f7fafc] px-3 py-2 text-[13px]">
                <div className="font-medium">Нарушения</div>
                <div className="mt-1 text-b24-text-muted">
                  Открытых {violations.open || 0}, подтверждённых {violations.confirmed || 0}
                </div>
                {(violations.recent || []).slice(0, 3).map((item) => (
                  <div key={item.id} className="mt-1 truncate text-[12px] text-b24-text-muted">
                    {item.summary}
                  </div>
                ))}
              </div>
            ) : null}

            <dl className="flex flex-col gap-3 text-[13px]">
              <div>
                <dt className="text-[11px] uppercase tracking-wide text-b24-text-muted">Отдел</dt>
                <dd className="mt-0.5 text-b24-text-dark">
                  {data?.can_edit_department ? (
                    <select
                      value={u.department_id || ''}
                      disabled={deptSaving}
                      onChange={(e) => void changeDepartment(e.target.value)}
                      className="mt-1 h-9 w-full rounded-[8px] border border-black/10 bg-white px-2 text-[13px] text-b24-text-dark outline-none focus:border-b24-cyan disabled:opacity-60"
                    >
                      <option value="">Без отдела</option>
                      {(data.departments ?? []).map((d) => (
                        <option key={d.id} value={d.id}>
                          {d.name}
                        </option>
                      ))}
                    </select>
                  ) : (
                    u.department || '—'
                  )}
                </dd>
              </div>
              <div>
                <dt className="text-[11px] uppercase tracking-wide text-b24-text-muted">Внутренний</dt>
                <dd className="mt-0.5 text-b24-text-dark tabular-nums inline-flex items-center gap-1.5">
                  {u.extension || u.phone_extension ? (
                    <>
                      <Hash size={13} className="text-b24-text-muted" />
                      {u.extension || u.phone_extension}
                    </>
                  ) : (
                    '—'
                  )}
                </dd>
              </div>
              <div>
                <dt className="text-[11px] uppercase tracking-wide text-b24-text-muted">Рабочий телефон</dt>
                <dd className="mt-0.5 text-b24-text-dark tabular-nums inline-flex items-center gap-1.5">
                  {u.work_phone ? (
                    <>
                      <Phone size={13} className="text-b24-green shrink-0" />
                      {formatPhone(u.work_phone)}
                    </>
                  ) : (
                    '—'
                  )}
                </dd>
              </div>
            </dl>
          </section>

          <section className="b24-glass-panel p-5 flex flex-col gap-5 min-w-0">
            <div>
              <div className="flex items-center gap-2">
                <MessageCircle size={18} className="text-b24-cyan" />
                <h2 className="text-[16px] font-semibold text-b24-text-dark">Аккаунты MAX</h2>
              </div>
              <p className="mt-1 text-[12px] text-b24-text-muted">
                Профили из Wappi · привязка — личная линия сотрудника в чате.
                {!data?.wappi ? ' Wappi не настроен в .env.' : ''}
              </p>
              {data?.wappi_error ? (
                <p className="mt-1 text-[12px] text-red-600">Wappi: {data.wappi_error}</p>
              ) : null}
            </div>

            <div>
              <div className="text-[12px] font-medium text-b24-text-muted uppercase tracking-wide mb-2">
                Привязаны · {bound.length}
              </div>
              {bound.length === 0 ? (
                <div className="rounded-[12px] border border-dashed border-black/10 bg-[#f8f9fb] px-4 py-5 text-center text-[13px] text-b24-text-muted">
                  Пока нет привязанных аккаунтов
                </div>
              ) : (
                <ul className="divide-y divide-black/[0.05] rounded-[12px] border border-black/6 overflow-hidden bg-white">
                  {bound.map((acc) => {
                    const phone = formatPhone(acc.phone || acc.name || '')
                    return (
                      <li key={acc.id} className="flex items-center gap-3 px-4 py-2.5">
                        <div className="size-8 rounded-full bg-[#eef6fb] text-b24-cyan inline-flex items-center justify-center shrink-0">
                          <MessageCircle size={14} />
                        </div>
                        <div className="min-w-0 flex-1">
                          <div
                            className={[
                              'text-[14px] font-medium tabular-nums text-b24-text-dark',
                              acc.send_down || acc.cold_limit ? 'underline decoration-[#b45353] underline-offset-2 text-[#7a3030]' : '',
                            ].join(' ')}
                          >
                            {phone}
                            {acc.send_down ? ' · не работает' : acc.cold_limit ? ' · лимит на новых' : ''}
                          </div>
                          <div className="text-[12px] text-b24-text-muted truncate">
                            {typeof acc.threads === 'number' ? `${acc.threads} диалогов` : 'личная линия'}
                          </div>
                        </div>
                        {data?.can_bind_max ? (
                          <button
                            type="button"
                            disabled={pending === acc.id}
                            onClick={() => void unbind(acc.id)}
                            className="inline-flex h-8 items-center gap-1.5 rounded-[8px] px-2.5 text-[12px] font-medium text-[#9a5050] hover:bg-red-50 disabled:opacity-50"
                          >
                            <Unlink size={13} />
                            Отвязать
                          </button>
                        ) : null}
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>

            {data?.can_bind_max && data?.wappi ? (
              <div className="rounded-[12px] border border-black/6 bg-[#f8f9fb] p-3.5 flex flex-col gap-2.5">
                <div className="text-[12px] font-medium text-b24-text-muted uppercase tracking-wide">
                  Проверить номер в MAX
                </div>
                <div className="flex flex-wrap gap-2">
                  <input
                    value={checkPhone}
                    onChange={(e) => {
                      setCheckPhone(e.target.value)
                      setCheckResult(null)
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        void checkMaxNumber()
                      }
                    }}
                    placeholder="79804771910"
                    className="h-10 min-w-[180px] flex-1 rounded-[10px] border border-black/10 bg-white px-3 text-[14px] tabular-nums text-b24-text-dark placeholder:text-[#9aa3ad] outline-none focus:border-b24-cyan"
                  />
                  <Button
                    type="button"
                    variant="filter"
                    disabled={checkBusy}
                    onClick={() => void checkMaxNumber()}
                    className="h-10 shrink-0 gap-1.5 rounded-[10px] px-3"
                    leftIcon={<ShieldCheck size={15} />}
                  >
                    {checkBusy ? 'Проверка…' : 'Проверить номер'}
                  </Button>
                </div>
                {checkResult?.error ? (
                  <p className="text-[13px] text-red-600">{checkResult.error}</p>
                ) : null}
                {checkResult && !checkResult.error ? (
                  <p
                    className={`text-[13px] font-medium ${
                      checkResult.on_max ? 'text-[#1a7a45]' : 'text-[#9a5050]'
                    }`}
                  >
                    {formatPhone(checkResult.phone || checkPhone)}
                    {checkResult.on_max ? ' — есть в MAX' : ' — нет в MAX'}
                    {checkResult.detail ? (
                      <span className="font-normal text-b24-text-muted"> · {checkResult.detail}</span>
                    ) : null}
                  </p>
                ) : null}
              </div>
            ) : null}

            {data?.can_bind_max ? (
              <div className="flex flex-col gap-3">
                <div className="flex flex-wrap items-end justify-between gap-2">
                  <div className="text-[12px] font-medium text-b24-text-muted uppercase tracking-wide">
                    Из Wappi · {filtered.length}
                    {filtered.length !== available.length ? ` из ${available.length}` : ''}
                  </div>
                </div>

                <div className="flex flex-col gap-2">
                  <label className="relative w-full">
                    <Search
                      size={15}
                      className="absolute left-3 top-1/2 -translate-y-1/2 text-b24-text-muted pointer-events-none"
                    />
                    <input
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                      placeholder="Поиск по номеру или сотруднику"
                      className="h-10 w-full rounded-[10px] border border-black/10 bg-[#f5f7f9] pl-9 pr-3 text-[14px] text-b24-text-dark placeholder:text-[#9aa3ad] outline-none focus:border-b24-cyan focus:bg-white"
                    />
                  </label>
                  <div className="flex flex-wrap gap-2">
                    <div className="inline-flex rounded-[10px] border border-black/8 bg-[#f5f7f9] p-0.5">
                      {workTabs.map((tab) => {
                        const active = workFilter === tab.id
                        return (
                          <button
                            key={tab.id}
                            type="button"
                            onClick={() => setWorkFilter(tab.id)}
                            className={[
                              'h-9 px-3 rounded-[8px] text-[12px] font-medium transition tabular-nums',
                              active
                                ? 'bg-white text-b24-text-dark shadow-sm'
                                : 'text-b24-text-muted hover:text-b24-text-dark',
                            ].join(' ')}
                          >
                            {tab.label}
                            <span className={active ? 'text-b24-text-muted' : 'opacity-70'}>
                              {' '}
                              {tab.count}
                            </span>
                          </button>
                        )
                      })}
                    </div>
                    <div className="inline-flex rounded-[10px] border border-black/8 bg-[#f5f7f9] p-0.5">
                      {filterTabs.map((tab) => {
                        const active = filter === tab.id
                        return (
                          <button
                            key={tab.id}
                            type="button"
                            onClick={() => setFilter(tab.id)}
                            className={[
                              'h-9 px-3 rounded-[8px] text-[12px] font-medium transition tabular-nums',
                              active
                                ? 'bg-white text-b24-text-dark shadow-sm'
                                : 'text-b24-text-muted hover:text-b24-text-dark',
                            ].join(' ')}
                          >
                            {tab.label}
                            <span className={active ? 'text-b24-text-muted' : 'opacity-70'}>
                              {' '}
                              {tab.count}
                            </span>
                          </button>
                        )
                      })}
                    </div>
                  </div>
                </div>

                {available.length === 0 ? (
                  <div className="rounded-[12px] border border-dashed border-black/10 bg-[#f8f9fb] px-4 py-6 text-center text-[13px] text-b24-text-muted">
                    {data?.wappi_error
                      ? 'Не удалось загрузить профили из Wappi'
                      : 'В Wappi нет авторизованных профилей MAX'}
                  </div>
                ) : filtered.length === 0 ? (
                  <div className="rounded-[12px] border border-dashed border-black/10 bg-[#f8f9fb] px-4 py-6 text-center text-[13px] text-b24-text-muted">
                    Ничего не найдено — смените фильтр или поиск
                  </div>
                ) : (
                  <div className="rounded-[12px] border border-black/6 overflow-hidden bg-white">
                    <div className="grid grid-cols-[1fr_88px_88px_auto] gap-2 px-4 py-2 text-[11px] uppercase tracking-wide text-b24-text-muted bg-[#f8f9fb] border-b border-black/6">
                      <span>Номер</span>
                      <span>Wappi</span>
                      <span>В CRM</span>
                      <span className="text-right pr-1">Действие</span>
                    </div>
                    <ul className="divide-y divide-black/[0.05]">
                      {pageItems.map((item) => {
                        const key = item.line_id || item.profile_id
                        const busy = isBusy(item)
                        const working = isWorking(item)
                        const phone = formatPhone(item.phone || item.name || '')
                        return (
                          <li
                            key={key}
                            className={[
                              'grid grid-cols-[1fr_88px_88px_auto] gap-2 items-center px-4 py-2.5 hover:bg-[#f7fafc]',
                              working ? '' : 'opacity-75',
                            ].join(' ')}
                          >
                            <div className="min-w-0">
                              <div className="text-[14px] font-medium tabular-nums text-b24-text-dark">
                                {phone}
                              </div>
                              {busy && item.owner ? (
                                <div className="text-[12px] text-b24-text-muted truncate">
                                  у {item.owner}
                                </div>
                              ) : null}
                            </div>
                            <div>
                              <span
                                className={[
                                  'inline-flex h-6 items-center rounded-full px-2 text-[11px] font-medium ring-1 ring-inset',
                                  working
                                    ? 'bg-emerald-50 text-emerald-800 ring-emerald-200'
                                    : 'bg-slate-100 text-slate-600 ring-slate-200',
                                ].join(' ')}
                              >
                                {working ? 'Онлайн' : 'Офлайн'}
                              </span>
                            </div>
                            <div>
                              <span
                                className={[
                                  'inline-flex h-6 items-center rounded-full px-2 text-[11px] font-medium ring-1 ring-inset',
                                  busy
                                    ? 'bg-amber-50 text-amber-800 ring-amber-200'
                                    : 'bg-sky-50 text-sky-800 ring-sky-200',
                                ].join(' ')}
                              >
                                {busy ? 'Занят' : 'Свободен'}
                              </span>
                            </div>
                            <button
                              type="button"
                              disabled={Boolean(pending)}
                              onClick={() => void bind(item)}
                              className={[
                                'inline-flex h-8 items-center justify-center gap-1.5 rounded-[8px] px-2.5 text-[12px] font-medium disabled:opacity-50',
                                busy || !working
                                  ? 'bg-white text-b24-text-dark ring-1 ring-inset ring-black/10 hover:bg-[#f3f5f7]'
                                  : 'bg-b24-green text-white hover:bg-b24-green-hover shadow-[0_2px_8px_rgba(47,192,110,0.3)]',
                              ].join(' ')}
                            >
                              <Link2 size={13} />
                              {pending === key ? '…' : busy ? 'Перенести' : 'Привязать'}
                            </button>
                          </li>
                        )
                      })}
                    </ul>
                    <ListPager
                      total={filtered.length}
                      limit={limit}
                      offset={offset}
                      onOffset={setOffset}
                      onLimit={(next) => {
                        setLimit(next)
                        setOffset(0)
                      }}
                    />
                  </div>
                )}
              </div>
            ) : (
              <p className="text-[12px] text-b24-text-muted">
                Привязывать и отвязывать MAX может только администратор.
              </p>
            )}
          </section>
        </div>
      ) : null}

      {telephonyOpen && u ? (
        <EmployeeTelephonyDrawer user={u} onClose={() => setTelephonyOpen(false)} />
      ) : null}
    </PageShell>
  )
}

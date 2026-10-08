import { Eye, EyeOff, Hash, KeyRound, Phone, Plus, RefreshCw, Search, UserRound, Users } from 'lucide-react'
import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button } from '../../components/ui/Button'
import { api } from '../../lib/api'
import { suggestLoginFromName } from '../../lib/loginSuggest'
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
  /** Личный Voximplant-аккаунт в vox_accounts; иначе softphone берёт дефолт из env */
  has_vox?: boolean
}

type EmpDept = {
  id: string
  name: string
  people?: number
  head_name?: string
}

type NumPerson = {
  id: string
  full_name: string
  department?: string
  is_active?: boolean
  extension?: string
  work_phone?: string
  lines?: number
}

type NumLine = {
  id: string
  name: string
  channel: string
  kind: string
  owner?: string
  is_active?: boolean
  external_ref?: string
}

type NumHold = {
  extension: string
  until_at: string
  last_owner?: string
}

type OffLog = {
  created_at: string
  actor?: string
  target?: string
}

type EmpData = {
  users: EmpUser[]
  departments: EmpDept[]
  active: number
  total: number
  can_offboard?: boolean
  can_edit_department?: boolean
  can_create?: boolean
  creatable_roles?: string[]
  numbers?: {
    people: NumPerson[]
    lines: NumLine[]
    quarantine: NumHold[]
    log: OffLog[]
    quarantine_days?: number
  }
}

type ViewTab = 'people' | 'numbers'

function when(raw: string) {
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function initial(name: string) {
  const t = name.trim()
  return (t.slice(0, 1) || '?').toUpperCase()
}

function roleTone(role: string) {
  switch (String(role || '').toLowerCase()) {
    case 'admin':
      return 'bg-violet-50 text-violet-800 ring-violet-200'
    case 'rop':
      return 'bg-sky-50 text-sky-800 ring-sky-200'
    case 'manager':
    case 'sales':
      return 'bg-emerald-50 text-emerald-800 ring-emerald-200'
    case 'logist':
    case 'night_logist':
      return 'bg-amber-50 text-amber-800 ring-amber-200'
    default:
      return 'bg-slate-100 text-slate-600 ring-slate-200'
  }
}

function statusTone(active: boolean) {
  return active
    ? 'bg-emerald-50 text-emerald-800 ring-emerald-200'
    : 'bg-rose-50 text-rose-700 ring-rose-200'
}

function voxTone(hasVox: boolean) {
  return hasVox
    ? 'bg-violet-50 text-violet-800 ring-violet-200'
    : 'bg-slate-100 text-slate-600 ring-slate-200'
}

function hasPersonalVox(u: EmpUser) {
  return u.has_vox === true
}

export function EmployeesPage() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const isRopView = String(user?.role || '').toLowerCase() === 'rop'
  const [data, setData] = useState<EmpData | null>(null)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')
  const [dept, setDept] = useState('')
  const [roleFilter, setRoleFilter] = useState('')
  const [tab, setTab] = useState<ViewTab>('people')
  const [pending, setPending] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const creatable = data?.creatable_roles?.length ? data.creatable_roles : user?.creatable_roles ?? []
  const [form, setForm] = useState({
    full_name: '',
    login: '',
    email: '',
    password: '',
    role: 'manager',
    department_id: '',
    msk_tower: false,
  })

  const genPassword = () => {
    const alphabet = 'abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'
    const bytes = new Uint8Array(12)
    crypto.getRandomValues(bytes)
    const pwd = Array.from(bytes, (b) => alphabet[b % alphabet.length]).join('')
    setForm((f) => ({ ...f, password: pwd }))
    setShowPassword(true)
  }

  const genLogin = () => {
    const suggested = suggestLoginFromName(form.full_name)
    if (!suggested) return
    setForm((f) => ({ ...f, login: suggested }))
  }

  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(query.trim().toLowerCase()), 280)
    return () => window.clearTimeout(t)
  }, [query])

  useEffect(() => {
    if (creatable.length && !creatable.includes(form.role)) {
      setForm((f) => ({ ...f, role: creatable[0] }))
    }
  }, [creatable, form.role])

  const load = () =>
    api<EmpData>('/employees')
      .then(setData)
      .catch((e: Error) => setError(e.message))

  useEffect(() => {
    void load()
  }, [])

  const createStaff = async (e: FormEvent) => {
    e.preventDefault()
    if (!data?.can_create && !creatable.length) return
    setCreating(true)
    setError('')
    setNote('')
    try {
      const payload = {
        ...form,
        role: isRopView ? 'manager' : form.role,
        department_id: isRopView ? '' : form.department_id,
        msk_tower: !isRopView && form.role === 'rop' ? form.msk_tower : false,
      }
      const res = await api<{ email?: string; login?: string; role?: string }>('/users', {
        method: 'POST',
        body: JSON.stringify(payload),
      })
      const loginShown = res.login || form.login || res.email || form.email
      setNote(`Создан ${roleLabel(res.role || payload.role)}: логин ${loginShown}`)
      setForm({
        full_name: '',
        login: '',
        email: '',
        password: '',
        role: creatable[0] || 'manager',
        department_id: '',
        msk_tower: false,
      })
      setCreateOpen(false)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'не удалось создать')
    } finally {
      setCreating(false)
    }
  }

  const users = useMemo(() => {
    const list = data?.users ?? []
    return list.filter((u) => {
      if (dept && u.department_id !== dept) return false
      if (roleFilter === 'no_vox') {
        if (hasPersonalVox(u)) return false
      } else if (roleFilter) {
        const r = String(u.role).toLowerCase()
        if (roleFilter === 'manager') {
          if (r !== 'manager' && r !== 'sales') return false
        } else if (r !== roleFilter) {
          return false
        }
      }
      if (!debounced) return true
      return [u.full_name, u.login, u.email, u.extension, u.phone_extension, u.work_phone, u.department, u.role]
        .join(' ')
        .toLowerCase()
        .includes(debounced)
    })
  }, [data, debounced, dept, roleFilter])

  const roleCounts = useMemo(() => {
    const list = data?.users ?? []
    const counts: Record<string, number> = { '': list.length, no_vox: 0 }
    for (const u of list) {
      const r = String(u.role || '').toLowerCase()
      counts[r] = (counts[r] || 0) + 1
      if (!hasPersonalVox(u)) counts.no_vox += 1
    }
    return counts
  }, [data])

  const fire = async (row: EmpUser) => {
    if (!data?.can_offboard) return
    if (row.id === user?.id) return
    if (!window.confirm(`Уволить ${row.full_name}? Вход закроется сразу, внутренний уйдёт в карантин на 14 дней.`)) {
      return
    }
    setPending(row.id)
    setError('')
    setNote('')
    try {
      const res = await api<{ checklist?: string[]; rotate_hooks?: boolean; paused_lines?: number }>(
        `/employees/${row.id}/offboard`,
        { method: 'POST', body: JSON.stringify({}) },
      )
      const extra = (res.checklist ?? []).join(' ')
      setNote(
        `Учётка закрыта.${res.paused_lines ? ` Личных линий на паузе: ${res.paused_lines}.` : ''} ${extra}`,
      )
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось уволить')
    } finally {
      setPending('')
    }
  }

  const changeDepartment = async (row: EmpUser, departmentId: string) => {
    if (!data?.can_edit_department) return
    const prev = row.department_id || ''
    if (prev === departmentId) return
    setPending(`dept:${row.id}`)
    setError('')
    setNote('')
    try {
      const res = await api<{ department?: string; department_id?: string }>(`/employees/${row.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ department_id: departmentId }),
      })
      setData((d) => {
        if (!d) return d
        return {
          ...d,
          users: d.users.map((u) =>
            u.id === row.id
              ? {
                  ...u,
                  department_id: res.department_id || '',
                  department: res.department || '',
                }
              : u,
          ),
        }
      })
      setNote(
        res.department
          ? `${row.full_name}: отдел «${res.department}»`
          : `${row.full_name}: отдел снят`,
      )
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось сменить отдел')
    } finally {
      setPending('')
    }
  }

  const canCreate = Boolean(data?.can_create || creatable.length)
  const inactive = Math.max(0, (data?.total ?? 0) - (data?.active ?? 0))
  const linesCount = data?.numbers?.lines?.length ?? 0

  const roleTabs = isRopView
    ? [
        {
          id: '',
          label: 'Менеджеры',
          count: (roleCounts.manager || 0) + (roleCounts.sales || 0) || data?.total || 0,
        },
        { id: 'no_vox', label: 'Нет вокси', count: roleCounts.no_vox || 0 },
      ]
    : [
        { id: '', label: 'Все', count: roleCounts[''] || 0 },
        { id: 'admin', label: 'Админы', count: roleCounts.admin || 0 },
        { id: 'rop', label: 'РОП', count: roleCounts.rop || 0 },
        { id: 'manager', label: 'Менеджеры', count: (roleCounts.manager || 0) + (roleCounts.sales || 0) },
        { id: 'logist', label: 'Логисты', count: roleCounts.logist || 0 },
        { id: 'night_logist', label: 'Ночные логисты', count: roleCounts.night_logist || 0 },
        { id: 'no_vox', label: 'Нет вокси', count: roleCounts.no_vox || 0 },
      ]

  return (
    <div className="flex flex-col gap-4 pt-1 pb-4 min-h-[calc(100%-2rem)]">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">Сотрудники</h1>
          <p className="mt-1.5 text-[13px] text-white/65">
            {isRopView
              ? `Менеджеры вашего отдела · ${data?.active ?? 0} активных · клик — карточка и MAX`
              : `Команда и доступы · ${data?.active ?? 0} активных из ${data?.total ?? 0} · клик — карточка`}
          </p>
        </div>
        {canCreate ? (
          <Button
            type="button"
            variant="green"
            className="h-10 rounded-[10px] px-4 shadow-[0_8px_24px_rgba(47,192,110,0.35)]"
            onClick={() => setCreateOpen((v) => !v)}
            leftIcon={<Plus size={16} strokeWidth={2.5} />}
          >
            {createOpen ? 'Скрыть форму' : 'Добавить сотрудника'}
          </Button>
        ) : null}
      </div>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-2.5">
        {(isRopView
          ? [
              { label: 'Менеджеров', value: data?.total ?? 0, hint: 'в вашем отделе' },
              { label: 'Активных', value: data?.active ?? 0, hint: 'работают сейчас' },
              { label: 'Неактивных', value: inactive, hint: 'учётки закрыты' },
              { label: 'Отдел', value: data?.departments.length ?? 0, hint: data?.departments[0]?.name || 'не назначен' },
            ]
          : [
              { label: 'Активных', value: data?.active ?? 0, hint: 'работают сейчас' },
              { label: 'Неактивных', value: inactive, hint: 'учётки закрыты' },
              { label: 'Отделов', value: data?.departments.length ?? 0, hint: 'в структуре' },
              { label: 'Линий', value: linesCount, hint: 'MAX / мессенджеры' },
            ]
        ).map((card) => (
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

      {createOpen && canCreate ? (
        <form
          onSubmit={(e) => void createStaff(e)}
          className="b24-glass-panel p-4 sm:p-5 flex flex-col gap-4"
        >
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="text-[15px] font-semibold text-b24-text-dark">Новый сотрудник</div>
              <p className="mt-0.5 text-[12px] text-b24-text-muted">
                Логин можно сгенерировать из ФИО. Email необязателен — вход по логину.
              </p>
            </div>
            <button
              type="button"
              onClick={() => setCreateOpen(false)}
              className="text-[12px] text-b24-text-muted hover:text-b24-text-dark shrink-0"
            >
              Закрыть
            </button>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-medium text-b24-text-dark">ФИО</span>
              <input
                required
                autoComplete="name"
                value={form.full_name}
                onChange={(e) => {
                  const full_name = e.target.value
                  setForm((f) => ({
                    ...f,
                    full_name,
                    login: f.login ? f.login : suggestLoginFromName(full_name),
                  }))
                }}
                placeholder="Иванов Иван Иванович"
                className="h-10 w-full rounded-[10px] border border-black/10 bg-[#f5f7f9] px-3 text-[14px] text-b24-text-dark placeholder:text-[#9aa3ad] outline-none focus:border-b24-cyan focus:bg-white"
              />
            </label>

            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-medium text-b24-text-dark">Логин</span>
              <div className="flex gap-2">
                <input
                  required
                  autoComplete="off"
                  value={form.login}
                  onChange={(e) => setForm((f) => ({ ...f, login: e.target.value.toLowerCase() }))}
                  placeholder="ivanov.i"
                  className="h-10 w-full rounded-[10px] border border-black/10 bg-[#f5f7f9] px-3 text-[14px] text-b24-text-dark placeholder:text-[#9aa3ad] outline-none focus:border-b24-cyan focus:bg-white"
                />
                <button
                  type="button"
                  onClick={genLogin}
                  className="h-10 shrink-0 inline-flex items-center gap-1.5 rounded-[10px] border border-black/10 bg-white px-3 text-[13px] text-b24-text-dark hover:bg-[#f8fafb]"
                  title="Сгенерировать из ФИО"
                >
                  <RefreshCw size={14} />
                  Из ФИО
                </button>
              </div>
            </label>

            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-medium text-b24-text-dark">Email (необязательно)</span>
              <input
                type="email"
                autoComplete="off"
                value={form.email}
                onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))}
                placeholder="ivanov@company.ru"
                className="h-10 w-full rounded-[10px] border border-black/10 bg-[#f5f7f9] px-3 text-[14px] text-b24-text-dark placeholder:text-[#9aa3ad] outline-none focus:border-b24-cyan focus:bg-white"
              />
            </label>

            <label className="flex flex-col gap-1.5 sm:col-span-2">
              <span className="text-[12px] font-medium text-b24-text-dark">Пароль</span>
              <div className="flex flex-wrap gap-2">
                <div className="relative flex-1 min-w-[200px]">
                  <input
                    required
                    minLength={6}
                    type={showPassword ? 'text' : 'password'}
                    autoComplete="new-password"
                    value={form.password}
                    onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
                    placeholder="минимум 6 символов"
                    className="h-10 w-full rounded-[10px] border border-black/10 bg-[#f5f7f9] pl-3 pr-10 text-[14px] text-b24-text-dark placeholder:text-[#9aa3ad] outline-none focus:border-b24-cyan focus:bg-white"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword((v) => !v)}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-b24-text-muted hover:text-b24-text-dark"
                    aria-label={showPassword ? 'Скрыть пароль' : 'Показать пароль'}
                  >
                    {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                  </button>
                </div>
                <button
                  type="button"
                  onClick={genPassword}
                  className="h-10 inline-flex items-center gap-1.5 rounded-[10px] border border-black/10 bg-white px-3 text-[13px] text-b24-text-dark hover:bg-[#f8fafb]"
                >
                  <RefreshCw size={14} />
                  Сгенерировать
                </button>
              </div>
            </label>

            <div className="flex flex-col gap-1.5 sm:col-span-2">
              <span className="text-[12px] font-medium text-b24-text-dark">Роль</span>
              {isRopView ? (
                <div className="h-10 px-3 rounded-[10px] border border-black/10 bg-[#f5f7f9] text-[14px] text-b24-text-dark inline-flex items-center">
                  Менеджер
                </div>
              ) : (
                <div className="flex flex-wrap gap-1.5">
                  {creatable.map((r) => {
                    const active = form.role === r
                    return (
                      <button
                        key={r}
                        type="button"
                        onClick={() => setForm((f) => ({ ...f, role: r }))}
                        className={[
                          'h-9 rounded-full px-3.5 text-[13px] font-medium transition-colors',
                          active
                            ? 'bg-b24-cyan text-white shadow-sm'
                            : 'bg-[#eef1f4] text-b24-text hover:bg-[#e4e8ec]',
                        ].join(' ')}
                      >
                        {roleLabel(r)}
                      </button>
                    )
                  })}
                </div>
              )}
            </div>

            {!isRopView ? (
            <label className="flex flex-col gap-1.5 sm:col-span-2 sm:max-w-sm">
              <span className="text-[12px] font-medium text-b24-text-dark">
                Отдел <span className="font-normal text-b24-text-muted">(необязательно)</span>
              </span>
              <select
                value={form.department_id}
                onChange={(e) => setForm((f) => ({ ...f, department_id: e.target.value }))}
                className="h-10 w-full rounded-[10px] border border-black/10 bg-white px-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan"
              >
                <option value="">Без отдела</option>
                {(data?.departments ?? []).map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name}
                  </option>
                ))}
              </select>
            </label>
            ) : (
              <div className="sm:col-span-2 text-[12px] text-b24-text-muted">
                Менеджер попадёт в ваш отдел
                {data?.departments[0]?.name ? ` «${data.departments[0].name}»` : ''}.
              </div>
            )}

            {!isRopView && form.role === 'rop' ? (
              <label className="sm:col-span-2 flex items-start gap-3 rounded-[12px] border border-black/8 bg-[#f7f8fa] px-3.5 py-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={form.msk_tower}
                  onChange={(e) => setForm((f) => ({ ...f, msk_tower: e.target.checked }))}
                  className="mt-0.5 size-4 rounded border-black/20 accent-b24-cyan"
                />
                <span className="min-w-0">
                  <span className="block text-[13px] font-semibold text-b24-text-dark">МСК Башня</span>
                  <span className="mt-0.5 block text-[12px] text-b24-text-muted leading-snug">
                    По умолчанию выкл. Если включено — лиды, которые создаёт этот РОП, уходят только менеджерам его отдела.
                  </span>
                </span>
              </label>
            ) : null}
          </div>

          <div className="flex flex-wrap items-center gap-2 pt-1 border-t border-black/6">
            <Button
              type="submit"
              variant="green"
              disabled={creating}
              className="h-10 rounded-[10px] px-5"
              leftIcon={<KeyRound size={15} />}
            >
              {creating ? 'Создаём…' : 'Создать сотрудника'}
            </Button>
            <button
              type="button"
              onClick={() => {
                setForm({
                  full_name: '',
                  login: '',
                  email: '',
                  password: '',
                  role: creatable[0] || 'manager',
                  department_id: '',
                  msk_tower: false,
                })
                setShowPassword(false)
              }}
              className="h-10 px-3 text-[13px] text-b24-text-muted hover:text-b24-text-dark"
            >
              Очистить
            </button>
          </div>
        </form>
      ) : null}

      {error ? <p className="text-[13px] text-orange-200">{error}</p> : null}
      {note ? <p className="text-[13px] text-emerald-200 max-w-4xl">{note}</p> : null}

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
                placeholder="Поиск по имени, почте, отделу, номеру…"
                className="h-10 w-full rounded-[10px] border border-black/8 bg-[#f5f7f9] pl-9 pr-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan focus:bg-white"
              />
            </div>
            <select
              value={dept}
              onChange={(e) => setDept(e.target.value)}
              className={[
                'h-10 rounded-[10px] border border-black/8 bg-white px-3 text-[13px] text-b24-text-dark outline-none',
                isRopView ? 'hidden' : '',
              ].join(' ')}
            >
              <option value="">Все отделы</option>
              {(data?.departments ?? []).map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name}
                  {d.people ? ` · ${d.people}` : ''}
                </option>
              ))}
            </select>
          </div>

          <div className="flex flex-wrap gap-1.5">
            {(
              isRopView
                ? ([{ id: 'people' as const, label: 'Менеджеры', count: data?.total ?? 0 }] as const)
                : ([
                    { id: 'people' as const, label: 'Сотрудники', count: data?.total ?? 0 },
                    { id: 'numbers' as const, label: 'Номера и линии', count: linesCount },
                  ] as const)
            ).map((item) => {
              const active = tab === item.id
              return (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => setTab(item.id)}
                  className={[
                    'h-8 px-3 rounded-full text-[12px] font-medium inline-flex items-center gap-1.5 transition',
                    active
                      ? 'bg-b24-text-dark text-white'
                      : 'bg-[#eef1f4] text-b24-text hover:bg-[#e4e8ec]',
                  ].join(' ')}
                >
                  {item.label}
                  <span
                    className={[
                      'tabular-nums text-[11px] px-1.5 py-0.5 rounded-full',
                      active ? 'bg-white/20' : 'bg-white text-b24-text-muted',
                    ].join(' ')}
                  >
                    {item.count}
                  </span>
                </button>
              )
            })}
          </div>

          {tab === 'people' ? (
            <div className="flex flex-wrap gap-1.5">
              {roleTabs.map((item) => {
                const active = roleFilter === item.id
                return (
                  <button
                    key={item.id || 'all'}
                    type="button"
                    onClick={() => setRoleFilter(item.id)}
                    className={[
                      'h-8 px-3 rounded-full text-[12px] font-medium inline-flex items-center gap-1.5 transition',
                      active
                        ? 'bg-[#e8f8fe] text-b24-cyan-deep ring-1 ring-b24-cyan/40'
                        : 'bg-white text-b24-text ring-1 ring-black/8 hover:bg-[#f8fafb]',
                    ].join(' ')}
                  >
                    {item.label}
                    <span className="tabular-nums text-[11px] text-b24-text-muted">{item.count}</span>
                  </button>
                )
              })}
            </div>
          ) : null}
        </div>

        {tab === 'people' ? (
          <div className="flex-1 overflow-x-auto b24-scrollbar">
            {users.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-20 px-6 text-center">
                <div className="size-14 rounded-2xl bg-[#e8f8fe] text-b24-cyan-deep inline-flex items-center justify-center mb-3">
                  <Users size={26} />
                </div>
                <div className="text-[16px] font-semibold text-b24-text-dark">
                  {debounced || dept || roleFilter ? 'Никого не найдено' : 'Пока нет сотрудников'}
                </div>
                <p className="mt-1 text-[13px] text-b24-text-muted max-w-sm">
                  {debounced || dept || roleFilter
                    ? 'Сбросьте фильтры или измените запрос.'
                    : 'Добавьте менеджера — логин и пароль задаются сразу.'}
                </p>
                {canCreate && !debounced && !dept && !roleFilter ? (
                  <Button
                    type="button"
                    variant="green"
                    className="mt-4 rounded-[10px]"
                    onClick={() => setCreateOpen(true)}
                    leftIcon={<Plus size={16} />}
                  >
                    Добавить сотрудника
                  </Button>
                ) : null}
              </div>
            ) : (
              <table className="w-full min-w-[1020px] text-left">
                <thead className="sticky top-0 z-10 bg-[#f8f9fb] text-[11px] uppercase tracking-wide text-b24-text-muted">
                  <tr>
                    <th className="px-4 py-3 font-semibold">Сотрудник</th>
                    <th className="px-3 py-3 font-semibold">Отдел</th>
                    <th className="px-3 py-3 font-semibold">Роль</th>
                    <th className="px-3 py-3 font-semibold">Внутренний</th>
                    <th className="px-3 py-3 font-semibold">Телефон</th>
                    <th className="px-3 py-3 font-semibold">Voxi</th>
                    <th className="px-3 py-3 font-semibold">Статус</th>
                    {data?.can_offboard ? <th className="w-28 px-2 py-3" /> : null}
                  </tr>
                </thead>
                <tbody className="divide-y divide-black/[0.05]">
                  {users.map((u) => {
                    const active = u.is_active !== false
                    const ext = u.extension || u.phone_extension || ''
                    const ownVox = hasPersonalVox(u)
                    return (
                      <tr
                        key={u.id}
                        className="group transition-colors hover:bg-[#f3fafd] cursor-pointer"
                        onClick={() => navigate(`/employees/${u.id}`)}
                      >
                        <td className="px-4 py-3.5">
                          <div className="flex items-center gap-3 min-w-0">
                            <div className="size-10 rounded-full bg-[linear-gradient(135deg,#2fc6f6,#1ea8d4)] text-white text-[14px] font-semibold inline-flex items-center justify-center shrink-0 shadow-[0_4px_12px_rgba(47,198,246,0.35)]">
                              {initial(u.full_name)}
                            </div>
                            <div className="min-w-0">
                              <div className="text-[14px] font-semibold text-b24-text-dark truncate">
                                {u.full_name}
                              </div>
                              <div className="mt-0.5 flex items-center gap-1 text-[12px] text-b24-text-muted truncate">
                                <UserRound size={12} />
                                {u.login || u.email || '—'}
                                {u.login && u.email ? ` · ${u.email}` : ''}
                              </div>
                            </div>
                          </div>
                        </td>
                        <td className="px-3 py-3.5 text-[13px] text-b24-text" onClick={(e) => e.stopPropagation()}>
                          {data?.can_edit_department ? (
                            <select
                              value={u.department_id || ''}
                              disabled={pending === `dept:${u.id}`}
                              onChange={(e) => void changeDepartment(u, e.target.value)}
                              className="h-9 max-w-[180px] rounded-[8px] border border-black/10 bg-white px-2 text-[13px] text-b24-text-dark outline-none focus:border-b24-cyan disabled:opacity-60"
                              aria-label={`Отдел: ${u.full_name}`}
                            >
                              <option value="">Без отдела</option>
                              {(data?.departments ?? []).map((d) => (
                                <option key={d.id} value={d.id}>
                                  {d.name}
                                </option>
                              ))}
                            </select>
                          ) : (
                            u.department || '—'
                          )}
                        </td>
                        <td className="px-3 py-3.5">
                          <span
                            className={[
                              'inline-flex h-7 items-center rounded-full px-2.5 text-[12px] font-medium ring-1 ring-inset',
                              roleTone(u.role),
                            ].join(' ')}
                          >
                            {roleLabel(u.role)}
                          </span>
                        </td>
                        <td className="px-3 py-3.5">
                          {ext ? (
                            <span className="inline-flex items-center gap-1.5 text-[13px] font-medium tabular-nums text-b24-text-dark">
                              <Hash size={13} className="text-b24-text-muted" />
                              {ext}
                            </span>
                          ) : (
                            <span className="text-[13px] text-b24-text-muted">—</span>
                          )}
                        </td>
                        <td className="px-3 py-3.5">
                          {u.work_phone ? (
                            <span className="inline-flex items-center gap-1.5 text-[13px] font-medium tabular-nums text-b24-text-dark">
                              <Phone size={13} className="text-b24-green shrink-0" />
                              {u.work_phone}
                            </span>
                          ) : (
                            <span className="text-[13px] text-b24-text-muted">—</span>
                          )}
                        </td>
                        <td className="px-3 py-3.5">
                          <span
                            className={[
                              'inline-flex h-7 items-center rounded-full px-2.5 text-[12px] font-medium ring-1 ring-inset',
                              voxTone(ownVox),
                            ].join(' ')}
                            title={
                              ownVox
                                ? 'Личный Voximplant-аккаунт'
                                : 'Без личного аккаунта — softphone на общем (дефолтном) Voxi'
                            }
                          >
                            {ownVox ? 'Свой' : 'Дефолт'}
                          </span>
                        </td>
                        <td className="px-3 py-3.5">
                          <span
                            className={[
                              'inline-flex h-7 items-center rounded-full px-2.5 text-[12px] font-medium ring-1 ring-inset',
                              statusTone(active),
                            ].join(' ')}
                          >
                            {active ? 'Работает' : 'Неактивен'}
                          </span>
                        </td>
                        {data?.can_offboard ? (
                          <td className="px-2 py-3.5">
                            {active && u.id !== user?.id ? (
                              <button
                                type="button"
                                className="h-8 px-2.5 rounded-full text-[12px] font-medium text-orange-700 opacity-0 group-hover:opacity-100 hover:bg-orange-50 transition"
                                disabled={pending === u.id}
                                onClick={(e) => {
                                  e.stopPropagation()
                                  void fire(u)
                                }}
                              >
                                Уволить
                              </button>
                            ) : null}
                          </td>
                        ) : null}
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            )}
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto b24-scrollbar p-4 flex flex-col gap-3">
            <section className="rounded-[14px] border border-black/6 overflow-hidden bg-white">
              <div className="px-4 py-2.5 text-[13px] text-b24-text-muted border-b border-black/6 bg-[#f8f9fb]">
                Внутренние и рабочие. После увольнения внутренний в карантине{' '}
                {data?.numbers?.quarantine_days ?? 14} дней.
              </div>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[720px] text-left">
                  <thead className="text-[11px] uppercase tracking-wide text-b24-text-muted">
                    <tr>
                      <th className="px-4 py-3 font-semibold">Сотрудник</th>
                      <th className="px-3 py-3 font-semibold">Отдел</th>
                      <th className="px-3 py-3 font-semibold">Внутренний</th>
                      <th className="px-3 py-3 font-semibold">Телефон</th>
                      <th className="px-3 py-3 font-semibold">Линии</th>
                      <th className="px-3 py-3 font-semibold">Статус</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-black/[0.05]">
                    {(data?.numbers?.people ?? []).length === 0 ? (
                      <tr>
                        <td colSpan={6} className="px-4 py-10 text-center text-b24-text-muted">
                          Номеров пока нет
                        </td>
                      </tr>
                    ) : (
                      (data?.numbers?.people ?? []).map((p) => (
                        <tr key={p.id} className="hover:bg-[#f3fafd]">
                          <td className="px-4 py-3 text-[14px] font-medium text-b24-text-dark">
                            {p.full_name}
                          </td>
                          <td className="px-3 py-3 text-[13px]">{p.department || '—'}</td>
                          <td className="px-3 py-3 tabular-nums text-[13px]">{p.extension || '—'}</td>
                          <td className="px-3 py-3 tabular-nums text-[13px]">{p.work_phone || '—'}</td>
                          <td className="px-3 py-3 text-[13px]">{p.lines || 0}</td>
                          <td className="px-3 py-3">
                            <span
                              className={[
                                'inline-flex h-7 items-center rounded-full px-2.5 text-[12px] font-medium ring-1 ring-inset',
                                statusTone(p.is_active !== false),
                              ].join(' ')}
                            >
                              {p.is_active === false ? 'Неактивен' : 'Работает'}
                            </span>
                          </td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </section>

            <section className="rounded-[14px] border border-black/6 overflow-hidden bg-white">
              <div className="px-4 py-2.5 text-[13px] text-b24-text-muted border-b border-black/6 bg-[#f8f9fb]">
                Линии MAX / WhatsApp / Telegram. Личные отвязываются при увольнении, пулы остаются.
              </div>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[640px] text-left">
                  <thead className="text-[11px] uppercase tracking-wide text-b24-text-muted">
                    <tr>
                      <th className="px-4 py-3 font-semibold">Линия</th>
                      <th className="px-3 py-3 font-semibold">Канал</th>
                      <th className="px-3 py-3 font-semibold">Тип</th>
                      <th className="px-3 py-3 font-semibold">Владелец</th>
                      <th className="px-3 py-3 font-semibold">Статус</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-black/[0.05]">
                    {(data?.numbers?.lines ?? []).length === 0 ? (
                      <tr>
                        <td colSpan={5} className="px-4 py-10 text-center text-b24-text-muted">
                          Линий нет — заведите на «Входящие»
                        </td>
                      </tr>
                    ) : (
                      (data?.numbers?.lines ?? []).map((l) => (
                        <tr key={l.id} className="hover:bg-[#f3fafd]">
                          <td className="px-4 py-3 text-[14px] font-medium text-b24-text-dark">{l.name}</td>
                          <td className="px-3 py-3 text-[13px]">{l.channel}</td>
                          <td className="px-3 py-3 text-[13px]">
                            {l.kind === 'personal' ? 'личная' : l.kind === 'spam' ? 'спам' : 'общая'}
                          </td>
                          <td className="px-3 py-3 text-[13px]">{l.owner || '—'}</td>
                          <td className="px-3 py-3 text-[13px]">
                            {l.is_active === false ? 'пауза' : 'вкл'}
                          </td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </section>

            {(data?.numbers?.quarantine ?? []).length > 0 ? (
              <section className="rounded-[14px] border border-black/6 overflow-hidden bg-white">
                <div className="px-4 py-2.5 text-[13px] text-b24-text-muted border-b border-black/6 bg-[#f8f9fb]">
                  Карантин внутренних
                </div>
                <ul className="divide-y divide-black/[0.05] text-[13px] text-b24-text-dark">
                  {(data?.numbers?.quarantine ?? []).map((h) => (
                    <li key={h.extension} className="px-4 py-3">
                      {h.extension} до {when(h.until_at)}
                      {h.last_owner ? ` · был у ${h.last_owner}` : ''}
                    </li>
                  ))}
                </ul>
              </section>
            ) : null}

            {(data?.numbers?.log ?? []).length > 0 ? (
              <section className="rounded-[14px] border border-black/6 overflow-hidden bg-white">
                <div className="px-4 py-2.5 text-[13px] text-b24-text-muted border-b border-black/6 bg-[#f8f9fb]">
                  Последние увольнения
                </div>
                <ul className="divide-y divide-black/[0.05] text-[13px] text-b24-text-dark">
                  {(data?.numbers?.log ?? []).map((row, i) => (
                    <li key={`${row.created_at}-${i}`} className="px-4 py-3">
                      {when(row.created_at)} · {row.target || 'сотрудник'} · {row.actor || 'админ'}
                    </li>
                  ))}
                </ul>
              </section>
            ) : null}
          </div>
        )}
      </div>
    </div>
  )
}

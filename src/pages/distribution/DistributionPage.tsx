import { Building2, Check, ChevronDown, Plus, Power, Save, Search, UserRound } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { PageHeader } from '../../components/layout/PageHeader'
import { Button } from '../../components/ui/Button'
import { api } from '../../lib/api'
import { roleLabel } from '../../lib/roles'
import { useAuth } from '../../store/auth'
import { RopTeamDistributionPage } from './RopTeamDistributionPage'

type DistDept = {
  id: string
  name: string
  head_id?: string
  head_name?: string
  people?: number
  assigned?: number
  access_enabled?: boolean
}
type DistUser = { id: string; full_name: string; department_id?: string; role?: string }
type DistQuota = { department_id: string; pool: string; share: number; daily_limit: number; today: number }
type DistData = {
  pools: { id: string; label: string }[]
  departments: DistDept[]
  users: DistUser[]
  rops?: DistUser[]
  quotas: DistQuota[]
  auto?: boolean
  sources?: { source: string; pool: string }[]
  bank?: number
}

const empty: DistData = {
  pools: [
    { id: 'depts', label: 'По отделам' },
    { id: 'skorozvon', label: 'Скорозвон' },
  ],
  departments: [],
  users: [],
  quotas: [],
  auto: true,
  sources: [],
}

function num(v: unknown) {
  const n = Number(v)
  return Number.isFinite(n) ? n : 0
}

function parseLimit(raw: string) {
  const cleaned = String(raw).replace(/\D/g, '')
  if (cleaned === '') return null
  const n = Number(cleaned)
  if (!Number.isFinite(n) || n < 0) return null
  return Math.min(100_000, Math.floor(n))
}

function initial(name: string) {
  return (name.trim().slice(0, 1) || '?').toUpperCase()
}

function avatarTone(i: number) {
  const tones = [
    'from-[#5b8def] to-[#3d6fd8]',
    'from-[#2fc06e] to-[#1fa85a]',
    'from-[#a78bfa] to-[#7c5cbf]',
    'from-[#f59e0b] to-[#d97706]',
    'from-[#ec4899] to-[#db2777]',
  ]
  return tones[i % tones.length]
}

/** Числовое поле: можно стереть и набрать заново, значение фиксируется на blur. */
function SoftNumber({
  value,
  onCommit,
  parse,
  format,
  suffix,
  className,
  placeholder,
}: {
  value: number
  onCommit: (n: number) => void
  parse: (raw: string) => number | null
  format: (n: number) => string
  suffix?: string
  className?: string
  placeholder?: string
}) {
  const [text, setText] = useState(() => (value > 0 ? format(value) : ''))
  const [focused, setFocused] = useState(false)

  useEffect(() => {
    if (!focused) setText(value > 0 ? format(value) : '')
  }, [value, focused, format])

  return (
    <div className="relative inline-flex">
      <input
        type="text"
        inputMode="decimal"
        value={text}
        placeholder={placeholder ?? '0'}
        onFocus={() => setFocused(true)}
        onChange={(e) => setText(e.target.value)}
        onBlur={() => {
          setFocused(false)
          const n = parse(text)
          onCommit(n == null ? 0 : n)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
        }}
        className={className}
      />
      {suffix ? (
        <span className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-[11px] text-b24-text-muted">
          {suffix}
        </span>
      ) : null}
    </div>
  )
}

function PersonPicker({
  value,
  options,
  onChange,
  placeholder = 'Выбрать РОПа',
}: {
  value: string
  options: DistUser[]
  onChange: (id: string) => void
  placeholder?: string
}) {
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const root = useRef<HTMLDivElement>(null)
  const selected = options.find((u) => u.id === value)

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  const filtered = useMemo(() => {
    const s = q.trim().toLowerCase()
    if (!s) return options
    return options.filter(
      (u) =>
        u.full_name.toLowerCase().includes(s) ||
        String(u.role || '')
          .toLowerCase()
          .includes(s),
    )
  }, [options, q])

  return (
    <div ref={root} className="relative min-w-[180px] max-w-[260px]">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={[
          'h-9 w-full rounded-[10px] border px-2.5 text-left text-[13px] inline-flex items-center gap-2 transition-colors',
          open
            ? 'border-b24-cyan bg-white ring-1 ring-b24-cyan/30'
            : 'border-black/10 bg-white hover:bg-[#f8fafb]',
        ].join(' ')}
      >
        {selected ? (
          <>
            <span className="size-6 rounded-full bg-sky-100 text-sky-800 text-[10px] font-semibold inline-flex items-center justify-center shrink-0">
              {initial(selected.full_name)}
            </span>
            <span className="truncate flex-1 text-b24-text-dark font-medium">{selected.full_name}</span>
          </>
        ) : (
          <span className="truncate flex-1 text-b24-text-muted">{placeholder}</span>
        )}
        <ChevronDown size={14} className={`shrink-0 text-b24-text-muted transition ${open ? 'rotate-180' : ''}`} />
      </button>

      {open ? (
        <div className="absolute z-30 mt-1 w-[min(300px,80vw)] rounded-[12px] border border-black/10 bg-white shadow-[0_12px_40px_rgba(15,23,42,0.14)] overflow-hidden">
          <div className="p-2 border-b border-black/6">
            <div className="relative">
              <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-b24-text-muted" />
              <input
                autoFocus
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder="Найти…"
                className="h-9 w-full rounded-[8px] border border-black/8 bg-[#f5f7f9] pl-8 pr-2 text-[13px] outline-none focus:border-b24-cyan focus:bg-white"
              />
            </div>
          </div>
          <ul className="max-h-56 overflow-y-auto py-1">
            <li>
              <button
                type="button"
                onClick={() => {
                  onChange('')
                  setOpen(false)
                  setQ('')
                }}
                className="w-full px-3 py-2 text-left text-[13px] text-b24-text-muted hover:bg-[#f5f8fa]"
              >
                Не задан
              </button>
            </li>
            {filtered.length === 0 ? (
              <li className="px-3 py-3 text-[12px] text-b24-text-muted">Никого не найдено</li>
            ) : (
              filtered.map((u, i) => {
                const active = u.id === value
                return (
                  <li key={u.id}>
                    <button
                      type="button"
                      onClick={() => {
                        onChange(u.id)
                        setOpen(false)
                        setQ('')
                      }}
                      className={[
                        'w-full px-3 py-2 flex items-center gap-2.5 text-left transition-colors',
                        active ? 'bg-[#e8f8fe]' : 'hover:bg-[#f5f8fa]',
                      ].join(' ')}
                    >
                      <span
                        className={`size-8 rounded-full bg-gradient-to-br ${avatarTone(i)} text-white text-[11px] font-semibold inline-flex items-center justify-center shrink-0`}
                      >
                        {initial(u.full_name)}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block text-[13px] font-medium text-b24-text-dark truncate">
                          {u.full_name}
                        </span>
                        <span className="block text-[11px] text-b24-text-muted">
                          {roleLabel(u.role || '')}
                        </span>
                      </span>
                      {active ? <Check size={14} className="text-b24-cyan shrink-0" /> : null}
                    </button>
                  </li>
                )
              })
            )}
          </ul>
        </div>
      ) : null}
    </div>
  )
}

export function DistributionPage() {
  const { user } = useAuth()
  const role = String(user?.role || '').toLowerCase()
  if (role === 'rop') {
    return <RopTeamDistributionPage />
  }
  return <AdminDistributionPage />
}

function AdminDistributionPage() {
  const [pool, setPool] = useState('depts')
  const [data, setData] = useState<DistData>(empty)
  const [limits, setLimits] = useState<Record<string, number>>({})
  const [heads, setHeads] = useState<Record<string, string>>({})
  const [query, setQuery] = useState('')
  const [auto, setAuto] = useState(true)
  const [dirty, setDirty] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [pending, setPending] = useState(false)
  const [createOpen, setCreateOpen] = useState(true)
  const [creating, setCreating] = useState(false)
  const [bankDept, setBankDept] = useState('')
  const [bankBusy, setBankBusy] = useState(false)
  const nameRef = useRef<HTMLInputElement>(null)
  const [quick, setQuick] = useState({
    name: '',
    rop_name: '',
    managersText: '',
    share: 0,
    daily_limit: 200,
    msk_tower: false,
  })
  const [lastTxt, setLastTxt] = useState<{ filename: string; txt: string } | null>(null)
  const [accessBusy, setAccessBusy] = useState<string | null>(null)

  const markDirty = () => {
    setDirty(true)
    setNote('')
  }

  const load = () =>
    api<DistData>('/distribution')
      .then((d) => {
        setData(d)
        const lim: Record<string, number> = {}
        for (const q of d.quotas ?? []) {
          lim[q.department_id] = Math.max(lim[q.department_id] ?? 0, num(q.daily_limit))
        }
        setLimits(lim)
        const ropIds = new Set(
          (d.rops ?? d.users ?? [])
            .filter((u) => String(u.role || '').toLowerCase() === 'rop')
            .map((u) => u.id),
        )
        const h: Record<string, string> = {}
        for (const dep of d.departments ?? []) {
          const hid = dep.head_id && dep.head_id !== 'null' ? String(dep.head_id) : ''
          // в пикере только РОПы — админ/менеджер как «голова» сбрасываем в UI
          if (hid && ropIds.has(hid)) h[dep.id] = hid
        }
        setHeads(h)
        setAuto(d.auto !== false)
        setDirty(false)
      })
      .catch((e: Error) => setError(e.message))

  useEffect(() => {
    void load()
  }, [])

  useEffect(() => {
    if (pool === 'bars') setPool('depts')
  }, [pool])

  const todayByDept = useMemo(() => {
    const m: Record<string, number> = {}
    for (const q of data.quotas ?? []) {
      m[q.department_id] = Math.max(m[q.department_id] ?? 0, num(q.today))
    }
    return m
  }, [data.quotas])

  const totalAssigned = useMemo(
    () => (data.departments ?? []).reduce((a, d) => a + (todayByDept[d.id] ?? 0), 0),
    [data.departments, todayByDept],
  )

  const sumLimits = useMemo(
    () => (data.departments ?? []).reduce((acc, dep) => acc + (limits[dep.id] ?? 0), 0),
    [data.departments, limits],
  )

  const totalTodayPool = useMemo(() => {
    let t = 0
    for (const dep of data.departments ?? []) {
      if ((limits[dep.id] ?? 0) <= 0) continue
      t += todayByDept[dep.id] ?? 0
    }
    return t
  }, [data.departments, limits, todayByDept])

  const rops = useMemo(() => {
    const fromApi = (data.rops ?? []).filter((u) => String(u.role || '').toLowerCase() === 'rop')
    if (fromApi.length) return fromApi
    return (data.users ?? []).filter((u) => String(u.role || '').toLowerCase() === 'rop')
  }, [data.rops, data.users])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return (data.departments ?? []).filter((dep) => {
      if (!q) return true
      const headName =
        (data.users ?? []).find((u) => u.id === (heads[dep.id] || dep.head_id))?.full_name ||
        dep.head_name ||
        ''
      return [dep.name, headName].join(' ').toLowerCase().includes(q)
    })
  }, [data.departments, data.users, heads, query])

  const stats = useMemo(() => {
    const depts = data.departments ?? []
    const withLimit = depts.filter((d) => (limits[d.id] ?? 0) > 0).length
    const withRop = depts.filter((d) => Boolean(heads[d.id])).length
    return { depts: depts.length, withLimit, withRop, assigned: totalAssigned, rops: rops.length }
  }, [data.departments, limits, heads, totalAssigned, rops.length])

  const setLimit = (depId: string, next: number) => {
    const v = Math.max(0, Math.min(100_000, Math.floor(next)))
    setLimits((prev) => ({ ...prev, [depId]: v }))
    markDirty()
  }

  const save = async () => {
    const pools = (data.pools ?? empty.pools).filter((p) => p.id !== 'bars')
    setPending(true)
    setError('')
    setNote('')
    try {
      const quotas: { department_id: string; pool: string; share: number; daily_limit: number }[] = []
      for (const p of pools) {
        for (const dep of data.departments ?? []) {
          quotas.push({
            department_id: dep.id,
            pool: p.id,
            share: 0,
            daily_limit: limits[dep.id] ?? 0,
          })
        }
      }
      await api('/distribution', {
        method: 'PUT',
        body: JSON.stringify({
          quotas,
          heads: (data.departments ?? []).map((dep) => ({
            department_id: dep.id,
            head_id: heads[dep.id] ?? '',
          })),
          auto,
          sources: data.sources ?? [],
        }),
      })
      setNote('Сохранено')
      setDirty(false)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось сохранить')
    } finally {
      setPending(false)
    }
  }

  const downloadTxt = (filename: string, txt: string) => {
    const blob = new Blob([txt], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename || 'otdel.txt'
    a.click()
    URL.revokeObjectURL(url)
  }

  const toggleDeptAccess = async (deptId: string, enabled: boolean) => {
    setAccessBusy(deptId)
    setError('')
    setNote('')
    try {
      await api(`/departments/${deptId}/access`, {
        method: 'POST',
        body: JSON.stringify({ enabled }),
      })
      setData((prev) => ({
        ...prev,
        departments: (prev.departments ?? []).map((d) =>
          d.id === deptId ? { ...d, access_enabled: enabled } : d,
        ),
      }))
      setNote(
        enabled
          ? 'Доступ отдела включён — сотрудники снова могут войти'
          : 'Доступ отдела выключен — сотрудники не смогут войти (данные сохранены)',
      )
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось переключить доступ')
    } finally {
      setAccessBusy(null)
    }
  }

  const createQuickDept = async (e: FormEvent) => {
    e.preventDefault()
    if (!quick.name.trim() || !quick.rop_name.trim()) return
    const managers = quick.managersText
      .split(/\n/)
      .map((s) => s.trim())
      .filter(Boolean)
    setCreating(true)
    setError('')
    setNote('')
    setLastTxt(null)
    try {
      const res = await api<{
        department_name?: string
        credentials?: { full_name: string; login: string; password: string; role_label: string }[]
        txt?: string
        filename?: string
      }>('/departments/quick', {
        method: 'POST',
        body: JSON.stringify({
          name: quick.name.trim(),
          rop_name: quick.rop_name.trim(),
          managers,
          share: 0,
          daily_limit: quick.daily_limit || 200,
          pool,
          msk_tower: quick.msk_tower,
        }),
      })
      const n = res.credentials?.length ?? 0
      setNote(`Отдел «${res.department_name || quick.name.trim()}» создан · учёток: ${n}`)
      if (res.txt) {
        const file = { filename: res.filename || 'otdel.txt', txt: res.txt }
        setLastTxt(file)
        downloadTxt(file.filename, file.txt)
      }
      setQuick((q) => ({ ...q, name: '', rop_name: '', managersText: '', msk_tower: false }))
      await load()
      queueMicrotask(() => nameRef.current?.focus())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'не удалось создать отдел')
    } finally {
      setCreating(false)
    }
  }

  const deleteDept = async () => {
    const deptId = bankDept || (data.departments ?? [])[0]?.id || ''
    if (!deptId) return
    const dep = (data.departments ?? []).find((d) => d.id === deptId)
    if (!window.confirm(`Удалить отдел «${dep?.name || ''}»? Лиды останутся у текущих владельцев.`)) return
    setBankBusy(true)
    setError('')
    setNote('')
    try {
      const res = await api<{ name?: string }>(`/departments/${deptId}`, { method: 'DELETE' })
      setNote(`Отдел «${res.name || dep?.name || ''}» удалён`)
      setBankDept('')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось')
    } finally {
      setBankBusy(false)
    }
  }

  const poolTabs = (data.pools ?? empty.pools)
    .filter((p) => p.id !== 'bars')
    .map((p) => ({
    id: p.id,
    label: p.label,
    count: (data.departments ?? []).filter((d) => (limits[d.id] ?? 0) > 0).length,
  }))

  return (
    <div className="flex flex-col gap-4 pt-1 pb-4 min-h-[calc(100%-2rem)]">
      <PageHeader
        title="Распределение"
        subtitle="Лидов в день на отдел · выдача по % выполнения плана"
        actions={
          <>
            <Button
              type="button"
              variant="green"
              className="h-10 rounded-[10px] px-4 shadow-[0_8px_24px_rgba(47,192,110,0.35)]"
              onClick={() => setCreateOpen((v) => !v)}
              leftIcon={<Plus size={16} strokeWidth={2.5} />}
            >
              {createOpen ? 'Скрыть' : 'Быстрый отдел'}
            </Button>
            <Button
              type="button"
              variant="green"
              className={[
                'h-10 rounded-[10px] px-4',
                dirty ? 'shadow-[0_8px_24px_rgba(47,192,110,0.35)]' : '',
              ].join(' ')}
              disabled={pending || !(data.departments ?? []).length}
              onClick={() => void save()}
              leftIcon={<Save size={16} />}
            >
              {pending ? 'Сохраняем…' : dirty ? 'Сохранить изменения' : 'Сохранить'}
            </Button>
          </>
        }
      />

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-2.5">
        {[
          { label: 'Отделов', value: stats.depts, hint: 'в структуре' },
          { label: 'С лимитом', value: stats.withLimit, hint: 'лидов/день > 0' },
          { label: 'С РОПом', value: stats.withRop, hint: `РОПов: ${stats.rops}` },
          { label: 'Начислено', value: stats.assigned, hint: 'за сегодня · сброс 00:00 МСК' },
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

      {error ? <p className="text-[13px] text-orange-200">{error}</p> : null}
      {note ? <p className="text-[13px] text-emerald-200">{note}</p> : null}

      <div className="b24-glass-panel p-4 flex flex-col gap-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <div className="text-[15px] font-semibold text-b24-text-dark">Автораздача</div>
            <p className="mt-0.5 text-[12px] text-b24-text-muted">
              Банк и отзыв отключены. Новая заявка: отдел → РОП → менеджер.
              Ставите лидов на день (100 / 60 / 100) — система держит всех на одном % плана.
            </p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <select
            value={bankDept}
            onChange={(e) => setBankDept(e.target.value)}
            className="h-10 rounded-[10px] border border-black/10 bg-white px-3 text-[13px] text-[#1a2140]"
          >
            <option value="">Отдел</option>
            {(data.departments ?? []).map((d) => (
              <option key={d.id} value={d.id}>{d.name}</option>
            ))}
          </select>
          <Button type="button" variant="filter" className="h-10 rounded-[10px] px-3" disabled={bankBusy || !bankDept} onClick={() => void deleteDept()}>
            Удалить отдел
          </Button>
        </div>
      </div>

      {createOpen ? (
        <form onSubmit={(e) => void createQuickDept(e)} className="b24-glass-panel p-4 flex flex-col gap-3">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="text-[15px] font-semibold text-b24-text-dark">Быстрый отдел</div>
              <p className="mt-0.5 text-[12px] text-b24-text-muted">
                Только ФИО — логины и пароли сгенерируются сами, файл скачается сразу
              </p>
            </div>
            <button
              type="button"
              onClick={() => setCreateOpen(false)}
              className="text-[12px] text-b24-text-muted hover:text-b24-text-dark shrink-0"
            >
              Скрыть
            </button>
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-12 gap-3">
            <label className="flex flex-col gap-1 lg:col-span-4">
              <span className="text-[11px] font-medium text-b24-text-muted">Название отдела</span>
              <input
                ref={nameRef}
                required
                autoFocus
                value={quick.name}
                onChange={(e) => setQuick((q) => ({ ...q, name: e.target.value }))}
                placeholder="Отдел Миши"
                className="h-10 w-full rounded-[10px] border border-black/10 bg-white px-3 text-[14px] text-[#1a2140] placeholder:text-[#9aa3ad] outline-none focus:border-b24-cyan"
              />
            </label>
            <label className="flex flex-col gap-1 lg:col-span-4">
              <span className="text-[11px] font-medium text-b24-text-muted">ФИО РОПа</span>
              <input
                required
                value={quick.rop_name}
                onChange={(e) => setQuick((q) => ({ ...q, rop_name: e.target.value }))}
                placeholder="Михаил РОП"
                className="h-10 w-full rounded-[10px] border border-black/10 bg-white px-3 text-[14px] text-[#1a2140] placeholder:text-[#9aa3ad] outline-none focus:border-b24-cyan"
              />
            </label>
            <label className="flex flex-col gap-1 lg:col-span-4">
              <span className="text-[11px] font-medium text-b24-text-muted">Лидов в день</span>
              <input
                type="number"
                min={0}
                value={quick.daily_limit}
                onChange={(e) => {
                  const n = Math.floor(Number(e.target.value) || 0)
                  setQuick((q) => ({ ...q, daily_limit: Math.max(0, Math.min(100_000, n)) }))
                }}
                className="h-10 w-full rounded-[10px] border border-black/10 bg-white px-2 text-[14px] text-[#1a2140] tabular-nums"
              />
            </label>
            <label className="flex flex-col gap-1 lg:col-span-12">
              <span className="text-[11px] font-medium text-b24-text-muted">
                Менеджеры — по одному ФИО на строку
              </span>
              <textarea
                value={quick.managersText}
                onChange={(e) => setQuick((q) => ({ ...q, managersText: e.target.value }))}
                rows={6}
                placeholder={'Корнеев Виктор\nМихаил Юрьевич\nФинист Соколов'}
                className="w-full rounded-[10px] border border-black/10 bg-white px-3 py-2 text-[14px] text-[#1a2140] placeholder:text-[#9aa3ad] outline-none focus:border-b24-cyan resize-y min-h-[120px]"
              />
            </label>
            <label className="lg:col-span-12 flex items-start gap-3 rounded-[12px] border border-black/8 bg-[#f7f8fa] px-3.5 py-3 cursor-pointer">
              <input
                type="checkbox"
                checked={quick.msk_tower}
                onChange={(e) => setQuick((q) => ({ ...q, msk_tower: e.target.checked }))}
                className="mt-0.5 size-4 rounded border-black/20 accent-b24-cyan"
              />
              <span className="min-w-0">
                <span className="block text-[13px] font-semibold text-b24-text-dark">МСК Башня</span>
                <span className="mt-0.5 block text-[12px] text-b24-text-muted leading-snug">
                  По умолчанию нет. Если да — лиды этого РОПа уходят только менеджерам его отдела.
                </span>
              </span>
            </label>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="submit"
              variant="green"
              disabled={creating || !quick.name.trim() || !quick.rop_name.trim()}
              className="h-10 rounded-[10px] px-4"
              leftIcon={<Plus size={16} strokeWidth={2.5} />}
            >
              {creating ? 'Создаём…' : 'Создать и скачать логины'}
            </Button>
            {lastTxt ? (
              <Button
                type="button"
                variant="filter"
                className="h-10 rounded-[10px] px-3"
                onClick={() => downloadTxt(lastTxt.filename, lastTxt.txt)}
              >
                Скачать снова
              </Button>
            ) : null}
          </div>
        </form>
      ) : null}

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
                placeholder="Поиск по отделу или РОПу…"
                className="h-10 w-full rounded-[10px] border border-black/8 bg-[#f5f7f9] pl-9 pr-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan focus:bg-white"
              />
            </div>
            <label className="h-10 px-3.5 rounded-[10px] text-[13px] font-medium border bg-white border-black/8 text-b24-text inline-flex items-center gap-2">
              <input
                type="checkbox"
                checked={auto}
                onChange={(e) => {
                  setAuto(e.target.checked)
                  markDirty()
                }}
              />
              Автораздача
            </label>
            <span className="h-10 px-3 rounded-[10px] text-[13px] font-medium inline-flex items-center bg-slate-50 text-slate-700 ring-1 ring-slate-200">
              План Σ {sumLimits.toLocaleString('ru-RU')}/день
            </span>
          </div>

          <div className="flex flex-wrap gap-1.5">
            {poolTabs.map((tab) => {
              const active = pool === tab.id
              return (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => setPool(tab.id)}
                  className={[
                    'h-8 rounded-full px-3 text-[13px] font-medium inline-flex items-center gap-1.5 transition-colors',
                    active
                      ? 'bg-[#e8f8fe] text-b24-cyan-deep ring-1 ring-b24-cyan/40'
                      : 'bg-white text-b24-text ring-1 ring-black/8 hover:bg-[#f8fafb]',
                  ].join(' ')}
                >
                  {tab.label}
                  <span className="tabular-nums text-[11px] text-b24-text-muted">{tab.count}</span>
                </button>
              )
            })}
          </div>
        </div>

        {(data.departments ?? []).length === 0 ? (
          <div className="flex-1 flex flex-col items-center justify-center px-6 py-16 text-center">
            <div className="size-14 rounded-2xl bg-[#e8f8fe] text-b24-cyan-deep inline-flex items-center justify-center mb-3">
              <Building2 size={26} />
            </div>
            <div className="text-[16px] font-semibold text-b24-text-dark">Пока нет отделов</div>
            <p className="mt-1 text-[13px] text-b24-text-muted max-w-sm">
              Создайте отдел, назначьте РОПа и лимит лидов на день — заявки пойдут по плану.
            </p>
            <Button
              type="button"
              variant="green"
              className="mt-4 h-10 rounded-[10px] px-4"
              onClick={() => setCreateOpen(true)}
              leftIcon={<Plus size={16} />}
            >
              Быстрый отдел
            </Button>
          </div>
        ) : filtered.length === 0 ? (
          <div className="flex-1 flex flex-col items-center justify-center py-16 text-b24-text-muted text-[14px]">
            Ничего не найдено
          </div>
        ) : (
          <div className="overflow-auto flex-1">
            <table className="w-full text-left">
              <thead className="sticky top-0 z-10 bg-[#f8f9fb] text-[11px] uppercase tracking-wide text-b24-text-muted">
                <tr>
                  <th className="px-4 py-2.5 font-medium">Отдел</th>
                  <th className="px-3 py-2.5 font-medium">РОП</th>
                  <th className="px-3 py-2.5 font-medium w-[130px]">Лидов/день</th>
                  <th className="px-3 py-2.5 font-medium">Сегодня</th>
                  <th className="px-3 py-2.5 font-medium">Статус</th>
                  <th className="px-3 py-2.5 font-medium">Сотр.</th>
                  <th className="px-3 py-2.5 font-medium w-[120px]">Доступ</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((dep, idx) => {
                  const today = todayByDept[dep.id] ?? 0
                  const lim = limits[dep.id] ?? 0
                  const fill = lim > 0 ? today / lim : 0
                  const avgFill = sumLimits > 0 ? totalTodayPool / sumLimits : 0
                  const accessOn = dep.access_enabled !== false
                  const status = !accessOn
                    ? 'доступ выкл'
                    : lim <= 0
                      ? 'выкл'
                      : today >= lim
                        ? 'лимит'
                        : fill + 0.03 < avgFill
                          ? 'догоняем'
                          : fill - 0.03 > avgFill
                            ? 'впереди'
                            : 'в норме'
                  const statusClass =
                    status === 'догоняем'
                      ? 'bg-sky-50 text-sky-800 ring-sky-200'
                      : status === 'впереди'
                        ? 'bg-amber-50 text-amber-800 ring-amber-200'
                        : status === 'лимит'
                          ? 'bg-rose-50 text-rose-700 ring-rose-200'
                          : status === 'выкл' || status === 'доступ выкл'
                            ? 'bg-slate-100 text-slate-500 ring-slate-200'
                            : 'bg-emerald-50 text-emerald-800 ring-emerald-200'
                  return (
                    <tr
                      key={dep.id}
                      className={`border-t border-black/[0.05] hover:bg-[#fafbfc] ${
                        accessOn ? '' : 'bg-slate-50/80 opacity-80'
                      }`}
                    >
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2.5 min-w-0">
                          <div
                            className={`size-9 rounded-full bg-gradient-to-br ${avatarTone(idx)} text-white text-[13px] font-semibold inline-flex items-center justify-center shrink-0`}
                          >
                            {initial(dep.name)}
                          </div>
                          <div className="text-[14px] font-semibold text-b24-text-dark truncate">
                            {dep.name}
                          </div>
                        </div>
                      </td>
                      <td className="px-3 py-3">
                        <PersonPicker
                          value={heads[dep.id] ?? ''}
                          options={rops}
                          placeholder="Выбрать РОПа"
                          onChange={(id) => {
                            setHeads((prev) => ({ ...prev, [dep.id]: id }))
                            markDirty()
                          }}
                        />
                      </td>
                      <td className="px-3 py-3">
                        <SoftNumber
                          value={lim}
                          onCommit={(n) => setLimit(dep.id, n)}
                          parse={parseLimit}
                          format={(n) => String(n)}
                          placeholder="0"
                          className="h-9 w-[100px] rounded-[10px] border border-black/10 bg-[#f5f7f9] px-2 text-[13px] text-b24-text-dark outline-none focus:border-b24-cyan focus:bg-white"
                        />
                      </td>
                      <td className="px-3 py-3 text-[13px] tabular-nums text-b24-text-dark font-semibold">
                        {today.toLocaleString('ru-RU')}
                        {lim > 0 ? (
                          <span className="ml-1 font-normal text-b24-text-muted">
                            / {lim.toLocaleString('ru-RU')} · {Math.round(fill * 100)}%
                          </span>
                        ) : null}
                      </td>
                      <td className="px-3 py-3">
                        <span
                          className={`inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium ring-1 ${statusClass}`}
                        >
                          {status}
                        </span>
                      </td>
                      <td className="px-3 py-3 text-[13px] text-b24-text-muted tabular-nums">
                        <span className="inline-flex items-center gap-1">
                          <UserRound size={13} />
                          {dep.people ?? 0}
                        </span>
                      </td>
                      <td className="px-3 py-3">
                        <button
                          type="button"
                          disabled={accessBusy === dep.id}
                          title={
                            accessOn
                              ? 'Отключить вход для сотрудников отдела'
                              : 'Включить вход для сотрудников отдела'
                          }
                          onClick={() => void toggleDeptAccess(dep.id, !accessOn)}
                          className={`inline-flex h-9 items-center gap-1.5 rounded-[10px] px-2.5 text-[12px] font-medium ring-1 transition disabled:opacity-60 ${
                            accessOn
                              ? 'bg-emerald-50 text-emerald-800 ring-emerald-200 hover:bg-emerald-100'
                              : 'bg-rose-50 text-rose-700 ring-rose-200 hover:bg-rose-100'
                          }`}
                        >
                          <Power size={14} />
                          {accessBusy === dep.id ? '…' : accessOn ? 'Вкл' : 'Выкл'}
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}

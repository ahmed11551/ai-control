import { Save, Search, Users } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { PageHeader } from '../../components/layout/PageHeader'
import { Button } from '../../components/ui/Button'
import { api } from '../../lib/api'

type TeamManager = {
  id: string
  full_name: string
  role?: string
  share?: number
  daily_limit?: number
  today?: number
  assigned?: number
}

type TeamData = {
  department_id: string
  department_name: string
  managers: TeamManager[]
  bank?: number
  today?: string
  mode_note?: string
  auto?: boolean
  sleeping_tab?: boolean
  pool?: string
}

type DistPool = 'new' | 'sleeping'

const LIMIT_PRESETS = [50, 100, 150, 200]

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

/** Страница распределения для РОПа — тот же визуальный язык, что у админки. */
export function RopTeamDistributionPage() {
  const navigate = useNavigate()
  const [pool, setPool] = useState<DistPool>('new')
  const [data, setData] = useState<TeamData | null>(null)
  const [limits, setLimits] = useState<Record<string, number>>({})
  const [query, setQuery] = useState('')
  const [dirty, setDirty] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [pending, setPending] = useState(false)
  const [auto, setAuto] = useState(true)

  const markDirty = () => {
    setDirty(true)
    setNote('')
  }

  const load = (next: DistPool = pool) =>
    api<TeamData>(next === 'sleeping' ? '/distribution/team?pool=sleeping' : '/distribution/team')
      .then((d) => {
        setData((prev) => ({
          ...d,
          sleeping_tab: d.sleeping_tab || prev?.sleeping_tab,
        }))
        setAuto(d.auto !== false)
        const lim: Record<string, number> = {}
        for (const m of d.managers ?? []) {
          lim[m.id] = num(m.daily_limit)
        }
        setLimits(lim)
        setDirty(false)
        setError('')
      })
      .catch((e: Error) => {
        const msg = e.message || 'ошибка'
        if (/not found/i.test(msg)) {
          setError('API ещё без раздачи менеджерам — пересоберите backend (git pull && docker compose up -d --build backend)')
        } else {
          setError(msg)
        }
      })

  useEffect(() => {
    void load(pool)
    // pool is the only reload key; load closes over the latest setters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pool])

  const managers = data?.managers ?? []

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return managers
    return managers.filter((m) => m.full_name.toLowerCase().includes(q))
  }, [managers, query])

  const sumLimits = useMemo(
    () => managers.reduce((acc, m) => acc + (limits[m.id] ?? 0), 0),
    [managers, limits],
  )

  const totalToday = useMemo(() => {
    let t = 0
    for (const m of managers) {
      if ((limits[m.id] ?? 0) <= 0) continue
      t += num(m.today)
    }
    return t
  }, [managers, limits])

  const totalAssigned = useMemo(
    () => managers.reduce((acc, m) => acc + num(m.today), 0),
    [managers],
  )

  const withLimit = managers.filter((m) => (limits[m.id] ?? 0) > 0).length

  const save = async () => {
    setPending(true)
    setError('')
    setNote('')
    try {
      await api(pool === 'sleeping' ? '/distribution/team?pool=sleeping' : '/distribution/team', {
        method: 'PUT',
        body: JSON.stringify({
          department_id: data?.department_id,
          auto,
          quotas: managers.map((m) => ({
            user_id: m.id,
            share: 0,
            daily_limit: limits[m.id] ?? 0,
          })),
        }),
      })
      setNote('Сохранено')
      setDirty(false)
      await load(pool)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось сохранить')
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="flex flex-col gap-4 pt-1 pb-4 min-h-[calc(100%-2rem)]">
      <PageHeader
        title="Распределение"
        subtitle={
          data?.sleeping_tab
            ? `${data.department_name || 'Отдел'} · ${pool === 'sleeping' ? 'спящие лиды' : 'новые лиды'} · лидов/день на менеджера`
            : data?.department_name
              ? `${data.department_name} · лидов/день на менеджера · catch-up по % плана`
              : 'Лидов в день на менеджера · авто: РОП → менеджер'
        }
        actions={
          <Button
            type="button"
            variant="green"
            className={[
              'h-10 rounded-[10px] px-4',
              dirty ? 'shadow-[0_8px_24px_rgba(47,192,110,0.35)]' : '',
            ].join(' ')}
            disabled={pending || !managers.length}
            onClick={() => void save()}
            leftIcon={<Save size={16} />}
          >
            {pending ? 'Сохраняем…' : dirty ? 'Сохранить изменения' : 'Сохранить'}
          </Button>
        }
      />

      {data?.sleeping_tab ? (
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={() => setPool('new')}
            className={[
              'h-10 rounded-[10px] px-4 text-[13px] font-medium',
              pool === 'new' ? 'bg-white text-b24-text-dark' : 'bg-white/10 text-white/80',
            ].join(' ')}
          >
            Новые лиды
          </button>
          <button
            type="button"
            onClick={() => setPool('sleeping')}
            className={[
              'h-10 rounded-[10px] px-4 text-[13px] font-medium',
              pool === 'sleeping' ? 'bg-white text-b24-text-dark' : 'bg-white/10 text-white/80',
            ].join(' ')}
          >
            Спящие лиды
          </button>
        </div>
      ) : null}

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-2.5">
        {[
          { label: 'Менеджеров', value: managers.length, hint: data?.department_name || 'в отделе' },
          { label: 'С лимитом', value: withLimit, hint: 'лидов/день > 0' },
          { label: 'План/день', value: sumLimits, hint: 'сумма лимитов' },
          { label: 'Начислено', value: totalAssigned, hint: 'за сегодня · сброс 00:00 МСК' },
          ...(pool === 'sleeping'
            ? [{ label: 'Без менеджера', value: num(data?.bank), hint: 'спящие, ещё не выданные' }]
            : []),
        ].map((card) => (
          <div
            key={card.label}
            className="rounded-[14px] bg-white/12 border border-white/10 px-4 py-3 backdrop-blur-sm"
          >
            <div className="text-[11px] uppercase tracking-wide text-white/55">{card.label}</div>
            <div className="mt-1 text-[26px] font-semibold text-white tabular-nums leading-none">
              {typeof card.value === 'number' ? card.value.toLocaleString('ru-RU') : card.value}
            </div>
            <div className="mt-1 text-[11px] text-white/45 truncate">{card.hint}</div>
          </div>
        ))}
      </div>

      {error ? <p className="text-[13px] text-orange-200">{error}</p> : null}
      {note ? <p className="text-[13px] text-emerald-200">{note}</p> : null}

      <div className="b24-glass-panel p-4">
        <div className="text-[15px] font-semibold text-b24-text-dark">Автораздача</div>
        <p className="mt-0.5 text-[12px] text-b24-text-muted">
          {data?.mode_note ||
            'Новая заявка: отдел → РОП → менеджер по доле. Банк и отзыв отключены.'}
        </p>
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
                placeholder="Поиск по менеджеру…"
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
          <p className="text-[12px] text-b24-text-muted">
            {data?.mode_note ||
              'Если долей нет или лимиты исчерпаны — заявка остаётся у вас. Счётчики обнуляются в полночь по Москве.'}
          </p>
        </div>

        {managers.length === 0 ? (
          <div className="flex-1 flex flex-col items-center justify-center px-6 py-16 text-center">
            <div className="size-14 rounded-2xl bg-[#e8f8fe] text-b24-cyan-deep inline-flex items-center justify-center mb-3">
              <Users size={26} />
            </div>
            <div className="text-[16px] font-semibold text-b24-text-dark">Нет менеджеров в отделе</div>
            <p className="mt-1 text-[13px] text-b24-text-muted max-w-sm">
              Добавьте менеджеров на странице «Сотрудники» — затем задайте им доли здесь.
            </p>
            <Button
              type="button"
              variant="green"
              className="mt-4 h-10 rounded-[10px] px-4"
              onClick={() => navigate('/employees')}
            >
              К сотрудникам
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
                  <th className="px-4 py-2.5 font-medium">Менеджер</th>
                  <th className="px-3 py-2.5 font-medium w-[130px]">Лидов/день</th>
                  <th className="px-3 py-2.5 font-medium">Начислено сегодня</th>
                  <th className="px-3 py-2.5 font-medium">Статус</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((m, idx) => {
                  const today = num(m.today)
                  const lim = limits[m.id] ?? 0
                  const fill = lim > 0 ? today / lim : 0
                  const avgFill = sumLimits > 0 ? totalToday / sumLimits : 0
                  const status =
                    lim <= 0
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
                          : status === 'выкл'
                            ? 'bg-slate-100 text-slate-500 ring-slate-200'
                            : 'bg-emerald-50 text-emerald-800 ring-emerald-200'
                  return (
                    <tr key={m.id} className="border-t border-black/[0.05] hover:bg-[#fafbfc]">
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2.5 min-w-0">
                          <div
                            className={`size-9 rounded-full bg-gradient-to-br ${avatarTone(idx)} text-white text-[13px] font-semibold inline-flex items-center justify-center shrink-0`}
                          >
                            {initial(m.full_name)}
                          </div>
                          <div className="min-w-0">
                            <div className="text-[14px] font-semibold text-b24-text-dark truncate">
                              {m.full_name}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td className="px-3 py-3">
                        <SoftNumber
                          value={lim}
                          onCommit={(n) => {
                            setLimits((prev) => ({ ...prev, [m.id]: n }))
                            markDirty()
                          }}
                          parse={parseLimit}
                          format={(n) => String(n)}
                          placeholder="0"
                          className="h-9 w-[100px] rounded-[10px] border border-black/10 bg-[#f5f7f9] px-2 text-[13px] text-b24-text-dark outline-none focus:border-b24-cyan focus:bg-white"
                        />
                        <div className="mt-1 flex flex-wrap gap-1">
                          {LIMIT_PRESETS.map((n) => (
                            <button
                              key={n}
                              type="button"
                              onClick={() => {
                                setLimits((prev) => ({ ...prev, [m.id]: n }))
                                markDirty()
                              }}
                              className={[
                                'h-6 rounded-full px-2 text-[10px] font-medium transition-colors',
                                lim === n
                                  ? 'bg-b24-cyan text-white'
                                  : 'bg-[#eef1f4] text-b24-text hover:bg-[#e4e8ec]',
                              ].join(' ')}
                            >
                              {n}
                            </button>
                          ))}
                        </div>
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

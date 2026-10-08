import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../../lib/api'

type Site = { code: string; name: string }
type Doc = { id: string; kind: string; filename: string; size_bytes?: number }
type Sent = {
  site_code: string
  site_name: string
  full_name?: string
  birth_date?: string
  passport_number?: string
  arrival_date?: string
  position?: string
  sent_at?: string
  arrived_at?: string | null
}

type Day = { y: number; m: number; d: number }

const MONTHS = ['Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь', 'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь']
const WEEK = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']

function digits(value: string, max: number) {
  return value.replace(/\D/g, '').slice(0, max)
}

function pad(n: number) {
  return String(n).padStart(2, '0')
}

function toISO(day: Day) {
  return `${day.y}-${pad(day.m)}-${pad(day.d)}`
}

function fromISO(raw?: string): Day | null {
  const m = String(raw || '').slice(0, 10).match(/^(\d{4})-(\d{2})-(\d{2})$/)
  if (!m) return null
  return { y: Number(m[1]), m: Number(m[2]), d: Number(m[3]) }
}

function realDay(day: Day) {
  const dt = new Date(day.y, day.m - 1, day.d)
  return dt.getFullYear() === day.y && dt.getMonth() === day.m - 1 && dt.getDate() === day.d
}

function moscowToday(): Day {
  const stamp = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Europe/Moscow',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date())
  return fromISO(stamp) as Day
}

function shift(day: Day, days: number): Day {
  const dt = new Date(Date.UTC(day.y, day.m - 1, day.d))
  dt.setUTCDate(dt.getUTCDate() + days)
  return { y: dt.getUTCFullYear(), m: dt.getUTCMonth() + 1, d: dt.getUTCDate() }
}

function diffDays(a: Day, b: Day) {
  return Math.round((Date.UTC(a.y, a.m - 1, a.d) - Date.UTC(b.y, b.m - 1, b.d)) / 86400000)
}

function maskBirth(raw: string) {
  const only = digits(raw, 8)
  if (only.length <= 2) return only
  if (only.length <= 4) return `${only.slice(0, 2)}.${only.slice(2)}`
  return `${only.slice(0, 2)}.${only.slice(2, 4)}.${only.slice(4)}`
}

function birthDay(text: string): Day | null {
  const m = text.match(/^(\d{2})\.(\d{2})\.(\d{4})$/)
  if (!m) return null
  const day = { d: Number(m[1]), m: Number(m[2]), y: Number(m[3]) }
  return realDay(day) && day.y >= 1920 ? day : null
}

function birthFromISO(raw?: string) {
  const day = fromISO(raw)
  return day ? `${pad(day.d)}.${pad(day.m)}.${day.y}` : ''
}

function ageOf(day: Day, today: Day) {
  let age = today.y - day.y
  if (today.m < day.m || (today.m === day.m && today.d < day.d)) age--
  return age
}

function yearsWord(n: number) {
  const a = n % 10
  const b = n % 100
  if (a === 1 && b !== 11) return 'год'
  if (a >= 2 && a <= 4 && (b < 12 || b > 14)) return 'года'
  return 'лет'
}

function arrivalLabel(day: Day, today: Day) {
  const text = new Date(day.y, day.m - 1, day.d).toLocaleDateString('ru-RU', {
    weekday: 'short',
    day: 'numeric',
    month: 'long',
  })
  const gap = diffDays(day, today)
  if (gap === 0) return `Сегодня, ${text}`
  if (gap === 1) return `Завтра, ${text}`
  if (gap === 2) return `Послезавтра, ${text}`
  if (gap < 0) return `${text} · уже прошло`
  return `${text} · через ${gap} дн.`
}

function splitPassport(raw?: string) {
  const only = String(raw || '').replace(/\D/g, '')
  return { series: only.slice(0, 4), number: only.slice(4, 10) }
}

function sentWhen(iso?: string) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
}

const BOX =
  'h-10 rounded-[10px] border border-black/10 bg-white px-3 text-[15px] font-medium text-[#1b2430] outline-none placeholder:font-normal placeholder:text-[#a0a8b5] focus:border-[#2f6fed] focus:ring-2 focus:ring-[#2f6fed]/15'
const INPUT = `${BOX} w-full`
const LABEL = 'mb-1 block text-[12px] font-medium text-[#475467]'

function Calendar({ value, today, onPick }: { value: Day | null; today: Day; onPick: (day: Day) => void }) {
  const [view, setView] = useState(() => ({ y: (value || today).y, m: (value || today).m }))
  const first = new Date(view.y, view.m - 1, 1)
  const lead = (first.getDay() + 6) % 7
  const total = new Date(view.y, view.m, 0).getDate()
  const cells: (number | null)[] = [...Array(lead).fill(null), ...Array.from({ length: total }, (_, i) => i + 1)]
  while (cells.length % 7) cells.push(null)
  const move = (step: number) => {
    const m = view.m + step
    setView({ y: view.y + Math.floor((m - 1) / 12), m: ((m - 1 + 120) % 12) + 1 })
  }
  return (
    <div className="mt-2 rounded-[12px] bg-white p-2.5 ring-1 ring-black/10">
      <div className="mb-1.5 flex items-center justify-between">
        <button type="button" className="inline-flex size-8 items-center justify-center rounded-lg text-[18px] hover:bg-black/5" onClick={() => move(-1)} aria-label="Предыдущий месяц">
          ‹
        </button>
        <span className="text-[14px] font-semibold">
          {MONTHS[view.m - 1]} {view.y}
        </span>
        <button type="button" className="inline-flex size-8 items-center justify-center rounded-lg text-[18px] hover:bg-black/5" onClick={() => move(1)} aria-label="Следующий месяц">
          ›
        </button>
      </div>
      <div className="grid grid-cols-7 gap-0.5 text-center">
        {WEEK.map((w, i) => (
          <span key={w} className={`py-1 text-[11px] font-semibold ${i > 4 ? 'text-[#c2410c]' : 'text-[#98a2b3]'}`}>
            {w}
          </span>
        ))}
        {cells.map((d, i) => {
          if (!d) return <span key={`e${i}`} />
          const day = { y: view.y, m: view.m, d }
          const picked = value && toISO(value) === toISO(day)
          const isToday = toISO(today) === toISO(day)
          const past = diffDays(day, today) < 0
          return (
            <button
              key={d}
              type="button"
              onClick={() => onPick(day)}
              className={[
                'h-8 rounded-lg text-[13px] font-medium transition',
                picked
                  ? 'bg-[#1f6feb] text-white'
                  : isToday
                    ? 'bg-[#eaf1ff] text-[#1f6feb] ring-1 ring-[#1f6feb]/40'
                    : past
                      ? 'text-[#c0c5cf] hover:bg-black/5'
                      : 'text-[#1b2430] hover:bg-[#eaf1ff]',
              ].join(' ')}
            >
              {d}
            </button>
          )
        })}
      </div>
    </div>
  )
}

export function SendToCustomer({
  dealId,
  files,
  canSend,
}: {
  dealId: string
  files: Doc[]
  canSend: boolean
}) {
  const [open, setOpen] = useState(false)
  const [sites, setSites] = useState<Site[]>([])
  const [sent, setSent] = useState<Sent[]>([])
  const [suggested, setSuggested] = useState('')
  const [site, setSite] = useState('')
  const [name, setName] = useState('')
  const [birth, setBirth] = useState('')
  const [series, setSeries] = useState('')
  const [passNo, setPassNo] = useState('')
  const [arrival, setArrival] = useState<Day | null>(null)
  const [calendar, setCalendar] = useState(false)
  const [position, setPosition] = useState('')
  const [picked, setPicked] = useState<string[]>([])
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  const [pending, setPending] = useState(false)
  const [pos, setPos] = useState({ x: 24, y: 72 })
  const drag = useRef<{ dx: number; dy: number } | null>(null)
  const numberRef = useRef<HTMLInputElement>(null)
  const seriesRef = useRef<HTMLInputElement>(null)

  const load = async () => {
    const data = await api<{ suggested_name?: string; sites?: Site[]; sent?: Sent[] }>(
      `/deals/${dealId}/customer-sends`,
    )
    setSuggested(data.suggested_name || '')
    setSites(data.sites || [])
    setSent(data.sent || [])
  }

  useEffect(() => {
    void load().catch(() => {})
  }, [dealId])

  const today = moscowToday()
  const docs = files.filter((f) => f.kind !== 'note' && f.id)
  const chosen = sites.find((s) => s.code === site)
  const already = sent.find((s) => s.site_code === site)
  const born = birthDay(birth)
  const birthBad = birth.length === 10 && (!born || diffDays(born, today) > 0)
  const age = born && !birthBad ? ageOf(born, today) : null
  const tomorrow = shift(today, 1)
  const quick: [string, Day][] = [
    ['Сегодня', today],
    ['Завтра', tomorrow],
    ['Послезавтра', shift(today, 2)],
  ]

  const openModal = () => {
    setError('')
    setMsg('')
    setSite('')
    setName(suggested)
    setBirth('')
    setSeries('')
    setPassNo('')
    setArrival(null)
    setCalendar(false)
    setPosition('')
    setPicked(docs.map((f) => f.id))
    setPos({
      x: Math.max(16, Math.min(80, window.innerWidth - 460)),
      y: 56,
    })
    setOpen(true)
  }

  const pickSite = (code: string) => {
    const prev = sent.find((s) => s.site_code === code)
    const pass = splitPassport(prev?.passport_number)
    setSite(code)
    setError('')
    setName(prev?.full_name || suggested || name)
    setBirth(birthFromISO(prev?.birth_date))
    setSeries(pass.series)
    setPassNo(pass.number)
    setArrival(fromISO(prev?.arrival_date))
    setCalendar(false)
    setPosition(prev?.position || '')
  }

  const toggleFile = (id: string) => {
    setPicked((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]))
  }

  const submit = async () => {
    if (name.trim().length < 2) return setError('Впишите ФИО.')
    if (!born || birthBad) return setError('Дата рождения: например 15.03.1994.')
    if (series.length !== 4 || passNo.length !== 6) return setError('Паспорт: серия 4 цифры, номер 6 цифр.')
    if (!arrival) return setError('Выберите дату прибытия.')
    if (!position.trim()) return setError('Впишите должность.')
    setPending(true)
    setError('')
    try {
      const res = await api<{ site_name?: string; missing_files?: string[] }>(
        `/deals/${dealId}/customer-sends`,
        {
          method: 'POST',
          body: JSON.stringify({
            site_code: site,
            full_name: name,
            birth_date: toISO(born),
            passport_number: `${series} ${passNo}`,
            arrival_date: toISO(arrival),
            position,
            file_ids: picked,
          }),
        },
      )
      const missed = (res.missing_files || []).length
      setMsg(
        missed
          ? `Отправлено в ${res.site_name || chosen?.name}. Часть файлов не ушла.`
          : `Отправлено в ${res.site_name || chosen?.name}`,
      )
      setOpen(false)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'ошибка')
    } finally {
      setPending(false)
    }
  }

  const recall = async (code: string, label: string) => {
    if (!window.confirm(`Убрать карточку у заказчика «${label}»?`)) return
    setError('')
    try {
      await api(`/deals/${dealId}/customer-sends/${encodeURIComponent(code)}`, { method: 'DELETE' })
      setMsg(`Отозвано: ${label}`)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'ошибка')
    }
  }

  return (
    <div className="mt-2">
      {sent.length ? (
        <div className="mb-2 flex flex-wrap gap-1.5">
          {sent.map((row) => (
            <span
              key={row.site_code}
              className="inline-flex items-center gap-1.5 rounded-full bg-[#eef6f1] px-2.5 py-1 text-[12px] text-[#1d6b45]"
            >
              {row.site_name}
              {sentWhen(row.sent_at) ? <span className="text-[#5d7a6b]">{sentWhen(row.sent_at)}</span> : null}
              {row.arrived_at ? (
                <span className="rounded-full bg-[#16a34a] px-1.5 text-white">прибыл</span>
              ) : (
                <span className="text-[#8a6d1f]">ещё не прибыл</span>
              )}
              {canSend ? (
                <button
                  type="button"
                  className="text-[#9a3b3b] hover:underline"
                  onClick={() => void recall(row.site_code, row.site_name)}
                >
                  отозвать
                </button>
              ) : null}
            </span>
          ))}
        </div>
      ) : null}
      {canSend ? (
        <button
          type="button"
          className="flex w-full items-center justify-between rounded-[12px] border border-black/8 bg-white px-3 py-2.5 text-left hover:bg-[#f7f8fa]"
          onClick={openModal}
        >
          <span>
            <span className="block text-[13px] font-semibold text-b24-text-dark">Отправить заказчику</span>
            <span className="block text-[12px] text-b24-text-muted">Анкета и документы уйдут в выбранный город</span>
          </span>
          <span className="text-[12px] text-b24-cyan">Открыть</span>
        </button>
      ) : null}
      {msg ? <p className="mt-2 text-[12px] text-[#1d6b45]">{msg}</p> : null}
      {error && !open ? <p className="mt-2 text-[12px] text-red-600">{error}</p> : null}

      {open
        ? createPortal(
            <div
              className="fixed z-[230] flex max-h-[calc(100vh-16px)] w-[min(420px,calc(100vw-16px))] flex-col overflow-hidden rounded-[16px] bg-[#f4efe4] text-[#10243e] shadow-[0_16px_48px_rgba(15,23,42,0.28)] ring-1 ring-black/10"
              style={{ left: pos.x, top: pos.y }}
            >
              <div
                className="flex cursor-grab select-none items-center justify-between gap-2 bg-[#10243e] px-3.5 py-2.5 text-white active:cursor-grabbing"
                title="Потяните, чтобы передвинуть"
                onPointerDown={(e) => {
                  if ((e.target as HTMLElement).closest('button')) return
                  drag.current = { dx: e.clientX - pos.x, dy: e.clientY - pos.y }
                  e.currentTarget.setPointerCapture(e.pointerId)
                }}
                onPointerMove={(e) => {
                  if (!drag.current) return
                  const x = e.clientX - drag.current.dx
                  const y = e.clientY - drag.current.dy
                  setPos({
                    x: Math.min(Math.max(8, x), Math.max(8, window.innerWidth - 80)),
                    y: Math.min(Math.max(8, y), Math.max(8, window.innerHeight - 48)),
                  })
                }}
                onPointerUp={() => {
                  drag.current = null
                }}
              >
                <div className="flex min-w-0 items-center gap-2">
                  <span className="text-[14px] leading-none text-white/40" aria-hidden>
                    ⠿
                  </span>
                  {site ? (
                    <button
                      type="button"
                      className="rounded-md px-1.5 py-0.5 text-[13px] text-white/70 hover:bg-white/10 hover:text-white"
                      onClick={() => setSite('')}
                      title="Выбрать другой город"
                    >
                      ←
                    </button>
                  ) : null}
                  <div className="min-w-0">
                    <p className="truncate text-[15px] font-semibold leading-tight">
                      {site ? chosen?.name : 'Отправить заказчику'}
                    </p>
                    {site ? <p className="text-[11px] leading-tight text-white/55">Анкета для заказчика</p> : null}
                  </div>
                </div>
                <button
                  type="button"
                  className="inline-flex size-8 shrink-0 items-center justify-center rounded-md text-[18px] text-white/80 hover:bg-white/10"
                  onClick={() => setOpen(false)}
                  aria-label="Закрыть"
                >
                  ×
                </button>
              </div>

              {!site ? (
                <div className="grid grid-cols-2 gap-2 p-3">
                  {sites.map((item) => {
                    const gone = sent.some((s) => s.site_code === item.code)
                    return (
                      <button
                        key={item.code}
                        type="button"
                        className="rounded-[12px] bg-white px-3 py-2.5 text-left shadow-sm ring-1 ring-black/5 hover:ring-2 hover:ring-[#2f6fed]"
                        onClick={() => pickSite(item.code)}
                      >
                        <div className="text-[14px] font-semibold leading-tight">{item.name}</div>
                        <div className={`mt-1 text-[12px] ${gone ? 'text-[#1d6b45]' : 'text-[#667085]'}`}>
                          {gone ? '✓ уже отправлен' : 'новая карточка'}
                        </div>
                      </button>
                    )
                  })}
                </div>
              ) : (
                <>
                  <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-3.5">
                    {already ? (
                      <p className="rounded-[10px] bg-[#fff7e8] px-3 py-1.5 text-[12px] text-[#8a5a12]">
                        Карточка уже есть — отправка обновит её.
                      </p>
                    ) : null}

                    <div>
                      <label className={LABEL} htmlFor="cs-name">
                        ФИО
                      </label>
                      <input
                        id="cs-name"
                        className={INPUT}
                        placeholder="Иванов Иван Иванович"
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                      />
                    </div>

                    <div className="grid grid-cols-[1fr_1.15fr] gap-2">
                      <div>
                        <label className={LABEL} htmlFor="cs-birth">
                          Дата рождения
                        </label>
                        <div className="relative">
                          <input
                            id="cs-birth"
                            inputMode="numeric"
                            autoComplete="off"
                            placeholder="ДД.ММ.ГГГГ"
                            className={`${INPUT} tabular-nums ${birthBad ? 'border-red-400 focus:border-red-500 focus:ring-red-500/15' : ''}`}
                            value={birth}
                            onChange={(e) => setBirth(maskBirth(e.target.value))}
                          />
                          {age !== null ? (
                            <span className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-[12px] text-[#16a34a]">
                              {age} {yearsWord(age)}
                            </span>
                          ) : null}
                        </div>
                      </div>
                      <div>
                        <span className={LABEL}>Паспорт: серия и номер</span>
                        <div className="flex gap-1.5">
                          <input
                            ref={seriesRef}
                            aria-label="Серия паспорта"
                            inputMode="numeric"
                            autoComplete="off"
                            placeholder="4510"
                            className={`${BOX} w-[64px] shrink-0 px-1 text-center tabular-nums`}
                            value={series}
                            onChange={(e) => {
                              const next = digits(e.target.value, 4)
                              setSeries(next)
                              if (next.length === 4) numberRef.current?.focus()
                            }}
                          />
                          <input
                            ref={numberRef}
                            aria-label="Номер паспорта"
                            inputMode="numeric"
                            autoComplete="off"
                            placeholder="112233"
                            className={`${BOX} w-0 min-w-0 flex-1 px-1 text-center tabular-nums`}
                            value={passNo}
                            onChange={(e) => setPassNo(digits(e.target.value, 6))}
                            onKeyDown={(e) => {
                              if (e.key === 'Backspace' && !passNo) seriesRef.current?.focus()
                            }}
                          />
                        </div>
                      </div>
                    </div>

                    <div>
                      <span className={LABEL}>Когда приедет</span>
                      <div className="grid grid-cols-4 gap-1.5">
                        {quick.map(([label, day]) => {
                          const on = arrival && toISO(arrival) === toISO(day)
                          return (
                            <button
                              key={label}
                              type="button"
                              onClick={() => {
                                setArrival(day)
                                setCalendar(false)
                              }}
                              className={[
                                'h-10 rounded-[10px] px-1 text-[13px] font-semibold leading-tight ring-1 transition',
                                on ? 'bg-[#1f6feb] text-white ring-[#1f6feb]' : 'bg-white text-[#10243e] ring-black/10 hover:ring-[#2f6fed]',
                              ].join(' ')}
                            >
                              {label}
                              <span className={`block text-[11px] font-normal ${on ? 'text-white/80' : 'text-[#667085]'}`}>
                                {day.d} {MONTHS[day.m - 1].slice(0, 3).toLowerCase()}
                              </span>
                            </button>
                          )
                        })}
                        {(() => {
                          const custom = arrival && !quick.some(([, d]) => toISO(d) === toISO(arrival))
                          return (
                            <button
                              type="button"
                              onClick={() => setCalendar((v) => !v)}
                              className={[
                                'h-10 rounded-[10px] px-1 text-[13px] font-semibold leading-tight ring-1 transition',
                                custom || calendar
                                  ? 'bg-[#1f6feb] text-white ring-[#1f6feb]'
                                  : 'bg-white text-[#10243e] ring-black/10 hover:ring-[#2f6fed]',
                              ].join(' ')}
                            >
                              📅 {custom ? `${arrival!.d} ${MONTHS[arrival!.m - 1].slice(0, 3).toLowerCase()}` : 'Дата'}
                              <span className={`block text-[11px] font-normal ${custom || calendar ? 'text-white/80' : 'text-[#667085]'}`}>
                                календарь
                              </span>
                            </button>
                          )
                        })()}
                      </div>
                      {calendar ? (
                        <Calendar
                          value={arrival}
                          today={today}
                          onPick={(day) => {
                            setArrival(day)
                            setCalendar(false)
                          }}
                        />
                      ) : null}
                      {arrival ? (
                        <p className={`mt-1 text-[12px] ${diffDays(arrival, today) < 0 ? 'text-[#b45309]' : 'text-[#1d6b45]'}`}>
                          {arrivalLabel(arrival, today)}
                        </p>
                      ) : null}
                    </div>

                    <div>
                      <label className={LABEL} htmlFor="cs-position">
                        Должность
                      </label>
                      <input
                        id="cs-position"
                        className={INPUT}
                        placeholder="Охранник"
                        value={position}
                        onChange={(e) => setPosition(e.target.value)}
                      />
                    </div>

                    <div>
                      <span className={LABEL}>Документы{docs.length ? ` · ${picked.length} из ${docs.length}` : ''}</span>
                      {docs.length === 0 ? (
                        <p className="text-[12px] text-[#667085]">В карточке нет файлов — уйдёт только анкета.</p>
                      ) : (
                        <div className="flex flex-wrap gap-1.5">
                          {docs.map((file) => {
                            const on = picked.includes(file.id)
                            return (
                              <button
                                key={file.id}
                                type="button"
                                onClick={() => toggleFile(file.id)}
                                className={[
                                  'max-w-full truncate rounded-full px-2.5 py-1 text-[12px] font-medium ring-1',
                                  on ? 'bg-[#eaf1ff] text-[#1f4fb8] ring-[#1f6feb]/40' : 'bg-white text-[#98a2b3] line-through ring-black/10',
                                ].join(' ')}
                                title={on ? 'Нажмите, чтобы не отправлять' : 'Нажмите, чтобы отправить'}
                              >
                                {on ? '✓ ' : ''}
                                {file.filename || 'документ'}
                              </button>
                            )
                          })}
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="border-t border-black/5 bg-[#f4efe4] p-3">
                    {error ? <p className="mb-2 text-[12px] font-medium text-red-600">{error}</p> : null}
                    <button
                      type="button"
                      disabled={pending}
                      className="h-11 w-full rounded-[12px] bg-[#1f6feb] text-[15px] font-semibold text-white hover:bg-[#1a5fd0] disabled:opacity-60"
                      onClick={() => void submit()}
                    >
                      {pending ? 'Отправляем…' : already ? `Обновить в «${chosen?.name}»` : `Отправить в «${chosen?.name}»`}
                    </button>
                  </div>
                </>
              )}
            </div>,
            document.body,
          )
        : null}
    </div>
  )
}

import { CalendarDays, ChevronLeft, ChevronRight, Clock } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  addDays,
  formatMonthYear,
  startOfDay,
  toDateKey,
  toDatetimeLocalValue,
} from '../../data/tasks'
import { Button } from './Button'

type DateTimePickerProps = {
  label: string
  value: string
  onChange: (next: string) => void
  withTime?: boolean
  required?: boolean
  placeholder?: string
}

const WEEKDAYS = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']

const TIME_PRESETS = ['09:00', '10:00', '11:00', '12:00', '14:00', '15:00', '16:00', '17:00', '18:00', '19:00']

function parseValue(value: string): { date: Date | null; time: string } {
  if (!value) return { date: null, time: '12:00' }
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    const [y, m, d] = value.split('-').map(Number)
    return { date: new Date(y, m - 1, d), time: '12:00' }
  }
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return { date: null, time: '12:00' }
  const pad = (n: number) => String(n).padStart(2, '0')
  return { date: d, time: `${pad(d.getHours())}:${pad(d.getMinutes())}` }
}

function formatDisplay(value: string, withTime: boolean) {
  if (!value) return ''
  const { date, time } = parseValue(value)
  if (!date) return ''
  const day = date.toLocaleDateString('ru-RU', {
    weekday: 'short',
    day: 'numeric',
    month: 'long',
  })
  return withTime ? `${day}, ${time}` : day
}

function buildMonthGrid(month: Date) {
  const first = new Date(month.getFullYear(), month.getMonth(), 1)
  const startOffset = (first.getDay() + 6) % 7
  const start = addDays(first, -startOffset)
  return Array.from({ length: 42 }, (_, i) => addDays(start, i))
}

function composeValue(date: Date, time: string, withTime: boolean) {
  const key = toDateKey(date)
  if (!withTime) return key
  return `${key}T${time}`
}

const chipClass = (active: boolean) =>
  [
    'h-8 px-2.5 rounded-[8px] text-[12px] font-medium border transition',
    active
      ? 'bg-b24-text-dark text-white border-b24-text-dark'
      : 'bg-[#f5f7f9] border-transparent text-b24-text hover:bg-[#eef1f4]',
  ].join(' ')

export function DateTimePicker({
  label,
  value,
  onChange,
  withTime = false,
  required,
  placeholder = 'Выберите дату',
}: DateTimePickerProps) {
  const panelId = useId()
  const btnRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [placed, setPlaced] = useState(false)
  const parsed = parseValue(value)
  const [viewMonth, setViewMonth] = useState(() => startOfDay(parsed.date ?? new Date()))
  const [time, setTime] = useState(parsed.time)
  const [coords, setCoords] = useState({ top: 0, left: 0, width: 320 })

  const measure = () => {
    const btn = btnRef.current
    if (!btn) return null
    const rect = btn.getBoundingClientRect()
    const width = Math.min(340, window.innerWidth - 16)
    let left = rect.left
    left = Math.max(8, Math.min(left, window.innerWidth - width - 8))
    let top = rect.bottom + 8
    const approxH = withTime ? 400 : 320
    if (top + approxH > window.innerHeight - 8) {
      top = Math.max(8, rect.top - approxH - 8)
    }
    return { top, left, width }
  }

  useEffect(() => {
    const next = parseValue(value)
    setTime(next.time)
    if (next.date) setViewMonth(startOfDay(next.date))
  }, [value])

  useEffect(() => {
    if (!open) {
      setPlaced(false)
      return
    }
    const place = () => {
      const next = measure()
      if (!next) return
      setCoords(next)
      setPlaced(true)
    }
    place()
    const close = (e: MouseEvent) => {
      const t = e.target as Node
      if (btnRef.current?.contains(t) || panelRef.current?.contains(t)) return
      setOpen(false)
    }
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    document.addEventListener('mousedown', close)
    return () => {
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
      document.removeEventListener('mousedown', close)
    }
  }, [open, withTime])

  const openPicker = () => {
    const next = measure()
    if (next) {
      setCoords(next)
      setPlaced(true)
    } else {
      setPlaced(false)
    }
    setOpen((v) => !v)
  }

  const days = useMemo(() => buildMonthGrid(viewMonth), [viewMonth])
  const todayKey = toDateKey(new Date())
  const selectedKey = parsed.date ? toDateKey(parsed.date) : ''

  const pickDate = (day: Date) => {
    const next = composeValue(day, time, withTime)
    onChange(next)
    if (!withTime) setOpen(false)
  }

  const pickQuick = (hoursFromNow: number) => {
    const d = new Date(Date.now() + hoursFromNow * 3600_000)
    const next = withTime ? toDatetimeLocalValue(d.toISOString()) : toDateKey(d)
    onChange(next)
    if (!withTime) setOpen(false)
  }

  const pickTomorrow = () => {
    const d = addDays(startOfDay(new Date()), 1)
    d.setHours(10, 0, 0, 0)
    onChange(withTime ? toDatetimeLocalValue(d.toISOString()) : toDateKey(d))
    if (!withTime) setOpen(false)
  }

  const applyTime = (t: string) => {
    setTime(t)
    const base = parsed.date ?? new Date()
    onChange(composeValue(base, t, true))
  }

  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-[12px] font-medium text-b24-text">
        {label}
        {required ? <span className="text-red-500"> *</span> : null}
      </span>
      <button
        ref={btnRef}
        type="button"
        onClick={openPicker}
        className={[
          'h-11 w-full rounded-[12px] border px-3.5 text-left text-[14px] outline-none transition',
          'inline-flex items-center gap-2.5',
          open
            ? 'border-b24-cyan bg-white ring-2 ring-b24-cyan/20'
            : 'border-black/10 bg-[#f5f7f9] hover:bg-white focus:border-b24-cyan focus:bg-white focus:ring-2 focus:ring-b24-cyan/20',
        ].join(' ')}
        aria-expanded={open}
        aria-controls={panelId}
      >
        <CalendarDays size={16} className="shrink-0 text-b24-cyan-deep" />
        <span className={value ? 'text-b24-text-dark truncate' : 'text-b24-text-muted truncate'}>
          {value ? formatDisplay(value, withTime) : placeholder}
        </span>
        {withTime && value ? (
          <span className="ml-auto inline-flex items-center gap-1 rounded-full bg-[#e8f8fe] px-2 py-0.5 text-[11px] font-medium text-b24-cyan-deep">
            <Clock size={11} />
            {parseValue(value).time}
          </span>
        ) : null}
      </button>

      {open && placed
        ? createPortal(
            <div
              ref={panelRef}
              id={panelId}
              role="dialog"
              className="fixed z-[160] rounded-[16px] border border-black/8 bg-white shadow-[0_20px_50px_rgba(15,23,42,0.22)] overflow-hidden"
              style={{ top: coords.top, left: coords.left, width: coords.width }}
            >
              <div className="px-3.5 pt-3 pb-2 border-b border-black/6 bg-[linear-gradient(135deg,#f0fbfd_0%,#ffffff_70%)]">
                <div className="flex flex-wrap gap-1.5">
                  <button type="button" onClick={() => pickQuick(0)} className={chipClass(false)}>
                    Сегодня
                  </button>
                  <button type="button" onClick={pickTomorrow} className={chipClass(false)}>
                    Завтра
                  </button>
                  {withTime ? (
                    <button type="button" onClick={() => pickQuick(1)} className={chipClass(false)}>
                      Через час
                    </button>
                  ) : null}
                </div>
              </div>

              <div className="px-3.5 py-3">
                <div className="flex items-center justify-between mb-2.5">
                  <button
                    type="button"
                    className="size-8 inline-flex items-center justify-center rounded-full text-b24-text-muted hover:bg-black/5"
                    onClick={() =>
                      setViewMonth(new Date(viewMonth.getFullYear(), viewMonth.getMonth() - 1, 1))
                    }
                    aria-label="Предыдущий месяц"
                  >
                    <ChevronLeft size={18} />
                  </button>
                  <div className="text-[14px] font-semibold text-b24-text-dark">
                    {formatMonthYear(viewMonth)}
                  </div>
                  <button
                    type="button"
                    className="size-8 inline-flex items-center justify-center rounded-full text-b24-text-muted hover:bg-black/5"
                    onClick={() =>
                      setViewMonth(new Date(viewMonth.getFullYear(), viewMonth.getMonth() + 1, 1))
                    }
                    aria-label="Следующий месяц"
                  >
                    <ChevronRight size={18} />
                  </button>
                </div>

                <div className="grid grid-cols-7 gap-0.5 mb-1">
                  {WEEKDAYS.map((d) => (
                    <div
                      key={d}
                      className="h-7 grid place-items-center text-[11px] font-medium text-b24-text-muted"
                    >
                      {d}
                    </div>
                  ))}
                </div>
                <div className="grid grid-cols-7 gap-0.5">
                  {days.map((day) => {
                    const key = toDateKey(day)
                    const inMonth = day.getMonth() === viewMonth.getMonth()
                    const selected = key === selectedKey
                    const isToday = key === todayKey
                    return (
                      <button
                        key={key + String(day.getMonth())}
                        type="button"
                        onClick={() => pickDate(day)}
                        className={[
                          'h-9 rounded-[10px] text-[13px] font-medium transition border',
                          selected
                            ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep shadow-[0_0_0_1px_rgba(47,198,246,0.25)]'
                            : isToday
                              ? 'bg-[#f0fbfd] border-transparent text-b24-cyan-deep'
                              : inMonth
                                ? 'border-transparent text-b24-text-dark hover:bg-[#f5f7f9]'
                                : 'border-transparent text-b24-text-muted/45 hover:bg-[#f8fafb]',
                        ].join(' ')}
                      >
                        {day.getDate()}
                      </button>
                    )
                  })}
                </div>
              </div>

              {withTime ? (
                <div className="px-3.5 pb-3.5 border-t border-black/6 pt-3">
                  <div className="flex items-center gap-2 mb-2">
                    <Clock size={14} className="text-b24-text-muted" />
                    <span className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted">
                      Время
                    </span>
                  </div>
                  <div className="flex flex-wrap gap-1.5">
                    {TIME_PRESETS.map((t) => (
                      <button
                        key={t}
                        type="button"
                        onClick={() => applyTime(t)}
                        className={chipClass(time === t)}
                      >
                        {t}
                      </button>
                    ))}
                  </div>
                  <div className="mt-3 flex items-center justify-between gap-2">
                    {value ? (
                      <button
                        type="button"
                        className="text-[12px] text-b24-text-muted hover:text-rose-600"
                        onClick={() => {
                          onChange('')
                          setOpen(false)
                        }}
                      >
                        Очистить
                      </button>
                    ) : (
                      <span />
                    )}
                    <Button
                      type="button"
                      variant="green"
                      className="h-9 rounded-[10px] px-4"
                      onClick={() => setOpen(false)}
                    >
                      Готово
                    </Button>
                  </div>
                </div>
              ) : value ? (
                <div className="px-3.5 pb-3">
                  <button
                    type="button"
                    className="text-[12px] text-b24-text-muted hover:text-rose-600"
                    onClick={() => {
                      onChange('')
                      setOpen(false)
                    }}
                  >
                    Очистить
                  </button>
                </div>
              ) : null}
            </div>,
            document.body,
          )
        : null}
    </div>
  )
}

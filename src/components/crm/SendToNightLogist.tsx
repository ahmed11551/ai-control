import { ChevronLeft, ChevronRight, Moon, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../../lib/api'
import { useAuth } from '../../store/auth'

const MONTHS = [
  'января',
  'февраля',
  'марта',
  'апреля',
  'мая',
  'июня',
  'июля',
  'августа',
  'сентября',
  'октября',
  'ноября',
  'декабря',
]

const MONTH_NAMES = [
  'Январь',
  'Февраль',
  'Март',
  'Апрель',
  'Май',
  'Июнь',
  'Июль',
  'Август',
  'Сентябрь',
  'Октябрь',
  'Ноябрь',
  'Декабрь',
]

const WEEKDAYS = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']
const WEEKDAY_WORDS = ['воскресенье', 'понедельник', 'вторник', 'среда', 'четверг', 'пятница', 'суббота']
const HOURS = Array.from({ length: 24 }, (_, i) => i)
const MINUTES = [0, 15, 30, 45]

const pad = (n: number) => String(n).padStart(2, '0')

function moscowNow() {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Europe/Moscow',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(new Date())
  const get = (type: string) => Number(parts.find((p) => p.type === type)?.value || 0)
  return {
    day: `${get('year')}-${pad(get('month'))}-${pad(get('day'))}`,
    year: get('year'),
    month: get('month') - 1,
    minutes: get('hour') * 60 + get('minute'),
  }
}

function isoOf(year: number, month: number, day: number) {
  return new Date(Date.UTC(year, month, day)).toISOString().slice(0, 10)
}

function shiftDay(iso: string, days: number) {
  const [y, m, d] = iso.split('-').map(Number)
  return isoOf(y, m - 1, d + days)
}

function monthGrid(year: number, month: number) {
  const offset = (new Date(Date.UTC(year, month, 1)).getUTCDay() + 6) % 7
  return Array.from({ length: 42 }, (_, i) => isoOf(year, month, 1 - offset + i))
}

function dayText(iso: string) {
  const [, m, d] = iso.split('-').map(Number)
  return `${d} ${MONTHS[m - 1] || ''}`
}

function weekdayOf(iso: string) {
  const [y, m, d] = iso.split('-').map(Number)
  return WEEKDAY_WORDS[new Date(Date.UTC(y, m - 1, d)).getUTCDay()]
}

function callLabel(raw?: string) {
  const value = (raw || '').trim()
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}:\d{2})/)
  if (!match) return value
  const day = Number(match[3])
  const month = MONTHS[Number(match[2]) - 1] || ''
  if (!day || !month) return value
  return `${day} ${month}, ${match[4]}`
}

export function SendToNightLogist({ dealId }: { dealId: string }) {
  const { user } = useAuth()
  const role = String(user?.role || '').toLowerCase()
  const allowed = role === 'manager' || role === 'sales' || role === 'rop' || role === 'admin'
  const [sent, setSent] = useState(false)
  const [sentCall, setSentCall] = useState('')
  const [ask, setAsk] = useState(false)
  const [day, setDay] = useState('')
  const [hour, setHour] = useState<number | null>(null)
  const [minute, setMinute] = useState(0)
  const [view, setView] = useState({ year: 0, month: 0 })
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!allowed) return
    let stop = false
    void api<{ sent?: boolean; first_call_at?: string }>(`/deals/${dealId}/night-logist-send`)
      .then((row) => {
        if (stop) return
        setSent(Boolean(row.sent))
        setSentCall(row.first_call_at || '')
      })
      .catch(() => {})
    return () => {
      stop = true
    }
  }, [dealId, allowed])

  useEffect(() => {
    if (!ask) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !pending) setAsk(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [ask, pending])

  if (!allowed) return null

  const now = moscowNow()
  const today = now.day
  const isToday = day === today
  const tooEarly = (h: number, m: number) => isToday && h * 60 + m <= now.minutes
  const hourClosed = (h: number) => MINUTES.every((m) => tooEarly(h, m))
  const ready = Boolean(day) && hour !== null && !tooEarly(hour, minute)
  const time = hour !== null ? `${pad(hour)}:${pad(minute)}` : ''
  const value = ready ? `${day}T${time}` : ''
  const dayWord = day === today ? 'Сегодня' : day === shiftDay(today, 1) ? 'Завтра' : day ? weekdayOf(day) : ''

  const open = () => {
    setError('')
    setDay('')
    setHour(null)
    setMinute(0)
    setView({ year: now.year, month: now.month })
    setAsk(true)
  }

  const pickDay = (iso: string) => {
    setDay(iso)
    setError('')
    if (iso !== today || hour === null) return
    const early = (m: number) => hour * 60 + m <= now.minutes
    if (MINUTES.every(early)) setHour(null)
    else if (early(minute)) setMinute(MINUTES.find((m) => !early(m)) ?? 0)
  }

  const pickHour = (h: number) => {
    setHour(h)
    setError('')
    if (tooEarly(h, minute)) setMinute(MINUTES.find((m) => !tooEarly(h, m)) ?? 0)
  }

  const moveMonth = (delta: number) => {
    const next = new Date(Date.UTC(view.year, view.month + delta, 1))
    setView({ year: next.getUTCFullYear(), month: next.getUTCMonth() })
  }

  const send = async () => {
    if (!ready) {
      setError('Выберите день и время первого звонка')
      return
    }
    setPending(true)
    setError('')
    try {
      const row = await api<{ first_call_at?: string }>(`/deals/${dealId}/night-logist-send`, {
        method: 'POST',
        body: JSON.stringify({ first_call_at: value }),
      })
      setSentCall(row.first_call_at || value.replace('T', ' '))
      setSent(true)
      setAsk(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось отправить')
    } finally {
      setPending(false)
    }
  }

  if (sent) {
    const when = callLabel(sentCall)
    return (
      <div className="rounded-[12px] bg-[#eef7f1] px-3 py-2.5">
        <p className="text-[13px] font-semibold text-[#1d6b45]">Отправлено ночному логисту</p>
        {when ? <p className="mt-0.5 text-[13px] text-[#1d6b45]">Первый звонок {when}</p> : null}
      </div>
    )
  }

  const canGoBack = view.year > now.year || (view.year === now.year && view.month > now.month)

  return (
    <>
      <button
        type="button"
        onClick={open}
        className="flex w-full items-center gap-2.5 rounded-[12px] border border-black/8 bg-white px-3 py-2.5 text-left transition hover:border-[#10243e]/30"
      >
        <span className="grid size-8 shrink-0 place-items-center rounded-full bg-[#10243e] text-white">
          <Moon size={15} />
        </span>
        <span className="min-w-0">
          <span className="block text-[13px] font-semibold text-b24-text-dark">Отправить ночному логисту</span>
          <span className="block text-[12px] text-b24-text-muted">Укажите, когда ему позвонить кандидату</span>
        </span>
      </button>

      {ask
        ? createPortal(
            <div
              className="fixed inset-0 z-[240] flex items-center justify-center bg-[#10243e]/45 p-3 backdrop-blur-[2px]"
              onMouseDown={(event) => {
                if (event.target === event.currentTarget && !pending) setAsk(false)
              }}
            >
              <div
                role="dialog"
                aria-label="Отправить ночному логисту"
                className="flex max-h-[calc(100vh-24px)] w-[min(460px,100%)] flex-col overflow-hidden rounded-[20px] bg-white shadow-[0_24px_64px_rgba(15,23,42,0.35)]"
              >
                <div className="flex items-start justify-between gap-3 bg-[#10243e] px-5 py-4 text-white">
                  <div>
                    <p className="text-[17px] font-bold leading-tight">Первый звонок кандидату</p>
                    <p className="mt-1 text-[13px] text-white/65">Ночной логист позвонит в это время. Время московское.</p>
                  </div>
                  <button
                    type="button"
                    disabled={pending}
                    onClick={() => setAsk(false)}
                    className="grid size-8 shrink-0 place-items-center rounded-full text-white/70 hover:bg-white/10 hover:text-white"
                    aria-label="Закрыть"
                  >
                    <X size={18} />
                  </button>
                </div>

                <div className="overflow-y-auto px-5 pb-2 pt-4">
                  <div className="flex items-center justify-between">
                    <p className="text-[15px] font-bold text-b24-text-dark">
                      {MONTH_NAMES[view.month]} {view.year}
                    </p>
                    <div className="flex gap-1">
                      <button
                        type="button"
                        disabled={!canGoBack}
                        onClick={() => moveMonth(-1)}
                        className="grid size-8 place-items-center rounded-full text-b24-text-dark hover:bg-black/5 disabled:opacity-25"
                        aria-label="Предыдущий месяц"
                      >
                        <ChevronLeft size={18} />
                      </button>
                      <button
                        type="button"
                        onClick={() => moveMonth(1)}
                        className="grid size-8 place-items-center rounded-full text-b24-text-dark hover:bg-black/5"
                        aria-label="Следующий месяц"
                      >
                        <ChevronRight size={18} />
                      </button>
                    </div>
                  </div>

                  <div className="mt-2 grid grid-cols-7 gap-1">
                    {WEEKDAYS.map((w) => (
                      <div key={w} className="grid h-6 place-items-center text-[11px] font-semibold text-b24-text-muted">
                        {w}
                      </div>
                    ))}
                    {monthGrid(view.year, view.month).map((iso) => {
                      const [, m, d] = iso.split('-').map(Number)
                      const inMonth = m - 1 === view.month
                      const past = iso < today
                      const selected = iso === day
                      return (
                        <button
                          key={iso}
                          type="button"
                          disabled={past}
                          onClick={() => pickDay(iso)}
                          className={[
                            'relative h-10 rounded-[10px] text-[14px] font-semibold transition',
                            selected
                              ? 'bg-[#10243e] text-white'
                              : past
                                ? 'cursor-not-allowed text-black/15'
                                : inMonth
                                  ? 'text-b24-text-dark hover:bg-[#eef2f7]'
                                  : 'text-b24-text-muted/60 hover:bg-[#eef2f7]',
                          ].join(' ')}
                        >
                          {d}
                          {iso === today ? (
                            <span
                              className={`absolute bottom-1 left-1/2 size-1 -translate-x-1/2 rounded-full ${selected ? 'bg-white' : 'bg-b24-cyan'}`}
                            />
                          ) : null}
                        </button>
                      )
                    })}
                  </div>

                  <div className={`mt-4 transition ${day ? '' : 'pointer-events-none opacity-35'}`}>
                    <p className="text-[15px] font-bold text-b24-text-dark">Час</p>
                    <div className="mt-2 grid grid-cols-6 gap-1">
                      {HOURS.map((h) => {
                        const closed = hourClosed(h)
                        const selected = hour === h
                        return (
                          <button
                            key={h}
                            type="button"
                            disabled={closed}
                            onClick={() => pickHour(h)}
                            className={[
                              'h-9 rounded-[10px] text-[14px] font-semibold tabular-nums transition',
                              selected
                                ? 'bg-[#10243e] text-white'
                                : closed
                                  ? 'cursor-not-allowed text-black/15'
                                  : 'bg-[#f3f5f8] text-b24-text-dark hover:bg-[#e6ebf2]',
                            ].join(' ')}
                          >
                            {pad(h)}
                          </button>
                        )
                      })}
                    </div>

                    <div className={`mt-3 flex items-center gap-2 transition ${hour !== null ? '' : 'pointer-events-none opacity-35'}`}>
                      <p className="w-[52px] text-[15px] font-bold text-b24-text-dark">Мин</p>
                      <div className="grid flex-1 grid-cols-4 gap-1 rounded-[12px] bg-[#f3f5f8] p-1">
                        {MINUTES.map((m) => {
                          const closed = hour !== null && tooEarly(hour, m)
                          return (
                            <button
                              key={m}
                              type="button"
                              disabled={closed}
                              onClick={() => setMinute(m)}
                              className={[
                                'h-8 rounded-[9px] text-[14px] font-semibold tabular-nums transition',
                                minute === m && hour !== null
                                  ? 'bg-white text-b24-text-dark shadow-sm'
                                  : closed
                                    ? 'cursor-not-allowed text-black/15'
                                    : 'text-b24-text-muted hover:text-b24-text-dark',
                              ].join(' ')}
                            >
                              :{pad(m)}
                            </button>
                          )
                        })}
                      </div>
                    </div>
                  </div>
                </div>

                <div className="border-t border-black/6 px-5 pb-5 pt-4">
                  <div className="flex items-center gap-3">
                    <div className="min-w-0 flex-1">
                      {day ? (
                        <>
                          <p className="text-[12px] font-medium capitalize text-b24-text-muted">{dayWord}</p>
                          <p className="truncate text-[20px] font-black leading-tight text-b24-text-dark">
                            {dayText(day)}
                            {ready ? <span className="text-b24-cyan-deep">, {time}</span> : null}
                          </p>
                        </>
                      ) : (
                        <p className="text-[14px] text-b24-text-muted">Выберите день в календаре</p>
                      )}
                    </div>
                    <button
                      type="button"
                      disabled={pending || !ready}
                      onClick={() => void send()}
                      className="h-12 shrink-0 rounded-[14px] bg-[#10243e] px-6 text-[15px] font-bold text-white transition hover:bg-[#1a3557] disabled:opacity-30"
                    >
                      {pending ? 'Отправка…' : 'Отправить'}
                    </button>
                  </div>
                  {error ? <p className="mt-2 text-[13px] text-red-600">{error}</p> : null}
                </div>
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  )
}


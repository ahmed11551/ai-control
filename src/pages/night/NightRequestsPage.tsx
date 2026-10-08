import { Check, Pencil, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { Navigate, useSearchParams } from 'react-router-dom'
import { PageHeader, PageShell } from '../../components/layout/PageHeader'
import { homePathForRole } from '../../data/nav'
import { api } from '../../lib/api'
import { useAuth } from '../../store/auth'
import { useCrmLeadDrawer } from '../../store/crmLeadDrawer'
import { useTelephony } from '../../store/telephony'

type NightNote = {
  id: string
  body: string
  author?: string
  created_at?: string
}

export type NightRequest = {
  id: string
  deal_id: string
  full_name: string
  phone: string
  position?: string
  city: string
  stage: string
  sent_by_name: string
  department: string
  sent_at: string
  first_call_at?: string
  first_call_due?: boolean
  done_at: string
  skip_at?: string
  skip_reason?: string
  self_note?: string
  self_note_at?: string
  brief_recruiter?: string
  brief_candidate?: string
  brief_rop?: string
  from_city?: string
  to_city?: string
  transport?: string
  depart_at?: string
  arrive_at?: string
  notes?: string | NightNote[]
}

type DeptCount = { id: string; name: string; count: number }

type NightList = {
  items?: NightRequest[]
  total?: number
  when_counts?: { today?: number; week?: number; past?: number; all?: number }
  done_counts?: { all?: number; yes?: number; wait?: number; skip?: number }
  call_counts?: { soon?: number; overdue?: number }
  dept_all?: number
  dept_counts?: { id?: string; name?: string; count?: number }[]
}

export function formatPhone(raw: string) {
  const digits = raw.replace(/\D/g, '')
  if (digits.length === 11 && (digits.startsWith('7') || digits.startsWith('8'))) {
    const n = digits.startsWith('8') ? `7${digits.slice(1)}` : digits
    return `+7 ${n.slice(1, 4)} ${n.slice(4, 7)}-${n.slice(7, 9)}-${n.slice(9)}`
  }
  return raw.trim()
}

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

function asNotes(value: NightRequest['notes']): NightNote[] {
  if (Array.isArray(value)) return value
  if (typeof value !== 'string' || !value.trim()) return []
  try {
    const parsed = JSON.parse(value) as unknown
    return Array.isArray(parsed) ? (parsed as NightNote[]) : []
  } catch {
    return []
  }
}

function tripWhen(iso?: string) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const mins = d.getHours() * 60 + d.getMinutes()
  let part = ''
  if (!(d.getHours() === 0 && d.getMinutes() === 0)) {
    if (mins >= 21 * 60 || mins < 6 * 60) part = 'ночь'
    else if (mins < 12 * 60) part = 'утро'
    else part = 'вечер'
  }
  const text = `${d.getDate()} ${MONTHS[d.getMonth()] || ''}`
  return part ? `${text}, ${part}` : text
}

function transportLabel(raw?: string) {
  const value = (raw || '').trim().toLowerCase()
  if (value === 'air' || value === 'самолет' || value === 'самолёт') return 'Самолёт'
  if (value === 'rail' || value === 'поезд' || value === 'жд') return 'Поезд'
  if (value === 'bus' || value === 'автобус') return 'Автобус'
  return (raw || '').trim()
}

export function noteWhen(raw?: string) {
  const value = (raw || '').trim()
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})\s+(\d{2}:\d{2})/)
  if (!match) return value
  const day = Number(match[3])
  const month = MONTHS[Number(match[2]) - 1] || ''
  if (!day || !month) return value
  return `${day} ${month}, ${match[4]}`
}

function groupNotes(notes: NightNote[]) {
  const groups: { author: string; when: string; notes: NightNote[] }[] = []
  for (const note of notes) {
    const author = (note.author || '').trim() || '—'
    const when = noteWhen(note.created_at)
    const last = groups[groups.length - 1]
    if (last && last.author === author && last.when === when) last.notes.push(note)
    else groups.push({ author, when, notes: [note] })
  }
  return groups
}

export function NightPosition({ position }: { position?: string }) {
  const value = (position || '').trim()
  if (!value) return null
  return (
    <p className="mt-1 text-[15px] leading-tight text-[#1c1915]">
      <span className="font-black tracking-[0.04em]">ДОЛЖНОСТЬ</span>{' '}
      <span className="font-bold">{value}</span>
    </p>
  )
}

export function NightCallMark({ item }: { item: NightRequest }) {
  const at = (item.first_call_at || '').trim()
  if (!at) return null
  const waiting = !(item.done_at || '').trim() && !(item.skip_at || '').trim()
  const due = Boolean(item.first_call_due) && waiting
  return (
    <div
      className={
        due
          ? 'mt-2 rounded-[12px] bg-[#fdecec] px-3 py-2 text-[15px] font-bold leading-tight text-[#9f1239]'
          : 'mt-2 rounded-[12px] bg-[#f6f3ee] px-3 py-2 text-[15px] font-bold leading-tight text-[#1c1915]'
      }
    >
      Первый звонок {noteWhen(at) || at}
      {due ? <span className="ml-2 text-[13px] font-semibold">пора звонить</span> : null}
    </div>
  )
}

function Person({ label, name }: { label: string; name: string }) {
  return (
    <span className="inline-flex min-w-0 items-baseline gap-1.5">
      <span className="shrink-0 text-[11px] text-[#8a8175]">{label}</span>
      <span className="truncate text-[13px] font-semibold">{name}</span>
    </span>
  )
}

export function NightBrief({ item }: { item: NightRequest }) {
  const notes = asNotes(item.notes)
  const [showAll, setShowAll] = useState(false)
  const recruiter = (item.brief_recruiter || '').trim()
  const rop = (item.brief_rop || '').trim()
  const candidate = (item.brief_candidate || '').trim()
  const showCandidate = candidate && candidate !== (item.full_name || '').trim()
  const from = (item.from_city || '').trim()
  const to = (item.to_city || '').trim()
  const transport = transportLabel(item.transport)
  const depart = tripWhen(item.depart_at)
  const arrive = tripWhen(item.arrive_at)
  const people = [recruiter, rop, showCandidate].filter(Boolean)
  const trip = [from, to, transport, depart, arrive].filter(Boolean)
  const visible = showAll ? notes : notes.slice(0, 2)
  const groups = groupNotes(visible)
  const hasPanel = people.length > 0 || trip.length > 0 || groups.length > 0

  return (
    <div className="mt-2 text-[#1c1915]" onClick={(event) => event.stopPropagation()}>
      {!hasPanel ? <p className="text-[12px] text-[#8a8175]">В справке пока пусто.</p> : null}
      {hasPanel ? (
        <div className="rounded-[12px] bg-[#f6f3ee] px-3 py-2">
          {people.length > 0 ? (
            <div className="flex flex-wrap gap-x-4 gap-y-1">
              {recruiter ? <Person label="Рекрутер" name={recruiter} /> : null}
              {rop ? <Person label="РОП" name={rop} /> : null}
              {showCandidate ? <Person label="Кандидат" name={candidate} /> : null}
            </div>
          ) : null}
          {trip.length > 0 ? (
            <div className={people.length > 0 ? 'mt-2 border-t border-[#e7e1d6] pt-2' : ''}>
              <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-start gap-x-2">
                <div className="min-w-0">
                  <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-[#8a8175]">Откуда</div>
                  <div className="truncate text-[15px] font-bold leading-tight">{from || '—'}</div>
                  {depart ? <div className="mt-0.5 text-[12px] leading-tight text-[#3d3832]">выезд {depart}</div> : null}
                </div>
                <div className="px-1 pt-3 text-center">
                  <div className="text-[15px] leading-none text-[#8a8175]">→</div>
                  {transport ? <div className="mt-1 text-[11px] font-semibold text-[#3d3832]">{transport}</div> : null}
                </div>
                <div className="min-w-0 text-right">
                  <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-[#8a8175]">Куда</div>
                  <div className="truncate text-[15px] font-bold leading-tight">{to || '—'}</div>
                  {arrive ? <div className="mt-0.5 text-[12px] leading-tight text-[#3d3832]">прибытие {arrive}</div> : null}
                </div>
              </div>
            </div>
          ) : null}
          {groups.length > 0 ? (
            <div className={people.length > 0 || trip.length > 0 ? 'mt-2 border-t border-[#e7e1d6] pt-2' : ''}>
              {groups.map((group, index) => (
                <div key={`${group.author}-${group.when}-${index}`} className={index > 0 ? 'mt-2' : ''}>
                  <ul className="space-y-1">
                    {group.notes.map((note) => (
                      <li key={note.id} className="whitespace-pre-wrap break-words text-[14px] font-medium leading-snug">
                        {note.body}
                      </li>
                    ))}
                  </ul>
                  <p className="mt-0.5 text-[11px] text-[#8a8175]">
                    {group.author}
                    {group.when ? ` · ${group.when}` : ''}
                  </p>
                </div>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
      {notes.length > 2 ? (
        <button
          type="button"
          className="mt-1 text-[12px] font-semibold text-[#2563eb]"
          onClick={() => setShowAll((value) => !value)}
        >
          {showAll ? 'Свернуть' : `Ещё ${notes.length - 2}`}
        </button>
      ) : null}
    </div>
  )
}

function CallRow({ dealId, phone }: { dealId: string; phone: string }) {
  const { dial, snap, configReady } = useTelephony()
  const [err, setErr] = useState('')
  const busy =
    snap.status === 'connecting' ||
    snap.status === 'dialing' ||
    snap.status === 'active' ||
    snap.status === 'ringing'
  return (
    <div>
      <button
        type="button"
        disabled={busy || !configReady}
        onClick={(event) => {
          event.stopPropagation()
          setErr('')
          void dial(phone, { dealId }).catch((e: Error) => setErr(e.message))
        }}
        className="inline-flex h-8 items-center rounded-full bg-[#1c1915] px-3 text-[13px] font-semibold text-white disabled:opacity-50"
      >
        Позвонить
      </button>
      {err ? <p className="mt-1 text-[12px] text-red-600">{err}</p> : null}
    </div>
  )
}

function FilterButton({
  active,
  label,
  count,
  hint,
  onClick,
}: {
  active: boolean
  label: string
  count: number
  hint?: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'rounded-[14px] px-3 py-2.5 text-left transition',
        active ? 'bg-[#1c1915] text-white' : 'bg-[#f4f1ec] text-[#1c1915] hover:bg-[#ebe6df]',
      ].join(' ')}
    >
      <span className="block text-[14px] font-bold leading-tight">{label}</span>
      <span className={['block text-[12px]', active ? 'text-white/70' : 'text-[#6d675f]'].join(' ')}>
        {count}
        {hint ? ` · ${hint}` : ''}
      </span>
    </button>
  )
}

export function NightActions({
  item,
  onChanged,
  preview,
}: {
  item: NightRequest
  onChanged?: () => void
  preview?: boolean
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [ask, setAsk] = useState(false)
  const [reason, setReason] = useState('')
  const [local, setLocal] = useState({
    doneAt: item.done_at || '',
    skipAt: item.skip_at || '',
    skipReason: item.skip_reason || '',
  })
  const doneAt = preview ? local.doneAt : item.done_at || ''
  const skipAt = preview ? local.skipAt : item.skip_at || ''
  const skipReason = preview ? local.skipReason : item.skip_reason || ''
  const done = Boolean(doneAt)
  const skipped = Boolean(skipAt) && !done

  const save = async (status: 'done' | 'skip' | 'wait', why = '') => {
    if (busy) return
    const text = why.trim()
    if (status === 'skip' && !text) {
      setError('Напишите причину')
      return
    }
    setBusy(true)
    setError('')
    try {
      if (preview) {
        const now = new Date()
        const pad = (n: number) => String(n).padStart(2, '0')
        const stamp = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(now.getMinutes())}`
        setLocal({
          doneAt: status === 'done' ? stamp : '',
          skipAt: status === 'skip' ? stamp : '',
          skipReason: status === 'skip' ? text : '',
        })
      } else {
        await api(`/night-logist/requests/${item.id}/done`, {
          method: 'PUT',
          body: JSON.stringify({ status, reason: text }),
        })
        onChanged?.()
      }
      setAsk(false)
      setReason('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не получилось')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div onClick={(event) => event.stopPropagation()}>
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          disabled={busy}
          title={done ? `Обработано ${doneAt}. Нажмите, чтобы вернуть в ожидание` : 'Отметить, что заявка обработана'}
          onClick={() => void save(done ? 'wait' : 'done')}
          className={[
            'inline-flex h-8 items-center gap-1.5 rounded-full border px-2.5 text-[13px] font-semibold disabled:opacity-60',
            done ? 'border-[#16a34a] bg-[#16a34a] text-white' : 'border-[#c9c1b5] bg-white text-[#1c1915]',
          ].join(' ')}
        >
          <Check size={14} strokeWidth={3} className={done ? '' : 'text-[#c9c1b5]'} />
          {busy && !ask ? 'Сохраняем…' : done ? `Обработано ${doneAt}` : 'Обработано'}
        </button>
        <button
          type="button"
          disabled={busy}
          title={skipped ? 'Изменить причину' : 'Отметить, что обработать не получилось'}
          onClick={() => {
            setError('')
            setReason(skipped ? skipReason : '')
            setAsk(true)
          }}
          className={[
            'inline-flex h-8 items-center gap-1.5 rounded-full border px-2.5 text-[13px] font-semibold disabled:opacity-60',
            skipped ? 'border-[#dc2626] bg-[#dc2626] text-white' : 'border-[#c9c1b5] bg-white text-[#1c1915]',
          ].join(' ')}
        >
          <X size={14} strokeWidth={3} className={skipped ? '' : 'text-[#c9c1b5]'} />
          Не обработано
        </button>
        {item.phone?.trim() && !preview ? <CallRow dealId={item.deal_id} phone={item.phone.trim()} /> : null}
        {preview ? (
          <span className="inline-flex h-8 items-center rounded-full bg-[#1c1915] px-3 text-[13px] font-semibold text-white">
            Позвонить
          </span>
        ) : null}
      </div>
      {ask ? (
        <form
          className="mt-2"
          onSubmit={(event) => {
            event.preventDefault()
            void save('skip', reason)
          }}
        >
          <textarea
            autoFocus
            rows={2}
            maxLength={500}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder="Почему не обработано"
            className="w-full resize-none rounded-[12px] border-2 border-[#e7e2da] px-3 py-2 text-[14px] outline-none focus:border-[#1c1915]"
          />
          <div className="mt-1.5 flex flex-wrap items-center gap-2">
            <button
              type="submit"
              disabled={busy}
              className="inline-flex h-8 items-center rounded-full bg-[#1c1915] px-3 text-[13px] font-semibold text-white disabled:opacity-60"
            >
              Сохранить
            </button>
            {skipped ? (
              <button
                type="button"
                disabled={busy}
                onClick={() => void save('wait')}
                className="inline-flex h-8 items-center rounded-full px-2 text-[13px] font-semibold text-[#dc2626]"
              >
                Снять
              </button>
            ) : null}
            <button
              type="button"
              onClick={() => {
                setAsk(false)
                setError('')
              }}
              className="inline-flex h-8 items-center rounded-full px-2 text-[13px] font-semibold text-[#6d675f]"
            >
              Отмена
            </button>
          </div>
        </form>
      ) : null}
      {skipped && !ask ? (
        <div className="mt-2 rounded-[12px] bg-[#fdecec] px-3 py-2 text-[13px] leading-snug text-[#7f1d1d]">
          <p className="whitespace-pre-wrap break-words font-medium">{skipReason}</p>
          <p className="mt-0.5 text-[11px] text-[#9f1239]/70">
            Не обработано{skipAt ? ` · ${noteWhen(skipAt) || skipAt}` : ''}
          </p>
        </div>
      ) : null}
      {error ? <p className="mt-1 text-[12px] text-red-600">{error}</p> : null}
    </div>
  )
}

function growNote(el: HTMLTextAreaElement | null) {
  if (!el) return
  el.style.height = '0px'
  el.style.height = `${Math.min(el.scrollHeight, 180)}px`
}

export function NightSelfNote({
  item,
  onSaved,
  preview,
}: {
  item: NightRequest
  onSaved?: (note: string, at: string) => void
  preview?: boolean
}) {
  const saved = (item.self_note || '').trim()
  const [text, setText] = useState(saved)
  const [open, setOpen] = useState(false)
  const [phase, setPhase] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const [error, setError] = useState('')
  const areaRef = useRef<HTMLTextAreaElement>(null)
  const textRef = useRef(text)
  const gen = useRef(0)
  textRef.current = text

  useEffect(() => {
    if (!open) setText(saved)
  }, [saved, open])

  useEffect(() => {
    if (open) growNote(areaRef.current)
  }, [open, text])

  const persist = async (raw: string) => {
    const next = raw.trim().slice(0, 1000)
    if (next === saved) {
      setPhase('idle')
      setError('')
      if (!next) setOpen(false)
      return
    }
    const ticket = ++gen.current
    setPhase('saving')
    setError('')
    try {
      let note = next
      let at = ''
      if (preview) {
        if (next) {
          const now = new Date()
          const pad = (n: number) => String(n).padStart(2, '0')
          at = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(now.getMinutes())}`
        }
      } else {
        const res = await api<{ note?: string; note_at?: string }>(`/night-logist/requests/${item.id}/note`, {
          method: 'PUT',
          body: JSON.stringify({ note: next }),
        })
        note = String(res.note || '')
        at = String(res.note_at || '')
      }
      if (ticket !== gen.current) return
      onSaved?.(note, at)
      setPhase(note ? 'saved' : 'idle')
      if (!note) setOpen(false)
    } catch (e) {
      if (ticket !== gen.current) return
      setPhase('error')
      setError(e instanceof Error ? e.message : 'Не сохранилось')
    }
  }

  useEffect(() => {
    if (!open) return
    if (text.trim() === saved) return
    const timer = window.setTimeout(() => void persist(textRef.current), 650)
    return () => window.clearTimeout(timer)
  }, [text, open, saved])

  const status =
    phase === 'saving' ? 'Сохраняем…' : phase === 'saved' ? 'Сохранено' : phase === 'error' ? '' : 'Только вы это видите'

  if (!open && !saved) {
    return (
      <button
        type="button"
        onClick={(event) => {
          event.stopPropagation()
          setOpen(true)
          setPhase('idle')
          setError('')
        }}
        className="mt-3 inline-flex h-9 items-center gap-1.5 rounded-full border border-dashed border-[#e4d7a4] bg-[#fffdf6] px-3 text-[13px] font-semibold text-[#8a6a12] transition hover:border-[#d4b85a] hover:bg-[#fff8dc]"
      >
        <Pencil size={14} />
        Написать себе
      </button>
    )
  }

  if (!open) {
    return (
      <button
        type="button"
        onClick={(event) => {
          event.stopPropagation()
          setOpen(true)
          setPhase('idle')
          setError('')
        }}
        className="mt-3 w-full rounded-[14px] border border-[#f0e2a8] bg-[#fff8dc] px-3 py-2.5 text-left shadow-[inset_0_1px_0_rgba(255,255,255,0.75)] transition hover:border-[#e4c96a]"
      >
        <span className="flex items-center justify-between gap-3">
          <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-[#8a6a12]">Себе</span>
          <span className="text-[11px] text-[#a08a3a]">{item.self_note_at ? noteWhen(item.self_note_at) || item.self_note_at : 'изменить'}</span>
        </span>
        <span className="mt-1 block whitespace-pre-wrap break-words text-[15px] font-medium leading-snug text-[#3d3416]">
          {saved}
        </span>
      </button>
    )
  }

  return (
    <div
      className="mt-3 rounded-[14px] border border-[#e4c96a] bg-[#fff8dc] px-3 py-2.5 shadow-[inset_0_1px_0_rgba(255,255,255,0.75)]"
      onClick={(event) => event.stopPropagation()}
    >
      <div className="flex items-center justify-between gap-3">
        <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-[#8a6a12]">Себе</span>
        <span className="text-[11px] text-[#a08a3a]">{status}</span>
      </div>
      <textarea
        ref={areaRef}
        autoFocus
        rows={2}
        maxLength={1000}
        value={text}
        placeholder="Например: не берёт трубку, перезвонить утром"
        onChange={(event) => {
          setText(event.target.value.slice(0, 1000))
          setPhase('idle')
          setError('')
          growNote(event.target)
        }}
        onBlur={() => void persist(textRef.current)}
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.preventDefault()
            gen.current += 1
            const keep = saved
            setText(keep)
            setOpen(false)
            setPhase('idle')
            setError('')
            if (textRef.current.trim() !== keep) void persist(keep)
          }
        }}
        className="mt-1 w-full resize-none overflow-hidden bg-transparent text-[15px] font-medium leading-snug text-[#3d3416] outline-none placeholder:font-normal placeholder:text-[#b7a56a]"
      />
      <div className="mt-1 flex items-center justify-between gap-3">
        <span className="text-[11px] text-[#a08a3a]">
          {text.trim().length > 800 ? `${text.trim().length}/1000` : 'Само сохранится'}
        </span>
        {text.trim() ? (
          <button
            type="button"
            className="text-[12px] font-semibold text-[#8a6a12] hover:text-[#6d5310]"
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => {
              setText('')
              void persist('')
            }}
          >
            Стереть
          </button>
        ) : (
          <button
            type="button"
            className="text-[12px] font-semibold text-[#8a6a12]"
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => {
              gen.current += 1
              setText(saved)
              setOpen(false)
            }}
          >
            Закрыть
          </button>
        )}
      </div>
      {error ? <p className="mt-1 text-[12px] text-red-600">{error}</p> : null}
    </div>
  )
}

export function NightRequestsPage() {
  const { user } = useAuth()
  const role = String(user?.role || '').toLowerCase()
  const [params, setParams] = useSearchParams()
  const { openLead } = useCrmLeadDrawer()
  const [items, setItems] = useState<NightRequest[] | null>(null)
  const [total, setTotal] = useState(0)
  const [whenCounts, setWhenCounts] = useState({ today: 0, week: 0, past: 0, all: 0 })
  const [doneCounts, setDoneCounts] = useState({ all: 0, yes: 0, wait: 0, skip: 0 })
  const [callCounts, setCallCounts] = useState({ soon: 0, overdue: 0 })
  const [deptCounts, setDeptCounts] = useState<DeptCount[]>([])
  const [deptAll, setDeptAll] = useState(0)
  const [error, setError] = useState('')
  const [reloadKey, setReloadKey] = useState(0)
  const openDeal = params.get('open') || ''
  const openLeadId = params.get('lead') || ''
  const when = params.get('when') || ''
  const doneParam = params.get('done') || ''
  const done = doneParam === 'yes' || doneParam === 'wait' || doneParam === 'skip' ? doneParam : doneParam === 'no' ? 'wait' : ''
  const dept = params.get('dept') || ''
  const q = params.get('q') || ''
  const from = params.get('from') || ''
  const to = params.get('to') || ''
  const call = params.get('call') === 'soon' || params.get('call') === 'overdue' ? params.get('call') || '' : ''

  const update = (patch: Record<string, string>) => {
    const next = new URLSearchParams(params)
    for (const [key, value] of Object.entries(patch)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    setParams(next, { replace: true })
  }

  useEffect(() => {
    if (role !== 'night_logist' && role !== 'admin') return
    const query = new URLSearchParams()
    if (when) query.set('when', when)
    if (done) query.set('done', done)
    if (dept) query.set('dept', dept)
    if (q.trim()) query.set('q', q.trim())
    if (from) query.set('from', from)
    if (to) query.set('to', to)
    if (call) query.set('call', call)
    const suffix = query.toString()
    void api<NightList>(`/night-logist/requests${suffix ? `?${suffix}` : ''}`)
      .then((res) => {
        setItems(res.items || [])
        setTotal(Number(res.total) || 0)
        setWhenCounts({
          today: Number(res.when_counts?.today) || 0,
          week: Number(res.when_counts?.week) || 0,
          past: Number(res.when_counts?.past) || 0,
          all: Number(res.when_counts?.all) || 0,
        })
        setDoneCounts({
          all: Number(res.done_counts?.all) || 0,
          yes: Number(res.done_counts?.yes) || 0,
          wait: Number(res.done_counts?.wait) || 0,
          skip: Number(res.done_counts?.skip) || 0,
        })
        setCallCounts({
          soon: Number(res.call_counts?.soon) || 0,
          overdue: Number(res.call_counts?.overdue) || 0,
        })
        setDeptAll(Number(res.dept_all) || 0)
        setDeptCounts(
          (res.dept_counts || []).map((row) => ({
            id: String(row.id || ''),
            name: String(row.name || 'Отдел'),
            count: Number(row.count) || 0,
          })),
        )
      })
      .catch((e: Error) => setError(e.message))
  }, [role, when, done, dept, q, from, to, call, reloadKey])

  useEffect(() => {
    if (role !== 'night_logist' && role !== 'admin') return
    let stop = false
    const run = async () => {
      if (openLeadId) {
        openLead(openLeadId)
        return
      }
      if (!openDeal) return
      try {
        const full = await api<{ lead_id?: string }>(`/deals/${openDeal}`)
        let leadId = String(full.lead_id || '')
        if (!leadId) {
          const created = await api<{ lead_id?: string }>(`/deals/${openDeal}/ensure-lead`, {
            method: 'POST',
          })
          leadId = String(created.lead_id || '')
        }
        if (stop) return
        if (!leadId) throw new Error('Не удалось открыть карточку')
        openLead(leadId)
      } catch (e) {
        if (!stop) setError(e instanceof Error ? e.message : 'Не удалось открыть карточку')
      }
    }
    void run()
    return () => {
      stop = true
    }
  }, [role, openDeal, openLeadId, openLead])

  const openCard = (dealId: string) => {
    const next = new URLSearchParams(params)
    next.delete('lead')
    next.set('open', dealId)
    setParams(next, { replace: true })
  }

  if (role !== 'night_logist' && role !== 'admin') {
    return <Navigate to={homePathForRole(user?.role)} replace />
  }

  const filtered = Boolean(q.trim() || when || done || dept || from || to || call)

  return (
    <PageShell>
      <PageHeader title="Заявки" subtitle="Карточки, которые менеджеры отправили ночному логисту" />
      {error ? <p className="mt-4 text-[14px] text-red-300">{error}</p> : null}

      <section className="mt-4 rounded-[20px] bg-white p-4 text-[#1c1915] sm:p-5">
        <div className="grid grid-cols-2 gap-2 lg:grid-cols-4">
          <FilterButton active={when === 'today'} label="Сегодня" count={whenCounts.today} hint="отправили сегодня" onClick={() => update({ when: when === 'today' ? '' : 'today', from: '', to: '' })} />
          <FilterButton active={when === 'week'} label="7 дней" count={whenCounts.week} hint="последняя неделя" onClick={() => update({ when: when === 'week' ? '' : 'week', from: '', to: '' })} />
          <FilterButton active={when === 'past'} label="Раньше" count={whenCounts.past} hint="до сегодня" onClick={() => update({ when: when === 'past' ? '' : 'past', from: '', to: '' })} />
          <FilterButton active={!when} label="Все" count={whenCounts.all} hint="весь список" onClick={() => update({ when: '', from: '', to: '' })} />
        </div>

        <div className="mt-3 grid grid-cols-2 gap-2 lg:grid-cols-4">
          <FilterButton active={done === 'wait'} label="Ждут" count={doneCounts.wait} hint="ещё без отметки" onClick={() => update({ done: done === 'wait' ? '' : 'wait', call: '' })} />
          <FilterButton active={done === 'yes'} label="Обработаны" count={doneCounts.yes} hint="уже сделали" onClick={() => update({ done: done === 'yes' ? '' : 'yes', call: '' })} />
          <FilterButton active={done === 'skip'} label="Не обработаны" count={doneCounts.skip} hint="есть причина" onClick={() => update({ done: done === 'skip' ? '' : 'skip', call: '' })} />
          <FilterButton active={!done && !call} label="Все" count={doneCounts.all} hint="весь список" onClick={() => update({ done: '', call: '' })} />
        </div>

        <div className="mt-3 grid grid-cols-2 gap-2">
          <FilterButton active={call === 'soon'} label="Скоро" count={callCounts.soon} hint="сегодня и раньше" onClick={() => update({ call: call === 'soon' ? '' : 'soon', done: '' })} />
          <FilterButton active={call === 'overdue'} label="Просрочено" count={callCounts.overdue} hint="время уже прошло" onClick={() => update({ call: call === 'overdue' ? '' : 'overdue', done: '' })} />
        </div>

        {deptCounts.length > 0 ? (
          <div className="mt-3 flex flex-wrap gap-2">
            <FilterButton active={!dept} label="Все отделы" count={deptAll} onClick={() => update({ dept: '' })} />
            {deptCounts.map((item) => (
              <FilterButton
                key={item.id}
                active={dept === item.id}
                label={item.name}
                count={item.count}
                onClick={() => update({ dept: dept === item.id ? '' : item.id })}
              />
            ))}
          </div>
        ) : null}

        <label className="mt-3 block">
          <span className="sr-only">Поиск по имени или номеру</span>
          <input
            value={q}
            onChange={(event) => update({ q: event.target.value })}
            placeholder="Имя, номер или своя пометка"
            className="h-12 w-full rounded-[14px] border-2 border-[#e7e2da] bg-[#faf9f7] px-4 text-[16px] outline-none focus:border-[#1c1915]"
          />
        </label>

        <details className="mt-3" open={when === 'range'}>
          <summary className="cursor-pointer text-[14px] font-semibold text-[#2563eb]">Свои даты отправки</summary>
          <div className="mt-2 grid gap-2 sm:grid-cols-2">
            <label className="block text-[13px] text-[#6d675f]">
              С какого числа
              <input
                type="date"
                value={from}
                onChange={(event) => update({ when: 'range', from: event.target.value })}
                className="mt-1 h-11 w-full rounded-[12px] border-2 border-[#e7e2da] px-3 text-[15px] text-[#1c1915]"
              />
            </label>
            <label className="block text-[13px] text-[#6d675f]">
              По какое число
              <input
                type="date"
                value={to}
                onChange={(event) => update({ when: 'range', to: event.target.value })}
                className="mt-1 h-11 w-full rounded-[12px] border-2 border-[#e7e2da] px-3 text-[15px] text-[#1c1915]"
              />
            </label>
          </div>
        </details>
        <p className="mt-3 text-[13px] text-[#6d675f]">
          {filtered ? `Найдено ${total}` : `В списке ${total}`}
          {total > (items?.length || 0) ? `. Показаны первые ${items?.length || 0}` : ''}
        </p>
      </section>

      {items && items.length === 0 ? (
        <p className="mt-6 text-[15px] text-white/70">
          {filtered ? 'Ничего не найдено.' : 'Пока никто ничего не отправил.'}
        </p>
      ) : null}
      <div className="mt-4 flex flex-col gap-3">
        {(items || []).map((item) => (
          <article
            key={item.id}
            className="cursor-pointer rounded-[16px] bg-white px-5 py-4 text-[#1c1915]"
            onClick={() => openCard(item.deal_id)}
          >
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h2 className="text-[18px] font-bold leading-tight">
                  {item.full_name || 'Клиент'}
                  <span className="ml-2 font-semibold tracking-tight text-[#3d3832]">
                    {item.phone?.trim() ? formatPhone(item.phone) : 'нет номера'}
                  </span>
                </h2>
              </div>
              <div className="text-right text-[12px] leading-tight text-[#6d675f]">
                <div className="font-semibold text-[#3d3832]">{noteWhen(item.sent_at) || item.sent_at}</div>
                {item.sent_by_name ? <div className="mt-0.5">отправил {item.sent_by_name}</div> : null}
              </div>
            </div>
            <NightPosition position={item.position} />
            <NightCallMark item={item} />
            <div className="mt-3 flex flex-wrap items-center gap-2 text-[13px] font-semibold text-[#3d3832]">
              {item.department ? <span className="rounded-full bg-[#f1efeb] px-3 py-1">{item.department}</span> : null}
              {item.city ? <span className="rounded-full bg-[#f1efeb] px-3 py-1">{item.city}</span> : null}
              {item.stage ? <span className="rounded-full bg-[#f1efeb] px-3 py-1">{item.stage}</span> : null}
              <button
                type="button"
                onClick={(event) => {
                  event.stopPropagation()
                  openCard(item.deal_id)
                }}
                className="rounded-full bg-[#1c1915] px-3 py-1 text-white"
              >
                Открыть карточку
              </button>
            </div>
            <NightSelfNote
              item={item}
              onSaved={(note, at) => {
                setItems((prev) =>
                  prev
                    ? prev.map((row) => (row.id === item.id ? { ...row, self_note: note, self_note_at: at } : row))
                    : prev,
                )
              }}
            />
            <NightBrief item={item} />
            <div className="mt-2">
              <NightActions item={item} onChanged={() => setReloadKey((value) => value + 1)} />
            </div>
          </article>
        ))}
      </div>
    </PageShell>
  )
}


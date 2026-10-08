import { Check, ChevronLeft, ChevronRight, Copy, Download, FileText, Search, X } from 'lucide-react'
import { type ReactNode, useCallback, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api, token } from '../../lib/api'
import { useAuth } from '../../store/auth'

type PersonFile = { id: string; filename: string; mime?: string; size_bytes?: number }

type Person = {
  id: string
  full_name: string
  birth_date?: string
  passport_number?: string
  phone?: string
  arrival_date?: string
  position?: string
  arrived_at?: string | null
  site_code?: string
  site_name?: string
  files?: PersonFile[]
}

type Loaded = { url: string; mime: string; text?: string }

type When = 'all' | 'today' | 'week' | 'past' | 'range'

type Arrived = '' | 'yes' | 'no'

type PeopleResponse = {
  items?: Person[]
  total?: number
  documents?: number
  page?: number
  page_size?: number
  when_counts?: { today?: number; week?: number; past?: number; all?: number }
  arrived_counts?: { all?: number; yes?: number; no?: number }
  site_counts?: { code?: string; name?: string; count?: number }[]
  dept_counts?: { id?: string; name?: string; count?: number }[]
}

const PAGE_SIZE = 20

function ruStamp(stamp?: string | null) {
  const value = String(stamp || '')
  if (value.length < 16) return ''
  return `${ruDate(value.slice(0, 10), false)}, ${value.slice(11, 16)}`
}

const WHEN_VALUES: When[] = ['all', 'today', 'week', 'past', 'range']

function mskToday() {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Moscow' }).format(new Date())
}

function dayNumber(iso?: string) {
  const value = String(iso || '').slice(0, 10)
  const [y, m, d] = value.split('-').map(Number)
  if (!y || !m || !d) return NaN
  return Date.UTC(y, m - 1, d) / 86_400_000
}

function daysFromToday(iso?: string) {
  return dayNumber(iso) - dayNumber(mskToday())
}

function ruDate(iso?: string, withYear = true) {
  const value = String(iso || '').slice(0, 10)
  const [y, m, d] = value.split('-').map(Number)
  if (!y || !m || !d) return '—'
  return new Date(y, m - 1, d).toLocaleDateString('ru-RU', {
    day: 'numeric',
    month: 'long',
    ...(withYear ? { year: 'numeric' } : {}),
  })
}

function plural(n: number, one: string, few: string, many: string) {
  const mod10 = n % 10
  const mod100 = n % 100
  if (mod10 === 1 && mod100 !== 11) return one
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return few
  return many
}

function arrivalBadge(iso?: string, arrivedAt?: string | null) {
  if (arrivedAt) return { text: 'Прибыл', tone: 'bg-[#dcfce7] text-[#166534]', bar: 'bg-[#16a34a]' }
  const diff = daysFromToday(iso)
  if (Number.isNaN(diff)) return { text: 'Дата не указана', tone: 'bg-[#f1efeb] text-[#5c564e]', bar: 'bg-[#d6d0c7]' }
  if (diff === 0) return { text: 'Приезжает сегодня', tone: 'bg-[#ede9fe] text-[#5b21b6]', bar: 'bg-[#8b5cf6]' }
  if (diff === 1) return { text: 'Приезжает завтра', tone: 'bg-[#fef3c7] text-[#92400e]', bar: 'bg-[#f59e0b]' }
  if (diff > 1)
    return { text: `Приедет через ${diff} ${plural(diff, 'день', 'дня', 'дней')}`, tone: 'bg-[#dbeafe] text-[#1e40af]', bar: 'bg-[#3b82f6]' }
  const ago = -diff
  return {
    text: `Ждали ${ago} ${plural(ago, 'день', 'дня', 'дней')} назад — не отмечен`,
    tone: 'bg-[#fee2e2] text-[#991b1b]',
    bar: 'bg-[#ef4444]',
  }
}

function asFiles(files?: PersonFile[] | number): PersonFile[] {
  return Array.isArray(files) ? files : []
}

function fileKind(file: PersonFile, loaded?: Loaded) {
  const mime = String(loaded?.mime || file.mime || '').toLowerCase()
  if (mime.startsWith('image/')) return 'image'
  if (mime.includes('pdf')) return 'pdf'
  if (mime.startsWith('text/')) return 'text'
  return 'other'
}

function niceName(name: string) {
  return name.replace(/\.[a-z0-9]{2,4}$/i, '') || 'Документ'
}

async function fetchFile(apiRoot: string, personId: string, file: PersonFile, inline: boolean) {
  const res = await fetch(`/api${apiRoot}/${personId}/files/${file.id}${inline ? '?inline=1' : ''}`, {
    headers: { Authorization: `Bearer ${token()}` },
  })
  if (!res.ok) throw new Error('Не удалось открыть файл')
  return res.blob()
}

async function downloadFile(apiRoot: string, personId: string, file: PersonFile) {
  const blob = await fetchFile(apiRoot, personId, file, false)
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = file.filename || 'document'
  a.click()
  URL.revokeObjectURL(url)
}

function useLoadedFile(apiRoot: string, personId: string, file: PersonFile) {
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    if (fileKind(file) === 'other') return
    let dead = false
    const holder = { url: '' }
    void fetchFile(apiRoot, personId, file, true)
      .then(async (blob) => {
        if (dead) return
        holder.url = URL.createObjectURL(blob)
        const mime = file.mime || blob.type
        const text = mime.startsWith('text/') ? await blob.text() : undefined
        if (!dead) setLoaded({ url: holder.url, mime, text })
      })
      .catch(() => {
        if (!dead) setFailed(true)
      })
    return () => {
      dead = true
      if (holder.url) URL.revokeObjectURL(holder.url)
    }
  }, [apiRoot, personId, file.id, file.mime])

  return { loaded, failed }
}

function DocBody({ file, loaded, failed, large }: { file: PersonFile; loaded: Loaded | null; failed: boolean; large?: boolean }) {
  const kind = fileKind(file, loaded || undefined)
  if (loaded && kind === 'image') {
    return <img src={loaded.url} alt={niceName(file.filename)} className="h-full w-full object-contain" />
  }
  if (loaded && kind === 'pdf') {
    return <iframe title={file.filename} src={loaded.url} className="h-full w-full bg-white" />
  }
  if (loaded && kind === 'text') {
    return (
      <pre
        className={[
          'h-full w-full overflow-auto whitespace-pre-wrap bg-white text-left text-[#1c1915]',
          large ? 'p-8 text-[18px] leading-relaxed' : 'p-4 text-[13px] leading-relaxed',
        ].join(' ')}
      >
        {loaded.text}
      </pre>
    )
  }
  return (
    <div className="flex h-full w-full flex-col items-center justify-center gap-2 bg-white px-4 text-center text-[#5c564e]">
      <FileText size={large ? 48 : 28} strokeWidth={1.4} />
      <span className={large ? 'text-[16px]' : 'text-[13px]'}>
        {failed ? 'Не открылось. Скачайте файл.' : kind === 'other' ? 'Этот файл можно только скачать' : 'Открываем…'}
      </span>
    </div>
  )
}

function DocThumb({ apiRoot, personId, file, onOpen }: { apiRoot: string; personId: string; file: PersonFile; onOpen: () => void }) {
  const { loaded, failed } = useLoadedFile(apiRoot, personId, file)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const save = async () => {
    if (saving) return
    setSaving(true)
    setError('')
    try {
      await downloadFile(apiRoot, personId, file)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось скачать')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex flex-col overflow-hidden rounded-[16px] border border-[#e7e2da] bg-white text-left">
      <button
        type="button"
        onClick={onOpen}
        className="group relative aspect-[4/3] w-full overflow-hidden border-b border-[#efebe5] bg-[#f6f4f0] focus:outline-none focus-visible:ring-4 focus-visible:ring-[#3b82f6]/40"
      >
        <DocBody file={file} loaded={loaded} failed={failed} />
      </button>
      <div className="flex flex-col gap-2.5 px-3.5 py-3">
        <span className="flex min-w-0 items-center gap-2">
          <FileText size={16} className="shrink-0 text-[#8a8175]" />
          <span className="truncate text-[14px] font-semibold text-[#1c1915]">{niceName(file.filename)}</span>
        </span>
        <div className="grid grid-cols-2 gap-2">
          <button
            type="button"
            onClick={onOpen}
            className="inline-flex h-10 items-center justify-center rounded-[12px] bg-[#1c1915] px-2 text-[13px] font-semibold text-white hover:bg-[#3b82f6] focus:outline-none focus-visible:ring-4 focus-visible:ring-[#3b82f6]/40"
          >
            Открыть
          </button>
          <button
            type="button"
            disabled={saving}
            onClick={() => void save()}
            className="inline-flex h-10 items-center justify-center gap-1.5 rounded-[12px] border border-[#e7e2da] bg-white px-2 text-[13px] font-semibold text-[#1c1915] hover:bg-[#f7f5f2] focus:outline-none focus-visible:ring-4 focus-visible:ring-[#3b82f6]/40 disabled:opacity-60"
          >
            <Download size={15} />
            {saving ? '…' : 'Скачать'}
          </button>
        </div>
        {error ? <p className="text-[12px] text-[#b91c1c]">{error}</p> : null}
      </div>
    </div>
  )
}

function Viewer({
  apiRoot,
  person,
  index,
  onIndex,
  onClose,
}: {
  apiRoot: string
  person: Person
  index: number
  onIndex: (next: number) => void
  onClose: () => void
}) {
  const files = asFiles(person.files)
  const file = files[index]
  const { loaded, failed } = useLoadedFile(apiRoot, person.id, file)
  const hasMany = files.length > 1

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      if (e.key === 'ArrowRight' && hasMany) onIndex((index + 1) % files.length)
      if (e.key === 'ArrowLeft' && hasMany) onIndex((index - 1 + files.length) % files.length)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [index, files.length, hasMany, onClose, onIndex])

  return createPortal(
    <div className="fixed inset-0 z-[300] flex flex-col bg-[#0b0814]/95 backdrop-blur-sm" role="dialog" aria-modal="true">
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-white sm:px-6">
        <div className="min-w-0">
          <div className="truncate text-[18px] font-semibold">{person.full_name}</div>
          <div className="text-[14px] text-white/60">
            {niceName(file.filename)}
            {hasMany ? ` · ${index + 1} из ${files.length}` : ''}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void downloadFile(apiRoot, person.id, file)}
            className="inline-flex h-11 items-center gap-2 rounded-[12px] bg-white px-4 text-[15px] font-semibold text-[#1c1915] hover:bg-white/90"
          >
            <Download size={18} />
            Скачать
          </button>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex h-11 items-center gap-2 rounded-[12px] bg-white/10 px-4 text-[15px] font-semibold text-white hover:bg-white/20"
          >
            <X size={18} />
            Закрыть
          </button>
        </div>
      </div>
      <div className="relative flex min-h-0 flex-1 items-center justify-center px-4 pb-6 sm:px-16" onClick={onClose}>
        <div
          className="h-full max-h-[82vh] w-full max-w-[1100px] overflow-hidden rounded-[18px] bg-white shadow-2xl"
          onClick={(e) => e.stopPropagation()}
        >
          <DocBody file={file} loaded={loaded} failed={failed} large />
        </div>
        {hasMany ? (
          <>
            <button
              type="button"
              aria-label="Предыдущий документ"
              onClick={(e) => {
                e.stopPropagation()
                onIndex((index - 1 + files.length) % files.length)
              }}
              className="absolute left-2 top-1/2 inline-flex size-12 -translate-y-1/2 items-center justify-center rounded-full bg-white/15 text-white hover:bg-white/25 sm:left-4"
            >
              <ChevronLeft size={26} />
            </button>
            <button
              type="button"
              aria-label="Следующий документ"
              onClick={(e) => {
                e.stopPropagation()
                onIndex((index + 1) % files.length)
              }}
              className="absolute right-2 top-1/2 inline-flex size-12 -translate-y-1/2 items-center justify-center rounded-full bg-white/15 text-white hover:bg-white/25 sm:right-4"
            >
              <ChevronRight size={26} />
            </button>
          </>
        ) : null}
      </div>
    </div>,
    document.body,
  )
}

function formatCardPhone(raw: string) {
  const d = raw.replace(/\D/g, '')
  if (d.length === 11 && (d[0] === '7' || d[0] === '8')) {
    const n = '7' + d.slice(1)
    return `+${n[0]} ${n.slice(1, 4)} ${n.slice(4, 7)}-${n.slice(7, 9)}-${n.slice(9, 11)}`
  }
  if (d.length === 10) {
    return `+7 ${d.slice(0, 3)} ${d.slice(3, 6)}-${d.slice(6, 8)}-${d.slice(8, 10)}`
  }
  return raw.trim()
}

function CopyValue({ value }: { value: string }) {
  const [done, setDone] = useState(false)
  if (!value) return <span>—</span>
  return (
    <button
      type="button"
      onClick={() => {
        void navigator.clipboard?.writeText(value).then(() => {
          setDone(true)
          window.setTimeout(() => setDone(false), 1400)
        })
      }}
      className="inline-flex items-center gap-2 rounded-[10px] text-left tabular-nums hover:text-[#1d4ed8]"
      title="Скопировать"
    >
      {value}
      {done ? <Check size={16} className="text-[#16a34a]" /> : <Copy size={15} className="text-[#a3988c]" />}
      {done ? <span className="text-[12px] font-medium text-[#16a34a]">Скопировано</span> : null}
    </button>
  )
}

function ArrivedToggle({ apiRoot, person, onChange }: { apiRoot: string; person: Person; onChange: (value: string | null) => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const done = Boolean(person.arrived_at)

  const toggle = async () => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      const res = await api<{ arrived_at?: string | null }>(`${apiRoot}/${person.id}/arrived`, {
        method: 'PUT',
        body: JSON.stringify({ arrived: !done }),
      })
      onChange(res.arrived_at || null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не получилось')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mt-4">
      <button
        type="button"
        role="checkbox"
        aria-checked={done}
        disabled={busy}
        onClick={() => void toggle()}
        className={[
          'flex w-full items-center gap-3 rounded-[16px] border-2 px-4 py-3.5 text-left transition focus:outline-none focus-visible:ring-4 focus-visible:ring-[#16a34a]/30 disabled:opacity-60',
          done
            ? 'border-[#16a34a] bg-[#16a34a] text-white hover:bg-[#15803d]'
            : 'border-dashed border-[#c9c1b5] bg-white text-[#1c1915] hover:border-[#16a34a] hover:bg-[#f0fdf4]',
        ].join(' ')}
      >
        <span
          className={[
            'inline-flex size-8 shrink-0 items-center justify-center rounded-[10px] border-2',
            done ? 'border-white bg-white text-[#16a34a]' : 'border-[#c9c1b5] bg-white text-transparent',
          ].join(' ')}
        >
          <Check size={20} strokeWidth={3} />
        </span>
        <span className="min-w-0">
          <span className="block text-[17px] font-bold leading-tight">{done ? 'Прибыл' : 'Отметить: прибыл'}</span>
          <span className={['block text-[13px]', done ? 'text-white/85' : 'text-[#8a8175]'].join(' ')}>
            {busy ? 'Сохраняем…' : done ? `Отмечено ${ruStamp(person.arrived_at)} · нажмите, чтобы снять` : 'Нажмите, когда человек приехал'}
          </span>
        </span>
      </button>
      {error ? <p className="mt-1.5 text-[13px] text-[#b91c1c]">{error}</p> : null}
    </div>
  )
}

function PersonCard({
  apiRoot,
  person,
  onArrived,
  showPhone = false,
}: {
  apiRoot: string
  person: Person
  onArrived: (id: string, value: string | null) => void
  showPhone?: boolean
}) {
  const files = asFiles(person.files)
  const [open, setOpen] = useState<number | null>(null)
  const badge = arrivalBadge(person.arrival_date, person.arrived_at)

  const downloadAll = async () => {
    for (const file of files) {
      await downloadFile(apiRoot, person.id, file)
    }
  }

  return (
    <article className="relative overflow-hidden rounded-[24px] bg-white shadow-[0_20px_50px_rgba(5,3,15,0.35)]">
      <span className={['absolute inset-y-0 left-0 w-[6px]', badge.bar].join(' ')} aria-hidden />
      <div className="flex flex-col gap-6 p-5 pl-7 sm:p-7 sm:pl-9 lg:flex-row">
        <div className="lg:w-[330px] lg:shrink-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className={['inline-flex rounded-full px-3 py-1 text-[13px] font-bold', badge.tone].join(' ')}>{badge.text}</span>
            {person.site_name ? (
              <span className="inline-flex rounded-full bg-[#1c1915] px-3 py-1 text-[13px] font-bold text-white">{person.site_name}</span>
            ) : null}
          </div>
          <h2 className="mt-3 text-[26px] font-bold leading-tight tracking-tight text-[#1c1915]">{person.full_name}</h2>
          <div className="mt-1.5 inline-flex rounded-[10px] bg-[#f1efeb] px-3 py-1 text-[15px] font-semibold text-[#3d3832]">
            {person.position || 'Должность не указана'}
          </div>
          <dl className="mt-5 grid gap-2 text-[15px] text-[#1c1915]">
            <div className="rounded-[14px] bg-[#f7f5f2] px-4 py-3">
              <dt className="text-[13px] text-[#8a8175]">Дата прибытия</dt>
              <dd className="mt-0.5 text-[17px] font-semibold">{ruDate(person.arrival_date)}</dd>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div className="rounded-[14px] bg-[#f7f5f2] px-4 py-3">
                <dt className="text-[13px] text-[#8a8175]">Дата рождения</dt>
                <dd className="mt-0.5 font-semibold">{ruDate(person.birth_date)}</dd>
              </div>
              <div className="rounded-[14px] bg-[#f7f5f2] px-4 py-3">
                <dt className="text-[13px] text-[#8a8175]">Серия и номер</dt>
                <dd className="mt-0.5 font-semibold">
                  <CopyValue value={person.passport_number || ''} />
                </dd>
              </div>
            </div>
            {showPhone ? (
              <div className="rounded-[14px] bg-[#f7f5f2] px-4 py-3">
                <dt className="text-[13px] text-[#8a8175]">Телефон</dt>
                <dd className="mt-0.5 text-[17px] font-semibold">
                  {person.phone?.trim() ? (
                    <CopyValue value={formatCardPhone(person.phone)} />
                  ) : (
                    <span className="text-[15px] font-medium text-[#8a8175]">На карточке нет номера</span>
                  )}
                </dd>
              </div>
            ) : null}
          </dl>
          <ArrivedToggle apiRoot={apiRoot} person={person} onChange={(value) => onArrived(person.id, value)} />
        </div>

        <div className="min-w-0 flex-1 lg:border-l lg:border-[#efebe5] lg:pl-6">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2 text-[16px] font-bold text-[#1c1915]">
              Документы
              <span className="rounded-full bg-[#1c1915] px-2 py-0.5 text-[12px] font-semibold text-white">{files.length}</span>
            </div>
            {files.length > 1 ? (
              <button
                type="button"
                onClick={() => void downloadAll()}
                className="inline-flex h-10 items-center gap-2 rounded-[12px] border border-[#e7e2da] bg-white px-3.5 text-[14px] font-semibold text-[#1c1915] hover:bg-[#f7f5f2]"
              >
                <Download size={16} />
                Скачать все
              </button>
            ) : null}
          </div>
          {files.length === 0 ? (
            <div className="mt-3 rounded-[16px] border-2 border-dashed border-[#e7e2da] px-4 py-10 text-center text-[15px] text-[#8a8175]">
              Логист не приложил документы
            </div>
          ) : (
            <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
              {files.map((file, i) => (
                <DocThumb key={file.id} apiRoot={apiRoot} personId={person.id} file={file} onOpen={() => setOpen(i)} />
              ))}
            </div>
          )}
        </div>
      </div>
      {open !== null && files[open] ? (
        <Viewer apiRoot={apiRoot} person={person} index={open} onIndex={setOpen} onClose={() => setOpen(null)} />
      ) : null}
    </article>
  )
}

const TILE_TONES = {
  violet: { dot: 'bg-[#8b5cf6]', active: 'bg-[#7c3aed] text-white', ring: 'ring-[#7c3aed]' },
  blue: { dot: 'bg-[#3b82f6]', active: 'bg-[#2563eb] text-white', ring: 'ring-[#2563eb]' },
  stone: { dot: 'bg-[#a8a29e]', active: 'bg-[#57534e] text-white', ring: 'ring-[#57534e]' },
  dark: { dot: 'bg-[#1c1915]', active: 'bg-[#1c1915] text-white', ring: 'ring-[#1c1915]' },
} as const

function Tile({
  active,
  label,
  count,
  hint,
  tone,
  onClick,
}: {
  active: boolean
  label: string
  count: number
  hint: string
  tone: keyof typeof TILE_TONES
  onClick: () => void
}) {
  const t = TILE_TONES[tone]
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={[
        'relative flex min-h-[124px] flex-col justify-between rounded-[20px] p-4 text-left shadow-[0_12px_30px_rgba(5,3,15,0.3)] transition hover:-translate-y-0.5 focus:outline-none focus-visible:ring-4 focus-visible:ring-[#3b82f6]/50 sm:p-5',
        active ? t.active : 'bg-white text-[#1c1915]',
      ].join(' ')}
    >
      <span className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-2 text-[15px] font-bold">
          <span className={['size-2.5 rounded-full', active ? 'bg-white' : t.dot].join(' ')} />
          {label}
        </span>
        {active ? <Check size={18} /> : null}
      </span>
      <span className="flex items-end justify-between gap-2">
        <span className="text-[44px] font-bold leading-none tabular-nums">{count}</span>
        <span className={['text-right text-[13px] leading-tight', active ? 'text-white/80' : 'text-[#8a8175]'].join(' ')}>{hint}</span>
      </span>
    </button>
  )
}

const ARRIVED_TONES = {
  dark: 'border-[#1c1915] bg-[#1c1915] text-white',
  green: 'border-[#16a34a] bg-[#16a34a] text-white',
  amber: 'border-[#f59e0b] bg-[#f59e0b] text-[#3f2d00]',
} as const

function ArrivedOption({
  active,
  tone,
  label,
  icon,
  count,
  onClick,
}: {
  active: boolean
  tone: keyof typeof ARRIVED_TONES
  label: string
  icon?: ReactNode
  count: number
  onClick: () => void
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={[
        'flex h-14 items-center justify-between gap-2 rounded-[16px] border-2 px-4 text-[16px] font-bold transition focus:outline-none focus-visible:ring-4 focus-visible:ring-[#3b82f6]/40',
        active ? ARRIVED_TONES[tone] : 'border-[#e7e2da] bg-white text-[#1c1915] hover:border-[#c9c1b5] hover:bg-[#f7f5f2]',
      ].join(' ')}
    >
      <span className="flex items-center gap-2">
        {icon}
        {label}
      </span>
      <span
        className={[
          'min-w-[32px] rounded-full px-2 py-0.5 text-center text-[14px] tabular-nums',
          active ? 'bg-white/90 text-[#1c1915]' : 'bg-[#f1efeb] text-[#5c564e]',
        ].join(' ')}
      >
        {count}
      </span>
    </button>
  )
}

export function CustomerHome({ admin = false }: { admin?: boolean }) {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const [all, setAll] = useState<Person[]>([])
  const [total, setTotal] = useState(0)
  const [documents, setDocuments] = useState(0)
  const [whenCounts, setWhenCounts] = useState({ today: 0, week: 0, past: 0, all: 0 })
  const [arrivedCounts, setArrivedCounts] = useState({ all: 0, yes: 0, no: 0 })
  const [siteCounts, setSiteCounts] = useState<{ code: string; name: string; count: number }[]>([])
  const [deptCounts, setDeptCounts] = useState<{ id: string; name: string; count: number }[]>([])
  const [q, setQ] = useState(params.get('q') || '')
  const [debouncedQ, setDebouncedQ] = useState(q.trim())
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [reloadKey, setReloadKey] = useState(0)
  const rawWhen = params.get('when') as When | null
  const when: When = rawWhen && WHEN_VALUES.includes(rawWhen) ? rawWhen : 'all'
  const rawArrived = params.get('arrived')
  const arrived: Arrived = rawArrived === 'yes' || rawArrived === 'no' ? rawArrived : ''
  const from = params.get('from') || ''
  const to = params.get('to') || ''
  const page = Math.max(1, Number(params.get('page')) || 1)
  const site = admin ? params.get('site') || '' : ''
  const dept = admin ? params.get('dept') || '' : ''
  const apiRoot = admin ? '/admin/arrivals' : '/customer/people'
  const city = user?.customer_site || user?.full_name || 'Ваш город'

  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedQ(q.trim()), 250)
    return () => window.clearTimeout(timer)
  }, [q])

  useEffect(() => {
    let closed = false
    const query = new URLSearchParams()
    if (debouncedQ) query.set('q', debouncedQ)
    if (when !== 'all') query.set('when', when)
    if (from) query.set('from', from)
    if (to) query.set('to', to)
    if (arrived) query.set('arrived', arrived)
    if (admin && site) query.set('site', site)
    if (admin && dept) query.set('dept', dept)
    if (page > 1) query.set('page', String(page))
    const qs = query.toString()
    setLoading(true)
    void api<PeopleResponse>(`${apiRoot}${qs ? `?${qs}` : ''}`)
      .then((res) => {
        if (closed) return
        setAll((res.items || []).map((p) => ({ ...p, files: asFiles(p.files) })))
        setTotal(Number(res.total) || 0)
        setDocuments(Number(res.documents) || 0)
        setWhenCounts({
          today: Number(res.when_counts?.today) || 0,
          week: Number(res.when_counts?.week) || 0,
          past: Number(res.when_counts?.past) || 0,
          all: Number(res.when_counts?.all) || 0,
        })
        setArrivedCounts({
          all: Number(res.arrived_counts?.all) || 0,
          yes: Number(res.arrived_counts?.yes) || 0,
          no: Number(res.arrived_counts?.no) || 0,
        })
        setSiteCounts(
          (res.site_counts || []).map((row) => ({
            code: String(row.code || ''),
            name: String(row.name || ''),
            count: Number(row.count) || 0,
          })),
        )
        setDeptCounts(
          (res.dept_counts || []).map((row) => ({
            id: String(row.id || ''),
            name: String(row.name || 'Без отдела'),
            count: Number(row.count) || 0,
          })),
        )
        setError('')
      })
      .catch((e: Error) => {
        if (!closed) setError(e.message)
      })
      .finally(() => {
        if (!closed) setLoading(false)
      })
    return () => {
      closed = true
    }
  }, [admin, apiRoot, site, dept, debouncedQ, when, from, to, arrived, page, reloadKey])

  const update = useCallback(
    (patch: Record<string, string>) => {
      const next = new URLSearchParams(params)
      if (!Object.prototype.hasOwnProperty.call(patch, 'page')) next.delete('page')
      for (const [key, value] of Object.entries(patch)) {
        if (value) next.set(key, value)
        else next.delete(key)
      }
      setParams(next, { replace: true })
    },
    [params, setParams],
  )

  const shown = all
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const rangeFrom = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1
  const rangeTo = Math.min(page * PAGE_SIZE, total)
  const pageNumbers = [...new Set([1, pages, page - 1, page, page + 1])]
    .filter((n) => n >= 1 && n <= pages)
    .sort((a, b) => a - b)

  const setArrivedLocal = useCallback((id: string, value: string | null) => {
    setAll((rows) => rows.map((p) => (p.id === id ? { ...p, arrived_at: value } : p)))
    setReloadKey((n) => n + 1)
  }, [])

  const filtered = Boolean(q.trim() || when !== 'all' || arrived || site || dept)

  const summary = [
    site ? siteCounts.find((item) => item.code === site)?.name || '' : '',
    dept ? deptCounts.find((item) => item.id === dept)?.name || '' : '',
    arrived === 'yes' ? 'отмечены как прибывшие' : '',
    arrived === 'no' ? 'ещё не отмечены' : '',
    when === 'today' ? 'приезжают сегодня' : '',
    when === 'week' ? 'приезжают в ближайшие 7 дней' : '',
    when === 'past' ? 'дата прибытия прошла' : '',
    when === 'range' ? `прибытие ${from ? `с ${ruDate(from, false)}` : ''} ${to ? `по ${ruDate(to, false)}` : ''}`.trim() : '',
    q.trim() ? `фамилия содержит «${q.trim()}»` : '',
  ].filter(Boolean)

  const reset = () => {
    setQ('')
    setParams({}, { replace: true })
  }

  return (
    <div className="mx-auto flex w-full max-w-[1180px] flex-col gap-6 py-6">
      <header>
        <p className="text-[13px] font-semibold uppercase tracking-[0.18em] text-white/45">
          {admin ? 'Админ' : 'Кабинет заказчика'}
        </p>
        <h1 className="mt-1 text-[42px] font-semibold leading-none tracking-tight text-white">{admin ? 'Прибывающие' : city}</h1>
        <p className="mt-3 max-w-[640px] text-[16px] leading-snug text-white/70">
          {admin
            ? 'Люди, которых логист отправил заказчикам. Галочку «Прибыл» можно поставить здесь.'
            : 'Здесь люди, которых к вам отправил логист. Нажмите на документ, чтобы открыть его на весь экран.'}
        </p>
      </header>

      {admin ? (
        <section aria-label="Город">
          <div className="mb-3 text-[15px] font-semibold text-white/80">Город</div>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-5">
            <ArrivedOption
              active={!site}
              tone="dark"
              label="Все города"
              count={siteCounts.reduce((sum, item) => sum + item.count, 0)}
              onClick={() => update({ site: '' })}
            />
            {siteCounts.map((item) => (
              <ArrivedOption
                key={item.code}
                active={site === item.code}
                tone="dark"
                label={item.name}
                count={item.count}
                onClick={() => update({ site: site === item.code ? '' : item.code })}
              />
            ))}
          </div>
        </section>
      ) : null}

      {admin && deptCounts.length > 0 ? (
        <section aria-label="Отдел">
          <div className="mb-3 text-[15px] font-semibold text-white/80">Отдел</div>
          <div className="flex flex-wrap gap-2">
            <ArrivedOption
              active={!dept}
              tone="dark"
              label="Все отделы"
              count={deptCounts.reduce((sum, item) => sum + item.count, 0)}
              onClick={() => update({ dept: '' })}
            />
            {deptCounts.map((item) => (
              <ArrivedOption
                key={item.id}
                active={dept === item.id}
                tone="dark"
                label={item.name}
                count={item.count}
                onClick={() => update({ dept: dept === item.id ? '' : item.id })}
              />
            ))}
          </div>
        </section>
      ) : null}

      <section aria-label="Когда приезжают">
        <div className="mb-3 flex items-center gap-2 text-[15px] font-semibold text-white/80">
          <span className="inline-flex size-6 items-center justify-center rounded-full bg-white text-[13px] font-bold text-[#1c1915]">1</span>
          Когда приезжают — нажмите на плитку
        </div>
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <Tile tone="violet" active={when === 'today'} label="Сегодня" count={whenCounts.today} hint="приезжают" onClick={() => update({ when: when === 'today' ? '' : 'today', from: '', to: '' })} />
          <Tile tone="blue" active={when === 'week'} label="7 дней" count={whenCounts.week} hint="ближайшая неделя" onClick={() => update({ when: when === 'week' ? '' : 'week', from: '', to: '' })} />
          <Tile tone="stone" active={when === 'past'} label="Дата прошла" count={whenCounts.past} hint="должны были приехать" onClick={() => update({ when: when === 'past' ? '' : 'past', from: '', to: '' })} />
          <Tile tone="dark" active={when === 'all'} label="Все люди" count={whenCounts.all} hint="весь список" onClick={() => update({ when: '', from: '', to: '' })} />
        </div>
      </section>

      <section className="overflow-hidden rounded-[24px] bg-white shadow-[0_20px_50px_rgba(5,3,15,0.35)]">
        <div className="flex flex-col gap-5 p-5 sm:p-6">
          <div>
            <div className="mb-2 flex items-center gap-2 text-[15px] font-bold text-[#1c1915]">
              <span className="inline-flex size-6 items-center justify-center rounded-full bg-[#1c1915] text-[13px] text-white">2</span>
              Прибыл или ещё нет
            </div>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
              <ArrivedOption active={arrived === ''} tone="dark" label="Все" count={arrivedCounts.all} onClick={() => update({ arrived: '' })} />
              <ArrivedOption
                active={arrived === 'yes'}
                tone="green"
                label="Прибыли"
                icon={<Check size={18} strokeWidth={3} />}
                count={arrivedCounts.yes}
                onClick={() => update({ arrived: arrived === 'yes' ? '' : 'yes' })}
              />
              <ArrivedOption
                active={arrived === 'no'}
                tone="amber"
                label="Ещё не прибыли"
                icon={<span className="size-3 rounded-full border-2 border-current" />}
                count={arrivedCounts.no}
                onClick={() => update({ arrived: arrived === 'no' ? '' : 'no' })}
              />
            </div>
          </div>

          <div>
            <div className="mb-2 flex items-center gap-2 text-[15px] font-bold text-[#1c1915]">
              <span className="inline-flex size-6 items-center justify-center rounded-full bg-[#1c1915] text-[13px] text-white">3</span>
              Найти по фамилии
            </div>
            <label className="relative block">
              <span className="sr-only">Поиск по фамилии</span>
              <Search size={20} className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-[#8a8175]" />
              <input
                value={q}
                onChange={(e) => {
                  setQ(e.target.value)
                  update({ q: e.target.value.trim() })
                }}
                placeholder="Например: Соколов"
                className="h-14 w-full rounded-[16px] border-2 border-[#e7e2da] bg-[#faf9f7] pl-12 pr-12 text-[17px] text-[#1c1915] outline-none transition placeholder:text-[#a3988c] focus:border-[#3b82f6] focus:bg-white"
              />
              {q ? (
                <button
                  type="button"
                  aria-label="Очистить поиск"
                  onClick={() => {
                    setQ('')
                    update({ q: '' })
                  }}
                  className="absolute right-3 top-1/2 inline-flex size-9 -translate-y-1/2 items-center justify-center rounded-full bg-[#f1efeb] text-[#5c564e] hover:bg-[#e7e2da]"
                >
                  <X size={18} />
                </button>
              ) : null}
            </label>
          </div>

          <details className="group" open={when === 'range'}>
            <summary className="inline-flex cursor-pointer list-none items-center gap-2 rounded-[10px] text-[14px] font-semibold text-[#2563eb] hover:text-[#1d4ed8]">
              <ChevronRight size={16} className="transition group-open:rotate-90" />
              Нужны конкретные даты? Выберите здесь
            </summary>
            <div className="mt-3 grid gap-3 sm:grid-cols-2">
              <label className="block">
                <span className="text-[13px] font-medium text-[#8a8175]">С какого числа</span>
                <input
                  type="date"
                  value={from}
                  onChange={(e) => update({ when: 'range', from: e.target.value })}
                  className="mt-1 h-12 w-full rounded-[14px] border-2 border-[#e7e2da] bg-[#faf9f7] px-3 text-[16px] text-[#1c1915] outline-none focus:border-[#3b82f6]"
                />
              </label>
              <label className="block">
                <span className="text-[13px] font-medium text-[#8a8175]">По какое число</span>
                <input
                  type="date"
                  value={to}
                  onChange={(e) => update({ when: 'range', to: e.target.value })}
                  className="mt-1 h-12 w-full rounded-[14px] border-2 border-[#e7e2da] bg-[#faf9f7] px-3 text-[16px] text-[#1c1915] outline-none focus:border-[#3b82f6]"
                />
              </label>
            </div>
          </details>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-[#efebe5] bg-[#faf9f7] px-5 py-4 sm:px-6">
          <div className="text-[16px] text-[#1c1915]">
            {loading ? (
              'Загружаем список…'
            ) : (
              <>
                <span className="font-bold">
                  Найдено: {total} {plural(total, 'человек', 'человека', 'человек')}
                </span>
                <span className="text-[#8a8175]">
                  {' '}
                  · показано {rangeFrom}–{rangeTo}
                  {pages > 1 ? `, по ${PAGE_SIZE}` : ''}
                  {' · '}
                  {documents} {plural(documents, 'документ', 'документа', 'документов')}
                </span>
                {summary.length ? <div className="mt-0.5 text-[14px] text-[#5c564e]">Показаны: {summary.join(', ')}</div> : null}
              </>
            )}
          </div>
          {filtered ? (
            <button
              type="button"
              onClick={reset}
              className="inline-flex h-11 items-center gap-2 rounded-[12px] bg-[#1c1915] px-4 text-[15px] font-semibold text-white hover:bg-black"
            >
              <X size={16} />
              Сбросить все фильтры
            </button>
          ) : null}
        </div>
      </section>

      {error ? <p className="text-[15px] text-red-200">{error}</p> : null}

      {!loading && shown.length === 0 ? (
        <div className="rounded-[24px] bg-white px-6 py-16 text-center shadow-[0_20px_50px_rgba(5,3,15,0.35)]">
          <div className="text-[22px] font-bold text-[#1c1915]">{filtered ? 'Никого не нашлось' : 'Пока никого нет'}</div>
          <p className="mx-auto mt-2 max-w-[420px] text-[16px] text-[#5c564e]">
            {filtered
              ? 'По этим условиям людей нет. Попробуйте другую дату или фамилию.'
              : 'Когда логист отправит человека, он появится здесь вместе с документами.'}
          </p>
          {filtered ? (
            <button
              type="button"
              onClick={reset}
              className="mt-5 inline-flex h-12 items-center rounded-[14px] bg-[#1c1915] px-5 text-[15px] font-semibold text-white"
            >
              Показать всех
            </button>
          ) : null}
        </div>
      ) : null}

      <div className="flex flex-col gap-4">
        {shown.map((person) => (
          <PersonCard key={person.id} apiRoot={apiRoot} person={person} showPhone={admin} onArrived={setArrivedLocal} />
        ))}
      </div>

      {pages > 1 ? (
        <nav aria-label="Страницы" className="flex flex-wrap items-center justify-center gap-2 rounded-[20px] bg-white px-4 py-4 shadow-[0_20px_50px_rgba(5,3,15,0.35)]">
          <button
            type="button"
            disabled={page <= 1}
            onClick={() => update({ page: page - 1 <= 1 ? '' : String(page - 1) })}
            className="inline-flex h-12 items-center gap-1 rounded-[14px] bg-[#1c1915] px-4 text-[16px] font-bold text-white disabled:bg-[#e7e2da] disabled:text-[#8a8175]"
          >
            <ChevronLeft size={18} />
            Назад
          </button>
          {pageNumbers.map((n, i) => {
            const prev = pageNumbers[i - 1]
            const gap = prev && n - prev > 1
            return (
              <span key={n} className="contents">
                {gap ? <span className="px-1 text-[#8a8175]">…</span> : null}
                <button
                  type="button"
                  aria-current={n === page ? 'page' : undefined}
                  onClick={() => update({ page: n <= 1 ? '' : String(n) })}
                  className={[
                    'inline-flex size-12 items-center justify-center rounded-[14px] text-[16px] font-bold',
                    n === page ? 'bg-[#1c1915] text-white' : 'bg-[#f1efeb] text-[#1c1915] hover:bg-[#e7e2da]',
                  ].join(' ')}
                >
                  {n}
                </button>
              </span>
            )
          })}
          <button
            type="button"
            disabled={page >= pages}
            onClick={() => update({ page: String(page + 1) })}
            className="inline-flex h-12 items-center gap-1 rounded-[14px] bg-[#1c1915] px-4 text-[16px] font-bold text-white disabled:bg-[#e7e2da] disabled:text-[#8a8175]"
          >
            Дальше
            <ChevronRight size={18} />
          </button>
        </nav>
      ) : null}
    </div>
  )
}

export function CustomerPerson() {
  const { id = '' } = useParams()
  const [person, setPerson] = useState<Person | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    void api<Person>(`/customer/people/${id}`)
      .then((row) => setPerson({ ...row, files: asFiles(row.files) }))
      .catch((e: Error) => setError(e.message))
  }, [id])

  if (error && !person) return <p className="py-8 text-[15px] text-red-200">{error}</p>
  if (!person) return <p className="py-8 text-[15px] text-white/70">Загрузка…</p>

  return (
    <div className="mx-auto w-full max-w-[1180px] py-6">
      <Link
        to="/customer"
        className="inline-flex h-11 items-center gap-2 rounded-[12px] bg-white px-4 text-[15px] font-semibold text-[#1c1915] hover:bg-white/90"
      >
        <ChevronLeft size={18} />
        Ко всем людям
      </Link>
      <div className="mt-4">
        <PersonCard apiRoot="/customer/people" person={person} onArrived={(_, value) => setPerson({ ...person, arrived_at: value })} />
      </div>
    </div>
  )
}

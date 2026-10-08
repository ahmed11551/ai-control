import { Bus, ChevronLeft, ChevronRight, Maximize2, Plane, TrainFront, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api, apiDownload, token } from '../../lib/api'
import { useAuth } from '../../store/auth'
import { Button } from '../ui/Button'

export const BILET_FILE_REQUIRED =
  'Вы не можете перейти в стадию «Билет». Сначала отправьте файл или напишите комментарий, почему файла нет.'

export async function biletGate(dealId: string, stageId?: string) {
  const q = stageId ? `?stage_id=${encodeURIComponent(stageId)}` : ''
  const res = await api<{
    required?: boolean
    bilet?: boolean
    need_cities?: boolean
    need_ticket?: boolean
  }>(`/deals/${dealId}/bilet-gate${q}`)
  const needTicket = Boolean(res.need_ticket ?? res.need_cities)
  return {
    required: Boolean(res.required),
    bilet: Boolean(res.bilet),
    need_cities: needTicket,
    need_ticket: needTicket,
  }
}

export async function biletFileRequired(dealId: string, stageId?: string) {
  return (await biletGate(dealId, stageId)).required
}

type BiletDoc = { id?: string; filename?: string; kind?: string; mime?: string }

const IMAGE_EXT = /\.(jpe?g|png|gif|webp|bmp|heic|heif)$/i

function isDocsViewer(role?: string) {
  const r = String(role || '').toLowerCase()
  return r === 'logist' || r === 'night_logist' || r === 'admin'
}

function isDocImage(file: BiletDoc) {
  if (file.kind === 'note') return false
  const mime = (file.mime || '').toLowerCase()
  if (mime.startsWith('image/')) return true
  return IMAGE_EXT.test(file.filename || '')
}

function DocSlide({
  dealId,
  file,
  tall,
}: {
  dealId: string
  file: BiletDoc
  tall?: boolean
}) {
  const [src, setSrc] = useState('')
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    if (!file.id) return
    let revoke = ''
    let cancel = false
    setSrc('')
    setFailed(false)
    const headers = new Headers()
    const t = token()
    if (t) headers.set('Authorization', `Bearer ${t}`)
    void fetch(`/api/deals/${dealId}/ticket/files/${file.id}`, { headers })
      .then(r => (r.ok ? r.blob() : Promise.reject(new Error('file'))))
      .then(blob => {
        if (cancel) return
        revoke = URL.createObjectURL(blob)
        setSrc(revoke)
      })
      .catch(() => {
        if (!cancel) setFailed(true)
      })
    return () => {
      cancel = true
      if (revoke) URL.revokeObjectURL(revoke)
    }
  }, [dealId, file.id])
  if (failed) {
    return <p className="px-6 text-center text-[13px] text-white/70">{file.filename || 'Фото не открылось'}</p>
  }
  if (!src) return <div className={tall ? 'h-[70vh] w-full animate-pulse bg-white/10' : 'h-64 w-full animate-pulse bg-white/10'} />
  return (
    <img
      src={src}
      alt={file.filename || 'Фото'}
      className={tall ? 'max-h-[86vh] max-w-[92vw] object-contain' : 'max-h-[360px] w-full object-contain'}
    />
  )
}

export function BiletFileModal({
  open,
  dealId,
  onClose,
  onSent,
  onChanged,
  mode = 'block',
}: {
  open: boolean
  dealId: string
  onClose: () => void
  onSent: () => void | Promise<void>
  onChanged?: () => void
  mode?: 'block' | 'add'
}) {
  const [picked, setPicked] = useState<File[]>([])
  const [saved, setSaved] = useState<BiletDoc[]>([])
  const [note, setNote] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [pos, setPos] = useState(() => ({
    x: Math.max(16, window.innerWidth - 1040),
    y: Math.max(16, window.innerHeight - 620),
  }))
  const drag = useRef<{ dx: number; dy: number } | null>(null)
  const { user } = useAuth()
  const viewer = isDocsViewer(user?.role)
  const [photoAt, setPhotoAt] = useState(0)
  const [full, setFull] = useState(false)
  const [loadingDocs, setLoadingDocs] = useState(false)

  useEffect(() => {
    if (!open || !dealId) return
    let cancel = false
    setSaved([])
    setLoadingDocs(viewer)
    const load = async () => {
      try {
        if (viewer) {
          const res = await api<{ files?: BiletDoc[] }>(`/deals/${dealId}/bilet-doc/from-chat`, {
            method: 'POST',
            body: '{}',
          })
          if (!cancel) setSaved(res.files || [])
          return
        }
        const res = await api<{ files?: BiletDoc[] }>(`/deals/${dealId}/bilet-doc`)
        if (!cancel) setSaved(res.files || [])
      } catch {
        if (cancel) return
        try {
          const res = await api<{ files?: BiletDoc[] }>(`/deals/${dealId}/bilet-doc`)
          if (!cancel) setSaved(res.files || [])
        } catch {
          if (!cancel) setSaved([])
        }
      } finally {
        if (!cancel) setLoadingDocs(false)
      }
    }
    void load()
    return () => {
      cancel = true
    }
  }, [open, dealId, viewer])

  useEffect(() => {
    if (open) return
    setPicked([])
    setNote('')
    setError('')
    setFull(false)
    setPhotoAt(0)
    setLoadingDocs(false)
  }, [open])

  const photos = saved.filter(isDocImage)
  const notes = saved.filter(file => file.kind === 'note')
  const otherDocs = saved.filter(file => file.kind !== 'note' && !isDocImage(file))
  const photo = photos[photoAt] || photos[0]

  useEffect(() => {
    if (photoAt >= photos.length) setPhotoAt(0)
  }, [photoAt, photos.length])

  useEffect(() => {
    if (!full) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setFull(false)
      if (e.key === 'ArrowRight' && photos.length > 1) setPhotoAt(i => (i + 1) % photos.length)
      if (e.key === 'ArrowLeft' && photos.length > 1) setPhotoAt(i => (i - 1 + photos.length) % photos.length)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [full, photos.length])

  if (!open) return null

  const stepPhoto = (dir: number) => {
    if (photos.length < 2) return
    setPhotoAt(i => (i + dir + photos.length) % photos.length)
  }

  const addPicked = (list: FileList | null) => {
    const next = [...picked]
    for (const file of Array.from(list || [])) {
      const same = next.some(
        item => item.name === file.name && item.size === file.size && item.lastModified === file.lastModified,
      )
      if (!same) next.push(file)
    }
    if (next.length > 10) {
      setError('За раз можно выбрать не больше 10 файлов')
      setPicked(next.slice(0, 10))
      return
    }
    setPicked(next)
    setError('')
  }

  const send = async () => {
    const text = note.trim()
    if (!picked.length && !text) {
      setError('Если файла нет, напишите комментарий, почему')
      return
    }
    setPending(true)
    setError('')
    try {
      const fd = new FormData()
      for (const file of picked) fd.append('file', file)
      if (text) fd.append('note', text)
      const res = await api<{ failed?: string[] }>(`/deals/${dealId}/bilet-doc`, { method: 'POST', body: fd })
      const fresh = await api<{ files?: BiletDoc[] }>(`/deals/${dealId}/bilet-doc`)
      setSaved(fresh.files || [])
      const failed = res.failed || []
      setPicked(picked.filter(file => failed.includes(file.name)))
      setNote('')
      if (failed.length) setError(`Не удалось добавить: ${failed.join(', ')}`)
      onChanged?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось сохранить')
    } finally {
      setPending(false)
    }
  }

  return createPortal(
    <>
    <div
      className={`fixed z-[220] overflow-hidden rounded-[16px] bg-[#f4efe4] text-[#10243e] shadow-[0_16px_48px_rgba(15,23,42,0.28)] ring-1 ring-black/10 ${viewer ? 'w-[min(520px,calc(100vw-16px))]' : 'w-[min(440px,calc(100vw-16px))]'}`}
      style={{ left: pos.x, top: pos.y }}
    >
      <div
        className="flex cursor-grab items-center justify-between bg-[#10243e] px-4 py-3 text-white active:cursor-grabbing"
        onPointerDown={e => {
          if ((e.target as HTMLElement).closest('button')) return
          drag.current = { dx: e.clientX - pos.x, dy: e.clientY - pos.y }
          e.currentTarget.setPointerCapture(e.pointerId)
        }}
        onPointerMove={e => {
          if (!drag.current) return
          setPos({ x: e.clientX - drag.current.dx, y: e.clientY - drag.current.dy })
        }}
        onPointerUp={() => {
          drag.current = null
        }}
      >
        <div>
          <p className="text-[10px] uppercase tracking-[0.2em] text-white/50">
            {viewer || mode === 'add' ? 'Карточка' : 'Стадия «Билет»'}
          </p>
          <p className="text-[15px] font-semibold">
            {viewer ? 'Документы' : mode === 'add' ? 'Добавить документы' : 'Вы не можете перейти'}
          </p>
        </div>
        <button
          type="button"
          className="inline-flex size-8 items-center justify-center rounded-md text-white/80 hover:bg-white/10"
          disabled={pending}
          onClick={onClose}
          aria-label="Закрыть"
        >
          <X className="size-4" />
        </button>
      </div>
      <div className="p-4">
        {viewer ? (
          <>
            {loadingDocs ? (
              <p className="py-10 text-center text-[14px] text-[#10243e]/60">Загружаем файлы из переписки…</p>
            ) : photo ? (
              <div>
                <div className="relative flex min-h-[280px] items-center justify-center overflow-hidden rounded-xl bg-[#10243e]">
                  <button type="button" className="block w-full" onClick={() => setFull(true)} aria-label="Открыть на весь экран">
                    <DocSlide dealId={dealId} file={photo} />
                  </button>
                  {photos.length > 1 ? (
                    <>
                      <button
                        type="button"
                        className="absolute left-2 top-1/2 z-10 inline-flex size-9 -translate-y-1/2 items-center justify-center rounded-full bg-black/45 text-white hover:bg-black/65"
                        onClick={() => stepPhoto(-1)}
                        aria-label="Предыдущее фото"
                      >
                        <ChevronLeft className="size-5" />
                      </button>
                      <button
                        type="button"
                        className="absolute right-2 top-1/2 z-10 inline-flex size-9 -translate-y-1/2 items-center justify-center rounded-full bg-black/45 text-white hover:bg-black/65"
                        onClick={() => stepPhoto(1)}
                        aria-label="Следующее фото"
                      >
                        <ChevronRight className="size-5" />
                      </button>
                    </>
                  ) : null}
                </div>
                <div className="mt-2 flex items-center justify-between gap-2 text-[12px] text-[#10243e]/60">
                  <span>{photos.length > 1 ? `${photoAt + 1} / ${photos.length}` : photo.filename || 'Фото'}</span>
                  <span className="flex items-center gap-3">
                    <button
                      type="button"
                      className="font-medium text-[#10243e]"
                      onClick={() => photo.id && void apiDownload(`/deals/${dealId}/ticket/files/${photo.id}`, photo.filename || 'photo')}
                    >
                      Скачать
                    </button>
                    <button type="button" className="inline-flex items-center gap-1 font-medium text-[#10243e]" onClick={() => setFull(true)}>
                      <Maximize2 className="size-3.5" />
                      На весь экран
                    </button>
                  </span>
                </div>
              </div>
            ) : otherDocs.length ? null : (
              <p className="text-[14px] text-[#10243e]/55">Фото пока нет</p>
            )}
            {!loadingDocs && photos.length + otherDocs.length > 0 ? (
              <button
                type="button"
                className="mt-3 flex h-10 w-full items-center justify-center rounded-xl bg-[#145c45] text-[14px] font-semibold text-[#f6f0e4]"
                onClick={() => void apiDownload(`/deals/${dealId}/bilet-doc/archive`, 'dokumenty.zip')}
              >
                Скачать все
              </button>
            ) : null}
            {!loadingDocs && otherDocs.length ? (
              <div className="mt-3">
                <p className="text-[11px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">Файлы</p>
                <ul className="mt-2 max-h-36 space-y-1 overflow-auto">
                  {otherDocs.map(file => (
                    <li key={file.id || file.filename} className="flex items-center gap-2 rounded-lg bg-white/70 px-3 py-2">
                      <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{file.filename || 'файл'}</span>
                      <button
                        type="button"
                        className="shrink-0 text-[12px] font-semibold text-[#145c45]"
                        onClick={() => file.id && void apiDownload(`/deals/${dealId}/ticket/files/${file.id}`, file.filename || 'file')}
                      >
                        Скачать
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
            {notes.length ? (
              <ul className="mt-3 max-h-28 space-y-1 overflow-auto">
                {notes.map(file => (
                  <li key={file.id || file.filename} className="rounded-lg bg-white/70 px-3 py-2 text-[13px] leading-snug">
                    <span className="block text-[11px] font-semibold uppercase tracking-[0.12em] text-[#10243e]/45">Комментарий</span>
                    {file.filename || 'без текста'}
                  </li>
                ))}
              </ul>
            ) : null}
          </>
        ) : (
        <>
        <p className="text-[14px] leading-relaxed">
          {mode === 'add'
            ? 'Можно выбрать несколько файлов. Если файла нет — напишите в комментарии, почему.'
            : 'Добавьте документы на карточку. Можно несколько файлов. Если файла нет — объясните в комментарии, почему.'}
        </p>
        <div className="mt-4">
          <p className="text-[11px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">Уже на карточке</p>
          {saved.length ? (
            <ul className="mt-2 max-h-36 space-y-1 overflow-auto">
              {saved.map(file => (
                <li
                  key={file.id || file.filename}
                  className={
                    file.kind === 'note'
                      ? 'rounded-lg bg-white/70 px-3 py-2 text-[14px] leading-snug'
                      : 'truncate rounded-lg bg-white/70 px-3 py-2 text-[14px] font-medium'
                  }
                >
                  {file.kind === 'note' ? (
                    <>
                      <span className="block text-[11px] font-semibold uppercase tracking-[0.12em] text-[#10243e]/45">
                        Комментарий
                      </span>
                      {file.filename || 'без текста'}
                    </>
                  ) : (
                    file.filename || 'файл'
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p className="mt-2 text-[13px] text-[#10243e]/45">Пока ничего не добавлено</p>
          )}
        </div>
        <label className="mt-4 flex cursor-pointer flex-col gap-1 rounded-xl bg-[#145c45] px-3.5 py-3 text-[#f6f0e4] shadow-[0_8px_18px_rgba(20,92,69,0.28)]">
          <span className="text-[11px] uppercase tracking-[0.14em] text-[#e7c56a]">Файлы</span>
          <span className="truncate text-[15px] font-semibold">
            {picked.length ? `Выбрано: ${picked.length}` : 'Выберите один или несколько'}
          </span>
          <input
            type="file"
            multiple
            className="hidden"
            disabled={pending}
            onChange={e => {
              addPicked(e.target.files)
              e.target.value = ''
            }}
          />
        </label>
        {picked.length ? (
          <ul className="mt-2 space-y-1">
            {picked.map((file, index) => (
              <li key={`${file.name}-${file.size}-${file.lastModified}`} className="flex items-center gap-2 text-[13px]">
                <span className="min-w-0 flex-1 truncate">{file.name}</span>
                <button
                  type="button"
                  className="shrink-0 text-[#10243e]/50 hover:text-[#10243e]"
                  disabled={pending}
                  onClick={() => setPicked(picked.filter((_, i) => i !== index))}
                >
                  убрать
                </button>
              </li>
            ))}
          </ul>
        ) : null}
        <label className="mt-3 block">
          <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
            Комментарий
          </span>
          <textarea
            className="mt-1 min-h-16 w-full resize-none bg-transparent text-[14px] outline-none placeholder:text-[#10243e]/35"
            placeholder="Если файла нет, объясните здесь, почему"
            value={note}
            disabled={pending}
            onChange={e => {
              setNote(e.target.value)
              if (e.target.value.trim()) setError('')
            }}
          />
        </label>
        {error ? <p className="mt-2 text-[13px] text-red-600">{error}</p> : null}
        <div className="mt-4 flex justify-end gap-2">
          <Button type="button" variant="filter" disabled={pending} onClick={onClose}>
            Закрыть
          </Button>
          {mode === 'block' && saved.length > 0 && picked.length === 0 && !note.trim() ? (
            <Button type="button" variant="green" disabled={pending} onClick={() => void onSent()}>
              Дальше
            </Button>
          ) : (
            <Button type="button" variant="green" disabled={pending} onClick={() => void send()}>
              {pending
                ? 'Сохраняем…'
                : !picked.length
                  ? 'Сохранить комментарий'
                  : picked.length > 1
                    ? `Добавить ${picked.length}`
                    : 'Добавить'}
            </Button>
          )}
        </div>
        </>
        )}
      </div>
    </div>
    {full && photo ? (
      <div className="fixed inset-0 z-[240] flex items-center justify-center bg-black/88 p-4">
        {photos.length > 1 ? (
          <button
            type="button"
            className="absolute left-4 top-1/2 inline-flex size-11 -translate-y-1/2 items-center justify-center rounded-full bg-white/15 text-white hover:bg-white/25"
            onClick={() => stepPhoto(-1)}
            aria-label="Предыдущее фото"
          >
            <ChevronLeft className="size-6" />
          </button>
        ) : null}
        <DocSlide dealId={dealId} file={photo} tall />
        {photos.length > 1 ? (
          <button
            type="button"
            className="absolute right-4 top-1/2 inline-flex size-11 -translate-y-1/2 items-center justify-center rounded-full bg-white/15 text-white hover:bg-white/25"
            onClick={() => stepPhoto(1)}
            aria-label="Следующее фото"
          >
            <ChevronRight className="size-6" />
          </button>
        ) : null}
        <button
          type="button"
          className="absolute right-4 top-4 inline-flex size-10 items-center justify-center rounded-full bg-white/15 text-white hover:bg-white/25"
          onClick={() => setFull(false)}
          aria-label="Закрыть полный экран"
        >
          <X className="size-5" />
        </button>
        {photos.length > 1 ? (
          <p className="absolute bottom-4 text-[13px] text-white/80">
            {photoAt + 1} / {photos.length}
          </p>
        ) : null}
      </div>
    ) : null}
    </>,
    document.body,
  )
}

export function BiletDocsButton({
  dealId,
  onSent,
}: {
  dealId: string
  onSent?: () => void
}) {
  const { user } = useAuth()
  const viewer = isDocsViewer(user?.role)
  const [open, setOpen] = useState(false)
  if (!dealId || !viewer) return null
  return (
    <>
      <button
        type="button"
        className="mt-2 flex w-full items-center justify-between rounded-[12px] border border-black/8 bg-white px-3 py-2.5 text-left hover:bg-[#f7f8fa]"
        onClick={() => setOpen(true)}
      >
        <span>
          <span className="block text-[13px] font-semibold text-b24-text-dark">
            Документы
          </span>
          <span className="block text-[12px] text-b24-text-muted">
            Фото и файлы
          </span>
        </span>
        <span className="text-[12px] text-b24-cyan">Открыть</span>
      </button>
      <BiletFileModal
        open={open}
        dealId={dealId}
        mode="add"
        onClose={() => setOpen(false)}
        onChanged={onSent}
        onSent={() => {
          setOpen(false)
          onSent?.()
        }}
      />
    </>
  )
}

export function BiletRequestModal({
  open,
  dealId,
  onClose,
  onDone,
}: {
  open: boolean
  dealId: string
  onClose: () => void
  onDone: () => Promise<void> | void
}) {
  const [fromCity, setFromCity] = useState('')
  const [toCity, setToCity] = useState('')
  const [date, setDate] = useState('')
  const [time, setTime] = useState('')
  const [transport, setTransport] = useState('поезд')
  const [notes, setNotes] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  if (!open) return null

  const transports = [
    { id: 'самолет', label: 'Самолёт', Icon: Plane },
    { id: 'поезд', label: 'Поезд', Icon: TrainFront },
    { id: 'автобус', label: 'Автобус', Icon: Bus },
  ] as const

  const submit = async () => {
    if (!fromCity.trim() || !toCity.trim()) {
      setError('Укажите город отправления и город прибытия')
      return
    }
    setPending(true)
    setError('')
    try {
      const depart = date ? `${date}T${time || '00:00'}` : ''
      const local = depart ? new Date(depart) : null
      await api(`/deals/${dealId}/ticket`, {
        method: 'PUT',
        body: JSON.stringify({
          transport,
          from_city: fromCity.trim(),
          to_city: toCity.trim(),
          route: `${fromCity.trim()} → ${toCity.trim()}`,
          depart_at: local && !Number.isNaN(local.getTime()) ? local.toISOString() : '',
          notes: notes.trim(),
          flight_or_train: '',
          arrive_at: '',
        }),
      })
      setFromCity('')
      setToCity('')
      setDate('')
      setTime('')
      setNotes('')
      await onDone()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'ошибка')
    } finally {
      setPending(false)
    }
  }

  const field =
    'w-full bg-transparent text-[22px] font-semibold uppercase tracking-wide text-[#10243e] outline-none placeholder:text-[#10243e]/25 disabled:opacity-60'

  return (
    <div className="fixed inset-0 z-[80] flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-[640px] overflow-hidden rounded-[18px] bg-[#f4efe4] shadow-2xl ring-1 ring-black/10">
        <div className="flex items-center justify-between bg-[#10243e] px-5 py-4 text-white">
          <div>
            <p className="text-[11px] uppercase tracking-[0.22em] text-white/60">Заявка</p>
            <p className="text-[18px] font-semibold tracking-wide">Посадочный билет</p>
          </div>
          <p className="text-[12px] uppercase tracking-[0.18em] text-[#d6b25e]">KR Flow</p>
        </div>

        <div className="px-5 pt-5">
          <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-3">
            <label>
              <span className="text-[10px] font-semibold uppercase tracking-[0.16em] text-[#10243e]/55">
                Откуда
              </span>
              <input
                className={field}
                placeholder="Москва"
                value={fromCity}
                disabled={pending}
                onChange={e => setFromCity(e.target.value)}
              />
            </label>
            <span className="pb-1 text-[22px] text-[#10243e]/40">→</span>
            <label>
              <span className="text-[10px] font-semibold uppercase tracking-[0.16em] text-[#10243e]/55">
                Куда
              </span>
              <input
                className={field}
                placeholder="Казань"
                value={toCity}
                disabled={pending}
                onChange={e => setToCity(e.target.value)}
              />
            </label>
          </div>

          <div className="mt-5 grid grid-cols-2 gap-4 border-t border-[#10243e]/10 pt-4 sm:grid-cols-3">
            <label>
              <span className="text-[10px] font-semibold uppercase tracking-[0.16em] text-[#10243e]/55">
                Дата
              </span>
              <input
                type="date"
                className="mt-1 w-full bg-transparent text-[16px] font-medium text-[#10243e] outline-none disabled:opacity-60"
                value={date}
                disabled={pending}
                onChange={e => setDate(e.target.value)}
              />
            </label>
            <label>
              <span className="text-[10px] font-semibold uppercase tracking-[0.16em] text-[#10243e]/55">
                Время
              </span>
              <input
                type="time"
                className="mt-1 w-full bg-transparent text-[16px] font-medium text-[#10243e] outline-none disabled:opacity-60"
                value={time}
                disabled={pending}
                onChange={e => setTime(e.target.value)}
              />
            </label>
            <div className="col-span-2 sm:col-span-1">
              <span className="text-[10px] font-semibold uppercase tracking-[0.16em] text-[#10243e]/55">
                Транспорт
              </span>
              <div className="mt-1 flex gap-1.5">
                {transports.map(({ id, label, Icon }) => {
                  const on = transport === id
                  return (
                    <button
                      key={id}
                      type="button"
                      disabled={pending}
                      title={label}
                      onClick={() => setTransport(id)}
                      className={
                        on
                          ? 'flex h-9 flex-1 items-center justify-center rounded-md bg-[#10243e] text-white'
                          : 'flex h-9 flex-1 items-center justify-center rounded-md bg-white/70 text-[#10243e]/70 ring-1 ring-[#10243e]/10'
                      }
                    >
                      <Icon className="size-4" />
                    </button>
                  )
                })}
              </div>
            </div>
          </div>
        </div>

        <div className="relative mt-5">
          <div className="absolute -left-3 top-0 size-6 -translate-y-1/2 rounded-full bg-black/50" />
          <div className="absolute -right-3 top-0 size-6 -translate-y-1/2 rounded-full bg-black/50" />
          <div className="border-t border-dashed border-[#10243e]/25" />
        </div>

        <div className="px-5 py-4">
          <label>
            <span className="text-[10px] font-semibold uppercase tracking-[0.16em] text-[#10243e]/55">
              Комментарий
            </span>
            <textarea
              className="mt-1 min-h-16 w-full resize-none bg-transparent text-[14px] text-[#10243e] outline-none placeholder:text-[#10243e]/30 disabled:opacity-60"
              placeholder="Пожелания к рейсу, поезду или времени"
              value={notes}
              disabled={pending}
              onChange={e => setNotes(e.target.value)}
            />
          </label>
          {error ? <p className="mt-2 text-[12px] text-red-600">{error}</p> : null}
          <div className="mt-3 flex justify-end gap-2">
            <Button type="button" variant="filter" disabled={pending} onClick={onClose}>
              Отмена
            </Button>
            <Button type="button" variant="green" disabled={pending} onClick={() => void submit()}>
              {pending ? '…' : 'Оформить билет'}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

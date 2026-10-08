import { ChevronLeft, ChevronRight, Download, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { apiDownload, token } from '../../lib/api'

const blobCache = new Map<string, Promise<string>>()

function supportFileURL(id: string) {
  let p = blobCache.get(id)
  if (!p) {
    const headers = new Headers()
    const t = token()
    if (t) headers.set('Authorization', `Bearer ${t}`)
    p = fetch(`/api/support/files/${id}`, { headers })
      .then((res) => (res.ok ? res.blob() : Promise.reject(new Error('file'))))
      .then((blob) => URL.createObjectURL(blob))
    p.catch(() => blobCache.delete(id))
    blobCache.set(id, p)
  }
  return p
}

export function useSupportFileURL(id: string) {
  const [url, setUrl] = useState('')
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let dead = false
    setFailed(false)
    supportFileURL(id)
      .then((u) => {
        if (!dead) setUrl(u)
      })
      .catch(() => {
        if (!dead) setFailed(true)
      })
    return () => {
      dead = true
    }
  }, [id])
  return { url, failed }
}

export function SupportImage({
  id,
  alt,
  className,
  onLoad,
}: {
  id: string
  alt: string
  className?: string
  onLoad?: () => void
}) {
  const { url, failed } = useSupportFileURL(id)
  if (failed) {
    return (
      <div className="grid h-28 w-40 place-items-center rounded-[12px] bg-black/5 text-[12px] text-b24-text-muted">
        Фото не загрузилось
      </div>
    )
  }
  if (!url) return <div className="h-40 w-56 animate-pulse rounded-[12px] bg-black/8" />
  return <img src={url} alt={alt} className={className} onLoad={onLoad} draggable={false} />
}

export type LightboxItem = { id: string; name: string }

export function SupportLightbox({
  items,
  index,
  onIndex,
  onClose,
}: {
  items: LightboxItem[]
  index: number
  onIndex: (i: number) => void
  onClose: () => void
}) {
  const item = items[index]
  const { url } = useSupportFileURL(item?.id || '')
  const many = items.length > 1

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      else if (e.key === 'ArrowLeft' && many) onIndex((index - 1 + items.length) % items.length)
      else if (e.key === 'ArrowRight' && many) onIndex((index + 1) % items.length)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [index, items.length, many, onClose, onIndex])

  if (!item) return null
  return createPortal(
    <div
      className="fixed inset-0 z-[300] flex flex-col bg-black/90 backdrop-blur-sm"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label="Просмотр фото"
    >
      <div className="flex items-center justify-between gap-3 px-4 py-3 text-white" onClick={(e) => e.stopPropagation()}>
        <div className="min-w-0 truncate text-[14px]">
          {item.name || 'Фото'}
          {many ? <span className="ml-2 text-white/60">{index + 1} из {items.length}</span> : null}
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            className="grid size-10 place-items-center rounded-full hover:bg-white/10"
            onClick={() => void apiDownload(`/support/files/${item.id}`, item.name || 'photo')}
            aria-label="Скачать"
            title="Скачать"
          >
            <Download size={18} />
          </button>
          <button
            type="button"
            className="grid size-10 place-items-center rounded-full hover:bg-white/10"
            onClick={onClose}
            aria-label="Закрыть"
            title="Закрыть (Esc)"
          >
            <X size={20} />
          </button>
        </div>
      </div>
      <div className="relative flex min-h-0 flex-1 items-center justify-center px-4 pb-6">
        {many ? (
          <button
            type="button"
            className="absolute left-3 grid size-12 place-items-center rounded-full bg-white/10 text-white hover:bg-white/20"
            onClick={(e) => {
              e.stopPropagation()
              onIndex((index - 1 + items.length) % items.length)
            }}
            aria-label="Предыдущее фото"
          >
            <ChevronLeft size={24} />
          </button>
        ) : null}
        {url ? (
          <img
            src={url}
            alt={item.name || 'Фото'}
            className="max-h-full max-w-full rounded-[8px] object-contain shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          />
        ) : (
          <div className="size-16 animate-spin rounded-full border-2 border-white/20 border-t-white" />
        )}
        {many ? (
          <button
            type="button"
            className="absolute right-3 grid size-12 place-items-center rounded-full bg-white/10 text-white hover:bg-white/20"
            onClick={(e) => {
              e.stopPropagation()
              onIndex((index + 1) % items.length)
            }}
            aria-label="Следующее фото"
          >
            <ChevronRight size={24} />
          </button>
        ) : null}
      </div>
    </div>,
    document.body,
  )
}


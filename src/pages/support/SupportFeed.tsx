import { AlertCircle, Check, ChevronDown, Clock3, Download, FileText, Loader2, RotateCw, SmilePlus } from 'lucide-react'
import { Fragment, useCallback, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { apiDownload } from '../../lib/api'
import { SupportImage, type LightboxItem } from './SupportMedia'
import { SupportAvatar } from './SupportThreadList'
import {
  clockTime,
  dayKey,
  dayLabel,
  fileSize,
  fullTime,
  isConfirmed,
  SUPPORT_REACTIONS,
  type SupportMessage,
} from './supportTypes'

const GROUP_GAP_MS = 5 * 60 * 1000
const URL_RE = /(https?:\/\/[^\s<>"']+)/g

function linkify(text: string, mine: boolean): ReactNode[] {
  return text.split(URL_RE).map((part, i) =>
    i % 2 === 1 ? (
      <a
        key={i}
        href={part}
        target="_blank"
        rel="noreferrer noopener"
        className={['break-all underline underline-offset-2', mine ? 'text-white' : 'text-[#1f6feb]'].join(' ')}
      >
        {part}
      </a>
    ) : (
      <Fragment key={i}>{part}</Fragment>
    ),
  )
}

function joins(a?: SupportMessage, b?: SupportMessage) {
  if (!a || !b) return false
  if (a.kind === 'system' || b.kind === 'system') return false
  if (a.author_id !== b.author_id || a.mine !== b.mine) return false
  if (dayKey(a.created_at) !== dayKey(b.created_at)) return false
  return Math.abs(new Date(b.created_at).getTime() - new Date(a.created_at).getTime()) < GROUP_GAP_MS
}

export function SupportFeed({
  messages,
  loading,
  hasMore,
  loadingOlder,
  onLoadOlder,
  onReact,
  onRetry,
  onOpenImage,
  showAuthorNames,
  emptyTitle,
  emptyText,
}: {
  messages: SupportMessage[]
  loading: boolean
  hasMore: boolean
  loadingOlder: boolean
  onLoadOlder: () => void
  onReact: (msg: SupportMessage, emoji: string) => void
  onRetry: (msg: SupportMessage) => void
  onOpenImage: (items: LightboxItem[], index: number) => void
  showAuthorNames: boolean
  emptyTitle: string
  emptyText: string
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const atBottomRef = useRef(true)
  const restoreRef = useRef<{ height: number; top: number } | null>(null)
  const prevRef = useRef<{ first: string; last: string; ids: Set<string> }>({ first: '', last: '', ids: new Set() })
  const [showDown, setShowDown] = useState(false)
  const [newCount, setNewCount] = useState(0)

  const scrollToBottom = useCallback((smooth = false) => {
    const el = scrollRef.current
    if (!el) return
    el.scrollTo({ top: el.scrollHeight, behavior: smooth ? 'smooth' : 'auto' })
    atBottomRef.current = true
    setShowDown(false)
    setNewCount(0)
  }, [])

  const requestOlder = useCallback(() => {
    const el = scrollRef.current
    if (!el || !hasMore || loadingOlder || restoreRef.current) return
    restoreRef.current = { height: el.scrollHeight, top: el.scrollTop }
    onLoadOlder()
  }, [hasMore, loadingOlder, onLoadOlder])

  const onScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80
    atBottomRef.current = atBottom
    setShowDown(!atBottom)
    if (atBottom) setNewCount(0)
    if (el.scrollTop < 160) requestOlder()
  }, [requestOlder])

  useLayoutEffect(() => {
    if (!loadingOlder && restoreRef.current) {
      const el = scrollRef.current
      const prevFirst = prevRef.current.first
      if (el && messages[0]?.id === prevFirst) restoreRef.current = null
    }
  }, [loadingOlder, messages])

  useLayoutEffect(() => {
    const el = scrollRef.current
    const first = messages[0]?.id || ''
    const last = messages[messages.length - 1]?.id || ''
    const prev = prevRef.current
    if (el) {
      if (!prev.first && first) {
        el.scrollTop = el.scrollHeight
        atBottomRef.current = true
      } else if (first !== prev.first && restoreRef.current) {
        el.scrollTop = el.scrollHeight - restoreRef.current.height + restoreRef.current.top
        restoreRef.current = null
      }
      if (last && last !== prev.last && prev.last) {
        const tail = messages[messages.length - 1]
        if ((tail.mine && tail.kind !== 'system') || atBottomRef.current) {
          el.scrollTop = el.scrollHeight
          atBottomRef.current = true
          setShowDown(false)
          setNewCount(0)
        } else {
          const fresh = messages.filter((m) => !prev.ids.has(m.id) && !m.mine && m.kind !== 'system').length
          if (fresh > 0) setNewCount((c) => c + fresh)
        }
      }
      if (hasMore && !loadingOlder && el.scrollHeight <= el.clientHeight + 40) requestOlder()
    }
    prevRef.current = { first, last, ids: new Set(messages.map((m) => m.id)) }
  }, [messages, hasMore, loadingOlder, requestOlder])

  const stick = useCallback(() => {
    const el = scrollRef.current
    if (el && atBottomRef.current) el.scrollTop = el.scrollHeight
  }, [])

  const images = useMemo(() => {
    const out: LightboxItem[] = []
    for (const m of messages) for (const f of m.files || []) if (f.kind === 'image') out.push({ id: f.id, name: f.name })
    return out
  }, [messages])

  const rows: ReactNode[] = []
  messages.forEach((m, i) => {
    const prev = messages[i - 1]
    const next = messages[i + 1]
    if (!prev || dayKey(prev.created_at) !== dayKey(m.created_at)) {
      rows.push(
        <div key={`day-${dayKey(m.created_at)}-${m.id}`} className="sticky top-2 z-[1] flex justify-center py-2">
          <span className="rounded-full bg-white/90 px-3 py-1 text-[11.5px] font-medium text-b24-text-muted shadow-sm ring-1 ring-black/5 backdrop-blur">
            {dayLabel(m.created_at)}
          </span>
        </div>,
      )
    }
    if (m.kind === 'system') {
      rows.push(
        <div key={m.id} className="flex justify-center py-1.5">
          <span className="rounded-full bg-[#1a7f37]/10 px-3 py-1 text-[12px] text-[#1a7f37]" title={fullTime(m.created_at)}>
            {m.body} · {clockTime(m.created_at)}
          </span>
        </div>,
      )
      return
    }
    rows.push(
      <SupportBubble
        key={m.client_id || m.id}
        msg={m}
        first={!joins(prev, m)}
        last={!joins(m, next)}
        showName={showAuthorNames}
        onReact={onReact}
        onRetry={onRetry}
        onImageLoad={stick}
        onOpenImage={(id) => {
          const idx = images.findIndex((x) => x.id === id)
          onOpenImage(images, Math.max(0, idx))
        }}
      />,
    )
  })

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-3 pb-3 sm:px-5"
      >
        {loading ? (
          <FeedSkeleton />
        ) : messages.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center px-6 py-16 text-center">
            <div className="mb-3 grid size-14 place-items-center rounded-full bg-[#1f6feb]/10 text-[26px]">💬</div>
            <div className="text-[15px] font-semibold text-b24-text-dark">{emptyTitle}</div>
            <div className="mt-1 max-w-[340px] text-[13px] text-b24-text-muted">{emptyText}</div>
          </div>
        ) : (
          <>
            <div className="flex h-10 items-center justify-center">
              {loadingOlder ? (
                <span className="flex items-center gap-2 text-[12px] text-b24-text-muted">
                  <Loader2 size={14} className="animate-spin" /> Загружаю раньше…
                </span>
              ) : hasMore ? (
                <button type="button" onClick={requestOlder} className="text-[12px] text-[#1f6feb] hover:underline">
                  Показать раньше
                </button>
              ) : (
                <span className="text-[11.5px] text-b24-text-muted">Начало переписки</span>
              )}
            </div>
            {rows}
          </>
        )}
      </div>
      {showDown && messages.length > 0 ? (
        <button
          type="button"
          onClick={() => scrollToBottom(true)}
          className="absolute bottom-3 right-4 grid size-11 place-items-center rounded-full bg-white text-b24-text-dark shadow-[0_6px_20px_rgba(15,23,42,0.18)] ring-1 ring-black/8 transition hover:scale-105"
          aria-label="Вниз к новым сообщениям"
          title="Вниз"
        >
          <ChevronDown size={20} />
          {newCount > 0 ? (
            <span className="absolute -top-1.5 -right-1 min-w-[20px] rounded-full bg-[#1f6feb] px-1.5 text-center text-[11px] font-semibold leading-[20px] text-white">
              {newCount > 99 ? '99+' : newCount}
            </span>
          ) : null}
        </button>
      ) : null}
    </div>
  )
}

function FeedSkeleton() {
  const rows = [
    { mine: false, w: 'w-56' },
    { mine: false, w: 'w-72' },
    { mine: true, w: 'w-48' },
    { mine: false, w: 'w-64' },
    { mine: true, w: 'w-80' },
    { mine: true, w: 'w-40' },
  ]
  return (
    <div className="space-y-3 py-6" aria-label="Загрузка">
      {rows.map((r, i) => (
        <div key={i} className={['flex items-end gap-2', r.mine ? 'justify-end' : 'justify-start'].join(' ')}>
          {!r.mine ? <div className="size-8 animate-pulse rounded-full bg-black/8" /> : null}
          <div className={['h-12 max-w-[70%] animate-pulse rounded-[18px]', r.w, r.mine ? 'bg-[#1f6feb]/15' : 'bg-black/8'].join(' ')} />
        </div>
      ))}
    </div>
  )
}

function SupportBubble({
  msg,
  first,
  last,
  showName,
  onReact,
  onRetry,
  onImageLoad,
  onOpenImage,
}: {
  msg: SupportMessage
  first: boolean
  last: boolean
  showName: boolean
  onReact: (msg: SupportMessage, emoji: string) => void
  onRetry: (msg: SupportMessage) => void
  onImageLoad: () => void
  onOpenImage: (fileId: string) => void
}) {
  const mine = msg.mine
  const confirmed = isConfirmed(msg)
  const imgs = (msg.files || []).filter((f) => f.kind === 'image')
  const docs = (msg.files || []).filter((f) => f.kind !== 'image')
  const local = msg.localFiles || []
  const onlyFileName = !!msg.body && (msg.files?.length === 1 || local.length === 1) && msg.body === (msg.files?.[0]?.name || local[0]?.name)
  const showBody = !!msg.body && !onlyFileName && !(msg.files?.length && /^\d+ вложени/.test(msg.body))
  const mediaOnly = !showBody && imgs.length > 0 && docs.length === 0
  const reactions = msg.reactions || []

  const radius = mine
    ? ['rounded-[18px]', !first ? 'rounded-tr-[6px]' : '', !last ? 'rounded-br-[6px]' : ''].join(' ')
    : ['rounded-[18px]', !first ? 'rounded-tl-[6px]' : '', !last ? 'rounded-bl-[6px]' : ''].join(' ')

  return (
    <div className={['group/msg flex gap-2', mine ? 'justify-end' : 'justify-start', first ? 'mt-3' : 'mt-0.5'].join(' ')}>
      {!mine ? (
        <div className="w-8 shrink-0">{first ? <SupportAvatar name={msg.author} seed={msg.author_id} size={32} /> : null}</div>
      ) : null}
      <div className={['flex min-w-0 max-w-[min(78%,620px)] flex-col', mine ? 'items-end' : 'items-start'].join(' ')}>
        {!mine && first && showName ? (
          <div className="mb-1 px-1 text-[12px] font-medium text-b24-text-muted">{msg.author || 'Сотрудник'}</div>
        ) : null}
        <div className={['relative flex items-center gap-1.5', mine ? 'flex-row-reverse' : 'flex-row'].join(' ')}>
          <div
            className={[
              'relative min-w-0 text-[14.5px] leading-[1.4] shadow-[0_1px_1px_rgba(15,23,42,0.06)]',
              mediaOnly ? 'p-1' : 'px-3.5 py-2',
              radius,
              mine ? 'bg-[#1f6feb] text-white' : 'bg-white text-b24-text-dark ring-1 ring-black/[0.06]',
              msg.failed ? 'opacity-70' : '',
            ].join(' ')}
            title={fullTime(msg.created_at)}
          >
            {imgs.length > 0 ? (
              <div
                className={[
                  'grid gap-1',
                  mediaOnly ? '' : '-mx-1.5 mb-1 mt-0.5',
                  imgs.length > 1 ? 'grid-cols-2' : 'grid-cols-1',
                ].join(' ')}
              >
                {imgs.map((f) => (
                  <button
                    key={f.id}
                    type="button"
                    className="overflow-hidden rounded-[12px] focus:outline-none focus-visible:ring-2 focus-visible:ring-white"
                    onClick={() => onOpenImage(f.id)}
                    aria-label={`Открыть ${f.name || 'фото'}`}
                  >
                    <SupportImage
                      id={f.id}
                      alt={f.name || 'Фото'}
                      onLoad={onImageLoad}
                      className={[
                        'block w-full cursor-zoom-in object-cover transition hover:brightness-95',
                        imgs.length > 1 ? 'h-36' : 'max-h-72 min-w-[160px]',
                      ].join(' ')}
                    />
                  </button>
                ))}
              </div>
            ) : null}
            {local.length > 0 && !confirmed ? <LocalFiles files={local} mine={mine} /> : null}
            {docs.map((f) => (
              <button
                key={f.id}
                type="button"
                onClick={() => void apiDownload(`/support/files/${f.id}`, f.name || 'file')}
                className={[
                  '-mx-1 my-1 flex w-[calc(100%+0.5rem)] min-w-[220px] items-center gap-3 rounded-[12px] px-2.5 py-2 text-left transition',
                  mine ? 'bg-white/15 hover:bg-white/25' : 'bg-black/[0.04] hover:bg-black/[0.07]',
                ].join(' ')}
              >
                <span className={['grid size-9 shrink-0 place-items-center rounded-[10px]', mine ? 'bg-white/20' : 'bg-[#1f6feb]/10 text-[#1f6feb]'].join(' ')}>
                  <FileText size={18} />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13.5px] font-medium">{f.name || 'Файл'}</span>
                  <span className={['block text-[11.5px]', mine ? 'text-white/75' : 'text-b24-text-muted'].join(' ')}>
                    {[fileSize(f.size), (f.name.split('.').pop() || '').toUpperCase()].filter(Boolean).join(' · ') || 'Скачать'}
                  </span>
                </span>
                <Download size={16} className="shrink-0 opacity-70" />
              </button>
            ))}
            {showBody ? <div className="whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{linkify(msg.body, mine)}</div> : null}
            <div
              className={[
                'float-right ml-2 mt-1 flex select-none items-center gap-1 text-[11px] leading-none',
                mine ? 'text-white/75' : 'text-b24-text-muted',
                last || !confirmed ? '' : 'hidden',
              ].join(' ')}
            >
              {clockTime(msg.created_at)}
              {mine ? (
                msg.pending ? (
                  <Clock3 size={12} aria-label="Отправляется" />
                ) : msg.failed ? (
                  <AlertCircle size={12} aria-label="Не отправилось" />
                ) : (
                  <Check size={12} aria-label="Доставлено" />
                )
              ) : null}
            </div>
            <div className="clear-both" />
          </div>
          {confirmed ? <ReactionBar msg={msg} onReact={onReact} /> : null}
        </div>
        {reactions.length > 0 ? (
          <div className={['mt-1 flex flex-wrap gap-1', mine ? 'justify-end' : 'justify-start'].join(' ')}>
            {reactions.map((r) => (
              <button
                key={r.emoji}
                type="button"
                onClick={() => onReact(msg, r.mine ? '' : r.emoji)}
                title={(r.names || []).filter(Boolean).join(', ') || undefined}
                className={[
                  'flex h-6 items-center gap-1 rounded-full px-2 text-[12.5px] transition',
                  r.mine
                    ? 'bg-[#1f6feb]/12 text-[#1f6feb] ring-1 ring-[#1f6feb]/45'
                    : 'bg-white text-b24-text-dark ring-1 ring-black/10 hover:ring-black/20',
                ].join(' ')}
              >
                <span>{r.emoji}</span>
                <span className="font-medium tabular-nums">{r.count}</span>
              </button>
            ))}
          </div>
        ) : null}
        {msg.failed ? (
          <div className="mt-1 flex items-center gap-1.5 px-1 text-[12px] text-[#d1242f]">
            <AlertCircle size={13} />
            <span>Не отправилось</span>
            <span aria-hidden>·</span>
            <button type="button" onClick={() => onRetry(msg)} className="inline-flex items-center gap-1 font-semibold hover:underline">
              <RotateCw size={12} /> Повторить
            </button>
          </div>
        ) : msg.pending ? (
          <div className="mt-0.5 px-1 text-[11px] text-b24-text-muted">Отправка…</div>
        ) : null}
      </div>
    </div>
  )
}

function ReactionBar({ msg, onReact }: { msg: SupportMessage; onReact: (msg: SupportMessage, emoji: string) => void }) {
  const mineCode = (msg.reactions || []).find((r) => r.mine)?.emoji || ''
  return (
    <div
      className="pointer-events-none flex shrink-0 items-center gap-0.5 rounded-full bg-white p-0.5 opacity-0 shadow-[0_4px_14px_rgba(15,23,42,0.14)] ring-1 ring-black/8 transition group-hover/msg:pointer-events-auto group-hover/msg:opacity-100 focus-within:pointer-events-auto focus-within:opacity-100"
      role="toolbar"
      aria-label="Реакции"
    >
      <SmilePlus size={14} className="mx-1 text-b24-text-muted" aria-hidden />
      {SUPPORT_REACTIONS.map((code) => (
        <button
          key={code}
          type="button"
          onClick={() => onReact(msg, mineCode === code ? '' : code)}
          className={[
            'grid size-7 place-items-center rounded-full text-[16px] transition hover:scale-125 hover:bg-black/5',
            mineCode === code ? 'bg-[#1f6feb]/12' : '',
          ].join(' ')}
          aria-label={mineCode === code ? `Убрать ${code}` : `Поставить ${code}`}
          aria-pressed={mineCode === code}
        >
          {code}
        </button>
      ))}
    </div>
  )
}

function LocalFiles({ files, mine }: { files: File[]; mine: boolean }) {
  const urls = useMemo(() => files.map((f) => (f.type.startsWith('image/') ? URL.createObjectURL(f) : '')), [files])
  useLayoutEffect(() => () => urls.forEach((u) => u && URL.revokeObjectURL(u)), [urls])
  return (
    <div className="-mx-1.5 mb-1 mt-0.5 grid grid-cols-2 gap-1">
      {files.map((f, i) =>
        urls[i] ? (
          <img key={i} src={urls[i]} alt={f.name} className="h-28 w-full rounded-[12px] object-cover opacity-80" />
        ) : (
          <div
            key={i}
            className={['col-span-2 flex items-center gap-2 rounded-[12px] px-2.5 py-2 text-[13px]', mine ? 'bg-white/15' : 'bg-black/[0.04]'].join(' ')}
          >
            <FileText size={16} /> <span className="truncate">{f.name}</span>
            <span className="ml-auto shrink-0 text-[11px] opacity-70">{fileSize(f.size)}</span>
          </div>
        ),
      )}
    </div>
  )
}


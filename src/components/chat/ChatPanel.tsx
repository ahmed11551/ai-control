import { MessageCircle, Paperclip, Send, Loader2 } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { token } from '../../lib/api'
import { Avatar, AvatarFallback } from '@/components/shadcn/avatar'
import { Button } from '@/components/shadcn/button'
import { Textarea } from '@/components/shadcn/textarea'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/shadcn/tooltip'
import { cn } from '@/lib/utils'

export type ChatBubble = {
  id: string
  dir: 'in' | 'out'
  body: string
  at: string
  author?: string
  has_media?: boolean
  media_kind?: string
  media_download?: boolean
  files_open?: boolean
  media_filename?: string
  media_count?: number
  media_url?: string
  sent_to_logist?: boolean
  /** pending | sent | delivered | read | failed */
  delivery_status?: string
}

function DeliveryTicks({ status }: { status?: string }) {
  const s = String(status || '').toLowerCase()
  if (!s) return null
  if (s === 'pending') {
    return (
      <span className="ml-1 inline-flex items-center opacity-70" title="Отправляется">
        <svg width="12" height="11" viewBox="0 0 16 15" aria-hidden className="text-[#8a9aa8]">
          <path
            fill="currentColor"
            d="M8 0a7.5 7.5 0 1 0 0 15A7.5 7.5 0 0 0 8 0Zm0 1.5a6 6 0 1 1 0 12 6 6 0 0 1 0-12Zm-.2 2.4v4.1l3.1 1.85.75-1.25-2.45-1.45V3.9h-1.4Z"
          />
        </svg>
      </span>
    )
  }
  if (s === 'failed') {
    return (
      <span className="ml-1 inline-flex items-center text-rose-600" title="Не доставлено">
        !
      </span>
    )
  }
  const read = s === 'read'
  const double = s === 'delivered' || read
  const color = read ? '#53bdeb' : '#8a9aa8'
  const title =
    s === 'sent' ? 'Отправлено' : s === 'delivered' ? 'Доставлено' : s === 'read' ? 'Прочитано' : ''
  return (
    <span className="ml-0.5 inline-flex items-center" title={title} style={{ color }}>
      <svg width={double ? 16 : 11} height="10" viewBox={double ? '0 0 16 11' : '0 0 11 10'} aria-hidden>
        {double ? (
          <>
            <path
              fill="currentColor"
              d="M11.07 0.34 4.55 6.86 1.93 4.24.5 5.67l4.05 4.05 7.95-7.95z"
            />
            <path
              fill="currentColor"
              d="M15.07 0.34 8.55 6.86 7.3 5.61 5.87 7.04l2.68 2.68 7.95-7.95z"
            />
          </>
        ) : (
          <path fill="currentColor" d="M10.57.34 3.55 7.36.93 4.74-.5 6.17l4.05 4.05L12 1.77z" />
        )}
      </svg>
    </span>
  )
}

type Props = {
  title: string
  subtitle?: string
  initials?: string
  messages: ChatBubble[]
  draft: string
  onDraftChange: (value: string) => void
  onSend: () => void
  pending?: boolean
  error?: string
  emptyText?: string
  headerRight?: ReactNode
  headerBelow?: ReactNode
  composerTop?: ReactNode
  composerDisabled?: boolean
  placeholder?: string
  className?: string
  compact?: boolean
  renderMessage?: (msg: ChatBubble) => ReactNode
  channelBadge?: ReactNode
  /** Разрешить скрепку (вложения в Wappi/MAX). */
  allowAttach?: boolean
  attachFiles?: File[]
  onAttachFiles?: (files: File[]) => void
  /** Подгрузка истории при скролле вверх */
  hasMoreOlder?: boolean
  loadingOlder?: boolean
  onLoadOlder?: () => void
}

function initialsOf(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

export function ChatMessageBubble({
  dir,
  body,
  at,
  author,
  deliveryStatus,
}: {
  dir: 'in' | 'out'
  body: string
  at: string
  author?: string
  deliveryStatus?: string
}) {
  const outgoing = dir === 'out'
  return (
    <div
      className={cn(
        'flex max-w-[78%] flex-col',
        outgoing ? 'self-end items-end' : 'self-start items-start',
      )}
    >
      <div
        className={cn(
          'px-3.5 py-2 text-[14px] leading-[1.45] break-words whitespace-pre-wrap',
          outgoing
            ? 'rounded-[16px] rounded-br-[5px] bg-[#dcf8c6] text-[#1a1a1a]'
            : 'rounded-[16px] rounded-bl-[5px] bg-white text-b24-text-dark shadow-sm ring-1 ring-black/5',
        )}
      >
        {body}
      </div>
      <div
        className={cn(
          'mt-1 flex items-center gap-0.5 px-1 text-[10px] tabular-nums text-b24-text-muted',
          outgoing && 'justify-end',
        )}
      >
        <span>
          {author ? `${author} · ` : ''}
          {at}
        </span>
        {outgoing ? <DeliveryTicks status={deliveryStatus} /> : null}
      </div>
    </div>
  )
}

function isChatAudio(kind?: string) {
  const k = String(kind || '').toLowerCase()
  return k === 'audio' || k === 'voice' || k === 'ptt'
}

function InlineChatAudio({ url }: { url: string }) {
  const [src, setSrc] = useState('')
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let revoke = ''
    let cancelled = false
    setSrc('')
    setFailed(false)
    const headers = new Headers()
    const t = token()
    if (t) headers.set('Authorization', `Bearer ${t}`)
    void fetch(url, { headers })
      .then((r) => (r.ok ? r.blob() : Promise.reject(new Error('audio'))))
      .then((blob) => {
        if (cancelled) return
        const typed = blob.type.startsWith('audio/') ? blob : new Blob([blob], { type: 'audio/ogg' })
        revoke = URL.createObjectURL(typed)
        setSrc(revoke)
      })
      .catch(() => {
        if (!cancelled) setFailed(true)
      })
    return () => {
      cancelled = true
      if (revoke) URL.revokeObjectURL(revoke)
    }
  }, [url])
  if (failed) {
    return <p className="text-[12px] text-b24-text-muted">Не удалось воспроизвести</p>
  }
  if (!src) {
    return <p className="text-[12px] text-b24-text-muted">Загрузка аудио…</p>
  }
  return (
    <audio controls preload="metadata" className="h-8 w-full max-w-xs" src={src}>
      Аудио
    </audio>
  )
}

function InlineChatImage({ url, alt, onFail }: { url: string; alt: string; onFail: () => void }) {
  const [src, setSrc] = useState('')
  const onFailRef = useRef(onFail)
  onFailRef.current = onFail
  useEffect(() => {
    let revoke = ''
    let cancelled = false
    const headers = new Headers()
    const t = token()
    if (t) headers.set('Authorization', `Bearer ${t}`)
    void fetch(url, { headers })
      .then((r) => (r.ok ? r.blob() : Promise.reject(new Error('image'))))
      .then((blob) => {
        if (cancelled) return
        revoke = URL.createObjectURL(blob)
        setSrc(revoke)
      })
      .catch(() => {
        if (!cancelled) onFailRef.current()
      })
    return () => {
      cancelled = true
      if (revoke) URL.revokeObjectURL(revoke)
    }
  }, [url])
  if (!src) {
    return <div className="h-36 w-52 animate-pulse rounded-lg bg-black/5" />
  }
  return (
    <button type="button" className="block" onClick={() => window.open(src, '_blank', 'noopener')}>
      <img
        src={src}
        alt={alt}
        className="max-h-64 max-w-full rounded-lg object-contain"
        onError={onFail}
      />
    </button>
  )
}

export function ChatMediaBubble({
  msg,
  managerMode,
  onSendToLogist,
  onDownload,
  sending,
}: {
  msg: ChatBubble
  managerMode: boolean
  onSendToLogist?: (id: string) => void
  onDownload?: (id: string) => void
  sending?: boolean
}) {
  const outgoing = msg.dir === 'out'
  const [photoFailed, setPhotoFailed] = useState(false)
  useEffect(() => {
    setPhotoFailed(false)
  }, [msg.id, msg.media_url])
  const onPhotoFail = () => setPhotoFailed(true)
  const singleAudio =
    isChatAudio(msg.media_kind) &&
    Boolean(msg.media_url) &&
    Number(msg.media_count || 0) <= 1
  const filesOpen = Boolean(msg.files_open) || Boolean(msg.media_download)
  const hideIncoming = managerMode && !outgoing && !filesOpen
  const showPhoto =
    !singleAudio &&
    !photoFailed &&
    msg.media_kind === 'image' &&
    Boolean(msg.media_url) &&
    Boolean(msg.media_download) &&
    Number(msg.media_count || 0) <= 1 &&
    !hideIncoming
  const label =
    msg.media_kind === 'passport_text'
      ? 'Паспортные данные'
      : msg.media_kind === 'image'
        ? 'Изображение'
        : msg.media_kind === 'video'
          ? 'Видео'
          : msg.media_kind === 'audio'
            ? 'Аудио'
            : 'Файл'
  return (
    <div
      className={cn(
        'flex max-w-[85%] flex-col',
        outgoing ? 'self-end items-end' : 'self-start items-start',
      )}
    >
      <div
        className={cn(
          showPhoto
            ? 'overflow-hidden p-1'
            : 'min-w-[200px] px-3.5 py-3',
          'rounded-[16px] text-[14px] shadow-sm ring-1 ring-black/5',
          outgoing
            ? 'rounded-br-[5px] bg-[#dcf8c6] text-[#1a1a1a]'
            : 'rounded-bl-[5px] bg-white text-b24-text-dark',
        )}
      >
        {singleAudio ? (
          <div className="min-w-[220px]">
            <div className="mb-1.5 text-[13px] font-medium">Аудио</div>
            <InlineChatAudio url={msg.media_url || ''} />
            {msg.media_download && !hideIncoming ? (
              <button
                type="button"
                onClick={() => onDownload?.(msg.id)}
                className="mt-2 h-8 rounded-full border border-black/10 bg-white px-3 text-[12px] font-medium text-b24-text-dark hover:bg-black/[0.03]"
              >
                Скачать
              </button>
            ) : null}
          </div>
        ) : showPhoto ? (
          <InlineChatImage url={msg.media_url || ''} alt={msg.media_filename || 'Фото'} onFail={onPhotoFail} />
        ) : (
        <div className="flex items-center gap-2 font-medium">
          <Paperclip className="size-4 shrink-0 opacity-70" />
          <span>
            {label} {outgoing ? 'отправлен' : 'получен'}
          </span>
        </div>
        )}
		{!showPhoto && !singleAudio ? <p className="mt-1 text-[12px] text-b24-text-muted">
          {hideIncoming
            ? msg.sent_to_logist
              ? 'Передано логисту · у вас в «Билет», у логиста в «Заявка»'
              : msg.media_kind === 'passport_text'
                ? 'Содержимое скрыто. Можно передать логисту.'
                : 'Содержимое скрыто. Можно передать логисту сразу — файл догрузится сам.'
            : msg.media_kind === 'passport_text'
              ? msg.body || 'Паспортные данные'
              : Number(msg.media_count || 0) > 1
                ? `${msg.media_count} вложения`
                : msg.media_filename || msg.body || 'Вложение'}
        </p> : null}
        {!showPhoto && !singleAudio ? <div className="mt-2.5 flex flex-wrap gap-2">
          {hideIncoming ? (
            msg.sent_to_logist ? (
              <span className="inline-flex h-8 items-center rounded-full bg-emerald-50 px-3 text-[12px] font-medium text-emerald-700 ring-1 ring-emerald-200/80">
                Отправлено логисту
              </span>
            ) : (
              <button
                type="button"
                disabled={sending}
                onClick={() => onSendToLogist?.(msg.id)}
                className="h-8 rounded-full bg-[#2b7de9] px-3 text-[12px] font-medium text-white hover:bg-[#246fce] disabled:opacity-50"
              >
                {sending ? 'Отправка…' : 'Отправить логисту'}
              </button>
            )
          ) : msg.media_kind === 'passport_text' ? null : (
            <button
              type="button"
              onClick={() => onDownload?.(msg.id)}
              className="h-8 rounded-full border border-black/10 bg-white px-3 text-[12px] font-medium text-b24-text-dark hover:bg-black/[0.03]"
            >
              {Number(msg.media_count || 0) > 1
                ? `Скачать все (${msg.media_count})`
                : msg.media_download
                  ? 'Скачать'
                  : 'Догрузить файл'}
            </button>
          )}
        </div> : null}
      </div>
      <div
        className={cn(
          'mt-1 flex items-center gap-0.5 px-1 text-[10px] tabular-nums text-b24-text-muted',
          outgoing && 'justify-end',
        )}
      >
        <span>
          {msg.author ? `${msg.author} · ` : ''}
          {msg.at}
        </span>
        {outgoing ? <DeliveryTicks status={msg.delivery_status} /> : null}
      </div>
    </div>
  )
}

export function ChatPanel({
  title,
  subtitle,
  initials,
  messages,
  draft,
  onDraftChange,
  onSend,
  pending,
  error,
  emptyText = 'Нет сообщений',
  headerRight,
  headerBelow,
  composerTop,
  composerDisabled,
  placeholder = 'Написать сообщение...',
  className,
  compact,
  renderMessage,
  channelBadge,
  allowAttach,
  attachFiles = [],
  onAttachFiles,
  hasMoreOlder,
  loadingOlder,
  onLoadOlder,
}: Props) {
  const endRef = useRef<HTMLDivElement>(null)
  const scrollerRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const nearBottomRef = useRef(true)
  const prevFirstIdRef = useRef<string>('')
  const prevScrollHeightRef = useRef(0)
  const skipStickRef = useRef(false)
  const canSend = Boolean(draft.trim() || attachFiles.length > 0)

  useLayoutEffect(() => {
    const el = scrollerRef.current
    if (!el) return
    const firstId = messages[0]?.id ?? ''
    const prevFirst = prevFirstIdRef.current
    const prepended =
      Boolean(firstId && prevFirst && firstId !== prevFirst) &&
      messages.some((m) => m.id === prevFirst)

    if (prepended) {
      const delta = el.scrollHeight - prevScrollHeightRef.current
      if (delta > 0) el.scrollTop += delta
      skipStickRef.current = true
    } else if (!skipStickRef.current && nearBottomRef.current) {
      el.scrollTop = el.scrollHeight
    }
    skipStickRef.current = false
    prevFirstIdRef.current = firstId
    prevScrollHeightRef.current = el.scrollHeight
  }, [messages, pending, loadingOlder])

  useEffect(() => {
    // смена диалога — снова прилипаем вниз
    nearBottomRef.current = true
    prevFirstIdRef.current = ''
    prevScrollHeightRef.current = 0
  }, [title])

  const onScroll = () => {
    const el = scrollerRef.current
    if (!el) return
    const distBottom = el.scrollHeight - el.scrollTop - el.clientHeight
    nearBottomRef.current = distBottom < 80
    if (el.scrollTop < 72 && hasMoreOlder && !loadingOlder && onLoadOlder) {
      prevScrollHeightRef.current = el.scrollHeight
      onLoadOlder()
    }
  }

  const submit = (e?: FormEvent) => {
    e?.preventDefault()
    if (!canSend || pending || composerDisabled) return
    nearBottomRef.current = true
    onSend()
  }

  return (
    <TooltipProvider delayDuration={400}>
      <div
        className={cn(
          'flex min-h-0 flex-col overflow-hidden rounded-[14px] bg-white',
          className,
        )}
      >
        <div className="flex shrink-0 items-center gap-3 border-b border-black/6 bg-white px-4 py-3">
          <Avatar className={cn('size-10', compact && 'size-9')}>
            <AvatarFallback className="bg-[#e8eef3] text-[13px] font-semibold text-b24-text-dark">
              {(initials || initialsOf(title)).slice(0, 2)}
            </AvatarFallback>
          </Avatar>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <div className="truncate text-[15px] font-semibold tracking-tight text-b24-text-dark">
                {title}
              </div>
              {channelBadge}
            </div>
            {subtitle ? (
              <div className="mt-0.5 truncate text-[12px] text-b24-text-muted">{subtitle}</div>
            ) : null}
          </div>
          {headerRight}
        </div>

        {headerBelow}

        <div
          ref={scrollerRef}
          onScroll={onScroll}
          className="b24-scrollbar min-h-0 flex-1 overflow-y-auto bg-[#f5f6f8]"
        >
          <div
            className={cn(
              'flex min-h-full flex-col px-4 py-4',
              compact ? 'min-h-[240px]' : '',
            )}
          >
            {hasMoreOlder || loadingOlder ? (
              <div className="mb-2 flex items-center justify-center py-1 text-[11px] text-b24-text-muted">
                {loadingOlder ? (
                  <span className="inline-flex items-center gap-1.5">
                    <Loader2 className="size-3.5 animate-spin" />
                    Загрузка истории…
                  </span>
                ) : (
                  <span>Прокрутите вверх для истории</span>
                )}
              </div>
            ) : null}
            {messages.length === 0 ? (
              <div className="m-auto flex max-w-[15rem] flex-col items-center gap-3 py-12 text-center">
                <div className="flex size-14 items-center justify-center rounded-full bg-white text-b24-cyan shadow-sm ring-1 ring-black/5">
                  <MessageCircle className="size-6 opacity-80" strokeWidth={1.75} />
                </div>
                <p className="text-[13px] leading-snug text-b24-text-muted">{emptyText}</p>
              </div>
            ) : (
              <div className="mt-auto flex flex-col gap-2.5">
                {messages.map((m) =>
                  renderMessage ? (
                    <div key={m.id} className="flex flex-col">
                      {renderMessage(m)}
                    </div>
                  ) : m.has_media ? (
                    <ChatMediaBubble key={m.id} msg={m} managerMode={false} />
                  ) : (
                    <ChatMessageBubble
                      key={m.id}
                      dir={m.dir}
                      body={m.body}
                      at={m.at}
                      author={m.author}
                      deliveryStatus={m.delivery_status}
                    />
                  ),
                )}
                <div ref={endRef} />
              </div>
            )}
          </div>
        </div>

        {error ? (
          <div className="shrink-0 border-t border-rose-200/60 bg-rose-50/90 px-4 py-2 text-[12px] text-destructive">
            {error}
          </div>
        ) : null}

        <form onSubmit={submit} className="relative z-20 shrink-0 border-t border-black/6 bg-white px-3 py-2.5">
          {composerTop ? <div className="relative z-20 mb-2">{composerTop}</div> : null}
          {attachFiles.length > 0 ? (
            <div className="mb-2 space-y-1.5">
              {attachFiles.map((f, idx) => (
                <div
                  key={`${f.name}-${f.size}-${idx}`}
                  className="flex items-center gap-2 rounded-[12px] bg-[#eef4fb] px-3 py-2 text-[12px] text-b24-text-dark"
                >
                  <Paperclip className="size-3.5 shrink-0 text-[#2b7de9]" />
                  <span className="min-w-0 flex-1 truncate font-medium">{f.name}</span>
                  <span className="shrink-0 tabular-nums text-b24-text-muted">
                    {f.size > 1024 * 1024
                      ? `${(f.size / (1024 * 1024)).toFixed(1)} МБ`
                      : `${Math.max(1, Math.round(f.size / 1024))} КБ`}
                  </span>
                  <button
                    type="button"
                    className="shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium text-rose-700 hover:bg-rose-50"
                    onClick={() => onAttachFiles?.(attachFiles.filter((_, i) => i !== idx))}
                    disabled={pending}
                  >
                    Убрать
                  </button>
                </div>
              ))}
            </div>
          ) : null}
          <div className="flex items-end gap-2 rounded-[24px] border border-black/8 bg-[#f5f6f8] px-1.5 py-1.5 focus-within:border-[#2b7de9]/45 focus-within:bg-white">
            {allowAttach ? (
              <>
                <input
                  ref={fileInputRef}
                  type="file"
                  multiple
                  className="hidden"
                  onChange={(e) => {
                    const picked = Array.from(e.target.files || [])
                    if (picked.length) {
                      onAttachFiles?.([...attachFiles, ...picked].slice(0, 10))
                    }
                    e.target.value = ''
                  }}
                />
                <button
                  type="button"
                  className="mb-0.5 flex size-9 shrink-0 items-center justify-center rounded-full text-b24-text-muted hover:bg-black/[0.04] disabled:opacity-40"
                  aria-label="Вложение"
                  disabled={pending || composerDisabled || attachFiles.length >= 10}
                  title="Прикрепить файлы"
                  onClick={() => fileInputRef.current?.click()}
                >
                  <Paperclip className="size-4" />
                </button>
              </>
            ) : (
              <button
                type="button"
                className="mb-0.5 flex size-9 shrink-0 items-center justify-center rounded-full text-b24-text-muted opacity-40"
                aria-label="Вложение"
                disabled
                title="Скоро"
              >
                <Paperclip className="size-4" />
              </button>
            )}
            <Textarea
              value={draft}
              onChange={(e) => onDraftChange(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  submit()
                }
              }}
              rows={1}
              placeholder={
                attachFiles.length > 0
                  ? attachFiles.length === 1
                    ? 'Подпись к файлу (необязательно)'
                    : 'Подпись к файлам (необязательно)'
                  : placeholder
              }
              disabled={composerDisabled || pending}
              className="max-h-[120px] min-h-[36px] flex-1 resize-none rounded-none border-0 bg-transparent px-1 py-2 text-[#333333] caret-[#333333] shadow-none placeholder:text-[#a8adb4] focus-visible:ring-0"
            />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="submit"
                  size="icon"
                  disabled={pending || composerDisabled || !canSend}
                  aria-label="Отправить"
                  className="mb-0.5 size-9 shrink-0 rounded-full bg-[#2b7de9] text-white shadow-sm hover:bg-[#246fce] disabled:opacity-40"
                >
                  <Send className="size-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="top">Отправить</TooltipContent>
            </Tooltip>
          </div>
        </form>
      </div>
    </TooltipProvider>
  )
}

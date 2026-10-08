import {
  ArrowRightLeft,
  Ban,
  Bell,
  CalendarDays,
  ChevronDown,
  GitBranch,
  Mail,
  MapPin,
  MessageCircle,
  MessageSquare,
  Paperclip,
  Phone,
  PhoneIncoming,
  PhoneMissed,
  PhoneOff,
  PhoneOutgoing,
  Plus,
  Star,
  Undo2,
  UserCheck,
  UserPlus,
  X,
  Zap,
} from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { ChatMediaBubble, ChatMessageBubble, ChatPanel } from '../chat/ChatPanel'
import { maxColdBanner, maxDownBanner } from './maxLineStatus'
import { Button as ShadButton } from '@/components/shadcn/button'
import { api, apiDownload, token } from '../../lib/api'
import { phoneCallHint } from '../../lib/phoneRegion'
import { canSeePhones, isManagerRole, maskPhonesInText } from '../../lib/roles'
import { useAuth } from '../../store/auth'
import { Button } from '../ui/Button'

export type TimelineEventKind =
  | 'default'
  | 'created'
  | 'converted'
  | 'comment'
  | 'distributed'
  | 'stage'
  | 'nedozvon'
  | 'call'
  | 'missed'
  | 'originate'
  | 'skorozvon'
  | 'sk_accept'
  | 'sk_fail'
  | 'sk_talk'
  | 'sk_take'
  | 'sk_return'
  | 'sk_bad'
  | 'task_planned'
  | 'task_done'

const TIMELINE_KIND_STYLE: Record<
  TimelineEventKind,
  { Icon: typeof Plus; circle: string }
> = {
  default: { Icon: Plus, circle: 'bg-[#e8f0fe] text-[#3b7ddd]' },
  created: { Icon: Plus, circle: 'bg-[#e8f0fe] text-[#3b7ddd]' },
  converted: { Icon: ArrowRightLeft, circle: 'bg-[#e8f0fe] text-[#3b7ddd]' },
  comment: { Icon: MessageSquare, circle: 'bg-[#fff4cc] text-[#b8860b]' },
  distributed: { Icon: UserPlus, circle: 'bg-[#e8f0fe] text-[#3b7ddd]' },
  stage: { Icon: GitBranch, circle: 'bg-[#f0e8fe] text-[#7c5cbf]' },
  nedozvon: { Icon: PhoneMissed, circle: 'bg-[#fff4e5] text-[#d97706]' },
  call: { Icon: Phone, circle: 'bg-[#e6f7ed] text-[#2f9e62]' },
  missed: { Icon: PhoneMissed, circle: 'bg-[#fde8e8] text-[#d64545]' },
  originate: { Icon: PhoneOutgoing, circle: 'bg-[#e6f7ed] text-[#2f9e62]' },
  skorozvon: { Icon: Zap, circle: 'bg-[#fff8e1] text-[#c9920a]' },
  sk_accept: { Icon: PhoneIncoming, circle: 'bg-[#e6f7ed] text-[#2f9e62]' },
  sk_fail: { Icon: PhoneOff, circle: 'bg-[#fde8e8] text-[#d64545]' },
  sk_talk: { Icon: Phone, circle: 'bg-[#e6f7ed] text-[#2f9e62]' },
  sk_take: { Icon: UserCheck, circle: 'bg-[#e6f7ed] text-[#2f9e62]' },
  sk_return: { Icon: Undo2, circle: 'bg-[#e8f0fe] text-[#3b7ddd]' },
  sk_bad: { Icon: Ban, circle: 'bg-[#fde8e8] text-[#d64545]' },
  task_planned: { Icon: CalendarDays, circle: 'bg-[#e8f8fe] text-[#0b82b5]' },
  task_done: { Icon: Star, circle: 'bg-[#e6f7ed] text-[#2f9e62]' },
}

/** Пикер аккаунта MAX: портал выше карточки лида, клики не уходят в backdrop. */
function MaxAccountPicker({
  accounts,
  value,
  onChange,
}: {
  accounts: { id: string; label: string; hasInbound?: boolean; sendDown?: boolean; coldLimit?: boolean }[]
  value: string
  onChange: (id: string) => void
}) {
  const [open, setOpen] = useState(false)
  const btnRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ top: 0, left: 0, width: 0, openUp: false })
  const selected = accounts.find((a) => a.id === value)

  const place = () => {
    const r = btnRef.current?.getBoundingClientRect()
    if (!r) return
    const openUp = window.innerHeight - r.bottom < 200
    setPos({
      top: openUp ? r.top - 4 : r.bottom + 4,
      left: r.left,
      width: Math.max(r.width, 200),
      openUp,
    })
  }

  useEffect(() => {
    if (!open) return
    place()
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (btnRef.current?.contains(t) || menuRef.current?.contains(t)) return
      setOpen(false)
    }
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    document.addEventListener('mousedown', onDown, true)
    return () => {
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
      document.removeEventListener('mousedown', onDown, true)
    }
  }, [open])

  return (
    <div className="flex items-center gap-2 text-[11px] text-b24-text-muted">
      <span className="shrink-0">С аккаунта</span>
      <button
        ref={btnRef}
        type="button"
        onMouseDown={(e) => e.stopPropagation()}
        onClick={(e) => {
          e.preventDefault()
          e.stopPropagation()
          if (!open) place()
          setOpen((v) => !v)
        }}
        className={[
          'relative z-[1] flex h-8 min-w-0 flex-1 items-center gap-1.5 rounded-lg border px-2 text-left text-[12px] outline-none',
          value
            ? 'border-[#2b7de9]/45 bg-[#e8f1ff] text-[#1a5fb4] font-medium'
            : 'border-black/8 bg-[#f5f6f8] text-[#12182a] hover:border-[#2b7de9]/60',
        ].join(' ')}
      >
        {selected?.hasInbound ? (
          <span className="size-2 shrink-0 rounded-full bg-[#2fc06e] shadow-[0_0_0_2px_rgba(47,192,110,0.25)]" />
        ) : (
          <span className="size-2 shrink-0 rounded-full bg-black/15" />
        )}
        <span
          className={[
            'min-w-0 flex-1 truncate',
            selected?.sendDown || selected?.coldLimit ? 'underline decoration-[#b45353] underline-offset-2' : '',
          ].join(' ')}
        >
          {selected?.label || 'Выберите'}
          {selected?.sendDown ? ' · не работает' : selected?.coldLimit ? ' · лимит на новых' : ''}
        </span>
        <ChevronDown
          size={14}
          className={`shrink-0 opacity-60 transition-transform ${open ? 'rotate-180' : ''}`}
        />
      </button>
      {open
        ? createPortal(
            <div
              ref={menuRef}
              role="listbox"
              onMouseDown={(e) => {
                e.preventDefault()
                e.stopPropagation()
              }}
              style={{
                position: 'fixed',
                top: pos.openUp ? undefined : pos.top,
                bottom: pos.openUp ? Math.max(0, window.innerHeight - pos.top) : undefined,
                left: pos.left,
                width: pos.width,
                zIndex: 10000,
              }}
              className="max-h-52 overflow-y-auto rounded-lg border border-black/10 bg-white p-1 shadow-[0_12px_32px_rgba(15,23,42,0.25)]"
            >
              {accounts.map((a) => {
                const active = a.id === value
                return (
                  <button
                    key={a.id}
                    type="button"
                    role="option"
                    aria-selected={active}
                    onClick={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      onChange(a.id)
                      setOpen(false)
                    }}
                    className={`flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-[12px] tabular-nums transition ${
                      active
                        ? 'bg-[#e8f1ff] font-semibold text-[#2b7de9]'
                        : 'text-[#12182a] hover:bg-[#f5f6f8]'
                    }`}
                  >
                    <span
                      className={[
                        'size-2 shrink-0 rounded-full',
                        a.hasInbound
                          ? 'bg-[#2fc06e] shadow-[0_0_0_2px_rgba(47,192,110,0.25)]'
                          : 'bg-black/15',
                      ].join(' ')}
                      title={a.hasInbound ? 'Есть переписка с кандидатом' : 'Нет переписки'}
                    />
                    <span
                      className={[
                        'min-w-0 flex-1 truncate',
                        a.sendDown || a.coldLimit ? 'underline decoration-[#b45353] underline-offset-2 text-[#7a3030]' : '',
                      ].join(' ')}
                    >
                      {a.label}
                      {a.sendDown ? ' · не работает' : a.coldLimit ? ' · лимит на новых' : ''}
                    </span>
                    {active ? (
                      <span className="shrink-0 text-[10px] font-semibold uppercase tracking-wide text-[#2b7de9]">
                        текущий
                      </span>
                    ) : null}
                  </button>
                )
              })}
            </div>,
            document.body,
          )
        : null}
    </div>
  )
}

type LeadCardPanelProps = {
  title: string
  subtitle?: string
  badge?: string
  clientName: string
  clientPhone: string
  onClose: () => void
  onCall?: () => void
  onWriteMax?: () => void
  callDisabled?: boolean
  /** Bitrix-style stage chevrons under the header */
  stageBar?: ReactNode
  left: ReactNode
  timeline: ReactNode
  chat: ReactNode
  footer?: ReactNode
  /** Inline rename of lead title (not contact name). */
  onTitleSave?: (title: string) => Promise<void>
}

function formatPhone(raw: string) {
  const d = raw.replace(/\D/g, '')
  if (d.length === 11 && (d[0] === '7' || d[0] === '8')) {
    const n = '7' + d.slice(1)
    return `+${n[0]} ${n.slice(1, 4)} ${n.slice(4, 7)}-${n.slice(7, 9)}-${n.slice(9, 11)}`
  }
  if (d.length === 10) {
    return `+7 ${d.slice(0, 3)} ${d.slice(3, 6)}-${d.slice(6, 8)}-${d.slice(8, 10)}`
  }
  return raw || '—'
}

/** Баннер местного времени по номеру. Только при полном незамаскированном телефоне. */
export function PhoneCallTimeBanner({ phone }: { phone?: string }) {
  const [hint, setHint] = useState(() => (phone ? phoneCallHint(phone) : null))
  useEffect(() => {
    const tick = () => setHint(phone ? phoneCallHint(phone) : null)
    tick()
    const id = window.setInterval(tick, 30_000)
    return () => window.clearInterval(id)
  }, [phone])
  if (!hint) return null

  const status =
    hint.advice === 'ok'
      ? {
          label: 'Можно звонить',
          dot: 'bg-b24-green',
          pill: 'bg-b24-green/12 text-b24-green',
        }
      : hint.advice === 'caution'
        ? {
            label: 'Лучше подождать',
            dot: 'bg-amber-500',
            pill: 'bg-amber-500/12 text-amber-700',
          }
        : {
            label: 'Не звонить',
            dot: 'bg-rose-500',
            pill: 'bg-rose-500/12 text-rose-700',
          }

  return (
    <div className="mt-3 rounded-[10px] border border-black/[0.07] bg-[#f7f8fa] overflow-hidden">
      <div className="flex items-stretch">
        <div className="w-[76px] shrink-0 flex flex-col items-center justify-center gap-0.5 border-r border-black/[0.06] bg-white px-2 py-3">
          <div className="text-[22px] font-semibold tabular-nums tracking-tight text-b24-text-dark leading-none">
            {hint.localTime}
          </div>
          <div className="text-[10px] font-medium uppercase tracking-wide text-b24-text-muted">
            {hint.mskLabel}
          </div>
        </div>
        <div className="min-w-0 flex-1 px-3.5 py-3 flex flex-col justify-center gap-1.5">
          <div className="flex items-center gap-1.5 min-w-0">
            <MapPin size={13} strokeWidth={2} className="shrink-0 text-b24-text-muted" />
            <div className="text-[13px] font-medium text-b24-text-dark truncate leading-snug">
              {hint.region}
            </div>
          </div>
          <div
            className={`inline-flex w-fit items-center gap-1.5 rounded-b24-pill px-2 py-0.5 text-[11px] font-semibold ${status.pill}`}
            title={hint.adviceText}
          >
            <span className={`size-1.5 rounded-full ${status.dot}`} />
            {status.label}
          </div>
        </div>
      </div>
    </div>
  )
}

export function LeadCardPanel({
  title,
  subtitle = 'Лид',
  badge,
  clientName,
  clientPhone,
  onClose,
  onCall,
  onWriteMax,
  callDisabled,
  stageBar,
  left,
  timeline,
  chat,
  footer,
  onTitleSave,
}: LeadCardPanelProps) {
  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState(title)
  const [titlePending, setTitlePending] = useState(false)
  const [titleError, setTitleError] = useState('')

  useEffect(() => {
    setTitleDraft(title)
    setEditingTitle(false)
    setTitleError('')
  }, [title])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (editingTitle) return
        onClose()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, editingTitle])

  const saveTitle = async () => {
    const next = titleDraft.trim()
    if (!onTitleSave || !next || next === title) {
      setEditingTitle(false)
      setTitleDraft(title)
      return
    }
    setTitlePending(true)
    setTitleError('')
    try {
      await onTitleSave(next)
      setEditingTitle(false)
    } catch (e) {
      setTitleError(e instanceof Error ? e.message : 'Не удалось сохранить')
    } finally {
      setTitlePending(false)
    }
  }

  return (
    <div className="fixed inset-0 z-[130] flex justify-end">
      <button
        type="button"
        className="absolute inset-0 z-0 bg-[#525c69]/55"
        aria-label="Закрыть"
        onClick={onClose}
      />
      <div className="relative z-[1] h-full w-full max-w-[min(1280px,calc(100vw-40px))] bg-[#eef2f4] shadow-2xl flex flex-col">
        <header className="shrink-0 flex items-center justify-between gap-3 px-5 py-3 bg-white border-b border-black/8">
          <div className="min-w-0 flex items-center gap-3 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-[11px] uppercase tracking-wide text-b24-text-muted">{subtitle}</div>
              {editingTitle && onTitleSave ? (
                <div className="mt-0.5 flex items-center gap-2 min-w-0">
                  <input
                    autoFocus
                    value={titleDraft}
                    disabled={titlePending}
                    aria-label="Изменить название лида"
                    onChange={(e) => setTitleDraft(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        void saveTitle()
                      }
                      if (e.key === 'Escape') {
                        e.preventDefault()
                        e.stopPropagation()
                        setEditingTitle(false)
                        setTitleDraft(title)
                        setTitleError('')
                      }
                    }}
                    className="h-9 w-full max-w-[420px] rounded-[10px] border border-b24-cyan/40 bg-[#f5f7f9] px-3 text-[16px] font-semibold text-b24-text-dark outline-none focus:bg-white"
                  />
                  <button
                    type="button"
                    disabled={titlePending || !titleDraft.trim()}
                    onClick={() => void saveTitle()}
                    className="h-8 shrink-0 rounded-b24-sm px-3 text-[12px] font-semibold text-white bg-[#2fc6f6] hover:bg-[#1eb5e5] disabled:opacity-50"
                  >
                    {titlePending ? '…' : 'OK'}
                  </button>
                </div>
              ) : (
                <h2 className="text-[18px] font-semibold text-b24-text-dark truncate leading-snug">
                  {title}
                </h2>
              )}
              {titleError ? <p className="mt-1 text-[12px] text-red-500">{titleError}</p> : null}
            </div>
            {badge ? (
              <span className="shrink-0 rounded-b24-pill px-2.5 py-1 text-[12px] font-medium bg-sky-50 text-sky-700">
                {badge}
              </span>
            ) : null}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="size-9 shrink-0 inline-flex items-center justify-center rounded-full text-b24-text-muted hover:bg-black/5"
            aria-label="Закрыть"
          >
            <X size={20} strokeWidth={1.75} />
          </button>
        </header>

        {stageBar}

        <div className="flex-1 min-h-0 grid grid-cols-1 xl:grid-cols-[340px_minmax(280px,1fr)_340px] lg:grid-cols-[320px_minmax(0,1fr)] gap-0 overflow-hidden">
          <aside className="bg-white border-r border-black/8 overflow-y-auto b24-scrollbar min-h-0">
            <div className="px-5 pt-5 pb-4 border-b border-black/6">
              <div className="text-[11px] font-semibold uppercase tracking-wide text-b24-text-muted mb-3">
                Кандидат
              </div>
              <div className="flex items-start gap-3">
                <div className="size-12 rounded-full bg-[#2fc6f6]/15 text-[#1ea8d4] inline-flex items-center justify-center text-[18px] font-semibold shrink-0">
                  {(clientName || title || '?').slice(0, 1).toUpperCase()}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="text-[17px] font-semibold text-b24-text-dark truncate">
                    {clientName || title || 'Без имени'}
                  </div>
                  {clientPhone ? (
                    <div className="mt-1 flex items-center gap-2 flex-wrap">
                      {clientPhone.includes('•') ? (
                        <span className="text-[15px] font-medium text-b24-text-dark tabular-nums">
                          {clientPhone}
                        </span>
                      ) : (
                        <a
                          href={`tel:${clientPhone.replace(/\s/g, '')}`}
                          className="text-[15px] font-medium text-b24-text-dark tabular-nums hover:text-b24-cyan"
                        >
                          {formatPhone(clientPhone)}
                        </a>
                      )}
                      <div className="flex items-center gap-1">
                        <button
                          type="button"
                          title="Позвонить"
                          disabled={callDisabled}
                          onClick={onCall}
                          className="size-8 rounded-full bg-b24-green/12 text-b24-green inline-flex items-center justify-center hover:bg-b24-green/20 disabled:opacity-40"
                        >
                          <Phone size={15} strokeWidth={2.25} />
                        </button>
                        <button
                          type="button"
                          title="Написать в MAX"
                          onClick={onWriteMax}
                          className="size-8 rounded-full bg-[#6c5ce7]/12 text-[#6c5ce7] inline-flex items-center justify-center hover:bg-[#6c5ce7]/20"
                        >
                          <MessageCircle size={15} strokeWidth={2.25} />
                        </button>
                        <span
                          className="size-8 rounded-full bg-black/5 text-b24-text-muted inline-flex items-center justify-center opacity-50"
                          title="Почта"
                        >
                          <Mail size={15} strokeWidth={2} />
                        </span>
                      </div>
                    </div>
                  ) : (
                    <div className="mt-1 text-[13px] text-b24-text-muted">Телефон не указан</div>
                  )}
                </div>
              </div>
            </div>
            <div className="px-5 py-4 flex flex-col gap-3">{left}</div>
          </aside>

          <section className="min-h-0 overflow-y-auto b24-scrollbar px-4 sm:px-5 py-4 flex flex-col gap-3 border-r border-black/8">
            {/* Скрыто: блок «Сообщения в MAX / Привязать номер» — код оставлен */}
            {false && (
              <button
                type="button"
                onClick={onWriteMax}
                className="w-full text-left rounded-[12px] bg-white border border-black/8 px-4 py-3.5 flex items-center justify-between gap-3 hover:border-b24-cyan/40 shadow-sm shrink-0"
              >
                <span className="flex items-center gap-3 min-w-0">
                  <span className="size-9 rounded-[10px] bg-[#6c5ce7] text-white inline-flex items-center justify-center shrink-0">
                    <MessageCircle size={16} />
                  </span>
                  <span className="min-w-0">
                    <span className="block text-[14px] font-semibold text-b24-text-dark">
                      Сообщения в MAX
                    </span>
                    <span className="block text-[12px] text-b24-text-muted truncate">
                      Привязать номер · чат справа
                    </span>
                  </span>
                </span>
                <span className="text-b24-text-muted text-[18px] leading-none">▾</span>
              </button>
            )}
            {timeline}
          </section>

          <aside className="min-h-0 overflow-y-auto b24-scrollbar px-3 sm:px-4 py-4 bg-[#e8edf1] lg:col-span-2 xl:col-span-1 flex justify-center items-start">
            {chat}
          </aside>
        </div>

        {footer || onTitleSave ? (
          <footer className="shrink-0 px-5 py-3 border-t border-black/8 bg-white flex flex-wrap items-center gap-2">
            {onTitleSave ? (
              <button
                type="button"
                className="h-9 rounded-b24-sm px-3.5 text-[13px] font-medium border border-black/10 bg-white text-b24-text-dark hover:bg-[#f5f7f9]"
                onClick={() => {
                  setEditingTitle(true)
                  setTitleDraft(title)
                  setTitleError('')
                }}
              >
                Изменить название лида
              </button>
            ) : null}
            {footer}
          </footer>
        ) : null}
      </div>
    </div>
  )
}

export function LeadCommentComposer({
  pending,
  error,
  value,
  onChange,
  onSubmit,
}: {
  pending?: boolean
  error?: string
  value: string
  onChange: (v: string) => void
  onSubmit: () => void
}) {
  return (
    <LeadTimelineCard title="Комментарий">
      <div className="rounded-[10px] border border-black/10 bg-[#fafbfc] focus-within:border-[#2fc6f6]/55 focus-within:bg-white transition-colors">
        <textarea
          value={value}
          onChange={(e) => onChange(e.target.value)}
          rows={3}
          placeholder="Написать комментарий…"
          disabled={pending}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
              e.preventDefault()
              onSubmit()
            }
          }}
          className="w-full resize-none bg-transparent px-3.5 pt-3 pb-2 text-[13px] text-b24-text-dark outline-none placeholder:text-b24-text-muted/80 disabled:opacity-60"
        />
        <div className="flex items-center justify-between gap-2 px-2.5 pb-2.5">
          {error ? <p className="text-[12px] text-red-600 min-w-0 truncate">{error}</p> : <span />}
          <button
            type="button"
            disabled={pending || !value.trim()}
            onClick={onSubmit}
            className="shrink-0 h-8 rounded-b24-sm px-3.5 text-[12px] font-semibold text-white bg-[#2fc6f6] hover:bg-[#1eb5e5] disabled:opacity-50 disabled:pointer-events-none transition-colors"
          >
            {pending ? '…' : 'Отправить'}
          </button>
        </div>
      </div>
      <p className="mt-1.5 text-[11px] text-b24-text-muted">Ctrl+Enter — отправить</p>
    </LeadTimelineCard>
  )
}

export function LeadTimelineCard({
  title,
  children,
  action,
}: {
  title: string
  children?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="rounded-[12px] bg-white border border-black/8 shadow-sm overflow-hidden shrink-0">
      <div className="px-4 py-3 border-b border-black/6 flex items-center justify-between gap-2">
        <h3 className="text-[13px] font-semibold text-b24-text-dark">{title}</h3>
        {action}
      </div>
      {children ? <div className="px-4 py-3">{children}</div> : null}
    </div>
  )
}

export function CallRecordingPlayer({ callId }: { callId: string }) {
  const [src, setSrc] = useState<string | null>(null)
  const [err, setErr] = useState('')

  useEffect(() => {
    let cancelled = false
    let objectUrl = ''
    setSrc(null)
    setErr('')
    ;(async () => {
      try {
        const headers = new Headers()
        const t = token()
        if (t) headers.set('Authorization', `Bearer ${t}`)
        const res = await fetch(`/api/telephony/calls/${callId}/recording`, { headers })
        if (!res.ok) {
          const data = (await res.json().catch(() => ({}))) as { error?: string }
          throw new Error(data.error || res.statusText)
        }
        const blob = await res.blob()
        if (cancelled) return
        objectUrl = URL.createObjectURL(blob)
        setSrc(objectUrl)
      } catch (e) {
        if (!cancelled) setErr(e instanceof Error ? e.message : 'нет записи')
      }
    })()
    return () => {
      cancelled = true
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [callId])

  if (err) {
    return <p className="mt-1.5 text-[11px] text-b24-text-muted">Запись: {err}</p>
  }
  if (!src) {
    return <p className="mt-1.5 text-[11px] text-b24-text-muted">Загрузка записи…</p>
  }
  return (
    <audio controls preload="metadata" className="mt-1.5 w-full max-w-md h-8" src={src}>
      Запись звонка
    </audio>
  )
}

export function LeadTimelineEvent({
  time,
  title,
  body,
  kind = 'default',
  recordingCallId,
}: {
  time: string
  title: string
  body?: string
  kind?: TimelineEventKind
  recordingCallId?: string
}) {
  const style = TIMELINE_KIND_STYLE[kind] || TIMELINE_KIND_STYLE.default
  const Icon = style.Icon

  if (kind === 'comment') {
    return (
      <div className="py-2.5 border-b border-black/5 last:border-0">
        <div className="rounded-[10px] border border-[#f0d78c]/80 bg-[#fffbeb] px-3 py-2.5 shadow-[0_1px_0_rgba(0,0,0,0.03)]">
          <div className="flex items-start gap-2.5">
            <div
              className={`size-8 rounded-full inline-flex items-center justify-center shrink-0 ${style.circle}`}
            >
              <Icon size={14} strokeWidth={2.5} />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline justify-between gap-2">
                <div className="text-[13px] font-semibold text-[#8a6d1d]">{title}</div>
                <div className="text-[11px] text-[#a8904a] tabular-nums shrink-0">{time}</div>
              </div>
              {body ? (
                <p className="mt-1.5 text-[13px] text-[#3d3420] leading-snug whitespace-pre-wrap break-words">
                  {body}
                </p>
              ) : null}
            </div>
          </div>
        </div>
      </div>
    )
  }

  if (kind === 'task_planned' || kind === 'task_done') {
    const planned = kind === 'task_planned'
    return (
      <div className="py-2.5 border-b border-black/5 last:border-0">
        <div
          className={[
            'rounded-[10px] border px-3 py-2.5 shadow-[0_1px_0_rgba(0,0,0,0.03)]',
            planned
              ? 'border-[#9fdcf0]/90 bg-[#f0fbfd]'
              : 'border-emerald-200/90 bg-emerald-50/70',
          ].join(' ')}
        >
          <div className="flex items-start gap-2.5">
            <div
              className={`size-8 rounded-full inline-flex items-center justify-center shrink-0 ${style.circle}`}
            >
              <Icon size={14} strokeWidth={2.5} />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline justify-between gap-2">
                <div
                  className={[
                    'text-[13px] font-semibold',
                    planned ? 'text-[#0b82b5]' : 'text-emerald-800',
                  ].join(' ')}
                >
                  {title}
                </div>
                <div
                  className={[
                    'text-[11px] tabular-nums shrink-0',
                    planned ? 'text-[#5a9bb5]' : 'text-emerald-700/80',
                  ].join(' ')}
                >
                  {time}
                </div>
              </div>
              {body ? (
                <p className="mt-1.5 text-[13px] text-b24-text-dark leading-snug whitespace-pre-wrap break-words">
                  {body}
                </p>
              ) : null}
            </div>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex gap-3 py-2.5 border-b border-black/5 last:border-0">
      <div
        className={`size-8 rounded-full inline-flex items-center justify-center shrink-0 mt-0.5 ${style.circle}`}
      >
        <Icon size={14} strokeWidth={2.5} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <div className="text-[13px] font-medium text-b24-text-dark">{title}</div>
          <div className="text-[11px] text-b24-text-muted tabular-nums shrink-0">{time}</div>
        </div>
        {body ? (
          <p className="mt-0.5 text-[12px] text-b24-text-muted leading-snug whitespace-pre-wrap break-words">
            {body}
          </p>
        ) : null}
        {recordingCallId ? <CallRecordingPlayer callId={recordingCallId} /> : null}
      </div>
    </div>
  )
}

export function LeadFieldLine({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[minmax(120px,42%)_1fr] gap-2 items-start text-[13px] py-1.5 border-b border-black/[0.04] last:border-0">
      <div className="text-b24-text-muted leading-snug">{label}</div>
      <div className="text-b24-text-dark min-w-0 break-words leading-snug">{children || '—'}</div>
    </div>
  )
}

export type PlannedTask = {
  id: string
  title: string
  description?: string
  due_at?: string
  status?: string
  is_important?: boolean
  block_work?: boolean
  kind?: string
}

function formatPlanDeadline(iso?: string) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const week = d.toLocaleDateString('ru-RU', { weekday: 'short' })
  const day = d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' })
  const time = d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
  return `${week}, ${day}, ${time}`
}

function dayNumber(iso?: string) {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return String(d.getDate())
}

function toLocalInputValue(d = new Date()) {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const CONTACT_TEMPLATES = [
  'Связаться с клиентом',
  'Перезвонить',
  'Написать в MAX',
]

/** Запланировать важную задачу «связаться» — UX как в Bitrix делах. */
export function LeadPlanPanel({
  leadId,
  dealId,
  leadTitle,
  tasks,
  onChanged,
}: {
  leadId: string
  dealId?: string
  leadTitle?: string
  tasks: PlannedTask[]
  onChanged: () => void
}) {
  const [openForm, setOpenForm] = useState(false)
  const [title, setTitle] = useState(CONTACT_TEMPLATES[0])
  const [when, setWhen] = useState(() => toLocalInputValue(new Date(Date.now() + 60 * 60 * 1000)))
  const [desc, setDesc] = useState('')
  const [important, setImportant] = useState(true)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const resetForm = () => {
    setTitle(CONTACT_TEMPLATES[0])
    setWhen(toLocalInputValue(new Date(Date.now() + 60 * 60 * 1000)))
    setDesc('')
    setImportant(true)
    setError('')
  }

  const create = async () => {
    const trimmed = title.trim()
    if (!trimmed) {
      setError('Укажите название')
      return
    }
    if (important && !when) {
      setError('Укажите дату и время')
      return
    }
    setPending(true)
    setError('')
    try {
      const entityType = dealId ? 'deal' : 'lead'
      const entityId = dealId || leadId
      let deadline = when
      if (deadline && deadline.length === 16) deadline += ':00'
      await api('/tasks', {
        method: 'POST',
        body: JSON.stringify({
          title: trimmed,
          description: desc.trim(),
          deadline: deadline || undefined,
          plannedDate: deadline ? deadline.slice(0, 10) : undefined,
          entity_type: entityType,
          entity_id: entityId,
          is_important: important,
          block_work: important,
          kind: 'task',
        }),
      })
      setOpenForm(false)
      resetForm()
      onChanged()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось создать')
    } finally {
      setPending(false)
    }
  }

  const complete = async (task: PlannedTask) => {
    setPending(true)
    setError('')
    try {
      if (task.is_important || task.block_work || task.kind === 'missed_call') {
        await api(`/tasks/${task.id}/resolve`, {
          method: 'POST',
          body: JSON.stringify({ result: 'done', note: '' }),
        })
      } else {
        await api(`/tasks/${task.id}`, {
          method: 'PATCH',
          body: JSON.stringify({ status: 'completed' }),
        })
      }
      onChanged()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось закрыть')
    } finally {
      setPending(false)
    }
  }

  return (
    <LeadTimelineCard
      title="Запланировать"
      action={
        <Button
          type="button"
          variant="filter"
          className="rounded-b24-sm h-8 text-[12px] px-2.5"
          onClick={() => {
            setOpenForm((v) => !v)
            if (!openForm) resetForm()
          }}
        >
          {openForm ? 'Скрыть' : 'Создать дело'}
        </Button>
      }
    >
      {error ? <p className="mb-2 text-[12px] text-red-600">{error}</p> : null}

      {openForm ? (
        <div className="mb-3 rounded-[12px] border border-black/10 bg-[#f5f7f9] p-3 flex flex-col gap-2.5 text-[#1a2140]">
          <div className="flex flex-wrap gap-1.5">
            {CONTACT_TEMPLATES.map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => setTitle(t)}
                className={[
                  'h-7 rounded-full px-2.5 text-[12px] font-medium border transition',
                  title === t
                    ? 'bg-[#e8f8fe] border-[#2fc6f6] text-[#0b82b5]'
                    : 'bg-white border-black/10 text-[#535c69] hover:bg-white',
                ].join(' ')}
              >
                {t}
              </button>
            ))}
          </div>
          <label className="flex flex-col gap-1">
            <span className="text-[11px] font-medium text-[#535c69]">Название</span>
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="h-9 rounded-[10px] border border-black/10 bg-white px-3 text-[13px] text-[#1a2140] placeholder:text-[#a0a7b0] outline-none focus:border-[#2fc6f6] caret-[#1a2140]"
              placeholder="Название дела"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-[11px] font-medium text-[#535c69]">Когда</span>
            <input
              type="datetime-local"
              value={when}
              onChange={(e) => setWhen(e.target.value)}
              className="h-9 rounded-[10px] border border-black/10 bg-white px-3 text-[13px] text-[#1a2140] outline-none focus:border-[#2fc6f6] [color-scheme:light]"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-[11px] font-medium text-[#535c69]">Комментарий</span>
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              rows={2}
              placeholder="Позвонить и обсудить даты вылета…"
              className="rounded-[10px] border border-black/10 bg-white px-3 py-2 text-[13px] text-[#1a2140] placeholder:text-[#a0a7b0] outline-none focus:border-[#2fc6f6] resize-none caret-[#1a2140]"
            />
          </label>
          <label className="inline-flex items-center gap-2 text-[13px] text-[#1a2140]">
            <input
              type="checkbox"
              checked={important}
              onChange={(e) => setImportant(e.target.checked)}
              className="size-4 accent-[#2fc6f6]"
            />
            Важная задача
          </label>
          <div className="flex gap-2">
            <Button
              type="button"
              variant="green"
              className="rounded-b24-sm h-8"
              disabled={pending}
              onClick={() => void create()}
            >
              {pending ? '…' : 'Запланировать'}
            </Button>
            <Button
              type="button"
              variant="filter"
              className="rounded-b24-sm h-8"
              onClick={() => {
                setOpenForm(false)
                resetForm()
              }}
            >
              Отмена
            </Button>
          </div>
        </div>
      ) : null}

      {tasks.length === 0 && !openForm ? (
        <p className="text-[13px] text-b24-text-muted">Нет запланированных дел</p>
      ) : null}

      <ul className="flex flex-col gap-2.5">
        {tasks.map((task) => {
          const done =
            task.status === 'completed' ||
            task.status === 'done' ||
            task.status === 'closed'
          return (
            <li
              key={task.id}
              className={[
                'rounded-[12px] border px-3 py-3 shadow-[0_1px_0_rgba(0,0,0,0.03)]',
                done ? 'border-black/6 bg-[#f7f8fa] opacity-80' : 'border-black/8 bg-white',
              ].join(' ')}
            >
              <div className="flex items-start gap-2.5">
                <input
                  type="checkbox"
                  className="mt-1 size-4 rounded border-black/20"
                  checked={done}
                  disabled={pending || done}
                  onChange={() => {
                    if (!done) void complete(task)
                  }}
                  aria-label="Выполнено"
                />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-baseline justify-between gap-2">
                    <div
                      className={[
                        'text-[14px] font-semibold leading-snug',
                        done ? 'text-[#8a94a6] line-through' : 'text-[#1a2140]',
                      ].join(' ')}
                    >
                      {task.title}
                      {!done && task.is_important ? (
                        <Star
                          size={13}
                          className="ml-1.5 inline text-sky-600 fill-sky-600 align-[-1px]"
                        />
                      ) : null}
                    </div>
                    {task.due_at ? (
                      <span
                        className={[
                          'text-[11px] tabular-nums',
                          done ? 'text-[#a0a7b0] line-through' : 'text-[#8a94a6]',
                        ].join(' ')}
                      >
                        {new Date(task.due_at).toLocaleString('ru-RU', {
                          day: 'numeric',
                          month: 'short',
                          hour: '2-digit',
                          minute: '2-digit',
                        })}
                      </span>
                    ) : null}
                  </div>
                  {task.due_at ? (
                    <div className={['mt-2 flex items-start gap-2.5', done ? 'opacity-70' : ''].join(' ')}>
                      <div
                        className={[
                          'size-11 shrink-0 rounded-[10px] flex flex-col items-center justify-center leading-none',
                          done ? 'bg-[#eef0f3] text-[#8a94a6]' : 'bg-[#eef6fb] text-[#1a5f8a]',
                        ].join(' ')}
                      >
                        <span className="text-[10px] uppercase opacity-70">
                          {new Date(task.due_at).toLocaleDateString('ru-RU', { month: 'short' })}
                        </span>
                        <span className="text-[18px] font-bold tabular-nums">{dayNumber(task.due_at)}</span>
                      </div>
                      <div className="min-w-0 pt-0.5">
                        <div
                          className={[
                            'flex items-center gap-1.5 text-[13px] font-medium',
                            done ? 'text-[#8a94a6] line-through' : 'text-[#1a2140]',
                          ].join(' ')}
                        >
                          <span>Сделать до {formatPlanDeadline(task.due_at)}</span>
                          {!done && task.is_important ? (
                            <Bell size={13} className="text-sky-600 shrink-0" aria-label="Напоминание" />
                          ) : null}
                        </div>
                        {task.description ? (
                          <p
                            className={[
                              'mt-1 text-[13px] leading-snug whitespace-pre-wrap',
                              done ? 'text-[#a0a7b0] line-through' : 'text-[#535c69]',
                            ].join(' ')}
                          >
                            {task.description}
                          </p>
                        ) : null}
                        <p className={['mt-1.5 text-[12px]', done ? 'text-[#a0a7b0]' : 'text-[#2fc6f6]'].join(' ')}>
                          {dealId ? `Сделка: ${leadTitle || 'кандидат'}` : `Лид: ${leadTitle || 'заявка'}`}
                        </p>
                      </div>
                    </div>
                  ) : task.description ? (
                    <p
                      className={[
                        'mt-1 text-[13px] leading-snug',
                        done ? 'text-[#a0a7b0] line-through' : 'text-[#535c69]',
                      ].join(' ')}
                    >
                      {task.description}
                    </p>
                  ) : null}
                  {!done ? (
                    <div className="mt-2.5 flex flex-wrap gap-2">
                      <Button
                        type="button"
                        variant="green"
                        className="rounded-b24-sm h-8 px-3"
                        disabled={pending}
                        onClick={() => void complete(task)}
                      >
                        Выполнено
                      </Button>
                    </div>
                  ) : null}
                </div>
              </div>
            </li>
          )
        })}
      </ul>
    </LeadTimelineCard>
  )
}

/** @deprecated use LeadPlanPanel */
export function LeadPlanStub() {
  return (
    <LeadTimelineCard title="Запланировать">
      <p className="text-[13px] text-b24-text-muted">Нет запланированных дел</p>
    </LeadTimelineCard>
  )
}

type ChatMsg = {
  id: string
  dir: 'in' | 'out'
  body: string
  at: string
  has_media?: boolean
  media_kind?: string
  media_download?: boolean
  files_open?: boolean
  media_filename?: string
  media_count?: number
  media_url?: string
  sent_to_logist?: boolean
  delivery_status?: string
}

function mapThreadMessages(
  rows: {
    id?: string
    direction?: string
    body?: string
    created_at?: string
    has_media?: boolean
    media_kind?: string
    media_download?: boolean
    files_open?: boolean
    media_filename?: string
    sent_to_logist?: boolean
    delivery_status?: string
  }[],
): ChatMsg[] {
  return rows
    .map((m) => ({
      id: String(m.id || Math.random()),
      dir: (m.direction === 'out' || m.direction === 'outbound' ? 'out' : 'in') as 'in' | 'out',
      body: String(m.body || ''),
      at: m.created_at
        ? new Date(m.created_at).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
        : timeNow(),
      has_media: Boolean(m.has_media),
      media_kind: m.media_kind,
      media_download: Boolean(m.media_download),
      files_open: Boolean(m.files_open),
      media_filename: m.media_filename,
      media_count: Number((m as { media_count?: number }).media_count || 0),
      media_url: `/api/inbox/messages/${m.id}/media`,
      sent_to_logist: Boolean(m.sent_to_logist),
      delivery_status: m.delivery_status,
    }))
    .filter((m) => m.body.trim() || m.has_media)
}

function timeNow() {
  return new Date().toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
}

/** Живой чат MAX в карточке лида. */
export function MaxIphonePreview({
  leadId,
  clientName,
  clientPhone,
  onNeedBind,
}: {
  leadId: string
  clientName: string
  clientPhone?: string
  onNeedBind?: () => void
}) {
  const { user } = useAuth()
  const hidePhones = !canSeePhones(user)
  const managerChat = isManagerRole(user?.role)
  const showErr = (msg: string) => (hidePhones ? maskPhonesInText(msg) : msg)
  const name = clientName || 'Клиент'
  const nameRef = useRef(name)
  nameRef.current = name
  const phoneRaw = clientPhone || ''
  // Маска (•/*) или обрезки <10 цифр — не шлём; бэкенд резолвит по lead_id.
  const phone =
    /[•*]/.test(phoneRaw) || phoneRaw.replace(/\D/g, '').length < 10
      ? ''
      : phoneRaw.replace(/\D/g, '')
  const canReach = Boolean(phone || leadId)
  const [messages, setMessages] = useState<ChatMsg[]>([])
  const [draft, setDraft] = useState('')
  const [attachFiles, setAttachFiles] = useState<File[]>([])
  const [threadId, setThreadId] = useState('')
  const [pending, setPending] = useState(false)
  const [mediaBusyId, setMediaBusyId] = useState('')
  const [error, setError] = useState('')
  const [accounts, setAccounts] = useState<{ id: string; label: string; hasInbound?: boolean; sendDown?: boolean; coldLimit?: boolean }[]>([])
  const [lineId, setLineId] = useState('')
  const [pickedLineId, setPickedLineId] = useState('')
  const [pickedLeadId, setPickedLeadId] = useState('')
  const linePicked = pickedLeadId === leadId && pickedLineId !== ''
  const [serverNotice, setServerNotice] = useState('')
  const [noticeCold, setNoticeCold] = useState(false)
  const [accountTick, setAccountTick] = useState(0)

  useEffect(() => {
    let cancelled = false
    const loadAccounts = async () => {
      try {
        const qs = leadId ? `?lead_id=${encodeURIComponent(leadId)}` : ''
        const res = await api<{
          items?: {
            id: string
            label?: string
            operator_label?: string
            name?: string
            channel?: string
            has_inbound?: boolean
            send_down?: boolean
            cold_limit?: boolean
          }[]
        }>(`/inbox/max/lines-for-lead${qs}`)
        if (cancelled) return
        const rows = (res.items ?? []).map((l) => ({
          id: l.id,
          label: String(l.operator_label || l.label || l.name || 'MAX').trim() || 'MAX',
          hasInbound: Boolean(l.has_inbound),
          sendDown: Boolean(l.send_down),
          coldLimit: Boolean(l.cold_limit),
        }))
        if (rows.length) {
          setAccounts(rows)
          setLineId((prev) => {
            if (prev && rows.some((r) => r.id === prev)) return prev
            const withInbound = rows.find((r) => r.hasInbound)
            if (withInbound) return withInbound.id
            const fresh = rows.find((r) => !r.sendDown && !r.coldLimit)
            return fresh?.id || rows[0]?.id || ''
          })
          return
        }
      } catch {
        /* fallback below */
      }
      try {
        const res = await api<{
          items?: { id: string; operator_label?: string; name?: string; channel?: string; send_down?: boolean; cold_limit?: boolean }[]
        }>('/inbox/lines?channel=max')
        if (cancelled) return
        const rows = (res.items ?? [])
          .filter((l) => String(l.channel || 'max').toLowerCase() === 'max')
          .map((l) => ({
            id: l.id,
            label: String(l.operator_label || l.name || 'MAX').trim() || 'MAX',
            hasInbound: false,
            sendDown: Boolean(l.send_down),
            coldLimit: Boolean(l.cold_limit),
          }))
        setAccounts(rows)
        setLineId((prev) => {
          if (prev && rows.some((r) => r.id === prev)) return prev
          const fresh = rows.find((r) => !r.sendDown && !r.coldLimit)
          return fresh?.id || rows[0]?.id || ''
        })
      } catch {
        if (!cancelled) setAccounts([])
      }
    }
    void loadAccounts()
    return () => {
      cancelled = true
    }
  }, [leadId, accountTick])

  useEffect(() => {
    // Переписка грузится один раз на лид. Приход списка аккаунтов и линия,
    // которую сервер сам вернул, чат не стирают. Заново — только смена лида
    // или явный выбор аккаунта.
    setMessages([])
    setThreadId('')
    setError('')
    setAttachFiles([])
    if (!canReach) return
    let cancelled = false
    const picked = linePicked ? pickedLineId : ''
    void (async () => {
      try {
        const opened = await api<{ thread_id?: string; line_id?: string }>('/inbox/max/open', {
          method: 'POST',
          body: JSON.stringify({
            ...(phone ? { phone } : {}),
            name: nameRef.current,
            lead_id: leadId,
            ...(picked ? { line_id: picked } : {}),
          }),
        })
        if (cancelled || !opened.thread_id) return
        setThreadId(opened.thread_id)
        if (opened.line_id && !picked) setLineId(opened.line_id)
        const thr = await api<{
          messages?: Parameters<typeof mapThreadMessages>[0]
        }>(`/inbox/${opened.thread_id}`)
        if (cancelled) return
        setMessages(mapThreadMessages(thr.messages ?? []))
      } catch (e) {
        if (!cancelled) {
          setError(showErr(e instanceof Error ? e.message : 'Не удалось открыть чат MAX'))
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [leadId, phone, canReach, linePicked, pickedLineId])

  // Сбрасываем черновик только при смене лида/телефона, не при смене аккаунта.
  useEffect(() => {
    setDraft('')
  }, [leadId, phone])

  useEffect(() => {
    if (!threadId) return
    let cancelled = false
    const pull = async () => {
      try {
        const thr = await api<{
          messages?: Parameters<typeof mapThreadMessages>[0]
        }>(`/inbox/${threadId}`)
        if (cancelled) return
        setMessages(mapThreadMessages(thr.messages ?? []))
      } catch {
        /* ignore */
      }
    }
    const id = window.setInterval(() => void pull(), 5000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [threadId])

  const send = async () => {
    const text = draft.trim()
    const files = attachFiles
    if ((!text && files.length === 0) || pending) return
    if (!canReach) {
      setError('У лида нет телефона')
      onNeedBind?.()
      return
    }
    if (!lineId && accounts.length > 0) {
      setError('Выберите аккаунт MAX')
      return
    }
    setPending(true)
    setError('')
    const first = files[0]
    const optimistic: ChatMsg = {
      id: `tmp-${Date.now()}`,
      dir: 'out',
      body:
        text ||
        (files.length === 1
          ? `Файл: ${first.name}`
          : files.length > 1
            ? `${files.length} вложения`
            : ''),
      at: timeNow(),
      has_media: files.length > 0,
      media_filename: first?.name,
      media_count: files.length,
      media_kind: first?.type.startsWith('image/')
        ? 'image'
        : first?.type.startsWith('video/')
          ? 'video'
          : first?.type.startsWith('audio/')
            ? 'audio'
            : 'file',
    }
    setDraft('')
    setAttachFiles([])
    setMessages((prev) => [...prev, optimistic])
    try {
      let tid = threadId
      if (!tid || linePicked) {
        const opened = await api<{ thread_id?: string; line_id?: string }>('/inbox/max/open', {
          method: 'POST',
          body: JSON.stringify({
            ...(phone ? { phone } : {}),
            name,
            lead_id: leadId,
            ...(linePicked ? { line_id: pickedLineId } : {}),
          }),
        })
        tid = opened.thread_id || ''
        if (!tid) throw new Error('Не удалось открыть чат')
        setThreadId(tid)
        if (opened.line_id) setLineId(opened.line_id)
      }
      let res: { sent?: boolean; note?: string; max_notice?: string; max_assigned?: boolean; max_cold_limit?: boolean }
      if (files.length > 0) {
        const fd = new FormData()
        fd.append('body', text)
        if (linePicked && pickedLineId) fd.append('line_id', pickedLineId)
        for (const f of files) fd.append('file', f)
        res = await api<{ sent?: boolean; note?: string; max_notice?: string; max_assigned?: boolean; max_cold_limit?: boolean }>(`/inbox/${tid}/messages`, {
          method: 'POST',
          body: fd,
        })
      } else {
        res = await api<{ sent?: boolean; note?: string; max_notice?: string; max_assigned?: boolean; max_cold_limit?: boolean }>(`/inbox/${tid}/messages`, {
          method: 'POST',
          body: JSON.stringify({
            body: text,
            ...(linePicked && pickedLineId ? { line_id: pickedLineId } : {}),
          }),
        })
      }
      if (res.max_notice) {
        setServerNotice(showErr(res.max_notice))
        setNoticeCold(Boolean(res.max_cold_limit))
        setError('')
        setAccountTick((n) => n + 1)
      } else if (!res.sent && res.note) setError(showErr(res.note))
      const thr = await api<{
        messages?: Parameters<typeof mapThreadMessages>[0]
      }>(`/inbox/${tid}`)
      setMessages(mapThreadMessages(thr.messages ?? []))
    } catch (e) {
      setError(showErr(e instanceof Error ? e.message : 'Не удалось отправить'))
      setMessages((prev) => prev.filter((m) => m.id !== optimistic.id))
      setDraft(text)
      if (files.length) setAttachFiles(files)
    } finally {
      setPending(false)
    }
  }

  const currentLine = accounts.find((a) => a.id === lineId)
  const otherLine = accounts.find((a) => a.id !== lineId && !a.sendDown && !a.coldLimit)
  const clientWrote = messages.some((m) => m.dir === 'in')
  const coldBlocked = Boolean(currentLine?.coldLimit) && !clientWrote
  const lineDown = Boolean(currentLine?.sendDown) || coldBlocked || Boolean(serverNotice)
  const downText =
    serverNotice ||
    (currentLine?.sendDown
      ? maxDownBanner(currentLine.label || '', otherLine?.label)
      : coldBlocked
        ? maxColdBanner(currentLine?.label || '')
        : '')

  const phoneLabel = clientPhone
    ? clientPhone.includes('•')
      ? clientPhone
      : formatPhone(clientPhone)
    : ''

  return (
    <div className="sticky top-0 w-full max-w-[340px]">
      <ChatPanel
        className="h-[min(620px,calc(100vh-11rem))]"
        compact
        title={name}
        subtitle={phoneLabel ? `MAX · ${phoneLabel}` : 'MAX'}
        messages={messages}
        draft={draft}
        onDraftChange={setDraft}
        onSend={() => void send()}
        pending={pending}
        error={error}
        emptyText="С этого аккаунта переписки ещё нет"
        placeholder={
          currentLine?.sendDown || (serverNotice && !noticeCold && !coldBlocked)
            ? 'Этот чат с этого номера не продолжится'
            : coldBlocked || (serverNotice && noticeCold)
              ? 'Новым с этого номера не напишете'
              : canReach
                ? 'Сообщение'
                : 'Нужен телефон у лида'
        }
        composerDisabled={!canReach || (accounts.length > 0 && !lineId) || lineDown}
        allowAttach={canReach}
        attachFiles={attachFiles}
        onAttachFiles={setAttachFiles}
        composerTop={
          accounts.length > 0 || downText ? (
            <div className="space-y-2">
              {downText ? (
                <div className="rounded-lg border border-[#e7c7c7] bg-[#fff6f6] px-2.5 py-2 text-[12px] leading-snug text-[#7a3030]">
                  {downText}
                </div>
              ) : null}
              {accounts.length > 0 ? (
                <MaxAccountPicker
                  accounts={accounts}
                  value={lineId}
                  onChange={(id) => {
                    setServerNotice('')
                    setNoticeCold(false)
                    setPickedLeadId(leadId)
                    setPickedLineId(id)
                    setLineId(id)
                  }}
                />
              ) : null}
            </div>
          ) : null
        }
        headerRight={
          onNeedBind ? (
            <ShadButton
              type="button"
              variant="ghost"
              size="icon"
              className="size-8"
              title="Привязать номер"
              onClick={onNeedBind}
            >
              <Paperclip />
            </ShadButton>
          ) : null
        }
        renderMessage={(msg) =>
          msg.has_media ? (
            <ChatMediaBubble
              msg={msg}
              managerMode={managerChat}
              sending={mediaBusyId === msg.id}
              onSendToLogist={(id) => {
                setMediaBusyId(id)
                void api(`/inbox/messages/${id}/send-to-logist`, { method: 'POST' })
                  .then(async () => {
                    setError('')
                    if (!threadId) return
                    const thr = await api<{
                      messages?: Parameters<typeof mapThreadMessages>[0]
                    }>(`/inbox/${threadId}`)
                    setMessages(mapThreadMessages(thr.messages ?? []))
                  })
                  .catch((e: Error) => setError(showErr(e.message)))
                  .finally(() => setMediaBusyId(''))
              }}
              onDownload={(id) => {
                const count = Number(msg.media_count || 0)
                const name =
                  count > 1 ? 'attachments.zip' : msg.media_filename || 'file.bin'
                void apiDownload(`/inbox/messages/${id}/media`, name)
                  .then(async () => {
                    if (!threadId) return
                    const thr = await api<{ messages?: Parameters<typeof mapThreadMessages>[0] }>(
                      `/inbox/${threadId}`,
                    )
                    setMessages(mapThreadMessages(thr.messages ?? []))
                  })
                  .catch(async (e: Error) => {
                    setError(showErr(e.message))
                    if (!threadId) return
                    try {
                      const thr = await api<{ messages?: Parameters<typeof mapThreadMessages>[0] }>(
                        `/inbox/${threadId}`,
                      )
                      setMessages(mapThreadMessages(thr.messages ?? []))
                    } catch {
                      /* ignore */
                    }
                  })
              }}
            />
          ) : (
            <ChatMessageBubble
              dir={msg.dir}
              body={msg.body}
              at={msg.at}
              deliveryStatus={msg.delivery_status}
            />
          )
        }
      />
    </div>
  )
}

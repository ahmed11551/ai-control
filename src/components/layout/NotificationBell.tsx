import { Bell, MessageCircle, PhoneMissed, X } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { api, wsURL } from '../../lib/api'
import { softphoneIconLive } from '../../telephony/callPhase'
import { useAuth } from '../../store/auth'
import { useTelephony } from '../../store/telephony'

const ALLOWED_KINDS = new Set(['staff_dialog', 'missed_call', 'inbox_max'])

type Notif = {
  id: string
  title: string
  body?: string
  is_read?: boolean
  channel?: string
  created_at?: string
  payload?: {
    kind?: string
    dialog_id?: string
    thread_id?: string
    deal_id?: string
    lead_id?: string
    call_id?: string
    peer?: string
  }
}

type Toast = {
  key: string
  kind: string
  title: string
  body: string
  dialogId?: string
  threadId?: string
  leadId?: string
  dealId?: string
}

/** Sidebar слушает этот event — без второго WS/poll. */
export const NOTIF_BADGE_EVENT = 'forge:notif-badge'

function fmtWhen(iso?: string) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function kindOf(n: { channel?: string; payload?: { kind?: string }; kind?: string }) {
  return String(n.payload?.kind || n.kind || n.channel || '')
}

function eyebrowFor(kind: string) {
  switch (kind) {
    case 'missed_call':
      return 'Пропущенный звонок'
    case 'inbox_max':
      return 'Сообщение в MAX'
    case 'staff_dialog':
      return 'Диалог по кандидату'
    default:
      return 'Уведомление'
  }
}

function ctaFor(kind: string) {
  switch (kind) {
    case 'missed_call':
      return 'Открыть карточку →'
    case 'inbox_max':
      return 'Открыть чат →'
    default:
      return 'Открыть диалог →'
  }
}

let audioCtx: AudioContext | null = null

function getAudioCtx() {
  const Ctx =
    window.AudioContext ||
    (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
  if (!Ctx) return null
  if (!audioCtx || audioCtx.state === 'closed') {
    audioCtx = new Ctx()
  }
  return audioCtx
}

function unlockNotifyAudio() {
  const ctx = getAudioCtx()
  if (!ctx) return
  if (ctx.state === 'suspended') void ctx.resume().catch(() => undefined)
}

function playNotifySound() {
  try {
    const ctx = getAudioCtx()
    if (!ctx) return
    void ctx
      .resume()
      .then(() => {
        const now = ctx.currentTime
        ;[880, 1174].forEach((freq, i) => {
          const osc = ctx.createOscillator()
          const gain = ctx.createGain()
          osc.type = 'sine'
          osc.frequency.value = freq
          gain.gain.setValueAtTime(0.0001, now)
          gain.gain.exponentialRampToValueAtTime(0.1, now + 0.02 + i * 0.08)
          gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.22 + i * 0.08)
          osc.connect(gain)
          gain.connect(ctx.destination)
          osc.start(now + i * 0.08)
          osc.stop(now + 0.28 + i * 0.08)
        })
      })
      .catch(() => undefined)
  } catch {
    /* ignore */
  }
}

function publishBadge(items: Notif[], unread: number) {
  const dialogUnread = items.filter((x) => x.channel === 'staff_dialog' && !x.is_read).length
  window.dispatchEvent(
    new CustomEvent(NOTIF_BADGE_EVENT, {
      detail: { unread, dialogUnread },
    }),
  )
}

function navigateNotif(
  navigate: ReturnType<typeof useNavigate>,
  t: {
    dialogId?: string
    threadId?: string
    leadId?: string
    dealId?: string
    kind?: string
  },
) {
  if (t.dialogId) {
    navigate(`/dialogs?open=${encodeURIComponent(t.dialogId)}`)
    return
  }
  if (t.kind === 'inbox_max' || t.threadId) {
    if (t.threadId) {
      navigate(`/inbox?open=${encodeURIComponent(t.threadId)}`)
      return
    }
    if (t.leadId) {
      navigate(`/crm/leads?open=${encodeURIComponent(t.leadId)}`)
      return
    }
  }
  if (t.leadId) {
    navigate(`/crm/leads?open=${encodeURIComponent(t.leadId)}`)
    return
  }
  if (t.dealId) {
    navigate(`/crm?open=${encodeURIComponent(t.dealId)}`)
  }
}

export function NotificationBell() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const { snap } = useTelephony()
  const myId = String(user?.id || '')
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<Notif[]>([])
  const [unread, setUnread] = useState(0)
  const [toasts, setToasts] = useState<Toast[]>([])
  const rootRef = useRef<HTMLDivElement>(null)
  const seenToastKeys = useRef<Set<string>>(new Set())
  const lastUnreadRef = useRef<number | null>(null)
  const primedRef = useRef(false)
  const loadTimer = useRef<number | undefined>(undefined)
  const inCallRef = useRef(false)

  inCallRef.current = softphoneIconLive(snap.status)

  const pushToast = useCallback((toast: Toast) => {
    // Во время звонка/гудков — только бейдж в колокольчике (load), без тоста и звука.
    if (inCallRef.current) return
    if (seenToastKeys.current.has(toast.key)) return
    seenToastKeys.current.add(toast.key)
    if (seenToastKeys.current.size > 40) {
      seenToastKeys.current = new Set([...seenToastKeys.current].slice(-20))
    }
    setToasts((prev) => [toast, ...prev.filter((t) => t.key !== toast.key)].slice(0, 2))
    playNotifySound()
    window.setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.key !== toast.key))
      seenToastKeys.current.delete(toast.key)
    }, 8000)
  }, [])

  const toastFromNotif = useCallback((n: Notif): Toast => {
    const kind = kindOf(n)
    const dialogId = String(n.payload?.dialog_id || '').trim()
    const threadId = String(n.payload?.thread_id || '').trim()
    const leadId = String(n.payload?.lead_id || '').trim()
    const dealId = String(n.payload?.deal_id || '').trim()
    const key =
      (dialogId && `dlg-${dialogId}`) ||
      (threadId && `thr-${threadId}`) ||
      (n.payload?.call_id && `call-${n.payload.call_id}`) ||
      `id-${n.id}`
    return {
      key,
      kind,
      title: n.title || 'Новое уведомление',
      body: n.body || '',
      dialogId: dialogId || undefined,
      threadId: threadId || undefined,
      leadId: leadId || undefined,
      dealId: dealId || undefined,
    }
  }, [])

  const load = useCallback(async () => {
    if (!myId) return
    try {
      const data = await api<{ items?: Notif[]; unread?: number }>('/notifications')
      const nextItems = data.items ?? []
      const nextUnread = Number(data.unread) || 0
      setItems(nextItems)
      setUnread(nextUnread)
      publishBadge(nextItems, nextUnread)

      if (primedRef.current && lastUnreadRef.current !== null && nextUnread > lastUnreadRef.current) {
        const fresh = nextItems.find((n) => !n.is_read && ALLOWED_KINDS.has(kindOf(n)))
        if (fresh) pushToast(toastFromNotif(fresh))
      }
      lastUnreadRef.current = nextUnread
      primedRef.current = true
    } catch {
      /* ignore */
    }
  }, [myId, pushToast, toastFromNotif])

  const scheduleLoad = useCallback(() => {
    if (loadTimer.current) window.clearTimeout(loadTimer.current)
    loadTimer.current = window.setTimeout(() => void load(), 400)
  }, [load])

  useEffect(() => {
    void load()
    const tick = () => {
      if (document.visibilityState === 'visible') void load()
    }
    const t = window.setInterval(tick, 60_000)
    const onVis = () => {
      if (document.visibilityState === 'visible') void load()
    }
    document.addEventListener('visibilitychange', onVis)
    return () => {
      window.clearInterval(t)
      document.removeEventListener('visibilitychange', onVis)
      if (loadTimer.current) window.clearTimeout(loadTimer.current)
    }
  }, [load])

  useEffect(() => {
    const unlock = () => unlockNotifyAudio()
    window.addEventListener('pointerdown', unlock, { once: true })
    window.addEventListener('keydown', unlock, { once: true })
    return () => {
      window.removeEventListener('pointerdown', unlock)
      window.removeEventListener('keydown', unlock)
    }
  }, [])

  useEffect(() => {
    if (!myId) return
    let closed = false
    let ws: WebSocket | null = null
    let retry: number | undefined

    const connect = () => {
      if (closed) return
      ws = new WebSocket(wsURL())
      ws.onmessage = (ev) => {
        try {
          const msg = JSON.parse(String(ev.data)) as {
            type?: string
            user_id?: string
            payload?: {
              user_id?: string
              kind?: string
              channel?: string
              dialog_id?: string
              thread_id?: string
              lead_id?: string
              deal_id?: string
              call_id?: string
              title?: string
              body?: string
            }
          }
          if (msg.type !== 'notification') return
          const p = msg.payload || {}
          const kind = String(p.kind || p.channel || '')
          if (!ALLOWED_KINDS.has(kind)) return
          const uid = String(p.user_id || msg.user_id || '')
          if (uid && uid !== myId) return
          const dialogId = String(p.dialog_id || '').trim()
          const threadId = String(p.thread_id || '').trim()
          const callId = String(p.call_id || '').trim()
          const key =
            (dialogId && `dlg-${dialogId}`) ||
            (threadId && `thr-${threadId}`) ||
            (callId && `call-${callId}`) ||
            `n-${kind}-${String(p.title || '').slice(0, 40)}`
          pushToast({
            key,
            kind,
            title: String(p.title || 'Новое уведомление'),
            body: String(p.body || ''),
            dialogId: dialogId || undefined,
            threadId: threadId || undefined,
            leadId: String(p.lead_id || '').trim() || undefined,
            dealId: String(p.deal_id || '').trim() || undefined,
          })
          scheduleLoad()
        } catch {
          /* ignore */
        }
      }
      ws.onclose = () => {
        if (closed) return
        retry = window.setTimeout(connect, 4000)
      }
    }
    connect()
    return () => {
      closed = true
      if (retry) window.clearTimeout(retry)
      ws?.close()
    }
  }, [myId, pushToast, scheduleLoad])

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  const openItem = async (n: Notif) => {
    if (!n.is_read) {
      try {
        await api(`/notifications/${n.id}/read`, { method: 'POST', body: '{}' })
      } catch {
        /* ignore */
      }
    }
    setOpen(false)
    navigateNotif(navigate, {
      kind: kindOf(n),
      dialogId: String(n.payload?.dialog_id || '').trim() || undefined,
      threadId: String(n.payload?.thread_id || '').trim() || undefined,
      leadId: String(n.payload?.lead_id || '').trim() || undefined,
      dealId: String(n.payload?.deal_id || '').trim() || undefined,
    })
    void load()
  }

  const openToast = (t: Toast) => {
    setToasts((prev) => prev.filter((x) => x.key !== t.key))
    navigateNotif(navigate, t)
    void load()
  }

  if (!myId) return null

  const toastHost =
    toasts.length > 0 && typeof document !== 'undefined'
      ? createPortal(
          <div className="pointer-events-none fixed top-14 right-4 z-[190] flex flex-col gap-2 w-[min(100%-2rem,22rem)]">
            {toasts.map((t) => {
              const Icon = t.kind === 'missed_call' ? PhoneMissed : MessageCircle
              return (
                <div
                  key={t.key}
                  className="pointer-events-auto relative rounded-b24-sm bg-white shadow-[0_12px_40px_rgba(15,23,42,0.22)] border border-black/8 p-3 flex flex-col gap-2"
                >
                  <button
                    type="button"
                    aria-label="Закрыть"
                    className="absolute top-2 right-2 size-7 rounded-full text-b24-text-muted hover:bg-black/5 inline-flex items-center justify-center"
                    onClick={() => setToasts((prev) => prev.filter((x) => x.key !== t.key))}
                  >
                    <X size={14} />
                  </button>
                  <button type="button" className="text-left pr-6" onClick={() => openToast(t)}>
                    <div className="flex items-start gap-2.5">
                      <div
                        className={[
                          'mt-0.5 size-9 shrink-0 rounded-xl inline-flex items-center justify-center',
                          t.kind === 'missed_call'
                            ? 'bg-[#fde8e8] text-[#e53935]'
                            : 'bg-[#e8f8fe] text-b24-cyan',
                        ].join(' ')}
                      >
                        <Icon size={18} strokeWidth={1.8} />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="text-[12px] text-b24-text-muted">{eyebrowFor(t.kind)}</div>
                        <div className="text-[15px] font-semibold text-b24-text-dark truncate">
                          {t.title}
                        </div>
                        {t.body ? (
                          <div className="mt-0.5 text-[13px] text-b24-text-muted line-clamp-2">
                            {t.body}
                          </div>
                        ) : null}
                      </div>
                    </div>
                    <div className="mt-2.5 text-[12px] font-medium text-b24-cyan">{ctaFor(t.kind)}</div>
                  </button>
                </div>
              )
            })}
          </div>,
          document.body,
        )
      : null

  return (
    <>
      <div className="relative" ref={rootRef}>
        <button
          type="button"
          aria-label="Уведомления"
          className={[
            'relative size-8 rounded-b24-sm inline-flex items-center justify-center',
            'text-b24-white-soft hover:text-white hover:bg-b24-glass transition-colors',
          ].join(' ')}
          onClick={() => {
            unlockNotifyAudio()
            setOpen((v) => !v)
            void load()
          }}
        >
          <Bell size={17} strokeWidth={1.7} />
          {unread > 0 ? (
            <span className="absolute -top-0.5 -right-0.5 min-w-[16px] h-4 px-1 rounded-full bg-[#f04438] text-[10px] font-semibold text-white leading-4 text-center">
              {unread > 99 ? '99+' : unread}
            </span>
          ) : null}
        </button>

        {open ? (
          <div className="absolute right-0 top-9 z-40 w-[320px] max-h-[400px] overflow-hidden rounded-[12px] bg-white shadow-xl border border-black/8 flex flex-col">
            <div className="px-3.5 py-2.5 border-b border-black/6 text-[13px] font-semibold text-b24-text-dark">
              Уведомления
              {unread > 0 ? (
                <span className="ml-1.5 text-[12px] font-normal text-b24-text-muted">
                  · {unread} новых
                </span>
              ) : null}
            </div>
            <div className="flex-1 overflow-y-auto b24-scrollbar">
              {items.length === 0 ? (
                <p className="p-4 text-[13px] text-b24-text-muted">Пока пусто</p>
              ) : (
                items.map((n) => (
                  <button
                    key={n.id}
                    type="button"
                    onClick={() => void openItem(n)}
                    className={[
                      'w-full text-left px-3.5 py-2.5 border-b border-black/5 hover:bg-[#f5f8fa]',
                      n.is_read ? 'opacity-70' : 'bg-[#e8f8fe]/40',
                    ].join(' ')}
                  >
                    <div className="text-[11px] text-b24-text-muted mb-0.5">{eyebrowFor(kindOf(n))}</div>
                    <div className="flex items-baseline justify-between gap-2">
                      <div className="text-[13px] font-semibold text-b24-text-dark truncate">
                        {n.title}
                      </div>
                      <div className="shrink-0 text-[10px] text-b24-text-muted tabular-nums">
                        {fmtWhen(n.created_at)}
                      </div>
                    </div>
                    {n.body ? (
                      <div className="mt-0.5 text-[12px] text-b24-text-muted line-clamp-2">
                        {n.body}
                      </div>
                    ) : null}
                  </button>
                ))
              )}
            </div>
          </div>
        ) : null}
      </div>
      {toastHost}
    </>
  )
}

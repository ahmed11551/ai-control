import { useEffect, useMemo, useRef, useState, type UIEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Briefcase,
  ChevronRight,
  ExternalLink,
  MessageCircle,
  MessageSquare,
  Plus,
  Search,
  Send,
  X,
} from 'lucide-react'
import { ChatMediaBubble, ChatMessageBubble, ChatPanel } from '../../components/chat/ChatPanel'
import {
  isMaxGuideDismissed,
  MaxGuidesInfoButton,
  MaxReasonsGuideModal,
  MaxWorkGuideModal,
  setMaxGuideDismissed,
  type MaxGuideId,
} from '../../components/crm/MaxGuideModals'
import { maxColdBanner, maxDownBanner } from '../../components/crm/maxLineStatus'
import { InboxCandidatePanel } from '../../components/inbox/InboxCandidatePanel'
import { Badge } from '@/components/shadcn/badge'
import { Button as ShadButton } from '@/components/shadcn/button'
import { Input } from '@/components/shadcn/input'
import { PageHeader, PageShell } from '../../components/layout/PageHeader'
import { api, apiDownload, wsURL } from '../../lib/api'
import { type CrmRow } from '../../lib/crm'
import { canManageMaxAccounts, canSeePhones, isManagerRole, maskPhone, maskPhonesInText } from '../../lib/roles'
import { useAuth } from '../../store/auth'
import { useCrmLeadDrawer } from '../../store/crmLeadDrawer'
import { useTelephony } from '../../store/telephony'

type Thread = {
  id: string
  channel: string
  title: string
  phone: string
  last_body: string
  last_at: string
  unread: number
  contact: string
  deal: string
  deal_id?: string
  contact_id?: string
  lead_id?: string
  lead_title?: string
  line_id?: string
  line_name?: string
  from_phone?: string
  send_down?: boolean
  cold_limit?: boolean
}

type Message = {
  id: string
  direction: string
  body: string
  author_label: string
  created_at: string
  has_media?: boolean
  media_kind?: string
  media_download?: boolean
  files_open?: boolean
  media_filename?: string
  media_count?: number
  sent_to_logist?: boolean
  delivery_status?: string
}

type InboxLine = {
  id: string
  provider: string
  channel: string
  name: string
  kind: string
  operator_label: string
  external_ref: string
  is_active: boolean
  threads: number
  owner_id?: string
  owner?: string
  send_down?: boolean
  cold_limit?: boolean
}

type InboxData = {
  items: Thread[]
  has_more?: boolean
  limit?: number
  counts?: { channel?: string; n?: number }[]
  hook: boolean
  connectors?: {
    max?: boolean
    max_username?: string
    max_name?: string
    max_link?: string
    wappi: boolean
    wappi_poll?: boolean
    hook: boolean
    missing: string
  }
}

type ThreadDetail = Thread & {
  messages?: Message[]
  has_more_messages?: boolean
}

const THREAD_PAGE = 40
const MSG_PAGE = 60

function threadTime(t: Thread) {
  return Date.parse(String(t.last_at || '')) || 0
}

function mergeThreadPages(loaded: Thread[], fresh: Thread[]): Thread[] {
  const map = new Map<string, Thread>()
  for (const t of loaded) map.set(t.id, t)
  for (const t of fresh) map.set(t.id, t)
  return [...map.values()].sort((a, b) => {
    const d = threadTime(b) - threadTime(a)
    if (d !== 0) return d
    return String(b.id).localeCompare(String(a.id))
  })
}

function mergeMessages(prev: Message[] | undefined, fresh: Message[] | undefined): Message[] {
  const map = new Map<string, Message>()
  for (const m of prev ?? []) map.set(m.id, m)
  for (const m of fresh ?? []) map.set(m.id, m)
  return [...map.values()].sort((a, b) => {
    const d = (Date.parse(String(a.created_at)) || 0) - (Date.parse(String(b.created_at)) || 0)
    if (d !== 0) return d
    return String(a.id).localeCompare(String(b.id))
  })
}

function countsFromInbox(list: InboxData): Record<string, number> {
  const next: Record<string, number> = { '': 0 }
  for (const row of list.counts ?? []) {
    const ch = String(row.channel || 'other')
    const n = Number(row.n) || 0
    next[ch] = (next[ch] || 0) + n
    next[''] += n
  }
  return next
}

const viewTabs = [
  { id: 'chats', label: 'Диалоги' },
  { id: 'lines', label: 'Аккаунты' },
]

const channelTabs: { id: string; label: string; tone: string }[] = [
  { id: '', label: 'Все диалоги', tone: 'blue' },
  { id: 'max', label: 'MAX', tone: 'purple' },
  { id: 'whatsapp', label: 'WhatsApp', tone: 'green' },
  { id: 'telegram', label: 'Telegram', tone: 'sky' },
  { id: 'avito', label: 'Avito', tone: 'orange' },
]

function ChannelGlyph({ channel, className = 'size-3.5' }: { channel: string; className?: string }) {
  switch (channel) {
    case 'whatsapp':
      return <MessageCircle className={`${className} text-[#25d366]`} strokeWidth={2.4} />
    case 'telegram':
      return <Send className={`${className} text-[#2AABEE]`} strokeWidth={2.4} />
    case 'avito':
      return <Briefcase className={`${className} text-[#00AAFF]`} strokeWidth={2.4} />
    case 'max':
      return (
        <span className="inline-flex size-3.5 items-center justify-center rounded-[3px] bg-[#7b61ff] text-[7px] font-black text-white leading-none">
          M
        </span>
      )
    default:
      return <MessageSquare className={`${className} text-[#2b7de9]`} strokeWidth={2.4} />
  }
}

function chLabel(v: string) {
  switch (v) {
    case 'max':
      return 'MAX'
    case 'whatsapp':
      return 'WhatsApp'
    case 'telegram':
      return 'Telegram'
    case 'avito':
      return 'Avito'
    default:
      return v || '—'
  }
}

function kindLabel(v: string) {
  switch (v) {
    case 'pool':
      return 'общая'
    case 'spam':
      return 'спам'
    default:
      return 'личная'
  }
}

function timeShort(raw: string) {
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return ''
  const now = new Date()
  const startToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const startMsg = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  const diffDays = Math.round((startToday.getTime() - startMsg.getTime()) / 86400000)
  if (diffDays === 0) {
    return d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
  }
  if (diffDays === 1) return 'Вчера'
  return d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' })
}

function formatMaxPhone(raw: string) {
  const d = String(raw || '').replace(/\D/g, '')
  if (d.length === 11 && (d[0] === '7' || d[0] === '8')) {
    const n = '7' + d.slice(1)
    return `+${n[0]} ${n.slice(1, 4)} ${n.slice(4, 7)}-${n.slice(7, 9)}-${n.slice(9, 11)}`
  }
  if (d.length === 10) {
    return `+7 ${d.slice(0, 3)} ${d.slice(3, 6)}-${d.slice(6, 8)}-${d.slice(8, 10)}`
  }
  return raw || '—'
}

/** Номер аккаунта для UI — только телефон, без UUID профиля Wappi. */
function looksLikeProfileId(s: string) {
  const t = s.trim()
  if (/^[0-9a-f]{8}-[0-9a-f-]{4,}/i.test(t)) return true
  if (/^[0-9a-f]{12,}$/i.test(t.replace(/-/g, ''))) return true
  return false
}

function phoneFromLineText(raw: string) {
  const s = String(raw || '').trim()
  if (!s || /^max$/i.test(s) || looksLikeProfileId(s)) return ''
  const stripped = s.replace(/^MAX\s*[·•.\-]\s*/i, '').trim()
  const digits = stripped.replace(/\D/g, '')
  if (digits.length >= 10) return formatMaxPhone(stripped)
  const allDigits = s.replace(/\D/g, '')
  if (allDigits.length >= 10) return formatMaxPhone(s)
  return ''
}

function lineAccountLabel(
  line: { operator_label?: string; name?: string; external_ref?: string; id?: string },
  fallbackIndex = 0,
) {
  for (const raw of [line.operator_label, line.name]) {
    const phone = phoneFromLineText(String(raw || ''))
    if (phone) return phone
  }
  return `Аккаунт ${fallbackIndex + 1}`
}

type MaxNumberItem = {
  id: string
  line_id?: string
  channel_id?: string
  phone: string
  label: string
}

function initials(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

/** В списке показываем название заявки — «Клиент»/номер только когда заявки нет. */
function threadName(t: Thread, fallback = 'Без имени') {
  const generic = (v?: string) => {
    const s = String(v ?? '').trim()
    return !s || s === 'Клиент' || /^[\d\s+()•-]+$/.test(s)
  }
  if (!generic(t.lead_title)) return String(t.lead_title)
  if (!generic(t.deal)) return t.deal
  if (!generic(t.contact)) return t.contact
  if (!generic(t.title)) return t.title
  return t.contact || t.title || fallback
}

export function InboxPage() {
  const { user } = useAuth()
  const hidePhones = !canSeePhones(user)
  const canManageAccounts = canManageMaxAccounts(user)
  const managerChat = isManagerRole(user?.role)
  const showErr = (msg: string) => (hidePhones ? maskPhonesInText(msg) : msg)
  const navigate = useNavigate()
  const { openLead } = useCrmLeadDrawer()
  const { dial, snap, configReady } = useTelephony()
  const [searchParams] = useSearchParams()
  const [view, setView] = useState(() => searchParams.get('view') || 'chats')
  const [channel, setChannel] = useState(() => searchParams.get('channel') ?? '')
  const [lineId, setLineId] = useState(() => searchParams.get('line_id') || '')
  const [q, setQ] = useState('')
  const [accountQ, setAccountQ] = useState('')
  const [listFilter, setListFilter] = useState<'all' | 'unread'>('all')
  const maxMode = view === 'chats' && channel === 'max'
  const [channelCounts, setChannelCounts] = useState<Record<string, number>>({})
  const [data, setData] = useState<InboxData | null>(null)
  const [hasMoreThreads, setHasMoreThreads] = useState(false)
  const [loadingMoreThreads, setLoadingMoreThreads] = useState(false)
  const [hasMoreMessages, setHasMoreMessages] = useState(false)
  const [loadingOlderMessages, setLoadingOlderMessages] = useState(false)
  const listFilterKeyRef = useRef('')
  const loadingOlderRef = useRef(false)
  const [lines, setLines] = useState<InboxLine[]>([])
  const [error, setError] = useState('')
  const [maxNotice, setMaxNotice] = useState('')
  const [openId, setOpenId] = useState(() => searchParams.get('open') || '')
  const [thread, setThread] = useState<ThreadDetail | null>(null)
  const [leadMeta, setLeadMeta] = useState<{
    owner?: string
    status?: string
    source?: string
    title?: string
    stage?: string
    stage_short?: string
  } | null>(null)
  const [draft, setDraft] = useState('')
  const [attachFiles, setAttachFiles] = useState<File[]>([])
  const [pending, setPending] = useState(false)
  const [callErr, setCallErr] = useState('')
  const [mediaBusyId, setMediaBusyId] = useState('')
  const [people, setPeople] = useState<{ id: string; full_name: string; is_active?: boolean }[]>([])
  const [maxNumbers, setMaxNumbers] = useState<MaxNumberItem[]>([])
  const [wappiOk, setWappiOk] = useState(false)
  const [lineOwner, setLineOwner] = useState('')
  const [lineKind, setLineKind] = useState<'personal' | 'pool'>('personal')
  const [lineQuery, setLineQuery] = useState('')
  const [connectKey, setConnectKey] = useState('')
  const [flashIds, setFlashIds] = useState<Record<string, number>>({})
  const prevSig = useRef<Record<string, string>>({})
  const [maxGuide, setMaxGuide] = useState<MaxGuideId | null>(null)
  const [newChatOpen, setNewChatOpen] = useState(false)
  const [newChatPhone, setNewChatPhone] = useState('')
  const [newChatBusy, setNewChatBusy] = useState(false)
  const [newChatErr, setNewChatErr] = useState('')
  const [newChatLineId, setNewChatLineId] = useState('')
  const maxGuideEntryRef = useRef(false)
  const reasonsShownRef = useRef(false)
  const chainReasonsAfterWorkRef = useRef(false)
  const maxPhone = (searchParams.get('phone') || '').replace(/\D/g, '')
  const canOpenByPhone = Boolean(data?.connectors?.wappi)
  const callBusy =
    snap.status === 'connecting' ||
    snap.status === 'dialing' ||
    snap.status === 'active' ||
    snap.status === 'ringing'

  const buildInboxParams = (extra?: Record<string, string>) => {
    const p = new URLSearchParams()
    if (channel) p.set('channel', channel)
    if (q.trim()) p.set('q', q.trim())
    if (lineId) p.set('line_id', lineId)
    p.set('limit', String(THREAD_PAGE))
    if (extra) {
      for (const [k, v] of Object.entries(extra)) {
        if (v) p.set(k, v)
      }
    }
    return p
  }

  const applyInboxList = (list: InboxData, mode: 'replace' | 'merge') => {
    setChannelCounts(countsFromInbox(list))
    if (mode === 'replace') {
      setData(list)
      setHasMoreThreads(Boolean(list.has_more))
      return
    }
    setData((prev) => {
      const freshLen = (list.items ?? []).length
      const hadExtra = (prev?.items?.length ?? 0) > freshLen
      setHasMoreThreads((prevMore) => Boolean(list.has_more) || (prevMore && hadExtra))
      return {
        ...list,
        items: mergeThreadPages(prev?.items ?? [], list.items ?? []),
      }
    })
  }

  const load = () => {
    if (channel === 'max' && !lineId) {
      setData((prev) => ({ items: [], hook: prev?.hook ?? false, connectors: prev?.connectors }))
      setHasMoreThreads(false)
      return Promise.resolve()
    }
    const key = `${channel}|${lineId}|${q.trim()}`
    listFilterKeyRef.current = key
    return api<InboxData>(`/inbox?${buildInboxParams()}`)
      .then((list) => applyInboxList(list, 'replace'))
      .catch((e: Error) => setError(e.message))
  }

  const loadMoreThreads = () => {
    if (loadingMoreThreads || !hasMoreThreads) return
    if (channel === 'max' && !lineId) return
    const items = data?.items ?? []
    const last = items[items.length - 1]
    if (!last?.last_at || !last.id) return
    setLoadingMoreThreads(true)
    api<InboxData>(
      `/inbox?${buildInboxParams({
        before_last_at: last.last_at,
        before_id: last.id,
      })}`,
    )
      .then((list) => {
        const known = new Set(items.map((t) => t.id))
        const extra = (list.items ?? []).filter((t) => !known.has(t.id))
        setData((prev) => ({
          ...(prev ?? list),
          ...list,
          items: [...(prev?.items ?? []), ...extra],
        }))
        setHasMoreThreads(Boolean(list.has_more))
        if (list.counts) setChannelCounts(countsFromInbox(list))
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoadingMoreThreads(false))
  }

  const onThreadsScroll = (e: UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget
    if (el.scrollHeight - el.scrollTop - el.clientHeight > 120) return
    loadMoreThreads()
  }

  const loadLines = () => {
    // На вкладке «Аккаунты» — все линии; в режиме MAX-диалогов — только max.
    const qs =
      view === 'lines'
        ? ''
        : channel === 'max'
          ? '?channel=max'
          : channel
            ? `?channel=${channel}`
            : ''
    return api<{ items: InboxLine[] }>(`/inbox/lines${qs}`)
      .then((d) => setLines(d.items ?? []))
      .catch((e: Error) => setError(e.message))
  }

  const loadMaxNumbers = () =>
    api<{ items?: MaxNumberItem[]; wappi?: boolean }>('/inbox/max/numbers')
      .then((d) => {
        setMaxNumbers(d.items ?? [])
        setWappiOk(Boolean(d.wappi))
      })
      .catch(() => {
        setMaxNumbers([])
        setWappiOk(false)
      })

  // Live refresh: MAX пишет в БД → WS/poll обновляют список и открытый чат.
  // seq отбрасывает устаревшие ответы, иначе старый GET затирает новые сообщения.
  useEffect(() => {
    let seq = 0
    let cancelled = false

    const refresh = async () => {
      const my = ++seq
      try {
        if (channel === 'max' && !lineId) {
          if (cancelled || my !== seq) return
          listFilterKeyRef.current = `${channel}|${lineId}|${q.trim()}`
          setData((prev) => ({
            items: [],
            hook: prev?.hook ?? false,
            connectors: prev?.connectors,
          }))
          setHasMoreThreads(false)
        } else {
          const key = `${channel}|${lineId}|${q.trim()}`
          const list = await api<InboxData>(`/inbox?${buildInboxParams()}`)
          if (cancelled || my !== seq) return
          const nextSig: Record<string, string> = {}
          const flashed: Record<string, number> = {}
          for (const t of list.items ?? []) {
            const sig = `${t.last_at}|${t.last_body}|${t.unread}`
            nextSig[t.id] = sig
            const prev = prevSig.current[t.id]
            if (prev && prev !== sig) {
              flashed[t.id] = Date.now()
            }
          }
          prevSig.current = { ...prevSig.current, ...nextSig }
          if (Object.keys(flashed).length) {
            setFlashIds((cur) => ({ ...cur, ...flashed }))
            const ids = Object.keys(flashed)
            window.setTimeout(() => {
              setFlashIds((cur) => {
                const copy = { ...cur }
                for (const id of ids) delete copy[id]
                return copy
              })
            }, 2800)
          }
          const mode = listFilterKeyRef.current === key ? 'merge' : 'replace'
          listFilterKeyRef.current = key
          applyInboxList(list, mode)
        }

        const id = openId
        if (!id) return
        const th = await api<ThreadDetail>(`/inbox/${id}?limit=${MSG_PAGE}`)
        if (cancelled || my !== seq) return
        setThread((prev) => {
          if (!prev || prev.id !== id) {
            setHasMoreMessages(Boolean(th.has_more_messages))
            return th
          }
          return {
            ...th,
            messages: mergeMessages(prev.messages, th.messages),
          }
        })
      } catch (e) {
        if (!cancelled && my === seq) {
          setError(e instanceof Error ? e.message : 'Ошибка загрузки')
        }
      }
    }

    void refresh()

    const ws = new WebSocket(wsURL())
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(String(ev.data)) as { type?: string }
        if (msg.type === 'inbox.updated') void refresh()
      } catch {
        /* ignore */
      }
    }

    const onVis = () => {
      if (document.visibilityState === 'visible') void refresh()
    }
    document.addEventListener('visibilitychange', onVis)

    return () => {
      cancelled = true
      document.removeEventListener('visibilitychange', onVis)
      try {
        ws.close()
      } catch {
        /* ignore */
      }
    }
    // buildInboxParams/applyInboxList стабильны по смыслу через channel/lineId/q
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openId, channel, lineId, q])

  useEffect(() => {
    void loadLines()
    if (view === 'lines' || view === 'chats') void loadMaxNumbers()
  }, [channel, lineId, view, canManageAccounts])

  // phone=? из CRM → открыть/создать диалог и перейти в чат
  useEffect(() => {
    if (!canOpenByPhone || !maxPhone) return
    let cancelled = false
    api<{ thread_id?: string }>('/inbox/max/open', {
      method: 'POST',
      body: JSON.stringify({ phone: maxPhone }),
    })
      .then((res) => {
        if (cancelled || !res.thread_id) return
        navigate(`/inbox?channel=max&open=${res.thread_id}`, { replace: true })
        setOpenId(res.thread_id)
        return api<ThreadDetail>(`/inbox/${res.thread_id}?limit=${MSG_PAGE}`).then((th) => {
          setThread(th)
          setHasMoreMessages(Boolean(th.has_more_messages))
        })
      })
      .catch((e: Error) => {
        if (!cancelled) setError(e.message)
      })
    return () => {
      cancelled = true
    }
  }, [canOpenByPhone, maxPhone])

  useEffect(() => {
    const id = searchParams.get('open')
    if (!id) return
    setOpenId(id)
    api<ThreadDetail>(`/inbox/${id}?limit=${MSG_PAGE}`)
      .then((th) => {
        setThread(th)
        setHasMoreMessages(Boolean(th.has_more_messages))
      })
      .catch((e: Error) => setError(e.message))
  }, [searchParams])

  useEffect(() => {
    api<{ id: string; full_name: string; is_active?: boolean }[] | { users?: { id: string; full_name: string; is_active?: boolean }[] }>('/users')
      .then((d) => {
        const rows = Array.isArray(d) ? d : d.users ?? []
        setPeople(rows.filter((u) => u.is_active !== false))
      })
      .catch(() => undefined)
  }, [])

  const open = (id: string) => {
    setOpenId(id)
    setCallErr('')
    setHasMoreMessages(false)
    setLoadingOlderMessages(false)
    loadingOlderRef.current = false
    api<ThreadDetail>(`/inbox/${id}?limit=${MSG_PAGE}`)
      .then((th) => {
        setThread(th)
        setHasMoreMessages(Boolean(th.has_more_messages))
      })
      .catch((e: Error) => setError(e.message))
  }

  const openNewChatByPhone = async () => {
    const digits = newChatPhone.replace(/\D/g, '')
    if (digits.length < 10) {
      setNewChatErr('Введите номер телефона')
      return
    }
    const useLineId =
      (maxMode ? lineId : newChatLineId) ||
      newChatLineId ||
      lineId ||
      maxLines.find((l) => l.is_active)?.id ||
      maxLines[0]?.id ||
      ''
    if (!useLineId) {
      setNewChatErr('Выберите номер, с которого писать')
      return
    }
    if (!canOpenByPhone) {
      setNewChatErr('Wappi не подключён — новый чат недоступен')
      return
    }
    setNewChatBusy(true)
    setNewChatErr('')
    try {
      const res = await api<{ thread_id?: string; phone?: string }>('/inbox/max/open', {
        method: 'POST',
        body: JSON.stringify({ phone: digits, line_id: useLineId, name: 'Клиент' }),
      })
      if (!res.thread_id) {
        setNewChatErr('Не удалось открыть чат')
        return
      }
      const phone = res.phone || digits
      const stub: Thread = {
        id: res.thread_id,
        channel: 'max',
        title: 'Клиент',
        phone,
        last_body: '',
        last_at: new Date().toISOString(),
        unread: 0,
        contact: '',
        deal: '',
        line_id: useLineId,
      }
      setData((prev) => {
        const items = prev?.items ?? []
        if (items.some((t) => t.id === stub.id)) return prev
        return {
          ...(prev ?? { items: [], hook: false, connectors: { wappi: true, hook: false, missing: '' } }),
          items: [stub, ...items],
        }
      })
      setNewChatOpen(false)
      setNewChatPhone('')
      setNewChatErr('')
      open(res.thread_id)
      const p = new URLSearchParams()
      if (channel) p.set('channel', channel)
      if (maxMode) p.set('line_id', useLineId)
      p.set('open', res.thread_id)
      navigate(`/inbox?${p}`, { replace: true })
    } catch (e) {
      setNewChatErr(showErr(e instanceof Error ? e.message : 'Ошибка'))
    } finally {
      setNewChatBusy(false)
    }
  }

  const toggleNewChatForm = () => {
    setNewChatOpen((v) => {
      const next = !v
      if (next) {
        const first = maxLines.find((l) => l.is_active) || maxLines[0]
        const prefer = (maxMode && lineId ? lineId : '') || first?.id || ''
        setNewChatLineId(prefer)
        setNewChatPhone('')
      }
      return next
    })
    setNewChatErr('')
  }

  const loadOlderMessages = () => {
    if (!openId || !hasMoreMessages || loadingOlderRef.current) return
    const oldest = thread?.messages?.[0]
    if (!oldest?.id) return
    loadingOlderRef.current = true
    setLoadingOlderMessages(true)
    api<{ items?: Message[]; has_more?: boolean }>(
      `/inbox/${openId}/messages?limit=${MSG_PAGE}&before=${encodeURIComponent(oldest.id)}`,
    )
      .then((res) => {
        const older = res.items ?? []
        setThread((prev) =>
          prev
            ? { ...prev, messages: mergeMessages(older, prev.messages) }
            : prev,
        )
        setHasMoreMessages(Boolean(res.has_more))
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => {
        loadingOlderRef.current = false
        setLoadingOlderMessages(false)
      })
  }

  const send = async (e?: React.FormEvent) => {
    e?.preventDefault()
    if (!openId || (!draft.trim() && attachFiles.length === 0)) return
    setPending(true)
    try {
      let res: { sent?: boolean; note?: string; max_notice?: string }
      if (attachFiles.length > 0) {
        const fd = new FormData()
        fd.append('body', draft.trim())
        if (lineId) fd.append('line_id', lineId)
        for (const f of attachFiles) fd.append('file', f)
        res = await api<{ sent?: boolean; note?: string; max_notice?: string }>(`/inbox/${openId}/messages`, {
          method: 'POST',
          body: fd,
        })
      } else {
        res = await api<{ sent?: boolean; note?: string; max_notice?: string }>(`/inbox/${openId}/messages`, {
          method: 'POST',
          body: JSON.stringify({
            body: draft.trim(),
            ...(lineId ? { line_id: lineId } : {}),
          }),
        })
      }
      setDraft('')
      setAttachFiles([])
      if (res.max_notice) {
        setMaxNotice(showErr(res.max_notice))
        setError('')
      } else {
        setMaxNotice('')
        setError(!res.sent && res.note ? showErr(res.note) : '')
      }
      const th = await api<ThreadDetail>(`/inbox/${openId}?limit=${MSG_PAGE}`)
      setThread((prev) =>
        prev?.id === openId
          ? { ...th, messages: mergeMessages(prev.messages, th.messages) }
          : th,
      )
      await load()
    } catch (err) {
      setError(showErr(err instanceof Error ? err.message : 'Не удалось отправить'))
    } finally {
      setPending(false)
    }
  }

  const connectMax = async (item: MaxNumberItem) => {
    const profileId = (item.channel_id || item.id || '').trim()
    if (!profileId) return
    const key = profileId
    setConnectKey(key)
    setError('')
    try {
      const phone = item.phone || ''
      const name = formatMaxPhone(phone)
      await api('/inbox/lines', {
        method: 'POST',
        body: JSON.stringify({
          provider: 'wappi',
          channel: 'max',
          name: name !== '—' ? `MAX · ${name}` : item.label || 'MAX',
          kind: lineKind,
          operator_label: phone,
          external_ref: profileId,
          owner_user_id: lineOwner,
        }),
      })
      await Promise.all([loadLines(), loadMaxNumbers()])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось подключить')
    } finally {
      setConnectKey('')
    }
  }

  const setLineOwnerId = async (line: InboxLine, ownerId: string) => {
    try {
      await api(`/inbox/lines/${line.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ owner_user_id: ownerId }),
      })
      await loadLines()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сменить владельца')
    }
  }

  const toggleLine = async (line: InboxLine) => {
    try {
      await api(`/inbox/lines/${line.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ is_active: !line.is_active }),
      })
      await loadLines()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось обновить')
    }
  }

  const maxLines = useMemo(
    () => lines.filter((l) => String(l.channel).toLowerCase() === 'max'),
    [lines],
  )
  const otherLines = useMemo(
    () => lines.filter((l) => String(l.channel).toLowerCase() !== 'max'),
    [lines],
  )
  const filteredMaxLines = useMemo(() => {
    const qq = accountQ.trim().toLowerCase()
    const digits = qq.replace(/\D/g, '')
    return maxLines.filter((l) => {
      if (!qq) return true
      const label = `${l.operator_label || ''} ${l.name || ''} ${l.owner || ''}`.toLowerCase()
      if (label.includes(qq)) return true
      if (digits && `${l.operator_label}${l.name}`.replace(/\D/g, '').includes(digits)) return true
      return false
    })
  }, [maxLines, accountQ])
  const selectedMaxLine = useMemo(
    () => maxLines.find((l) => l.id === lineId) || null,
    [maxLines, lineId],
  )

  // Пока форма открыта — если линии подгрузились позже, подставить первый номер
  useEffect(() => {
    if (!newChatOpen) return
    if (newChatLineId && maxLines.some((l) => l.id === newChatLineId)) return
    const first =
      (maxMode && lineId && maxLines.find((l) => l.id === lineId)) ||
      maxLines.find((l) => l.is_active) ||
      maxLines[0]
    if (first) setNewChatLineId(first.id)
  }, [newChatOpen, newChatLineId, maxLines, maxMode, lineId])

  // Режим MAX: автовыбор первой линии менеджера
  useEffect(() => {
    if (!maxMode) return
    if (maxLines.length === 0) return
    if (lineId && maxLines.some((l) => l.id === lineId)) return
    const first = maxLines.find((l) => l.is_active) || maxLines[0]
    if (first) setLineId(first.id)
  }, [maxMode, maxLines, lineId])

  // При входе в MAX во Входящих — памятки (сначала работа, потом причины)
  useEffect(() => {
    if (!maxMode) {
      maxGuideEntryRef.current = false
      reasonsShownRef.current = false
      chainReasonsAfterWorkRef.current = false
      setMaxGuide(null)
      return
    }
    if (maxGuideEntryRef.current) return
    maxGuideEntryRef.current = true
    const uid = user?.id || ''
    if (!isMaxGuideDismissed('work', uid)) {
      chainReasonsAfterWorkRef.current = true
      setMaxGuide('work')
    } else if (!isMaxGuideDismissed('reasons', uid)) {
      setMaxGuide('reasons')
    }
  }, [maxMode, user?.id])

  // URL sync для MAX / open
  useEffect(() => {
    if (view !== 'chats') return
    const p = new URLSearchParams()
    if (channel) p.set('channel', channel)
    if (channel === 'max' && lineId) p.set('line_id', lineId)
    if (openId) p.set('open', openId)
    const next = p.toString()
    const cur = searchParams.toString()
    if (next === cur) return
    navigate(next ? `/inbox?${next}` : '/inbox', { replace: true })
  }, [view, channel, lineId, openId, navigate, searchParams])

  const confirmMaxGuide = (id: MaxGuideId, dontShowAgain: boolean) => {
    const uid = user?.id || ''
    if (dontShowAgain) setMaxGuideDismissed(id, true, uid)
    if (
      id === 'work' &&
      chainReasonsAfterWorkRef.current &&
      !isMaxGuideDismissed('reasons', uid)
    ) {
      chainReasonsAfterWorkRef.current = false
      setMaxGuide('reasons')
      return
    }
    chainReasonsAfterWorkRef.current = false
    setMaxGuide(null)
  }

  useEffect(() => {
    setMaxNotice('')
  }, [openId])

  const selectMaxLine = (id: string) => {
    if (id === lineId) return
    const prevHadDialogs = maxLines.some((l) => l.id === lineId && Number(l.threads || 0) > 0)
    const next = maxLines.find((l) => l.id === id)
    const nextEmpty = Number(next?.threads || 0) === 0
    setLineId(id)
    setOpenId('')
    setThread(null)
    setDraft('')
    setAttachFiles([])
    setCallErr('')
    setNewChatOpen(false)
    setNewChatPhone('')
    setNewChatErr('')
    const uid = user?.id || ''
    if (
      prevHadDialogs &&
      nextEmpty &&
      !reasonsShownRef.current &&
      !isMaxGuideDismissed('reasons', uid)
    ) {
      reasonsShownRef.current = true
      setMaxGuide('reasons')
    }
  }

  const selectChannel = (id: string) => {
    setChannel(id)
    if (id !== 'max') {
      setLineId('')
    }
    setOpenId('')
    setThread(null)
    setDraft('')
    setAttachFiles([])
    setCallErr('')
  }
  const availableMax = useMemo(() => {
    const q = lineQuery.trim().toLowerCase()
    const qDigits = q.replace(/\D/g, '')
    return maxNumbers
      .filter((n) => !n.line_id)
      .filter((n) => {
        if (!q) return true
        const hay = `${n.phone} ${n.label} ${n.channel_id || n.id}`.toLowerCase()
        if (hay.includes(q)) return true
        if (qDigits && `${n.phone}${n.label}`.replace(/\D/g, '').includes(qDigits)) return true
        return false
      })
  }, [maxNumbers, lineQuery])

  const threads = useMemo(() => {
    const items = data?.items ?? []
    if (listFilter === 'unread') return items.filter((t) => (t.unread || 0) > 0)
    return items
  }, [data, listFilter])
  const unreadTotal = useMemo(
    () => (data?.items ?? []).reduce((n, t) => n + (t.unread || 0), 0),
    [data],
  )
  const titleName = thread ? threadName(thread, '') : ''

  useEffect(() => {
    const leadId = thread?.lead_id || ''
    if (!leadId) {
      setLeadMeta(null)
      return
    }
    let cancelled = false
    api<CrmRow>(`/leads/${leadId}`)
      .then((row) => {
        if (cancelled) return
        setLeadMeta({
          owner: row.owner != null ? String(row.owner) : '',
          status: row.status != null ? String(row.status) : '',
          source: row.source != null ? String(row.source) : '',
          title: row.title != null ? String(row.title) : '',
          stage: row.stage != null ? String(row.stage) : '',
          stage_short: row.stage_short != null ? String(row.stage_short) : '',
        })
      })
      .catch(() => {
        if (!cancelled) setLeadMeta(null)
      })
    return () => {
      cancelled = true
    }
  }, [thread?.lead_id])

  const openCard = () => {
    if (thread?.lead_id) {
      openLead(thread.lead_id)
      return
    }
    if (thread?.deal_id) navigate(`/crm?open=${thread.deal_id}`)
  }

  const callCandidate = () => {
    if (!thread) return
    setCallErr('')
    void (async () => {
      let dest = thread.phone || ''
      if (!dest || dest.includes('•')) {
        const res = await api<{ to?: string }>('/calls/resolve-dest', {
          method: 'POST',
          body: JSON.stringify({
            lead_id: thread.lead_id || '',
            contact_id: thread.contact_id || '',
            deal_id: thread.deal_id || '',
          }),
        })
        dest = String(res.to || '')
      }
      if (!dest) throw new Error('Нет номера для звонка')
      await dial(dest, { leadId: thread.lead_id || undefined })
    })().catch((e: Error) => setCallErr(showErr(e.message)))
  }

  const showLinePhone = (raw: string) => formatMaxPhone(raw)

  const resolveLinePhone = (line: InboxLine, idx = 0) => {
    const fromLine = phoneFromLineText(line.operator_label || '') || phoneFromLineText(line.name || '')
    if (fromLine) return fromLine
    const hit = maxNumbers.find(
      (n) =>
        n.line_id === line.id ||
        (line.external_ref && (n.channel_id === line.external_ref || n.id === line.external_ref)),
    )
    if (hit?.phone) {
      const phone = phoneFromLineText(hit.phone) || phoneFromLineText(hit.label)
      if (phone) return phone
    }
    return lineAccountLabel(line, idx)
  }

  const linePhoneVisible = (line: InboxLine, idx = 0) => resolveLinePhone(line, idx)

  const selectedMaxLabel = selectedMaxLine
    ? linePhoneVisible(selectedMaxLine, Math.max(0, maxLines.findIndex((l) => l.id === selectedMaxLine.id)))
    : 'Аккаунт'

  const newChatFromLine =
    maxLines.find((l) => l.id === (maxMode ? lineId || newChatLineId : newChatLineId)) ||
    maxLines.find((l) => l.is_active) ||
    maxLines[0] ||
    null

  const canShowNewChat = canOpenByPhone && (maxMode ? Boolean(lineId) : maxLines.length > 0)

  const newChatButton = canShowNewChat ? (
    <button
      type="button"
      onClick={toggleNewChatForm}
      className={[
        'inline-flex h-8 shrink-0 items-center gap-1.5 rounded-[9px] px-2.5 text-[12px] font-semibold transition',
        newChatOpen
          ? 'bg-[#e8f1ff] text-[#1a6fb5]'
          : 'bg-[#2b7de9] text-white hover:bg-[#246fce]',
      ].join(' ')}
      title="Написать клиенту на новый номер"
    >
      {newChatOpen ? <X size={14} /> : <Plus size={14} />}
      <span>{newChatOpen ? 'Закрыть' : 'Написать'}</span>
    </button>
  ) : null

  const newChatPanel = newChatOpen ? (
    <form
      className="mt-3 flex flex-col gap-3 rounded-[12px] border border-[#cfe3ff] bg-[#f5f9ff] p-3"
      onSubmit={(e) => {
        e.preventDefault()
        void openNewChatByPhone()
      }}
    >
      <div>
        <div className="text-[11px] font-semibold uppercase tracking-wide text-b24-text-muted">
          Наш номер (от кого)
        </div>
        <p className="mt-0.5 text-[11px] text-b24-text-muted">
          Сообщение уйдёт с выбранного корпоративного номера
        </p>
        {maxMode && newChatFromLine ? (
          <div className="mt-1.5 flex items-center gap-2 rounded-[10px] border border-[#2b7de9]/25 bg-white px-3 py-2">
            <span className="inline-flex size-7 items-center justify-center rounded-lg bg-[#efeaff] text-[10px] font-black text-[#7b61ff]">
              M
            </span>
            <div className="min-w-0">
              <div className="truncate text-[13px] font-semibold tabular-nums text-[#333333]">
                {linePhoneVisible(
                  newChatFromLine,
                  Math.max(0, maxLines.findIndex((l) => l.id === newChatFromLine.id)),
                )}
              </div>
              <div className="text-[11px] text-b24-text-muted">Выбран в списке слева</div>
            </div>
          </div>
        ) : maxLines.length > 0 ? (
          <div className="mt-1.5">
            <div className="b24-scrollbar -mx-0.5 flex gap-1.5 overflow-x-auto pb-1 snap-x snap-mandatory">
              {maxLines.map((l, idx) => {
                const active = (newChatLineId || newChatFromLine?.id) === l.id
                return (
                  <button
                    key={l.id}
                    type="button"
                    disabled={newChatBusy || !l.is_active}
                    onClick={() => setNewChatLineId(l.id)}
                    className={[
                      'snap-start shrink-0 rounded-[10px] border px-3 py-2 text-left transition',
                      active
                        ? 'border-[#2b7de9] bg-white shadow-sm ring-1 ring-[#2b7de9]/25'
                        : 'border-black/8 bg-white/80 hover:border-[#2b7de9]/35 hover:bg-white',
                      l.is_active ? '' : 'opacity-50',
                    ].join(' ')}
                  >
                    <span className="block whitespace-nowrap text-[13px] font-semibold tabular-nums text-[#333333]">
                      {linePhoneVisible(l, idx)}
                    </span>
                    <span className="mt-0.5 block whitespace-nowrap text-[10px] text-b24-text-muted">
                      {idx === 0 ? 'По умолчанию' : kindLabel(l.kind)}
                      {!l.is_active ? ' · выкл' : ''}
                    </span>
                  </button>
                )
              })}
            </div>
            {maxLines.length > 2 ? (
              <p className="mt-1 text-[10px] text-b24-text-muted">Листайте вправо — все номера</p>
            ) : null}
          </div>
        ) : (
          <p className="mt-1.5 text-[12px] text-orange-600">Нет подключённых номеров</p>
        )}
      </div>

      <div>
        <label className="text-[11px] font-semibold uppercase tracking-wide text-b24-text-muted">
          Номер клиента (кому)
        </label>
        <input
          value={newChatPhone}
          onChange={(e) => {
            setNewChatPhone(e.target.value)
            if (newChatErr) setNewChatErr('')
          }}
          placeholder="+7 900 000-00-00"
          inputMode="tel"
          autoFocus
          disabled={newChatBusy}
          className="mt-1.5 h-10 w-full rounded-[10px] border border-black/10 bg-white px-3 text-[14px] tabular-nums text-[#333333] caret-[#333333] outline-none placeholder:text-[#a8adb4] focus:border-[#2b7de9]/50 focus:ring-2 focus:ring-[#2b7de9]/20 disabled:opacity-60"
        />
      </div>

      {newChatErr ? (
        <p className="text-[12px] text-orange-600">{newChatErr}</p>
      ) : (
        <p className="text-[11px] leading-snug text-b24-text-muted">
          Откроется новый чат — можно сразу отправить сообщение
        </p>
      )}
      <button
        type="submit"
        disabled={newChatBusy || !(newChatLineId || newChatFromLine?.id || lineId)}
        className="h-10 rounded-[10px] bg-[#2b7de9] text-[13px] font-semibold text-white hover:bg-[#246fce] disabled:opacity-60"
      >
        {newChatBusy ? 'Открываю…' : 'Открыть чат'}
      </button>
    </form>
  ) : null

  const clientWrote = (thread?.messages ?? []).some((m) => m.direction === 'inbound')
  const coldBlocked = Boolean(thread?.cold_limit) && !clientWrote
  const otherMax = maxLines.find((l) => l.id !== thread?.line_id && !l.send_down && !l.cold_limit)
  const otherMaxLabel = otherMax?.operator_label || otherMax?.name

  const chatPanel = thread ? (
    <ChatPanel
      className="h-full min-h-0 rounded-none shadow-none"
      title={titleName}
      subtitle={
        [
          thread.phone ? (hidePhones ? maskPhone(thread.phone) : thread.phone) : null,
          thread.from_phone && thread.from_phone !== thread.phone
            ? `с ${formatMaxPhone(thread.from_phone)}`
            : null,
        ]
          .filter(Boolean)
          .join(' · ') || undefined
      }
      channelBadge={<ChannelGlyph channel={thread.channel} />}
      messages={(thread.messages ?? []).map((m) => ({
        id: m.id,
        dir: m.direction === 'outbound' ? ('out' as const) : ('in' as const),
        body: m.body,
        at: timeShort(m.created_at),
        author: (m.author_label || '').trim() || undefined,
        has_media: Boolean(m.has_media),
        media_kind: m.media_kind,
        media_download: Boolean(m.media_download),
        files_open: Boolean(m.files_open),
        media_filename: m.media_filename,
        media_count: Number(m.media_count || 0),
        media_url: `/api/inbox/messages/${m.id}/media`,
        sent_to_logist: Boolean(m.sent_to_logist),
        delivery_status: m.delivery_status,
      }))}
      draft={draft}
      onDraftChange={setDraft}
      onSend={() => void send()}
      pending={pending}
      error={callErr || undefined}
      placeholder={
        thread.send_down
          ? 'Этот чат с этого номера не продолжится'
          : coldBlocked || maxNotice
            ? 'Новым с этого номера не напишете'
            : 'Написать сообщение...'
      }
      composerDisabled={Boolean(thread.send_down) || coldBlocked || Boolean(maxNotice)}
      composerTop={
        thread.send_down || coldBlocked || maxNotice ? (
          <div className="rounded-lg border border-[#e7c7c7] bg-[#fff6f6] px-2.5 py-2 text-[12px] leading-snug text-[#7a3030]">
            {maxNotice ||
              (thread.send_down
                ? maxDownBanner(thread.from_phone || thread.line_name || '', otherMaxLabel)
                : maxColdBanner(thread.from_phone || thread.line_name || ''))}
          </div>
        ) : null
      }
      emptyText="Нет сообщений"
      allowAttach
      attachFiles={attachFiles}
      onAttachFiles={setAttachFiles}
      hasMoreOlder={hasMoreMessages}
      loadingOlder={loadingOlderMessages}
      onLoadOlder={loadOlderMessages}
      headerRight={
        <div className="flex items-center gap-2">
          {thread.deal_id || thread.lead_id ? (
            <ShadButton
              type="button"
              size="sm"
              variant="outline"
              className="h-8 rounded-[9px] border-[#2b7de9]/40 bg-white px-3 text-[12px] font-semibold text-[#2b7de9] hover:bg-[#e8f1ff]"
              onClick={openCard}
            >
              <ExternalLink className="mr-1 size-3.5" />
              Открыть карточку
            </ShadButton>
          ) : null}
        </div>
      }
      headerBelow={
        <>
          {maxMode && selectedMaxLine ? (
            <div className="flex shrink-0 items-start gap-2 border-b border-[#cfe3ff] bg-[#eaf3ff] px-4 py-2.5 text-[12px] text-[#1a5fb4]">
              <span className="min-w-0 leading-snug">
                Вы просматриваете диалоги корпоративного аккаунта{' '}
                <span className="font-semibold tabular-nums">{selectedMaxLabel}</span>. Все
                сообщения отправляются от имени этого аккаунта.
              </span>
            </div>
          ) : null}
          {thread.deal || thread.lead_title || thread.deal_id || thread.lead_id ? (
            <div className="flex shrink-0 items-center justify-between gap-2 border-b border-[#cfe3ff] bg-[#eaf3ff] px-4 py-2 text-[12px]">
              <span className="truncate font-medium text-b24-text-dark">
                {thread.deal || thread.lead_title || leadMeta?.title || 'Заявка'}
              </span>
              <button
                type="button"
                className="inline-flex shrink-0 items-center gap-1 font-semibold text-[#2b7de9] hover:underline"
                onClick={openCard}
              >
                Перейти в карточку
                <ExternalLink className="size-3" />
              </button>
            </div>
          ) : null}
        </>
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
                .then(() => {
                  setError('')
                  if (!thread?.id) return
                  return api<ThreadDetail>(`/inbox/${thread.id}?limit=${MSG_PAGE}`).then((th) => {
                    setThread((prev) =>
                      prev?.id === th.id
                        ? { ...th, messages: mergeMessages(prev.messages, th.messages) }
                        : th,
                    )
                  })
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
                  if (!thread?.id) return
                  const thr = await api<ThreadDetail>(`/inbox/${thread.id}?limit=${MSG_PAGE}`)
                  setThread((prev) =>
                    prev?.id === thr.id
                      ? { ...thr, messages: mergeMessages(prev.messages, thr.messages) }
                      : thr,
                  )
                })
                .catch(async (e: Error) => {
                  setError(showErr(e.message))
                  if (!thread?.id) return
                  try {
                    const thr = await api<ThreadDetail>(`/inbox/${thread.id}?limit=${MSG_PAGE}`)
                    setThread((prev) =>
                      prev?.id === thr.id
                        ? { ...thr, messages: mergeMessages(prev.messages, thr.messages) }
                        : thr,
                    )
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
            author={msg.author}
            deliveryStatus={msg.delivery_status}
          />
        )
      }
    />
  ) : (
    <div className="flex flex-1 items-center justify-center bg-[#f5f6f8] px-6">
      <div className="flex max-w-[16rem] flex-col items-center gap-3 text-center">
        <div className="flex size-14 items-center justify-center rounded-full bg-white text-[#2b7de9] shadow-sm ring-1 ring-black/5">
          <MessageSquare className="size-6" />
        </div>
        <p className="text-[14px] text-b24-text-muted">Выберите диалог</p>
      </div>
    </div>
  )

  return (
    <PageShell className="h-[calc(100vh-5.5rem)] min-h-[560px]">
      <PageHeader
        title="Мессенджер"
        subtitle="Все диалоги с кандидатами в одном месте"
        actions={
          <>
            <div className="flex rounded-[12px] border border-white/15 bg-white/10 p-1 backdrop-blur-sm">
              {viewTabs.map((t) => (
                <button
                  key={t.id}
                  type="button"
                  onClick={() => setView(t.id)}
                  className={[
                    'h-9 rounded-[9px] px-3.5 text-[13px] font-medium transition',
                    view === t.id
                      ? 'bg-white text-[#1a2332] shadow-sm'
                      : 'text-white/80 hover:text-white hover:bg-white/10',
                  ].join(' ')}
                >
                  {t.label}
                </button>
              ))}
            </div>
          </>
        }
      />

      {view === 'chats' ? (
        <div className="flex flex-wrap items-center gap-2 shrink-0">
          {channelTabs.map((tab) => {
            const active = channel === tab.id
            const count = channelCounts[tab.id] ?? (tab.id === '' ? channelCounts[''] : 0)
            return (
              <button
                key={tab.id || 'all'}
                type="button"
                onClick={() => selectChannel(tab.id)}
                className={[
                  'inline-flex h-9 items-center gap-2 rounded-full border px-3.5 text-[13px] font-semibold transition-all',
                  active
                    ? 'border-[#1e6fd4] bg-[#2b7de9] text-white shadow-[0_4px_14px_rgba(43,125,233,0.32)]'
                    : 'border-black/10 bg-white text-b24-text-dark hover:border-[#2b7de9]/35 hover:bg-[#f5f9ff]',
                ].join(' ')}
              >
                <ChannelGlyph channel={tab.id || 'all'} />
                <span>{tab.label}</span>
                <span
                  className={[
                    'min-w-[1.35rem] rounded-full px-1.5 text-center text-[11px] tabular-nums font-semibold',
                    active ? 'bg-white/20 text-white' : 'bg-[#eef2f6] text-b24-text-muted',
                  ].join(' ')}
                >
                  {count || 0}
                </span>
              </button>
            )
          })}
        </div>
      ) : null}

      {error ? (
        <p className="text-[13px] text-orange-200 shrink-0">{error}</p>
      ) : null}

      {view === 'lines' ? (
        <div className="flex flex-col gap-4 flex-1 min-h-0 overflow-y-auto b24-scrollbar">
          <section className="b24-glass-panel p-4 sm:p-5 flex flex-col gap-3">
            <div className="flex flex-wrap items-end justify-between gap-2">
              <div>
                <h2 className="text-[16px] font-semibold text-b24-text-dark">
                  {canManageAccounts ? 'Подключённые MAX' : 'Мои аккаунты MAX'}
                </h2>
                <p className="mt-0.5 text-[12px] text-b24-text-muted">
                  {canManageAccounts
                    ? `Аккаунты в мессенджере · ${maxLines.length}`
                    : `Привязанные к вам · ${maxLines.length} · видны только последние цифры`}
                </p>
              </div>
            </div>

            {maxLines.length === 0 ? (
              <div className="rounded-[12px] border border-dashed border-black/10 bg-[#f8f9fb] px-4 py-8 text-center text-[13px] text-b24-text-muted">
                {canManageAccounts
                  ? 'Пока ни одного аккаунта — подключите из списка Wappi ниже'
                  : 'Вам ещё не привязали аккаунт MAX — обратитесь к администратору'}
              </div>
            ) : (
              <ul className="divide-y divide-black/[0.05] rounded-[12px] border border-black/6 overflow-hidden bg-white">
                {maxLines.map((line) => {
                  const phone = linePhoneVisible(line)
                  const selected = lineId === line.id
                  return (
                    <li
                      key={line.id}
                      className={[
                        'flex flex-wrap items-center gap-3 px-4 py-3',
                        line.is_active ? '' : 'opacity-60 bg-[#fafafa]',
                        selected ? 'bg-[#eef5ff]' : '',
                      ].join(' ')}
                    >
                      <div className="flex size-10 items-center justify-center rounded-full bg-[#efeaff] text-[#7b61ff] shrink-0">
                        <span className="text-[11px] font-black">M</span>
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="text-[14px] font-semibold tabular-nums text-b24-text-dark">
                          {phone}
                        </div>
                        <div className="text-[12px] text-b24-text-muted truncate">
                          {kindLabel(line.kind)}
                          {line.threads ? ` · ${line.threads} диалогов` : ''}
                          {!line.is_active ? ' · выключен' : ''}
                        </div>
                      </div>
                      {canManageAccounts ? (
                        <>
                          <select
                            value={line.owner_id || ''}
                            onChange={(e) => void setLineOwnerId(line, e.target.value)}
                            onClick={(e) => e.stopPropagation()}
                            className="h-9 max-w-[160px] rounded-[9px] border border-black/10 bg-[#f5f7f9] px-2 text-[12px] text-b24-text-dark"
                          >
                            <option value="">Без владельца</option>
                            {people.map((u) => (
                              <option key={u.id} value={u.id}>
                                {u.full_name}
                              </option>
                            ))}
                          </select>
                          <button
                            type="button"
                            className="h-9 rounded-[9px] px-3 text-[12px] font-medium text-b24-text-muted hover:bg-black/[0.04]"
                            onClick={() => void toggleLine(line)}
                          >
                            {line.is_active ? 'Выкл' : 'Вкл'}
                          </button>
                        </>
                      ) : null}
                      <button
                        type="button"
                        className={[
                          'h-9 rounded-[9px] px-3 text-[12px] font-medium text-white',
                          selected ? 'bg-[#1a6fb5]' : 'bg-[#2b7de9] hover:bg-[#246fce]',
                        ].join(' ')}
                        onClick={() => {
                          selectMaxLine(line.id)
                          setChannel('max')
                          setView('chats')
                        }}
                      >
                        {selected ? 'Выбран' : 'Открыть'}
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}
          </section>

          {canManageAccounts ? (
            <section className="b24-glass-panel p-4 sm:p-5 flex flex-col gap-3">
              <div>
                <h2 className="text-[16px] font-semibold text-b24-text-dark">Добавить из Wappi</h2>
                <p className="mt-0.5 text-[12px] text-b24-text-muted">
                  Выберите номер — остальное подставится само
                  {!wappiOk ? ' · Wappi не настроен' : ''}
                </p>
              </div>

              <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
                <label className="relative flex-1 min-w-[200px]">
                  <Search
                    size={14}
                    className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-b24-text-muted"
                  />
                  <Input
                    value={lineQuery}
                    onChange={(e) => setLineQuery(e.target.value)}
                    placeholder="Поиск номера"
                    className="h-10 border-black/10 bg-[#f5f7f9] pl-9 text-[13px]"
                  />
                </label>
                <div className="inline-flex rounded-[10px] border border-black/8 bg-[#f5f7f9] p-0.5">
                  {(
                    [
                      { id: 'personal' as const, label: 'Личный' },
                      { id: 'pool' as const, label: 'Общий' },
                    ] as const
                  ).map((tab) => (
                    <button
                      key={tab.id}
                      type="button"
                      onClick={() => setLineKind(tab.id)}
                      className={[
                        'h-9 px-3 rounded-[8px] text-[12px] font-medium transition',
                        lineKind === tab.id
                          ? 'bg-white text-b24-text-dark shadow-sm'
                          : 'text-b24-text-muted hover:text-b24-text-dark',
                      ].join(' ')}
                    >
                      {tab.label}
                    </button>
                  ))}
                </div>
                <select
                  value={lineOwner}
                  onChange={(e) => setLineOwner(e.target.value)}
                  className="h-10 min-w-[180px] rounded-[10px] border border-black/10 bg-[#f5f7f9] px-3 text-[13px] text-b24-text-dark"
                >
                  <option value="">Владелец (необязательно)</option>
                  {people.map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.full_name}
                    </option>
                  ))}
                </select>
              </div>

              {availableMax.length === 0 ? (
                <div className="rounded-[12px] border border-dashed border-black/10 bg-[#f8f9fb] px-4 py-8 text-center text-[13px] text-b24-text-muted">
                  {wappiOk
                    ? lineQuery
                      ? 'Ничего не найдено'
                      : 'Все аккаунты Wappi уже подключены'
                    : 'Нет WAPPI_TOKEN — нечего подключать'}
                </div>
              ) : (
                <ul className="divide-y divide-black/[0.05] rounded-[12px] border border-black/6 overflow-hidden bg-white max-h-[420px] overflow-y-auto b24-scrollbar">
                  {availableMax.map((item) => {
                    const key = item.channel_id || item.id
                    const phone = showLinePhone(item.phone || item.label)
                    return (
                      <li key={key} className="flex items-center gap-3 px-4 py-2.5 hover:bg-[#f7fafc]">
                        <div className="flex size-9 items-center justify-center rounded-full bg-[#f0f2f5] text-b24-text-muted shrink-0">
                          <span className="text-[10px] font-black">M</span>
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="text-[14px] font-medium tabular-nums text-b24-text-dark">
                            {phone}
                          </div>
                        </div>
                        <button
                          type="button"
                          disabled={Boolean(connectKey)}
                          onClick={() => void connectMax(item)}
                          className="inline-flex h-8 items-center rounded-[8px] bg-b24-green px-3 text-[12px] font-medium text-white hover:bg-b24-green-hover disabled:opacity-50 shadow-[0_2px_8px_rgba(47,192,110,0.28)]"
                        >
                          {connectKey === key ? '…' : 'Подключить'}
                        </button>
                      </li>
                    )
                  })}
                </ul>
              )}
            </section>
          ) : null}

          {canManageAccounts && otherLines.length > 0 ? (
            <section className="b24-glass-panel p-4 sm:p-5 flex flex-col gap-3">
              <h2 className="text-[15px] font-semibold text-b24-text-dark">Другие каналы</h2>
              <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-2">
                {otherLines.map((line) => (
                  <button
                    key={line.id}
                    type="button"
                    onClick={() => {
                      setLineId(line.id)
                      setChannel(line.channel || '')
                      setView('chats')
                    }}
                    className={[
                      'rounded-[12px] border border-black/6 bg-white text-left px-3 py-3 hover:bg-[#f7fafc]',
                      line.is_active ? '' : 'opacity-55',
                    ].join(' ')}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-[13px] font-semibold text-b24-text-dark truncate">
                        {line.name}
                      </span>
                      <span className="text-[10px] font-semibold uppercase tracking-wide text-b24-cyan shrink-0">
                        {chLabel(line.channel)}
                      </span>
                    </div>
                    <div className="mt-1 text-[12px] text-b24-text-muted truncate">
                      {line.provider} · {kindLabel(line.kind)}
                      {line.owner ? ` · ${line.owner}` : ''}
                    </div>
                  </button>
                ))}
              </div>
            </section>
          ) : null}
        </div>
      ) : maxMode ? (
        <div className="grid grid-cols-1 gap-0 flex-1 min-h-0 overflow-hidden rounded-[16px] bg-white shadow-[0_12px_40px_rgba(10,8,40,0.18)] lg:grid-cols-[minmax(260px,300px)_minmax(280px,340px)_minmax(0,1fr)]">
          {/* MAX: аккаунты */}
          <div className="flex min-h-0 flex-col border-r border-black/6 bg-white">
            <div className="shrink-0 border-b border-black/6 px-4 py-3">
              <div className="flex items-center gap-2">
                <div className="flex size-9 items-center justify-center rounded-lg bg-[#efeaff] text-[#7b61ff]">
                  <span className="text-[11px] font-black">M</span>
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[14px] font-semibold tabular-nums text-b24-text-dark">
                    {selectedMaxLabel}
                  </div>
                  <div className="text-[11px] text-b24-text-muted">Корпоративный аккаунт</div>
                </div>
                <MaxGuidesInfoButton
                  onOpenWork={() => {
                    chainReasonsAfterWorkRef.current = false
                    setMaxGuide('work')
                  }}
                  onOpenReasons={() => {
                    chainReasonsAfterWorkRef.current = false
                    setMaxGuide('reasons')
                  }}
                />
              </div>
              <div className="mt-3 text-[12px] font-semibold text-b24-text-dark">
                Мои аккаунты
                <span className="ml-1.5 rounded-full bg-[#eef2f6] px-1.5 py-0.5 text-[11px] tabular-nums text-b24-text-muted">
                  {maxLines.length}
                </span>
              </div>
            </div>
            <div className="relative shrink-0 border-b border-black/6 px-3 py-2">
              <Search
                size={14}
                className="pointer-events-none absolute left-5 top-1/2 -translate-y-1/2 text-b24-text-muted"
              />
              <Input
                value={accountQ}
                onChange={(e) => setAccountQ(e.target.value)}
                placeholder="Поиск по аккаунтам..."
                className="h-8 border-black/8 bg-[#f5f6f8] pl-8 text-[12px]"
              />
            </div>
            <div className="b24-scrollbar min-h-0 flex-1 overflow-y-auto">
              {filteredMaxLines.length === 0 ? (
                <div className="px-4 py-10 text-center text-[13px] text-b24-text-muted">
                  {maxLines.length === 0 ? 'Нет привязанных аккаунтов MAX' : 'Ничего не найдено'}
                </div>
              ) : (
                <ul>
                  {filteredMaxLines.map((line) => {
                    const label = linePhoneVisible(line)
                    const active = lineId === line.id
                    const hasDialogs = Number(line.threads || 0) > 0
                    return (
                      <li key={line.id}>
                        <button
                          type="button"
                          onClick={() => selectMaxLine(line.id)}
                          className={[
                            'flex w-full items-center gap-3 border-l-4 px-3 py-3 text-left transition-colors',
                            active
                              ? 'border-[#2b7de9] bg-[#e8f1ff]'
                              : 'border-transparent hover:bg-[#f7f8fa]',
                            line.is_active ? '' : 'opacity-55',
                          ].join(' ')}
                        >
                          <div className="relative shrink-0">
                            <div className="flex size-10 items-center justify-center rounded-lg bg-[#efeaff] text-[#7b61ff]">
                              <span className="text-[11px] font-black">M</span>
                            </div>
                            {hasDialogs ? (
                              <span className="absolute -bottom-0.5 -right-0.5 size-2.5 rounded-full bg-[#2fc06e] ring-2 ring-white" />
                            ) : null}
                          </div>
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-[13px] font-semibold tabular-nums text-b24-text-dark">
                              {label}
                            </div>
                            <div className="truncate text-[11px] text-b24-text-muted">
                              Корпоративный аккаунт
                              {hasDialogs ? ` · ${line.threads}` : ''}
                            </div>
                          </div>
                          <ChevronRight size={16} className="shrink-0 text-b24-text-muted/70" />
                        </button>
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>
          </div>

          {/* MAX: диалоги выбранного аккаунта */}
          <div className="flex min-h-0 flex-col border-r border-black/6 bg-white">
            <div className="shrink-0 border-b border-black/6 px-4 py-3">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="text-[14px] font-semibold text-b24-text-dark">
                    Диалоги аккаунта
                  </div>
                  <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px] text-b24-text-muted">
                    <span className="tabular-nums">{selectedMaxLabel}</span>
                    <span>·</span>
                    <span className="tabular-nums">
                      {(data?.items ?? []).length}
                      {hasMoreThreads ? '+' : ''}{' '}
                      {(data?.items ?? []).length === 1 ? 'диалог' : 'диалогов'}
                    </span>
                  </div>
                </div>
                {newChatButton}
              </div>
              {newChatPanel}
            </div>
            <div className="flex shrink-0 items-center gap-4 border-b border-black/6 px-4 pt-2">
              {(
                [
                  { id: 'all' as const, label: 'Все' },
                  {
                    id: 'unread' as const,
                    label: unreadTotal > 0 ? `Непрочитанные ${unreadTotal}` : 'Непрочитанные',
                  },
                ] as const
              ).map((tab) => {
                const active = listFilter === tab.id
                return (
                  <button
                    key={tab.id}
                    type="button"
                    onClick={() => setListFilter(tab.id)}
                    className={[
                      'relative pb-2 text-[13px] font-semibold transition-colors',
                      active ? 'text-[#2b7de9]' : 'text-b24-text-muted hover:text-b24-text-dark',
                    ].join(' ')}
                  >
                    {tab.label}
                    {active ? (
                      <span className="absolute inset-x-0 -bottom-px h-0.5 rounded-full bg-[#2b7de9]" />
                    ) : null}
                  </button>
                )
              })}
            </div>
            <div className="relative shrink-0 border-b border-black/6 px-3 py-2">
              <Search
                size={14}
                className="pointer-events-none absolute left-5 top-1/2 -translate-y-1/2 text-b24-text-muted"
              />
              <Input
                value={q}
                onChange={(e) => setQ(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') void load()
                }}
                placeholder="Поиск по диалогам..."
                className="h-8 border-black/8 bg-[#f5f6f8] pl-8 text-[12px] text-b24-text-dark placeholder:text-b24-text-muted"
              />
            </div>
            <div
              className="b24-scrollbar min-h-0 flex-1 overflow-y-auto"
              onScroll={onThreadsScroll}
            >
              {!lineId ? (
                <div className="px-4 py-14 text-center text-[13px] text-b24-text-muted">
                  Выберите аккаунт слева
                </div>
              ) : threads.length === 0 ? (
                <div className="px-4 py-14 text-center text-[13px] text-b24-text-muted">
                  {canOpenByPhone ? (
                    <>
                      Нет диалогов.{' '}
                      <button
                        type="button"
                        className="font-semibold text-[#2b7de9] hover:underline"
                        onClick={toggleNewChatForm}
                      >
                        Написать на новый номер
                      </button>
                    </>
                  ) : (
                    'Нет диалогов с этого аккаунта'
                  )}
                </div>
              ) : (
                <ul>
                  {threads.map((t) => {
                    const name = threadName(t)
                    const selected = openId === t.id
                    const flashing = Boolean(flashIds[t.id])
                    return (
                      <li key={t.id}>
                        <button
                          type="button"
                          onClick={() => open(t.id)}
                          className={[
                            'flex w-full gap-3 border-l-4 px-3 py-3 text-left transition-colors',
                            selected
                              ? 'border-[#2b7de9] bg-[#e8f1ff]'
                              : 'border-transparent hover:bg-[#f7f8fa]',
                            flashing && !selected ? 'animate-pulse bg-emerald-50' : '',
                          ].join(' ')}
                        >
                          <div className="relative shrink-0">
                            <div className="flex size-11 items-center justify-center rounded-full bg-[#e8eef3] text-[13px] font-semibold text-b24-text-dark">
                              {initials(name)}
                            </div>
                          </div>
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center justify-between gap-2">
                              <span className="truncate text-[13px] font-semibold text-b24-text-dark">
                                {name}
                              </span>
                              <span className="shrink-0 text-[11px] tabular-nums text-b24-text-muted">
                                {timeShort(t.last_at)}
                              </span>
                            </div>
                            <div className="mt-0.5 flex items-center gap-2">
                              <span className="min-w-0 flex-1 truncate text-[12px] text-b24-text-muted">
                                {t.last_body || '—'}
                              </span>
                              {t.unread > 0 ? (
                                <Badge className="h-[18px] min-w-[18px] shrink-0 rounded-full bg-[#e5484d] px-1 text-[10px] text-white hover:bg-[#e5484d]">
                                  {t.unread > 99 ? '99+' : t.unread}
                                </Badge>
                              ) : null}
                            </div>
                          </div>
                        </button>
                      </li>
                    )
                  })}
                  {loadingMoreThreads ? (
                    <li className="px-3 py-3 text-center text-[12px] text-b24-text-muted">
                      Загрузка…
                    </li>
                  ) : null}
                </ul>
              )}
            </div>
          </div>

          {/* MAX: чат */}
          <div className="flex min-h-0 flex-col overflow-hidden">{chatPanel}</div>
        </div>
      ) : (
        <div
          className={[
            'grid grid-cols-1 gap-0 flex-1 min-h-0 overflow-hidden rounded-[16px] bg-white shadow-[0_12px_40px_rgba(10,8,40,0.18)]',
            thread
              ? 'lg:grid-cols-[minmax(300px,340px)_minmax(0,1fr)_minmax(280px,320px)]'
              : 'lg:grid-cols-[minmax(300px,340px)_minmax(0,1fr)]',
          ].join(' ')}
        >
          {/* Общий мессенджер: список */}
          <div className="flex min-h-0 flex-col border-r border-black/6 bg-white">
            <div className="shrink-0 border-b border-black/6 px-4 pt-3 pb-0">
              <div className="flex items-start justify-between gap-2">
                <div className="flex min-w-0 flex-1 items-center gap-4">
                  {(
                    [
                      { id: 'all' as const, label: 'Все' },
                      {
                        id: 'unread' as const,
                        label: unreadTotal > 0 ? `Непрочитанные ${unreadTotal}` : 'Непрочитанные',
                      },
                    ] as const
                  ).map((tab) => {
                    const active = listFilter === tab.id
                    return (
                      <button
                        key={tab.id}
                        type="button"
                        onClick={() => setListFilter(tab.id)}
                        className={[
                          'relative pb-2.5 text-[13px] font-semibold transition-colors',
                          active ? 'text-[#2b7de9]' : 'text-b24-text-muted hover:text-b24-text-dark',
                        ].join(' ')}
                      >
                        {tab.label}
                        {active ? (
                          <span className="absolute inset-x-0 -bottom-px h-0.5 rounded-full bg-[#2b7de9]" />
                        ) : null}
                      </button>
                    )
                  })}
                </div>
                <div className="pb-1.5">{newChatButton}</div>
              </div>
              {newChatPanel ? <div className="pb-3">{newChatPanel}</div> : null}
            </div>

            <div className="relative shrink-0 border-b border-black/6 px-3 py-2">
              <Search
                size={14}
                className="pointer-events-none absolute left-5 top-1/2 -translate-y-1/2 text-b24-text-muted"
              />
              <Input
                value={q}
                onChange={(e) => setQ(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') void load()
                }}
                placeholder="Поиск по чатам"
                className="h-8 border-black/8 bg-[#f5f6f8] pl-8 text-[12px] text-b24-text-dark placeholder:text-b24-text-muted"
              />
            </div>

            <div
              className="b24-scrollbar min-h-0 flex-1 overflow-y-auto"
              onScroll={onThreadsScroll}
            >
              {threads.length === 0 ? (
                <div className="px-4 py-14 text-center text-[13px] text-b24-text-muted">
                  {canShowNewChat ? (
                    <>
                      Нет диалогов.{' '}
                      <button
                        type="button"
                        className="font-semibold text-[#2b7de9] hover:underline"
                        onClick={toggleNewChatForm}
                      >
                        Написать на новый номер
                      </button>
                    </>
                  ) : (
                    'Нет диалогов'
                  )}
                </div>
              ) : (
                <ul>
                  {threads.map((t) => {
                    const name = threadName(t)
                    const selected = openId === t.id
                    const flashing = Boolean(flashIds[t.id])
                    return (
                      <li key={t.id}>
                        <button
                          type="button"
                          onClick={() => open(t.id)}
                          className={[
                            'flex w-full gap-3 border-l-4 px-3 py-3 text-left transition-colors',
                            selected
                              ? 'border-[#2b7de9] bg-[#e8f1ff]'
                              : 'border-transparent hover:bg-[#f7f8fa]',
                            flashing && !selected ? 'animate-pulse bg-emerald-50' : '',
                          ].join(' ')}
                        >
                          <div className="relative shrink-0">
                            <div className="flex size-11 items-center justify-center rounded-full bg-[#e8eef3] text-[13px] font-semibold text-b24-text-dark">
                              {initials(name)}
                            </div>
                            <span className="absolute -bottom-0.5 -right-0.5 flex size-4 items-center justify-center rounded-full bg-white ring-1 ring-black/5">
                              <ChannelGlyph channel={t.channel} className="size-2.5" />
                            </span>
                          </div>
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center justify-between gap-2">
                              <span className="truncate text-[13px] font-semibold text-b24-text-dark">
                                {name}
                              </span>
                              <span className="shrink-0 text-[11px] tabular-nums text-b24-text-muted">
                                {timeShort(t.last_at)}
                              </span>
                            </div>
                            <div className="mt-0.5 flex items-center gap-2">
                              <span className="min-w-0 flex-1 truncate text-[12px] text-b24-text-muted">
                                {t.last_body || '—'}
                              </span>
                              {t.unread > 0 ? (
                                <Badge className="h-[18px] min-w-[18px] shrink-0 rounded-full bg-[#2b7de9] px-1 text-[10px] text-white hover:bg-[#2b7de9]">
                                  {t.unread > 99 ? '99+' : t.unread}
                                </Badge>
                              ) : null}
                            </div>
                          </div>
                        </button>
                      </li>
                    )
                  })}
                  {loadingMoreThreads ? (
                    <li className="px-3 py-3 text-center text-[12px] text-b24-text-muted">
                      Загрузка…
                    </li>
                  ) : null}
                </ul>
              )}
            </div>
          </div>

          {/* Общий мессенджер: чат */}
          <div
            className={['flex min-h-0 flex-col overflow-hidden', thread ? 'border-r border-black/6' : ''].join(
              ' ',
            )}
          >
            {chatPanel}
          </div>

          {/* Кандидат — только в общем мессенджере */}
          {thread ? (
            <div className="hidden min-h-0 lg:flex">
              <InboxCandidatePanel
                candidate={{
                  name: titleName || 'Кандидат',
                  phone: thread.phone,
                  channelLabel: chLabel(thread.channel),
                  channelGlyph: <ChannelGlyph channel={thread.channel} />,
                  leadTitle: thread.deal || thread.lead_title || leadMeta?.title || undefined,
                  owner: leadMeta?.owner,
                  source: leadMeta?.source,
                  stage: leadMeta?.stage_short || leadMeta?.stage || undefined,
                  leadId: thread.lead_id,
                  dealId: thread.deal_id,
                }}
                hidePhones={hidePhones}
                callDisabled={!configReady || callBusy || (!thread.phone && !thread.lead_id)}
                callPending={callBusy}
                onOpenCard={openCard}
                onCall={() => void callCandidate()}
              />
            </div>
          ) : null}
        </div>
      )}

      <MaxWorkGuideModal
        open={maxGuide === 'work'}
        accountsCount={maxLines.length}
        onClose={() => confirmMaxGuide('work', false)}
        onConfirm={(dont) => confirmMaxGuide('work', dont)}
      />
      <MaxReasonsGuideModal
        open={maxGuide === 'reasons'}
        onClose={() => setMaxGuide(null)}
        onConfirm={(dont) => confirmMaxGuide('reasons', dont)}
      />
    </PageShell>
  )
}

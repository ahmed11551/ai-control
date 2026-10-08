export type SupportFile = { id: string; name: string; mime: string; kind: string; size?: number }

export type SupportReaction = { emoji: string; count: number; mine: boolean; names?: string[] }

export type SupportMessage = {
  id: string
  body: string
  created_at: string
  author_id: string
  author: string
  mine: boolean
  kind?: 'message' | 'system' | string
  client_id?: string
  files: SupportFile[]
  reactions: SupportReaction[]
  /** Только на клиенте: сообщение ещё не подтверждено сервером. */
  pending?: boolean
  /** Только на клиенте: текст ошибки, если не ушло. */
  failed?: string
  /** Только на клиенте: вложения и исходный текст для повторной отправки. */
  localFiles?: File[]
  localText?: string
}

export type SupportThread = {
  id: string
  user_id?: string
  name?: string
  department?: string
  role?: string
  last_body?: string
  last_at?: string | null
  unread?: number
  admin_unread?: number
  resolved_at?: string | null
}

export type SupportPageResponse = {
  thread?: SupportThread | null
  messages?: SupportMessage[]
  has_more?: boolean
}

export const SUPPORT_PAGE_SIZE = 40
export const SUPPORT_MAX_FILES = 8
export const SUPPORT_MAX_FILE_BYTES = 20 << 20

export const SUPPORT_REACTIONS = ['👍', '✅', '👀'] as const

export const SUPPORT_QUICK_REPLIES = [
  'Принято, смотрю',
  'Исправили, проверьте, пожалуйста',
  'Пришлите, пожалуйста, скриншот',
  'Уточните, в какой карточке это случилось?',
  'Обновите страницу (Cmd+R) и попробуйте ещё раз',
]

export function newClientId() {
  const c = globalThis.crypto
  if (c && typeof c.randomUUID === 'function') return `c_${c.randomUUID().replace(/-/g, '')}`
  return `c_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 12)}`
}

export function isConfirmed(m: SupportMessage) {
  return !m.pending && !m.failed && !m.id.startsWith('local-')
}

/** Склеивает пришедшие сообщения с уже показанными: без дублей по id и client_id. */
export function mergeMessages(prev: SupportMessage[], incoming: SupportMessage[]) {
  if (incoming.length === 0) return prev
  const next = prev.slice()
  for (const m of incoming) {
    const byId = next.findIndex((x) => x.id === m.id)
    if (byId >= 0) {
      next[byId] = { ...m }
      continue
    }
    const byClient = m.client_id ? next.findIndex((x) => x.client_id === m.client_id) : -1
    if (byClient >= 0) {
      next[byClient] = { ...m }
      continue
    }
    next.push(m)
  }
  const confirmed = next.filter(isConfirmed)
  const local = next.filter((m) => !isConfirmed(m))
  confirmed.sort((a, b) => {
    const d = new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
    return d !== 0 ? d : a.id < b.id ? -1 : a.id > b.id ? 1 : 0
  })
  return [...confirmed, ...local]
}

export function initials(name?: string) {
  const parts = String(name || '')
    .trim()
    .split(/\s+/)
    .filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

const AVATAR_COLORS = ['#1f6feb', '#8250df', '#bf3989', '#cf222e', '#bc4c00', '#1a7f37', '#0969da', '#6639ba']

export function avatarColor(seed?: string) {
  const s = String(seed || '')
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
  return AVATAR_COLORS[Math.abs(h) % AVATAR_COLORS.length]
}

function sameDay(a: Date, b: Date) {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}

export function dayKey(iso?: string) {
  const d = new Date(iso || '')
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

export function dayLabel(iso?: string) {
  const d = new Date(iso || '')
  if (Number.isNaN(d.getTime())) return ''
  const now = new Date()
  if (sameDay(d, now)) return 'Сегодня'
  const y = new Date(now)
  y.setDate(now.getDate() - 1)
  if (sameDay(d, y)) return 'Вчера'
  return d.toLocaleDateString('ru-RU', {
    day: 'numeric',
    month: 'long',
    ...(d.getFullYear() !== now.getFullYear() ? { year: 'numeric' } : {}),
  })
}

export function clockTime(iso?: string) {
  const d = new Date(iso || '')
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
}

export function fullTime(iso?: string) {
  const d = new Date(iso || '')
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' })
}

export function listTime(iso?: string | null) {
  const d = new Date(iso || '')
  if (Number.isNaN(d.getTime())) return ''
  const now = new Date()
  if (sameDay(d, now)) return clockTime(iso || '')
  const y = new Date(now)
  y.setDate(now.getDate() - 1)
  if (sameDay(d, y)) return 'вчера'
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit' })
}

export function fileSize(bytes?: number) {
  const n = Number(bytes) || 0
  if (n <= 0) return ''
  if (n < 1024) return `${n} Б`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} КБ`
  return `${(n / 1024 / 1024).toFixed(n < 10 * 1024 * 1024 ? 1 : 0)} МБ`
}

export function canUseSupport(role?: string) {
  const r = String(role || '').toLowerCase()
  return r === 'admin' || r === 'rop' || r === 'manager' || r === 'sales'
}

const DRAFT_PREFIX = 'support_draft:'

export function loadDraft(userId: string, threadId: string) {
  try {
    return localStorage.getItem(`${DRAFT_PREFIX}${userId}:${threadId || 'mine'}`) || ''
  } catch {
    return ''
  }
}

export function saveDraft(userId: string, threadId: string, text: string) {
  try {
    const key = `${DRAFT_PREFIX}${userId}:${threadId || 'mine'}`
    if (text) localStorage.setItem(key, text)
    else localStorage.removeItem(key)
  } catch {
    /* storage full or disabled */
  }
}


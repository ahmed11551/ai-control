import type { CallHistoryItem } from './types'

const KEY = 'b24-isolation-call-history'
let activeUserId = ''

export function setCallHistoryUser(userId: string) {
  activeUserId = String(userId || '').trim()
}

function storageKey() {
  return activeUserId ? `${KEY}:${activeUserId}` : `${KEY}:anon`
}

export function loadCallHistory(): CallHistoryItem[] {
  try {
    const raw = localStorage.getItem(storageKey())
    if (!raw) return []
    const parsed = JSON.parse(raw) as CallHistoryItem[]
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

export function saveCallHistory(items: CallHistoryItem[]) {
  localStorage.setItem(storageKey(), JSON.stringify(items.slice(0, 100)))
}

export function pushCallHistory(item: CallHistoryItem) {
  const next = [item, ...loadCallHistory()].slice(0, 100)
  saveCallHistory(next)
  return next
}

export function formatDuration(sec: number) {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

type ServerCall = {
  id?: string
  direction?: string
  from_number?: string
  to_number?: string
  duration_sec?: number
  disposition?: string
  started_at?: string
}

export function historyFromServerCalls(calls: ServerCall[]): CallHistoryItem[] {
  return (calls || []).slice(0, 20).map((c) => {
    const dir = String(c.direction || '').toLowerCase()
    const incoming = dir.includes('in')
    const peer = incoming
      ? String(c.from_number || c.to_number || '')
      : String(c.to_number || c.from_number || '')
    const disp = String(c.disposition || '').toUpperCase()
    let status: CallHistoryItem['status'] = 'failed'
    if (disp.includes('ANSWER') && !disp.includes('NO')) status = 'answered'
    else if (disp.includes('NO') || disp.includes('MISS') || disp.includes('REACH')) status = 'missed'
    else if (disp.includes('CANCEL')) status = 'cancelled'
    return {
      id: String(c.id || `${peer}-${c.started_at}`),
      direction: incoming ? 'incoming' : 'outgoing',
      peer,
      startedAt: String(c.started_at || ''),
      durationSec: Number(c.duration_sec) || 0,
      status,
    }
  })
}

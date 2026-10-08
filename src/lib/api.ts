export const TOKEN_KEY = 'forge_token'

export function token() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(value: string | null) {
  if (value) localStorage.setItem(TOKEN_KEY, value)
  else localStorage.removeItem(TOKEN_KEY)
}

/** WebSocket URL with JWT (?token=) — /ws is outside authMW. */
export function wsURL() {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  const t = token()
  const q = t ? `?token=${encodeURIComponent(t)}` : ''
  return `${proto}://${window.location.host}/ws${q}`
}

export async function api<T = unknown>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  // FormData: не ставим Content-Type — браузер сам добавит boundary
  if (!headers.has('Content-Type') && init.body && !(init.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  const t = token()
  if (t) headers.set('Authorization', `Bearer ${t}`)

  const res = await fetch(`/api${path}`, { ...init, headers })
  const data = (await res.json().catch(() => ({}))) as { error?: string }

  if (res.status === 401) {
    setToken(null)
    if (!path.startsWith('/auth/')) {
      window.location.assign('/login')
    }
  }
  if (res.status === 403 && data.error === 'Доступ только из офиса' && !path.startsWith('/auth/')) {
    setToken(null)
    window.location.assign('/login?office=1')
  }
  if (
    res.status === 403 &&
    data.error === 'Отдел временно отключён администратором' &&
    !path.startsWith('/auth/')
  ) {
    setToken(null)
    window.location.assign('/login?dept=1')
  }

  if (!res.ok) {
    throw new Error(data.error || res.statusText)
  }
  return data as T
}

export async function apiDownload(path: string, filename = 'file.bin'): Promise<void> {
  const headers = new Headers()
  const t = token()
  if (t) headers.set('Authorization', `Bearer ${t}`)
  const res = await fetch(`/api${path}`, { headers })
  if (res.status === 401) {
    setToken(null)
    window.location.assign('/login')
    throw new Error('unauthorized')
  }
  if (!res.ok) {
    const data = (await res.json().catch(() => ({}))) as { error?: string }
    throw new Error(data.error || res.statusText)
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

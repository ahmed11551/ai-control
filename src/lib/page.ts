export type Page<T> = {
  items: T[]
  total: number
  limit: number
  offset: number
  overdue?: number
  commentTotal?: number
}

export function qs(params: Record<string, string | number | undefined | null>) {
  const p = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue
    p.set(key, String(value))
  }
  const s = p.toString()
  return s ? `?${s}` : ''
}

export function pageRange(total: number, offset: number, limit: number) {
  if (total <= 0) return 'Нет записей'
  const from = offset + 1
  const to = Math.min(offset + limit, total)
  return `${from}–${to} из ${total.toLocaleString('ru-RU')}`
}

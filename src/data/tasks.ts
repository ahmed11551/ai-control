export type TaskStatus = 'in_progress' | 'completed'

export type TaskKind = 'task' | 'missed_call' | string

export type Task = {
  id: string
  title: string
  status: TaskStatus
  deadline?: string
  plannedDate?: string
  creator: string
  assignee: string
  helpers: string[]
  watchers: string[]
  project?: string
  tags: string[]
  commentCount: number
  createdAt: string
  activityAt: string
  isImportant?: boolean
  kind?: TaskKind
  blockWork?: boolean
  entityType?: string
  entityId?: string
  priority?: string
}

export type CreateTaskInput = {
  title: string
  deadline?: string
  plannedDate?: string
  assignee?: string
  tags?: string[]
  project?: string
  isImportant?: boolean
  kind?: TaskKind
  entityType?: string
  entityId?: string
}

export type AttentionItem = {
  id: string
  title: string
  kind: TaskKind
  isImportant: boolean
  blockWork: boolean
  deadline?: string
  entityType?: string
  entityId?: string
  /** Заголовок лида/сделки — чтобы понять, с кем связаться */
  entityLabel?: string
  resultNote?: string
  attentionType: 'important' | 'missed_call'
}

export type TaskResolveResult =
  | 'done'
  | 'not_done'
  | 'no_answer'
  | 'reschedule'
  | 'other'

export const TASK_RESOLVE_OPTIONS: { value: TaskResolveResult; label: string }[] = [
  { value: 'done', label: 'Задача выполнена' },
  { value: 'not_done', label: 'Задача не выполнена' },
  { value: 'no_answer', label: 'Не ответил' },
  { value: 'reschedule', label: 'Перенести' },
  { value: 'other', label: 'Иное' },
]

export type DeadlineBucket =
  | 'overdue'
  | 'today'
  | 'this_week'
  | 'next_week'
  | 'later'
  | 'none'

export const DEFAULT_CREATOR = 'Админ Изоляции'

export function createTaskId() {
  return `task_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`
}

export function startOfDay(date = new Date()) {
  const d = new Date(date)
  d.setHours(0, 0, 0, 0)
  return d
}

export function endOfDay(date = new Date()) {
  const d = new Date(date)
  d.setHours(23, 59, 59, 999)
  return d
}

export function addDays(date: Date, days: number) {
  const d = new Date(date)
  d.setDate(d.getDate() + days)
  return d
}

export function toDateKey(date: Date | string) {
  const d = typeof date === 'string' ? parseDateInput(date) : date
  if (!d) return ''
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

export function parseDateInput(value?: string) {
  if (!value) return null
  // Prefer YYYY-MM-DD local parse
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    const [y, m, d] = value.split('-').map(Number)
    return new Date(y, m - 1, d)
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  return startOfDay(date)
}

export function isSameDay(a: Date | string, b: Date | string) {
  const da = typeof a === 'string' ? parseDateInput(a) : startOfDay(a)
  const db = typeof b === 'string' ? parseDateInput(b) : startOfDay(b)
  if (!da || !db) return false
  return toDateKey(da) === toDateKey(db)
}

/** Monday-based week: Mon=0 ... Sun=6 */
export function getWeekdayMon(date: Date) {
  return (date.getDay() + 6) % 7
}

export function startOfWeek(date = new Date()) {
  const d = startOfDay(date)
  return addDays(d, -getWeekdayMon(d))
}

export function endOfWeek(date = new Date()) {
  return endOfDay(addDays(startOfWeek(date), 6))
}

export function getWeekRange(date = new Date()) {
  const start = startOfWeek(date)
  const end = endOfWeek(date)
  return { start, end }
}

export function formatTaskDate(iso?: string) {
  if (!iso) return '—'
  const date = parseDateInput(iso) ?? new Date(iso)
  if (Number.isNaN(date.getTime())) return '—'
  const hasTime = /T\d{2}:\d{2}/.test(iso) || /\d{2}:\d{2}/.test(iso)
  if (hasTime && !/^\d{4}-\d{2}-\d{2}$/.test(iso)) {
    return date.toLocaleString('ru-RU', {
      day: '2-digit',
      month: '2-digit',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  }
  return date.toLocaleDateString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
  })
}

export function toDatetimeLocalValue(iso?: string) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function formatShortDay(date: Date) {
  return date.toLocaleDateString('ru-RU', {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
  })
}

export function formatMonthYear(date: Date) {
  const label = date.toLocaleDateString('ru-RU', { month: 'long', year: 'numeric' })
  return label.charAt(0).toUpperCase() + label.slice(1)
}

export function formatActivity(iso: string) {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return '—'
  const now = Date.now()
  const diff = now - date.getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'только что'
  if (mins < 60) return `${mins} мин назад`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours} ч назад`
  return formatTaskDate(iso)
}

export function isTaskOverdue(task: Task) {
  if (!task.deadline || task.status === 'completed') return false
  if (/^\d{4}-\d{2}-\d{2}$/.test(task.deadline)) {
    const end = parseDateInput(task.deadline)
    if (!end) return false
    return endOfDay(end).getTime() < Date.now()
  }
  const d = new Date(task.deadline)
  if (Number.isNaN(d.getTime())) return false
  return d.getTime() < Date.now()
}

export function getDeadlineBucket(task: Task, now = new Date()): DeadlineBucket {
  if (!task.deadline) return 'none'
  if (task.status !== 'completed' && isTaskOverdue(task)) return 'overdue'

  const deadline = parseDateInput(task.deadline)
  if (!deadline) return 'none'

  const today = startOfDay(now)
  if (isSameDay(deadline, today)) return 'today'

  const thisWeek = getWeekRange(today)
  if (deadline >= thisWeek.start && deadline <= thisWeek.end) return 'this_week'

  const nextWeekStart = addDays(thisWeek.end, 1)
  const nextWeekEnd = endOfWeek(nextWeekStart)
  if (deadline >= startOfDay(nextWeekStart) && deadline <= nextWeekEnd) return 'next_week'

  return 'later'
}

export function deadlineForBucket(bucket: DeadlineBucket, now = new Date()): string | undefined {
  const today = startOfDay(now)
  switch (bucket) {
    case 'overdue':
      return toDateKey(addDays(today, -1))
    case 'today':
      return toDateKey(today)
    case 'this_week': {
      const fri = addDays(startOfWeek(today), 4)
      return toDateKey(fri < today ? today : fri)
    }
    case 'next_week':
      return toDateKey(addDays(startOfWeek(today), 9))
    case 'later':
      return toDateKey(addDays(today, 21))
    case 'none':
      return undefined
  }
}

export function getGanttRange(task: Task) {
  const planned = parseDateInput(task.plannedDate)
  const created = parseDateInput(toDateKey(new Date(task.createdAt))) ?? startOfDay()
  const start = planned ?? created
  const deadline = parseDateInput(task.deadline)
  const end = deadline ?? addDays(start, 3)
  return {
    start: startOfDay(start),
    end: endOfDay(end < start ? addDays(start, 1) : end),
  }
}

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import {
  type AttentionItem,
  type CreateTaskInput,
  type Task,
  type TaskResolveResult,
  type TaskStatus,
} from '../data/tasks'
import { api } from '../lib/api'
import { type Page, qs } from '../lib/page'

const LOCAL_KEY = 'b24-isolation-tasks'
const MIGRATED_KEY = 'b24-isolation-tasks-migrated'

type TasksContextValue = {
  list: Task[]
  total: number
  overdue: number
  commentTotal: number
  limit: number
  offset: number
  query: string
  status: string
  role: string
  loading: boolean
  error: string
  attention: AttentionItem[]
  attentionBlocked: boolean
  attentionLoading: boolean
  setQuery: (q: string) => void
  setStatus: (s: string) => void
  setRole: (s: string) => void
  setOffset: (n: number) => void
  loadById: (id: string) => Promise<Task | undefined>
  add: (input: CreateTaskInput) => Promise<Task>
  update: (id: string, patch: Partial<Omit<Task, 'id'>>) => Promise<void>
  remove: (id: string) => Promise<void>
  toggleComplete: (id: string) => Promise<void>
  getById: (id: string) => Task | undefined
  reload: () => Promise<void>
  reloadAttention: () => Promise<void>
  resolve: (id: string, payload: {
    result: TaskResolveResult
    note?: string
    rescheduleAt?: string
  }) => Promise<void>
}

const TasksContext = createContext<TasksContextValue | null>(null)

function parseTags(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String).filter(Boolean)
  if (typeof value === 'string') {
    try {
      const parsed = JSON.parse(value) as unknown
      return Array.isArray(parsed) ? parsed.map(String).filter(Boolean) : []
    } catch {
      return value
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean)
    }
  }
  return []
}

function fromApi(row: Record<string, unknown>): Task {
  const status: TaskStatus = row.status === 'completed' ? 'completed' : 'in_progress'
  const deadline = typeof row.deadline === 'string' && row.deadline ? row.deadline : undefined
  const planned =
    (typeof row.plannedDate === 'string' && row.plannedDate) ||
    (typeof row.planned_date === 'string' && row.planned_date) ||
    undefined
  return {
    id: String(row.id),
    title: String(row.title ?? ''),
    status,
    deadline,
    plannedDate: planned || undefined,
    creator: String(row.creator ?? ''),
    assignee: String(row.assignee ?? ''),
    helpers: parseTags(row.helpers),
    watchers: parseTags(row.watchers),
    project: row.project ? String(row.project) : undefined,
    tags: parseTags(row.tags),
    commentCount: Number(row.commentCount ?? row.comment_count ?? 0) || 0,
    createdAt: String(row.createdAt ?? row.created_at ?? ''),
    activityAt: String(row.activityAt ?? row.activity_at ?? ''),
    isImportant: Boolean(row.isImportant ?? row.is_important),
    kind: row.kind ? String(row.kind) : 'task',
    blockWork: Boolean(row.blockWork ?? row.block_work),
    entityType: row.entityType || row.entity_type ? String(row.entityType ?? row.entity_type) : undefined,
    entityId: row.entityId || row.entity_id ? String(row.entityId ?? row.entity_id) : undefined,
    priority: row.priority ? String(row.priority) : undefined,
  }
}

function fromAttention(row: Record<string, unknown>): AttentionItem {
  const kind = String(row.kind ?? 'task')
  const attentionType =
    row.attentionType === 'missed_call' || kind === 'missed_call' ? 'missed_call' : 'important'
  return {
    id: String(row.id),
    title: String(row.title ?? ''),
    kind,
    isImportant: Boolean(row.isImportant ?? row.is_important),
    blockWork: Boolean(row.blockWork ?? row.block_work),
    deadline: typeof row.deadline === 'string' && row.deadline ? row.deadline : undefined,
    entityType: row.entityType || row.entity_type ? String(row.entityType ?? row.entity_type) : undefined,
    entityId: row.entityId || row.entity_id ? String(row.entityId ?? row.entity_id) : undefined,
    entityLabel:
      row.entityLabel || row.entity_label
        ? String(row.entityLabel ?? row.entity_label)
        : undefined,
    resultNote: row.resultNote || row.result_note ? String(row.resultNote ?? row.result_note) : undefined,
    attentionType,
  }
}

function loadLocalDrafts(): Task[] {
  try {
    const raw = localStorage.getItem(LOCAL_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw) as Task[]
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

export function TasksProvider({ children }: { children: ReactNode }) {
  const [list, setList] = useState<Task[]>([])
  const [extra, setExtra] = useState<Record<string, Task>>({})
  const [total, setTotal] = useState(0)
  const [overdue, setOverdue] = useState(0)
  const [commentTotal, setCommentTotal] = useState(0)
  const [limit] = useState(50)
  const [offset, setOffset] = useState(0)
  const [query, setQueryRaw] = useState('')
  const [debounced, setDebounced] = useState('')
  const [status, setStatusRaw] = useState('')
  const [role, setRoleRaw] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [attention, setAttention] = useState<AttentionItem[]>([])
  const [attentionBlocked, setAttentionBlocked] = useState(false)
  const [attentionLoading, setAttentionLoading] = useState(false)

  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(query.trim()), 280)
    return () => window.clearTimeout(t)
  }, [query])

  const setQuery = useCallback((q: string) => {
    setQueryRaw(q)
    setOffset(0)
  }, [])

  const setStatus = useCallback((s: string) => {
    setStatusRaw(s)
    setOffset(0)
  }, [])

  const setRole = useCallback((s: string) => {
    setRoleRaw(s)
    setOffset(0)
  }, [])

  const reloadAttention = useCallback(async () => {
    setAttentionLoading(true)
    try {
      const data = await api<{ items?: Record<string, unknown>[]; blocked?: boolean }>(
        '/tasks/attention',
      )
      setAttention((data.items ?? []).map(fromAttention))
      setAttentionBlocked(Boolean(data.blocked ?? (data.items?.length ?? 0) > 0))
    } catch {
      // keep previous attention state on transient errors
    } finally {
      setAttentionLoading(false)
    }
  }, [])

  const reload = useCallback(async () => {
    const page = await api<Page<Record<string, unknown>>>(
      `/tasks${qs({ q: debounced, status, role, limit, offset })}`,
    )
    setList((page.items ?? []).map(fromApi))
    setTotal(page.total ?? 0)
    setOverdue(page.overdue ?? 0)
    setCommentTotal(page.commentTotal ?? 0)
  }, [debounced, status, role, limit, offset])

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setLoading(true)
      setError('')
      try {
        await reload()
        if (cancelled) return
        if (!localStorage.getItem(MIGRATED_KEY)) {
          const drafts = loadLocalDrafts()
          const current = await api<Page<Record<string, unknown>>>('/tasks?limit=1')
          if (drafts.length > 0 && (current.total ?? 0) === 0) {
            for (const draft of drafts) {
              await api('/tasks', {
                method: 'POST',
                body: JSON.stringify({
                  title: draft.title,
                  deadline: draft.deadline,
                  plannedDate: draft.plannedDate,
                  assignee: draft.assignee,
                  tags: draft.tags,
                  project: draft.project,
                  is_important: draft.isImportant,
                }),
              })
            }
            await reload()
          }
          localStorage.setItem(MIGRATED_KEY, '1')
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Не удалось загрузить задачи')
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [reload])

  useEffect(() => {
    void reloadAttention()
    const timer = window.setInterval(() => {
      void reloadAttention()
    }, 5000)
    const onFocus = () => {
      void reloadAttention()
    }
    window.addEventListener('focus', onFocus)
    document.addEventListener('visibilitychange', onFocus)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', onFocus)
      document.removeEventListener('visibilitychange', onFocus)
    }
  }, [reloadAttention])

  const add = useCallback(
    async (input: CreateTaskInput) => {
      const row = await api<Record<string, unknown>>('/tasks', {
        method: 'POST',
        body: JSON.stringify({
          title: input.title.trim(),
          deadline: input.deadline,
          plannedDate: input.plannedDate,
          assignee: input.assignee,
          tags: input.tags,
          project: input.project,
          is_important: Boolean(input.isImportant),
          kind: input.kind || 'task',
          entity_type: input.entityType,
          entity_id: input.entityId,
          block_work: Boolean(input.isImportant),
        }),
      })
      const task = fromApi(row)
      setList((prev) => [task, ...prev.filter((t) => t.id !== task.id)])
      setTotal((n) => n + 1)
      void reloadAttention()
      return task
    },
    [reloadAttention],
  )

  const update = useCallback(
    async (id: string, patch: Partial<Omit<Task, 'id'>>) => {
      const apply = (task: Task) =>
        task.id === id ? { ...task, ...patch, activityAt: new Date().toISOString() } : task
      setList((prev) => prev.map(apply))
      setExtra((prev) => (prev[id] ? { ...prev, [id]: apply(prev[id]) } : prev))
      try {
        const body: Record<string, unknown> = { ...patch }
        if ('isImportant' in patch) body.is_important = patch.isImportant
        if ('blockWork' in patch) body.block_work = patch.blockWork
        if ('entityType' in patch) body.entity_type = patch.entityType
        if ('entityId' in patch) body.entity_id = patch.entityId
        const row = await api<Record<string, unknown>>(`/tasks/${id}`, {
          method: 'PATCH',
          body: JSON.stringify(body),
        })
        const next = fromApi(row)
        setList((prev) => prev.map((task) => (task.id === id ? next : task)))
        setExtra((prev) => (prev[id] ? { ...prev, [id]: next } : prev))
        void reloadAttention()
      } catch {
        await reload()
      }
    },
    [reload, reloadAttention],
  )

  const remove = useCallback(
    async (id: string) => {
      setList((prev) => prev.filter((task) => task.id !== id))
      setExtra((prev) => {
        const next = { ...prev }
        delete next[id]
        return next
      })
      setTotal((n) => Math.max(0, n - 1))
      try {
        await api(`/tasks/${id}`, { method: 'DELETE' })
        void reloadAttention()
      } catch {
        await reload()
      }
    },
    [reload, reloadAttention],
  )

  const toggleComplete = useCallback(
    async (id: string) => {
      const current = list.find((t) => t.id === id) ?? extra[id]
      if (current?.blockWork || current?.isImportant || current?.kind === 'missed_call') {
        return
      }
      const nextStatus: TaskStatus = current?.status === 'completed' ? 'in_progress' : 'completed'
      await update(id, { status: nextStatus })
    },
    [list, extra, update],
  )

  const resolve = useCallback(
    async (
      id: string,
      payload: { result: TaskResolveResult; note?: string; rescheduleAt?: string },
    ) => {
      await api(`/tasks/${id}/resolve`, {
        method: 'POST',
        body: JSON.stringify({
          result: payload.result,
          note: payload.note ?? '',
          reschedule_at: payload.rescheduleAt || undefined,
        }),
      })
      setList((prev) =>
        prev.map((t) =>
          t.id === id
            ? {
                ...t,
                status: 'completed',
                blockWork: false,
                isImportant: false,
                activityAt: new Date().toISOString(),
              }
            : t,
        ),
      )
      setExtra((prev) => {
        if (!prev[id]) return prev
        return {
          ...prev,
          [id]: {
            ...prev[id],
            status: 'completed',
            blockWork: false,
            isImportant: false,
            activityAt: new Date().toISOString(),
          },
        }
      })
      await reloadAttention()
    },
    [reloadAttention],
  )

  const getById = useCallback(
    (id: string) => list.find((task) => task.id === id) ?? extra[id],
    [list, extra],
  )

  const loadById = useCallback(async (id: string) => {
    try {
      const row = await api<Record<string, unknown>>(`/tasks/${id}`)
      const task = fromApi(row)
      setExtra((prev) => ({ ...prev, [id]: task }))
      return task
    } catch {
      return getById(id)
    }
  }, [getById])

  const value = useMemo(
    () => ({
      list,
      total,
      overdue,
      commentTotal,
      limit,
      offset,
      query,
      status,
      role,
      loading,
      error,
      attention,
      attentionBlocked,
      attentionLoading,
      setQuery,
      setStatus,
      setRole,
      setOffset,
      loadById,
      add,
      update,
      remove,
      toggleComplete,
      getById,
      reload,
      reloadAttention,
      resolve,
    }),
    [
      list,
      total,
      overdue,
      commentTotal,
      limit,
      offset,
      query,
      status,
      role,
      loading,
      error,
      attention,
      attentionBlocked,
      attentionLoading,
      setQuery,
      setStatus,
      setRole,
      loadById,
      add,
      update,
      remove,
      toggleComplete,
      getById,
      reload,
      reloadAttention,
      resolve,
    ],
  )

  return <TasksContext.Provider value={value}>{children}</TasksContext.Provider>
}

export function useTasks() {
  const ctx = useContext(TasksContext)
  if (!ctx) {
    throw new Error('useTasks must be used within TasksProvider')
  }
  return ctx
}

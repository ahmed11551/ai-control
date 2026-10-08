import { MessageSquare, Plus, Search, TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { CreateTaskModal } from '../../components/tasks/CreateTaskModal'
import { DeadlinesBoard } from '../../components/tasks/DeadlinesBoard'
import { MyPlanBoard } from '../../components/tasks/MyPlanBoard'
import { TaskSidePanel } from '../../components/tasks/TaskSidePanel'
import { TasksCalendar } from '../../components/tasks/TasksCalendar'
import { TasksGantt } from '../../components/tasks/TasksGantt'
import { TasksTable } from '../../components/tasks/TasksTable'
import { Button } from '../../components/ui/Button'
import { ListPager } from '../../components/ui/ListPager'
import { Tabs } from '../../components/ui/Tabs'
import { tasksViewTabs } from '../../data/nav'
import { deadlineForBucket, type DeadlineBucket, type Task } from '../../data/tasks'
import { resolveLeadIdFromEntity } from '../../lib/openLeadFromEntity'
import { useCrmLeadDrawer } from '../../store/crmLeadDrawer'
import { useTasks } from '../../store/tasks'

const ROLES = [
  { id: '', label: 'Все роли' },
  { id: 'doing', label: 'Делаю' },
  { id: 'helping', label: 'Помогаю' },
  { id: 'assigned', label: 'Поручил' },
  { id: 'watching', label: 'Наблюдаю' },
]

export function TasksPage() {
  const navigate = useNavigate()
  const { openLead } = useCrmLeadDrawer()
  const {
    list,
    total,
    overdue,
    commentTotal,
    limit,
    offset,
    query,
    status,
    role,
    setQuery,
    setStatus,
    setRole,
    setOffset,
    loadById,
    add,
    remove,
    update,
    toggleComplete,
    getById,
    loading,
    error,
  } = useTasks()
  const [view, setView] = useState('list')
  const [createOpen, setCreateOpen] = useState(false)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [picked, setPicked] = useState<Task | null>(null)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())

  const selectedTask = selectedId ? getById(selectedId) ?? (picked?.id === selectedId ? picked : null) : null

  const openTask = (id: string) => {
    setSelectedId(id)
    setPicked(getById(id) ?? picked)
    void loadById(id).then(async (task) => {
      if (task) setPicked(task)
      const t = task ?? getById(id)
      if (!t) return
      const leadId = await resolveLeadIdFromEntity(t.entityType, t.entityId)
      if (leadId) {
        openLead(leadId)
        navigate(`/crm/leads?open=${encodeURIComponent(leadId)}`)
      }
    })
  }

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleSelectAll = () => {
    setSelectedIds((prev) => {
      if (list.length === 0) return prev
      const allSelected = list.every((t) => prev.has(t.id))
      if (allSelected) return new Set()
      return new Set(list.map((t) => t.id))
    })
  }

  const moveToBucket = (id: string, bucket: DeadlineBucket) => {
    update(id, { deadline: deadlineForBucket(bucket) })
  }

  return (
    <div className="flex flex-col gap-4 pt-1 pb-4 min-h-[calc(100%-2rem)]">
      {error ? <p className="text-[13px] text-orange-200">{error}</p> : null}
      {loading ? <p className="text-[13px] text-white/70">Загрузка задач…</p> : null}

      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">Мои задачи</h1>
          <p className="mt-1.5 text-[13px] text-white/65">
            {total.toLocaleString('ru-RU')} в списке
            {overdue > 0 ? ` · ${overdue} просрочено` : ''}
          </p>
        </div>
        <Button
          type="button"
          variant="green"
          className="h-10 rounded-[10px] px-4 shadow-[0_8px_24px_rgba(47,192,110,0.35)]"
          leftIcon={<Plus size={16} strokeWidth={2.5} />}
          onClick={() => setCreateOpen(true)}
        >
          Новая задача
        </Button>
      </div>

      <div className="grid grid-cols-2 lg:grid-cols-3 gap-2.5">
        {[
          { label: 'Всего', value: total, hint: 'в текущем фильтре' },
          { label: 'Просрочено', value: overdue, hint: 'нужно закрыть' },
          { label: 'Комментарии', value: commentTotal, hint: 'по задачам' },
        ].map((card) => (
          <div
            key={card.label}
            className="rounded-[14px] bg-white/12 border border-white/10 px-4 py-3 backdrop-blur-sm"
          >
            <div className="text-[11px] uppercase tracking-wide text-white/55">{card.label}</div>
            <div className="mt-1 text-[26px] font-semibold text-white tabular-nums leading-none">
              {card.value.toLocaleString('ru-RU')}
            </div>
            <div className="mt-1 text-[11px] text-white/45">{card.hint}</div>
          </div>
        ))}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2">
        <Tabs items={tasksViewTabs} activeId={view} onChange={setView} tone="dark" />
        <div className="flex items-center gap-2 text-[12px] text-white/55">
          <span className="inline-flex items-center gap-1.5">
            <TriangleAlert size={13} className="text-orange-300/80" />
            {overdue}
          </span>
          <span className="inline-flex items-center gap-1.5">
            <MessageSquare size={13} />
            {commentTotal.toLocaleString('ru-RU')}
          </span>
        </div>
      </div>

      <div className="b24-glass-panel overflow-hidden flex flex-col min-h-[420px]">
        <div className="px-4 py-3 border-b border-black/6 bg-white/80 flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex-1 min-w-[200px] relative">
              <Search
                size={16}
                className="absolute left-3 top-1/2 -translate-y-1/2 text-b24-text-muted pointer-events-none"
              />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Поиск по названию…"
                className="h-10 w-full rounded-[10px] border border-black/8 bg-[#f5f7f9] pl-9 pr-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan focus:bg-white"
              />
            </div>
            <button
              type="button"
              onClick={() => setStatus(status === 'in_progress' ? '' : 'in_progress')}
              className={[
                'h-10 px-3.5 rounded-[10px] text-[13px] font-medium border inline-flex items-center gap-1.5 transition',
                status === 'in_progress'
                  ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep'
                  : 'bg-white border-black/8 text-b24-text hover:bg-[#f8fafb]',
              ].join(' ')}
            >
              В работе
            </button>
          </div>
          <div className="flex flex-wrap gap-1.5">
            {ROLES.map((r) => {
              const active = role === r.id
              return (
                <button
                  key={r.id || 'all'}
                  type="button"
                  onClick={() => setRole(r.id)}
                  className={[
                    'h-8 px-3 rounded-full text-[12px] font-medium transition',
                    active
                      ? 'bg-b24-text-dark text-white'
                      : 'bg-[#eef1f4] text-b24-text hover:bg-[#e4e8ec]',
                  ].join(' ')}
                >
                  {r.label}
                </button>
              )
            })}
          </div>
        </div>

        <div className="flex-1 min-h-0">
          {view === 'list' ? (
            <div className="flex flex-col">
              <TasksTable
                tasks={list}
                selectedIds={selectedIds}
                onToggleSelect={toggleSelect}
                onToggleSelectAll={toggleSelectAll}
                onOpen={openTask}
                onToggleComplete={toggleComplete}
              />
              <div className="px-4 py-2 border-t border-black/6">
                <ListPager total={total} limit={limit} offset={offset} onOffset={setOffset} />
              </div>
            </div>
          ) : null}

          {view === 'deadlines' ? (
            <div className="p-3">
              <DeadlinesBoard
                tasks={list}
                onOpen={openTask}
                onToggleComplete={toggleComplete}
                onMoveToBucket={moveToBucket}
              />
            </div>
          ) : null}

          {view === 'plan' ? (
            <div className="p-3">
              <MyPlanBoard
                tasks={list}
                onOpen={openTask}
                onToggleComplete={toggleComplete}
                onSetPlannedDate={(id, plannedDate) => update(id, { plannedDate })}
              />
            </div>
          ) : null}

          {view === 'calendar' ? (
            <div className="p-3">
              <TasksCalendar tasks={list} onOpen={openTask} />
            </div>
          ) : null}

          {view === 'gantt' ? (
            <div className="p-3">
              <TasksGantt tasks={list} onOpen={openTask} />
            </div>
          ) : null}

          {view === 'chats' ? (
            <div className="flex flex-col items-center justify-center min-h-[360px] px-8 py-16 text-center">
              <h3 className="text-[22px] font-normal text-[#a0a5ab] mb-2">Чаты задач</h3>
              <p className="text-[14px] text-[#b0b5bb]">Чаты по задачам появятся позже.</p>
            </div>
          ) : null}
        </div>
      </div>

      <CreateTaskModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onSubmit={(data) => {
          void add(data).then((task) => {
            setSelectedIds(new Set())
            setSelectedId(task.id)
          })
        }}
      />

      <TaskSidePanel
        task={selectedTask}
        onClose={() => setSelectedId(null)}
        onToggleComplete={toggleComplete}
        onRemove={(id) => {
          void remove(id)
          setSelectedIds((prev) => {
            const next = new Set(prev)
            next.delete(id)
            return next
          })
        }}
        onOpenLead={
          selectedTask?.entityType === 'lead' || selectedTask?.entityType === 'deal'
            ? () => {
                void (async () => {
                  const leadId = await resolveLeadIdFromEntity(
                    selectedTask.entityType,
                    selectedTask.entityId,
                  )
                  if (!leadId) return
                  openLead(leadId)
                  navigate(`/crm/leads?open=${encodeURIComponent(leadId)}`)
                })()
              }
            : undefined
        }
      />
    </div>
  )
}

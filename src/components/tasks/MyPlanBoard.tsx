import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useMemo, useState } from 'react'
import {
  addDays,
  formatShortDay,
  isSameDay,
  startOfDay,
  toDateKey,
  type Task,
} from '../../data/tasks'
import { TaskBoardCard } from './TaskBoardCard'

type MyPlanBoardProps = {
  tasks: Task[]
  onOpen: (id: string) => void
  onToggleComplete: (id: string) => void
  onSetPlannedDate: (id: string, plannedDate: string | undefined) => void
}

const DAY_COUNT = 7

export function MyPlanBoard({
  tasks,
  onOpen,
  onToggleComplete,
  onSetPlannedDate,
}: MyPlanBoardProps) {
  const [offset, setOffset] = useState(0)
  const [dragOver, setDragOver] = useState<string | null>(null)

  const days = useMemo(() => {
    const today = startOfDay()
    return Array.from({ length: DAY_COUNT }, (_, i) => addDays(today, offset + i))
  }, [offset])

  const unplanned = useMemo(
    () => tasks.filter((t) => !t.plannedDate),
    [tasks],
  )

  const byDay = useMemo(() => {
    const map = new Map<string, Task[]>()
    for (const day of days) {
      map.set(toDateKey(day), [])
    }
    for (const task of tasks) {
      if (!task.plannedDate) continue
      const key = toDateKey(task.plannedDate)
      if (map.has(key)) {
        map.get(key)!.push(task)
      }
    }
    return map
  }, [tasks, days])

  const today = startOfDay()

  return (
    <div className="b24-glass-panel overflow-hidden min-h-[480px] flex flex-col">
      <div className="flex items-center justify-between px-3 py-2.5 border-b border-black/6 bg-b24-panel-header">
        <div className="text-[13px] text-b24-text font-medium">Мой план на неделю</div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => setOffset((v) => v - 7)}
            className="size-8 inline-flex items-center justify-center rounded-full text-b24-text hover:bg-black/5"
            aria-label="Предыдущая неделя"
          >
            <ChevronLeft size={18} strokeWidth={1.75} />
          </button>
          <button
            type="button"
            onClick={() => setOffset(0)}
            className="h-8 px-3 rounded-b24-pill text-[12px] font-medium text-b24-text-dark bg-white border border-black/8 hover:bg-[#f5f7f8]"
          >
            Сегодня
          </button>
          <button
            type="button"
            onClick={() => setOffset((v) => v + 7)}
            className="size-8 inline-flex items-center justify-center rounded-full text-b24-text hover:bg-black/5"
            aria-label="Следующая неделя"
          >
            <ChevronRight size={18} strokeWidth={1.75} />
          </button>
        </div>
      </div>

      <div className="overflow-x-auto b24-scrollbar flex-1">
        <div className="flex gap-3 p-3 min-w-max items-stretch">
          <PlanColumn
            title="Незапланированные"
            count={unplanned.length}
            dropKey="unplanned"
            isOver={dragOver === 'unplanned'}
            isToday={false}
            onDragOver={() => setDragOver('unplanned')}
            onDragLeave={() => setDragOver((p) => (p === 'unplanned' ? null : p))}
            onDrop={() => {
              setDragOver(null)
            }}
            onDropTask={(id) => onSetPlannedDate(id, undefined)}
          >
            {unplanned.length === 0 ? (
              <EmptyDropHint />
            ) : (
              unplanned.map((task) => (
                <TaskBoardCard
                  key={task.id}
                  task={task}
                  accent="plan"
                  onOpen={onOpen}
                  onToggleComplete={onToggleComplete}
                />
              ))
            )}
          </PlanColumn>

          {days.map((day) => {
            const key = toDateKey(day)
            const items = byDay.get(key) ?? []
            const isToday = isSameDay(day, today)
            return (
              <PlanColumn
                key={key}
                title={formatShortDay(day)}
                count={items.length}
                dropKey={key}
                isOver={dragOver === key}
                isToday={isToday}
                onDragOver={() => setDragOver(key)}
                onDragLeave={() => setDragOver((p) => (p === key ? null : p))}
                onDrop={() => setDragOver(null)}
                onDropTask={(id) => onSetPlannedDate(id, key)}
              >
                {items.length === 0 ? (
                  <EmptyDropHint />
                ) : (
                  items.map((task) => (
                    <TaskBoardCard
                      key={task.id}
                      task={task}
                      accent={isToday ? 'today' : 'plan'}
                      onOpen={onOpen}
                      onToggleComplete={onToggleComplete}
                    />
                  ))
                )}
              </PlanColumn>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function PlanColumn({
  title,
  count,
  dropKey,
  isOver,
  isToday,
  onDragOver,
  onDragLeave,
  onDrop,
  onDropTask,
  children,
}: {
  title: string
  count: number
  dropKey: string
  isOver: boolean
  isToday: boolean
  onDragOver: () => void
  onDragLeave: () => void
  onDrop: () => void
  onDropTask: (id: string) => void
  children: React.ReactNode
}) {
  return (
    <section
      className={[
        'w-[220px] shrink-0 rounded-[12px] border flex flex-col max-h-[min(68vh,640px)]',
        isToday
          ? 'bg-[#eef9fd]/90 border-b24-cyan/30'
          : 'bg-[#f4f6f8]/85 border-black/5',
        isOver ? 'ring-2 ring-[#8b7cf6]/45 bg-[#f3f0ff]' : '',
      ].join(' ')}
      onDragOver={(e) => {
        e.preventDefault()
        onDragOver()
      }}
      onDragLeave={onDragLeave}
      onDrop={(e) => {
        e.preventDefault()
        const id = e.dataTransfer.getData('text/task-id')
        onDrop()
        if (id) onDropTask(id)
      }}
      data-drop={dropKey}
    >
      <header
        className={[
          'flex items-center justify-between px-3 py-2.5 rounded-t-[12px] border-b border-black/5',
          isToday ? 'bg-[#dff4fb] text-[#0b7ea4]' : 'bg-[#f3f5f7] text-b24-text-dark',
        ].join(' ')}
      >
        <h3 className="text-[13px] font-semibold capitalize">{title}</h3>
        <span className="text-[12px] tabular-nums opacity-70">{count}</span>
      </header>
      <div className="flex-1 overflow-y-auto b24-scrollbar p-2 flex flex-col gap-2">
        {children}
      </div>
    </section>
  )
}

function EmptyDropHint() {
  return (
    <div className="flex-1 min-h-[72px] rounded-[10px] border border-dashed border-black/10 text-[12px] text-b24-text-muted flex items-center justify-center px-3 text-center">
      Сюда
    </div>
  )
}

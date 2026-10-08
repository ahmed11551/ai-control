import { useMemo, useState } from 'react'
import {
  getDeadlineBucket,
  type DeadlineBucket,
  type Task,
} from '../../data/tasks'
import { TaskBoardCard } from './TaskBoardCard'

type DeadlinesBoardProps = {
  tasks: Task[]
  onOpen: (id: string) => void
  onToggleComplete: (id: string) => void
  onMoveToBucket: (id: string, bucket: DeadlineBucket) => void
}

const COLUMNS: {
  id: DeadlineBucket
  title: string
  accent: 'default' | 'overdue' | 'today' | 'plan'
  headerClass: string
}[] = [
  {
    id: 'overdue',
    title: 'Просрочены',
    accent: 'overdue',
    headerClass: 'text-orange-700 bg-orange-50/80',
  },
  {
    id: 'today',
    title: 'Сегодня',
    accent: 'today',
    headerClass: 'text-[#0b7ea4] bg-[#e8f8fd]',
  },
  {
    id: 'this_week',
    title: 'На этой неделе',
    accent: 'default',
    headerClass: 'text-b24-text-dark bg-[#f3f5f7]',
  },
  {
    id: 'next_week',
    title: 'На следующей',
    accent: 'default',
    headerClass: 'text-b24-text-dark bg-[#f3f5f7]',
  },
  {
    id: 'later',
    title: 'Позже',
    accent: 'default',
    headerClass: 'text-b24-text-dark bg-[#f3f5f7]',
  },
  {
    id: 'none',
    title: 'Без срока',
    accent: 'default',
    headerClass: 'text-b24-text-muted bg-[#f7f8fa]',
  },
]

export function DeadlinesBoard({
  tasks,
  onOpen,
  onToggleComplete,
  onMoveToBucket,
}: DeadlinesBoardProps) {
  const [dragOver, setDragOver] = useState<DeadlineBucket | null>(null)

  const grouped = useMemo(() => {
    const map: Record<DeadlineBucket, Task[]> = {
      overdue: [],
      today: [],
      this_week: [],
      next_week: [],
      later: [],
      none: [],
    }
    for (const task of tasks) {
      map[getDeadlineBucket(task)].push(task)
    }
    return map
  }, [tasks])

  return (
    <div className="b24-glass-panel overflow-hidden min-h-[480px]">
      <div className="overflow-x-auto b24-scrollbar">
        <div className="flex gap-3 p-3 min-w-max items-stretch">
          {COLUMNS.map((col) => {
            const items = grouped[col.id]
            const isOver = dragOver === col.id
            return (
              <section
                key={col.id}
                className={[
                  'w-[240px] shrink-0 rounded-[12px] bg-[#f4f6f8]/85 border border-black/5 flex flex-col max-h-[min(68vh,640px)]',
                  isOver ? 'ring-2 ring-b24-cyan/50 bg-[#eef9fd]' : '',
                ].join(' ')}
                onDragOver={(e) => {
                  e.preventDefault()
                  setDragOver(col.id)
                }}
                onDragLeave={() => setDragOver((prev) => (prev === col.id ? null : prev))}
                onDrop={(e) => {
                  e.preventDefault()
                  const id = e.dataTransfer.getData('text/task-id')
                  setDragOver(null)
                  if (id) onMoveToBucket(id, col.id)
                }}
              >
                <header
                  className={[
                    'flex items-center justify-between px-3 py-2.5 rounded-t-[12px] border-b border-black/5',
                    col.headerClass,
                  ].join(' ')}
                >
                  <h3 className="text-[13px] font-semibold">{col.title}</h3>
                  <span className="text-[12px] tabular-nums opacity-70">{items.length}</span>
                </header>

                <div className="flex-1 overflow-y-auto b24-scrollbar p-2 flex flex-col gap-2">
                  {items.length === 0 ? (
                    <div className="flex-1 min-h-[80px] rounded-[10px] border border-dashed border-black/10 text-[12px] text-b24-text-muted flex items-center justify-center px-3 text-center">
                      Перетащите задачу сюда
                    </div>
                  ) : (
                    items.map((task) => (
                      <TaskBoardCard
                        key={task.id}
                        task={task}
                        accent={col.accent}
                        onOpen={onOpen}
                        onToggleComplete={onToggleComplete}
                      />
                    ))
                  )}
                </div>
              </section>
            )
          })}
        </div>
      </div>
    </div>
  )
}
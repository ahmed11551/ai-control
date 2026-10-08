import { Check } from 'lucide-react'
import {
  formatTaskDate,
  isTaskOverdue,
  type Task,
} from '../../data/tasks'

type TaskBoardCardProps = {
  task: Task
  onOpen: (id: string) => void
  onToggleComplete: (id: string) => void
  draggable?: boolean
  onDragStart?: (taskId: string) => void
  accent?: 'default' | 'overdue' | 'today' | 'plan'
}

export function TaskBoardCard({
  task,
  onOpen,
  onToggleComplete,
  draggable = true,
  onDragStart,
  accent = 'default',
}: TaskBoardCardProps) {
  const overdue = isTaskOverdue(task)
  const completed = task.status === 'completed'

  const accentBorder =
    accent === 'overdue' || overdue
      ? 'border-l-orange-400'
      : accent === 'today'
        ? 'border-l-b24-cyan'
        : accent === 'plan'
          ? 'border-l-[#8b7cf6]'
          : 'border-l-[#d5dbe0]'

  return (
    <div
      role="button"
      tabIndex={0}
      draggable={draggable}
      onDragStart={(e) => {
        e.dataTransfer.setData('text/task-id', task.id)
        e.dataTransfer.effectAllowed = 'move'
        onDragStart?.(task.id)
      }}
      onClick={() => onOpen(task.id)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onOpen(task.id)
        }
      }}
      className={[
        'group rounded-[10px] bg-white border border-black/6 border-l-[3px] shadow-sm',
        'px-2.5 py-2 cursor-grab active:cursor-grabbing select-none',
        'hover:shadow-md hover:border-black/10 transition-shadow',
        accentBorder,
        completed ? 'opacity-70' : '',
      ].join(' ')}
    >
      <div className="flex items-start gap-2">
        <button
          type="button"
          title={completed ? 'Вернуть в работу' : 'Выполнить'}
          onClick={(e) => {
            e.stopPropagation()
            onToggleComplete(task.id)
          }}
          className={[
            'mt-0.5 size-[18px] shrink-0 rounded-full border inline-flex items-center justify-center transition-colors',
            completed
              ? 'bg-b24-green border-b24-green text-white'
              : 'border-[#c6cdd3] text-transparent hover:border-b24-green hover:text-b24-green',
          ].join(' ')}
        >
          <Check size={10} strokeWidth={3} />
        </button>
        <div className="min-w-0 flex-1">
          <div
            className={[
              'text-[13px] leading-snug text-b24-text-dark font-medium break-words',
              completed ? 'line-through text-b24-text-muted' : '',
            ].join(' ')}
          >
            {task.title}
          </div>
          <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-b24-text-muted">
            <span className="truncate max-w-[110px]">{task.assignee}</span>
            {task.deadline ? (
              <span className={overdue ? 'text-orange-600 font-medium' : ''}>
                {formatTaskDate(task.deadline)}
              </span>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  )
}

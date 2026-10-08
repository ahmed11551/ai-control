import { Check, PhoneMissed, Settings2, Star } from 'lucide-react'
import {
  formatActivity,
  formatTaskDate,
  isTaskOverdue,
  type Task,
} from '../../data/tasks'
import { taskTableColumns } from '../../data/nav'

type TasksTableProps = {
  tasks: Task[]
  selectedIds: Set<string>
  onToggleSelect: (id: string) => void
  onToggleSelectAll: () => void
  onOpen: (id: string) => void
  onToggleComplete: (id: string) => void
}

const gridClass =
  'grid grid-cols-[40px_36px_minmax(180px,1.6fr)_repeat(6,minmax(100px,1fr))] gap-0 min-w-[900px]'

export function TasksTable({
  tasks,
  selectedIds,
  onToggleSelect,
  onToggleSelectAll,
  onOpen,
  onToggleComplete,
}: TasksTableProps) {
  const allSelected = tasks.length > 0 && tasks.every((t) => selectedIds.has(t.id))

  if (tasks.length === 0) {
    return (
      <div className="flex flex-col min-h-[360px] overflow-hidden">
        <TableHeader
          allSelected={false}
          onToggleSelectAll={onToggleSelectAll}
          disabled
        />
        <div className="flex-1 flex flex-col items-center justify-center px-8 py-16 text-center">
          <h3 className="text-[28px] font-normal text-[#a0a5ab] tracking-tight mb-2">
            Создайте задачу
          </h3>
          <p className="max-w-[420px] text-[14px] leading-relaxed text-[#b0b5bb]">
            Здесь будет список задач, которые вы выполняете или поручаете своим
            сотрудникам.
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col overflow-hidden">
      <div className="overflow-x-auto b24-scrollbar flex-1">
        <TableHeader
          allSelected={allSelected}
          onToggleSelectAll={onToggleSelectAll}
        />
        <div className="divide-y divide-b24-border">
          {tasks.map((task) => {
            const overdue = isTaskOverdue(task)
            const completed = task.status === 'completed'
            return (
              <div
                key={task.id}
                role="button"
                tabIndex={0}
                onClick={() => onOpen(task.id)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    onOpen(task.id)
                  }
                }}
                className={[
                  gridClass,
                  'text-[13px] text-b24-text-dark cursor-pointer transition-colors',
                  'hover:bg-[#f5f8fa]',
                  selectedIds.has(task.id) ? 'bg-[#eef8fc]' : 'bg-transparent',
                ].join(' ')}
              >
                <div
                  className="flex items-center justify-center py-3"
                  onClick={(e) => e.stopPropagation()}
                >
                  <input
                    type="checkbox"
                    checked={selectedIds.has(task.id)}
                    onChange={() => onToggleSelect(task.id)}
                    className="size-3.5 rounded border-gray-300 accent-b24-cyan"
                    aria-label={`Выбрать ${task.title}`}
                  />
                </div>
                <div
                  className="flex items-center justify-center py-3"
                  onClick={(e) => e.stopPropagation()}
                >
                  {task.blockWork || task.isImportant || task.kind === 'missed_call' ? (
                    <span
                      title="Нужна фиксация результата"
                      className="size-5 rounded-full border border-amber-300 bg-amber-50 text-amber-600 inline-flex items-center justify-center"
                    >
                      {task.kind === 'missed_call' ? (
                        <PhoneMissed size={10} strokeWidth={2.5} />
                      ) : (
                        <Star size={10} strokeWidth={2.5} fill="currentColor" />
                      )}
                    </span>
                  ) : (
                    <button
                      type="button"
                      title={completed ? 'Вернуть в работу' : 'Выполнить'}
                      onClick={() => onToggleComplete(task.id)}
                      className={[
                        'size-5 rounded-full border inline-flex items-center justify-center transition-colors',
                        completed
                          ? 'bg-b24-green border-b24-green text-white'
                          : 'border-[#c6cdd3] text-transparent hover:border-b24-green hover:text-b24-green',
                      ].join(' ')}
                    >
                      <Check size={11} strokeWidth={3} />
                    </button>
                  )}
                </div>
                <div className="flex items-center px-2 py-3 min-w-0 gap-2">
                  {task.kind === 'missed_call' ? (
                    <span className="shrink-0 inline-flex items-center gap-1 rounded-b24-sm bg-rose-50 text-rose-700 px-1.5 py-0.5 text-[11px] font-medium">
                      <PhoneMissed size={11} />
                      Пропуск
                    </span>
                  ) : task.isImportant ? (
                    <span className="shrink-0 inline-flex items-center gap-1 rounded-b24-sm bg-[#e8f8fe] text-b24-cyan-deep px-1.5 py-0.5 text-[11px] font-medium">
                      <Star size={11} fill="currentColor" />
                      Важная
                    </span>
                  ) : null}
                  <span
                    className={[
                      'truncate font-medium',
                      completed ? 'text-b24-text-muted line-through' : '',
                    ].join(' ')}
                  >
                    {task.title}
                  </span>
                </div>
                <div
                  className={[
                    'flex items-center px-2 py-3 text-b24-text truncate',
                    completed ? 'line-through opacity-60' : '',
                  ].join(' ')}
                >
                  {formatActivity(task.activityAt)}
                </div>
                <div
                  className={[
                    'flex items-center px-2 py-3 truncate',
                    completed ? 'line-through opacity-60' : '',
                    overdue && !completed ? 'text-orange-600 font-medium' : 'text-b24-text',
                  ].join(' ')}
                >
                  {formatTaskDate(task.deadline)}
                </div>
                <div
                  className={[
                    'flex items-center px-2 py-3 truncate',
                    completed ? 'line-through opacity-60' : '',
                  ].join(' ')}
                >
                  {task.creator}
                </div>
                <div
                  className={[
                    'flex items-center px-2 py-3 truncate',
                    completed ? 'line-through opacity-60' : '',
                  ].join(' ')}
                >
                  {task.assignee}
                </div>
                <div
                  className={[
                    'flex items-center px-2 py-3 truncate text-b24-text',
                    completed ? 'line-through opacity-60' : '',
                  ].join(' ')}
                >
                  {task.project || '—'}
                </div>
                <div className="flex items-center px-2 py-3 gap-1 flex-wrap">
                  {task.tags.length
                    ? task.tags.map((tag) => (
                        <span
                          key={tag}
                          className="inline-flex rounded-b24-sm bg-[#eef2f4] px-1.5 py-0.5 text-[11px] text-b24-text"
                        >
                          {tag}
                        </span>
                      ))
                    : '—'}
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function TableHeader({
  allSelected,
  onToggleSelectAll,
  disabled,
}: {
  allSelected: boolean
  onToggleSelectAll: () => void
  disabled?: boolean
}) {
  return (
    <div
      className={[
        gridClass,
        'border-b border-b24-border bg-b24-panel-header text-[12px] text-b24-text-muted font-medium',
      ].join(' ')}
    >
      <div className="flex items-center justify-center py-2.5">
        <input
          type="checkbox"
          checked={allSelected}
          disabled={disabled}
          onChange={onToggleSelectAll}
          className="size-3.5 rounded border-gray-300 accent-b24-cyan disabled:opacity-40"
          aria-label="Выбрать все"
        />
      </div>
      <div className="flex items-center justify-center py-2.5 text-b24-text-muted">
        <Settings2 size={14} strokeWidth={1.75} />
      </div>
      {taskTableColumns.map((col, index) => (
        <div
          key={col}
          className={['flex items-center py-2.5 px-2 truncate', index === 1 ? 'gap-1' : ''].join(
            ' ',
          )}
        >
          <span className="truncate">{col}</span>
          {index === 1 ? (
            <span className="text-[10px] opacity-60" aria-hidden>
              ↓
            </span>
          ) : null}
        </div>
      ))}
    </div>
  )
}

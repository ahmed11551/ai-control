import { Check, PhoneMissed, RotateCcw, Star, Trash2, X } from 'lucide-react'
import { useEffect } from 'react'
import {
  formatActivity,
  formatTaskDate,
  isTaskOverdue,
  type Task,
} from '../../data/tasks'
import { useTasks } from '../../store/tasks'
import { Button } from '../ui/Button'
import { TaskComments } from './TaskComments'
import { TaskResolveForm } from './TaskResolveForm'

type TaskSidePanelProps = {
  task: Task | null
  onClose: () => void
  onToggleComplete: (id: string) => void
  onRemove: (id: string) => void
  onOpenLead?: () => void
}

function MetaRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 py-2.5 border-b border-black/5 last:border-0">
      <div className="text-[11px] font-medium uppercase tracking-wide text-b24-text-muted">{label}</div>
      <div className="text-[14px] text-b24-text-dark">{children}</div>
    </div>
  )
}

export function TaskSidePanel({
  task,
  onClose,
  onToggleComplete,
  onRemove,
  onOpenLead,
}: TaskSidePanelProps) {
  const { resolve } = useTasks()

  useEffect(() => {
    if (!task) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [task, onClose])

  if (!task) return null

  const overdue = isTaskOverdue(task)
  const completed = task.status === 'completed'
  const needsResolve =
    !completed && (Boolean(task.blockWork) || Boolean(task.isImportant) || task.kind === 'missed_call')
  const isMissed = task.kind === 'missed_call'

  return (
    <div className="fixed inset-0 z-[130] flex justify-center sm:justify-end">
      <button
        type="button"
        className="absolute inset-0 bg-[#525c69]/55"
        aria-label="Закрыть"
        onClick={onClose}
      />

      <div className="relative z-10 h-full w-full max-w-[min(1100px,calc(100vw-24px))] sm:mr-3 sm:my-3 sm:h-[calc(100%-24px)] sm:rounded-[16px] bg-[#eef2f4] shadow-2xl flex flex-col overflow-hidden">
        <header className="shrink-0 flex items-start justify-between gap-3 px-5 py-4 bg-white border-b border-black/8">
          <div className="min-w-0">
            <div className="text-[11px] font-semibold uppercase tracking-wide text-b24-cyan-deep mb-1">
              {isMissed ? 'Пропущенный звонок' : task.isImportant ? 'Важная задача' : 'Задача'}
            </div>
            <h2 className="text-[22px] font-semibold text-b24-text-dark leading-snug break-words">
              {task.title}
            </h2>
            <div className="mt-3 flex flex-wrap gap-1.5">
              <span
                className={[
                  'inline-flex items-center rounded-full px-2.5 py-1 text-[12px] font-medium',
                  completed ? 'bg-emerald-50 text-emerald-700' : 'bg-sky-50 text-sky-700',
                ].join(' ')}
              >
                {completed ? 'Выполнена' : 'В работе'}
              </span>
              {task.isImportant ? (
                <span className="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-[12px] font-medium bg-[#e8f8fe] text-b24-cyan-deep">
                  <Star size={12} fill="currentColor" />
                  Важная
                </span>
              ) : null}
              {isMissed ? (
                <span className="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-[12px] font-medium bg-rose-50 text-rose-700">
                  <PhoneMissed size={12} />
                  Пропуск
                </span>
              ) : null}
              {overdue ? (
                <span className="inline-flex items-center rounded-full px-2.5 py-1 text-[12px] font-medium bg-orange-50 text-orange-700">
                  Просрочена
                </span>
              ) : null}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="size-9 shrink-0 inline-flex items-center justify-center rounded-full text-b24-text-muted hover:bg-black/5"
            aria-label="Закрыть"
          >
            <X size={20} strokeWidth={1.75} />
          </button>
        </header>

        <div className="flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-[minmax(280px,360px)_minmax(0,1fr)] gap-0 overflow-hidden">
          <aside className="bg-white border-b lg:border-b-0 lg:border-r border-black/8 overflow-y-auto b24-scrollbar p-5">
            <h3 className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted mb-1">
              Сведения
            </h3>
            <MetaRow label="Крайний срок">
              <span className={overdue ? 'text-orange-600 font-medium' : undefined}>
                {formatTaskDate(task.deadline)}
              </span>
            </MetaRow>
            <MetaRow label="В плане">{formatTaskDate(task.plannedDate)}</MetaRow>
            <MetaRow label="Постановщик">{task.creator || '—'}</MetaRow>
            <MetaRow label="Исполнитель">{task.assignee || '—'}</MetaRow>
            <MetaRow label="Помогаю">
              {task.helpers?.length ? task.helpers.join(', ') : '—'}
            </MetaRow>
            <MetaRow label="Наблюдаю">
              {task.watchers?.length ? task.watchers.join(', ') : '—'}
            </MetaRow>
            <MetaRow label="Проект">{task.project || '—'}</MetaRow>
            <MetaRow label="Активность">{formatActivity(task.activityAt)}</MetaRow>
            <MetaRow label="Теги">
              {task.tags.length ? (
                <div className="flex flex-wrap gap-1.5 mt-0.5">
                  {task.tags.map((tag) => (
                    <span
                      key={tag}
                      className="inline-flex rounded-[8px] bg-[#eef2f4] px-2 py-0.5 text-[12px]"
                    >
                      {tag}
                    </span>
                  ))}
                </div>
              ) : (
                '—'
              )}
            </MetaRow>

            <div className="mt-5 flex flex-col gap-2">
              {onOpenLead && (task.entityType === 'lead' || task.entityType === 'deal') ? (
                <Button
                  variant="filter"
                  className="w-full rounded-[10px] h-10 justify-center"
                  onClick={() => onOpenLead()}
                >
                  Открыть карточку лида
                </Button>
              ) : null}
              {!needsResolve ? (
                <Button
                  variant="green"
                  className="w-full rounded-[10px] h-10 justify-center"
                  leftIcon={
                    completed ? (
                      <RotateCcw size={15} strokeWidth={2} />
                    ) : (
                      <Check size={15} strokeWidth={2.25} />
                    )
                  }
                  onClick={() => onToggleComplete(task.id)}
                >
                  {completed ? 'Вернуть в работу' : 'Выполнить'}
                </Button>
              ) : null}
              <Button
                variant="filter"
                className="w-full rounded-[10px] h-10 justify-center text-red-600 hover:bg-red-50"
                leftIcon={<Trash2 size={15} strokeWidth={1.75} />}
                onClick={() => {
                  onRemove(task.id)
                  onClose()
                }}
              >
                Удалить
              </Button>
            </div>
          </aside>

          <div className="min-h-0 overflow-y-auto b24-scrollbar p-4 sm:p-5 flex flex-col gap-4">
            {needsResolve ? (
              <TaskResolveForm
                variant="panel"
                isMissed={isMissed}
                noteRequired={isMissed}
                onSubmit={async (payload) => {
                  await resolve(task.id, payload)
                  onClose()
                }}
              />
            ) : (
              <div className="rounded-[12px] border border-black/8 bg-white p-4 shadow-sm">
                <div className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted mb-1">
                  Статус
                </div>
                <p className="text-[14px] text-b24-text-dark">
                  {completed
                    ? 'Задача выполнена. Можно вернуть в работу или удалить.'
                    : 'Отметьте выполнение слева или оставьте комментарий ниже.'}
                </p>
              </div>
            )}

            <div className="rounded-[12px] border border-black/8 bg-white p-4 shadow-sm flex-1 min-h-[280px]">
              <div className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted mb-3">
                Комментарии
              </div>
              <TaskComments taskId={task.id} />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

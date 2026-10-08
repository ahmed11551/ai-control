import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useMemo, useState } from 'react'
import {
  addDays,
  formatMonthYear,
  formatTaskDate,
  getWeekdayMon,
  isSameDay,
  isTaskOverdue,
  startOfDay,
  toDateKey,
  type Task,
} from '../../data/tasks'

type TasksCalendarProps = {
  tasks: Task[]
  onOpen: (id: string) => void
}

const WEEKDAYS = ['пн', 'вт', 'ср', 'чт', 'пт', 'сб', 'вс']

export function TasksCalendar({ tasks, onOpen }: TasksCalendarProps) {
  const [cursor, setCursor] = useState(() => startOfDay())
  const [selectedDay, setSelectedDay] = useState<string | null>(null)

  const monthStart = useMemo(
    () => new Date(cursor.getFullYear(), cursor.getMonth(), 1),
    [cursor],
  )

  const cells = useMemo(() => {
    const firstWeekday = getWeekdayMon(monthStart)
    const gridStart = addDays(monthStart, -firstWeekday)
    return Array.from({ length: 42 }, (_, i) => addDays(gridStart, i))
  }, [monthStart])

  const byDay = useMemo(() => {
    const map = new Map<string, Task[]>()
    for (const task of tasks) {
      if (!task.deadline) continue
      const key = toDateKey(task.deadline)
      const list = map.get(key) ?? []
      list.push(task)
      map.set(key, list)
    }
    return map
  }, [tasks])

  const today = startOfDay()
  const selectedTasks = selectedDay ? byDay.get(selectedDay) ?? [] : []

  return (
    <div className="b24-glass-panel overflow-hidden min-h-[480px] flex flex-col lg:flex-row">
      <div className="flex-1 min-w-0 flex flex-col border-b lg:border-b-0 lg:border-r border-black/6">
        <div className="flex items-center justify-between px-3 py-2.5 border-b border-black/6 bg-b24-panel-header">
          <h3 className="text-[15px] font-semibold text-b24-text-dark">
            {formatMonthYear(monthStart)}
          </h3>
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() =>
                setCursor(new Date(cursor.getFullYear(), cursor.getMonth() - 1, 1))
              }
              className="size-8 inline-flex items-center justify-center rounded-full text-b24-text hover:bg-black/5"
              aria-label="Предыдущий месяц"
            >
              <ChevronLeft size={18} strokeWidth={1.75} />
            </button>
            <button
              type="button"
              onClick={() => {
                const t = startOfDay()
                setCursor(t)
                setSelectedDay(toDateKey(t))
              }}
              className="h-8 px-3 rounded-b24-pill text-[12px] font-medium text-b24-text-dark bg-white border border-black/8 hover:bg-[#f5f7f8]"
            >
              Сегодня
            </button>
            <button
              type="button"
              onClick={() =>
                setCursor(new Date(cursor.getFullYear(), cursor.getMonth() + 1, 1))
              }
              className="size-8 inline-flex items-center justify-center rounded-full text-b24-text hover:bg-black/5"
              aria-label="Следующий месяц"
            >
              <ChevronRight size={18} strokeWidth={1.75} />
            </button>
          </div>
        </div>

        <div className="grid grid-cols-7 border-b border-black/6 bg-[#f7f8fa]">
          {WEEKDAYS.map((d) => (
            <div
              key={d}
              className="px-2 py-2 text-center text-[11px] uppercase tracking-wide text-b24-text-muted font-medium"
            >
              {d}
            </div>
          ))}
        </div>

        <div className="grid grid-cols-7 flex-1 auto-rows-fr min-h-[360px]">
          {cells.map((day) => {
            const key = toDateKey(day)
            const dayTasks = byDay.get(key) ?? []
            const inMonth = day.getMonth() === monthStart.getMonth()
            const isToday = isSameDay(day, today)
            const isSelected = selectedDay === key
            const hasOverdue = dayTasks.some((t) => isTaskOverdue(t))

            return (
              <button
                key={key}
                type="button"
                onClick={() => setSelectedDay(key)}
                className={[
                  'relative min-h-[84px] border-r border-b border-black/5 p-1.5 text-left transition-colors',
                  'hover:bg-[#f5f9fb] align-top',
                  !inMonth ? 'bg-[#fafbfc] text-b24-text-muted' : 'bg-white text-b24-text-dark',
                  isSelected ? 'bg-[#e8f8fd] ring-1 ring-inset ring-b24-cyan/40' : '',
                ].join(' ')}
              >
                <div className="flex items-center justify-between mb-1">
                  <span
                    className={[
                      'inline-flex size-6 items-center justify-center rounded-full text-[12px] font-semibold',
                      isToday ? 'bg-b24-cyan text-white' : '',
                      hasOverdue && !isToday ? 'text-orange-600' : '',
                    ].join(' ')}
                  >
                    {day.getDate()}
                  </span>
                  {dayTasks.length > 0 ? (
                    <span className="text-[10px] text-b24-text-muted tabular-nums">
                      {dayTasks.length}
                    </span>
                  ) : null}
                </div>

                <div className="flex flex-col gap-0.5">
                  {dayTasks.slice(0, 3).map((task) => (
                    <span
                      key={task.id}
                      role="link"
                      tabIndex={0}
                      onClick={(e) => {
                        e.stopPropagation()
                        onOpen(task.id)
                      }}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.stopPropagation()
                          onOpen(task.id)
                        }
                      }}
                      className={[
                        'block truncate rounded px-1 py-[2px] text-[11px] leading-tight cursor-pointer',
                        isTaskOverdue(task)
                          ? 'bg-orange-50 text-orange-700'
                          : task.status === 'completed'
                            ? 'bg-[#eef2f4] text-b24-text-muted line-through'
                            : 'bg-[#e8f8fd] text-[#0b7ea4]',
                      ].join(' ')}
                    >
                      {task.title}
                    </span>
                  ))}
                  {dayTasks.length > 3 ? (
                    <span className="text-[10px] text-b24-text-muted px-1">
                      ещё {dayTasks.length - 3}
                    </span>
                  ) : null}
                </div>
              </button>
            )
          })}
        </div>
      </div>

      <aside className="w-full lg:w-[280px] shrink-0 bg-[#f8f9fb] flex flex-col max-h-[480px] lg:max-h-none">
        <div className="px-4 py-3 border-b border-black/6">
          <div className="text-[12px] text-b24-text-muted">День</div>
          <div className="text-[15px] font-semibold text-b24-text-dark">
            {selectedDay
              ? formatTaskDate(selectedDay)
              : 'Выберите день'}
          </div>
        </div>
        <div className="flex-1 overflow-y-auto b24-scrollbar p-3 flex flex-col gap-2">
          {!selectedDay ? (
            <p className="text-[13px] text-b24-text-muted px-1 py-6 text-center">
              Кликните по дню, чтобы увидеть задачи со сроком
            </p>
          ) : selectedTasks.length === 0 ? (
            <p className="text-[13px] text-b24-text-muted px-1 py-6 text-center">
              Нет задач на этот день
            </p>
          ) : (
            selectedTasks.map((task) => {
              const overdue = isTaskOverdue(task)
              return (
                <button
                  key={task.id}
                  type="button"
                  onClick={() => onOpen(task.id)}
                  className="text-left rounded-[10px] bg-white border border-black/6 px-3 py-2.5 hover:shadow-sm transition-shadow"
                >
                  <div
                    className={[
                      'text-[13px] font-medium text-b24-text-dark',
                      task.status === 'completed' ? 'line-through text-b24-text-muted' : '',
                    ].join(' ')}
                  >
                    {task.title}
                  </div>
                  <div
                    className={[
                      'mt-1 text-[11px]',
                      overdue ? 'text-orange-600 font-medium' : 'text-b24-text-muted',
                    ].join(' ')}
                  >
                    {task.assignee}
                    {task.deadline ? ` · ${formatTaskDate(task.deadline)}` : ''}
                  </div>
                </button>
              )
            })
          )}
        </div>
      </aside>
    </div>
  )
}

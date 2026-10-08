import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useMemo, useState } from 'react'
import {
  addDays,
  getGanttRange,
  isTaskOverdue,
  startOfDay,
  toDateKey,
  type Task,
} from '../../data/tasks'

type TasksGanttProps = {
  tasks: Task[]
  onOpen: (id: string) => void
}

const DAY_WIDTH = 36
const LABEL_WIDTH = 220
const ROW_HEIGHT = 40
const TOTAL_DAYS = 42

export function TasksGantt({ tasks, onOpen }: TasksGanttProps) {
  const [offset, setOffset] = useState(-7)

  const rangeStart = useMemo(() => addDays(startOfDay(), offset), [offset])
  const days = useMemo(
    () => Array.from({ length: TOTAL_DAYS }, (_, i) => addDays(rangeStart, i)),
    [rangeStart],
  )
  const today = startOfDay()
  const todayKey = toDateKey(today)
  const todayIndex = days.findIndex((d) => toDateKey(d) === todayKey)

  const timelineWidth = TOTAL_DAYS * DAY_WIDTH

  return (
    <div className="b24-glass-panel overflow-hidden min-h-[480px] flex flex-col">
      <div className="flex items-center justify-between px-3 py-2.5 border-b border-black/6 bg-b24-panel-header">
        <div className="text-[13px] text-b24-text font-medium">Диаграмма Ганта</div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => setOffset((v) => v - 14)}
            className="size-8 inline-flex items-center justify-center rounded-full text-b24-text hover:bg-black/5"
            aria-label="Назад"
          >
            <ChevronLeft size={18} strokeWidth={1.75} />
          </button>
          <button
            type="button"
            onClick={() => setOffset(-7)}
            className="h-8 px-3 rounded-b24-pill text-[12px] font-medium text-b24-text-dark bg-white border border-black/8 hover:bg-[#f5f7f8]"
          >
            Сегодня
          </button>
          <button
            type="button"
            onClick={() => setOffset((v) => v + 14)}
            className="size-8 inline-flex items-center justify-center rounded-full text-b24-text hover:bg-black/5"
            aria-label="Вперёд"
          >
            <ChevronRight size={18} strokeWidth={1.75} />
          </button>
        </div>
      </div>

      {tasks.length === 0 ? (
        <div className="flex-1 flex flex-col items-center justify-center px-8 py-16 text-center">
          <h3 className="text-[22px] font-normal text-[#a0a5ab] mb-2">Нет задач</h3>
          <p className="text-[14px] text-[#b0b5bb]">
            Создайте задачу — она появится на диаграмме
          </p>
        </div>
      ) : (
        <div className="overflow-auto b24-scrollbar flex-1">
          <div style={{ minWidth: LABEL_WIDTH + timelineWidth }} className="relative">
            <div className="sticky top-0 z-20 flex border-b border-black/8 bg-b24-panel-header">
              <div
                className="sticky left-0 z-30 shrink-0 border-r border-black/8 bg-b24-panel-header px-3 py-2 text-[12px] font-medium text-b24-text-muted"
                style={{ width: LABEL_WIDTH }}
              >
                Задача
              </div>
              <div className="flex" style={{ width: timelineWidth }}>
                {days.map((day) => {
                  const key = toDateKey(day)
                  const isToday = key === todayKey
                  const isWeekend = day.getDay() === 0 || day.getDay() === 6
                  return (
                    <div
                      key={key}
                      className={[
                        'shrink-0 border-r border-black/5 px-0.5 py-1.5 text-center',
                        isWeekend ? 'bg-[#f3f5f7]' : '',
                        isToday ? 'bg-[#e8f8fd]' : '',
                      ].join(' ')}
                      style={{ width: DAY_WIDTH }}
                    >
                      <div className="text-[9px] uppercase text-b24-text-muted leading-none">
                        {day.toLocaleDateString('ru-RU', { weekday: 'short' })}
                      </div>
                      <div
                        className={[
                          'mt-0.5 text-[11px] font-semibold leading-none',
                          isToday ? 'text-[#0b7ea4]' : 'text-b24-text-dark',
                        ].join(' ')}
                      >
                        {day.getDate()}
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>

            {tasks.map((task) => {
              const { start, end } = getGanttRange(task)
              const startOffset = Math.round(
                (startOfDay(start).getTime() - rangeStart.getTime()) / 86400000,
              )
              const durationDays = Math.max(
                1,
                Math.round((end.getTime() - start.getTime()) / 86400000) + 1,
              )
              const left = startOffset * DAY_WIDTH
              const width = durationDays * DAY_WIDTH - 6
              const overdue = isTaskOverdue(task)
              const completed = task.status === 'completed'
              const visible =
                startOffset + durationDays > 0 && startOffset < TOTAL_DAYS

              return (
                <div
                  key={task.id}
                  className="flex border-b border-black/5 hover:bg-[#f7fafc]"
                  style={{ height: ROW_HEIGHT }}
                >
                  <button
                    type="button"
                    onClick={() => onOpen(task.id)}
                    className="sticky left-0 z-10 shrink-0 border-r border-black/8 bg-white px-3 text-left hover:bg-[#f5f8fa] truncate"
                    style={{ width: LABEL_WIDTH, height: ROW_HEIGHT }}
                    title={task.title}
                  >
                    <span
                      className={[
                        'text-[13px] font-medium text-b24-text-dark',
                        completed ? 'line-through text-b24-text-muted' : '',
                      ].join(' ')}
                    >
                      {task.title}
                    </span>
                  </button>

                  <div
                    className="relative"
                    style={{ width: timelineWidth, height: ROW_HEIGHT }}
                  >
                    {days.map((day, i) => {
                      const isWeekend = day.getDay() === 0 || day.getDay() === 6
                      return (
                        <div
                          key={toDateKey(day)}
                          className={[
                            'absolute top-0 bottom-0 border-r border-black/4',
                            isWeekend ? 'bg-[#f6f7f9]' : '',
                          ].join(' ')}
                          style={{ left: i * DAY_WIDTH, width: DAY_WIDTH }}
                        />
                      )
                    })}

                    {todayIndex >= 0 ? (
                      <div
                        className="absolute top-0 bottom-0 w-px bg-b24-cyan/80 z-[1]"
                        style={{ left: todayIndex * DAY_WIDTH + DAY_WIDTH / 2 }}
                      />
                    ) : null}

                    {visible ? (
                      <button
                        type="button"
                        onClick={() => onOpen(task.id)}
                        className={[
                          'absolute top-2 h-6 rounded-md px-2 text-[11px] font-medium text-white truncate shadow-sm z-[2]',
                          'hover:brightness-105 transition-[filter]',
                          completed
                            ? 'bg-[#a8adb4]'
                            : overdue
                              ? 'bg-orange-500'
                              : 'bg-[linear-gradient(90deg,#2fc6f6,#2fc06e)]',
                        ].join(' ')}
                        style={{
                          left: Math.max(left, 0) + 3,
                          width: Math.max(
                            width -
                              (left < 0 ? -left : 0) -
                              Math.max(0, left + width - timelineWidth),
                            24,
                          ),
                        }}
                        title={task.title}
                      >
                        {task.title}
                      </button>
                    ) : null}
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}

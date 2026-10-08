import { ListTodo, X } from 'lucide-react'
import { useEffect, useId, useState } from 'react'
import { Button } from '../ui/Button'
import { DateTimePicker } from '../ui/DateTimePicker'

type CreateTaskModalProps = {
  open: boolean
  onClose: () => void
  onSubmit: (data: {
    title: string
    deadline?: string
    plannedDate?: string
    assignee?: string
    tags?: string[]
    isImportant?: boolean
  }) => void
}

const fieldClass =
  'h-11 w-full rounded-[12px] border border-black/10 bg-[#f5f7f9] px-3.5 text-[14px] text-b24-text-dark outline-none transition focus:border-b24-cyan focus:bg-white focus:ring-2 focus:ring-b24-cyan/20'

export function CreateTaskModal({ open, onClose, onSubmit }: CreateTaskModalProps) {
  const titleId = useId()
  const [title, setTitle] = useState('')
  const [deadline, setDeadline] = useState('')
  const [plannedDate, setPlannedDate] = useState('')
  const [assignee, setAssignee] = useState('')
  const [tags, setTags] = useState('')
  const [isImportant, setIsImportant] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) return
    setTitle('')
    setDeadline('')
    setPlannedDate('')
    setAssignee('')
    setTags('')
    setIsImportant(false)
    setError('')
  }, [open])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = title.trim()
    if (!trimmed) {
      setError('Укажите название задачи')
      return
    }
    if (isImportant && !deadline) {
      setError('Для важной задачи укажите дату и время')
      return
    }
    onSubmit({
      title: trimmed,
      deadline: deadline || undefined,
      plannedDate: plannedDate || undefined,
      assignee: assignee.trim() || undefined,
      tags: tags
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean),
      isImportant,
    })
    onClose()
  }

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[6vh] px-4">
      <button
        type="button"
        className="absolute inset-0 bg-[#1a1545]/55 backdrop-blur-[3px]"
        aria-label="Закрыть"
        onClick={onClose}
      />

      <form
        onSubmit={handleSubmit}
        className="relative z-10 w-full max-w-[640px] rounded-[18px] bg-white shadow-[0_24px_80px_rgba(26,21,69,0.35)] border border-black/5 overflow-hidden"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
      >
        <div className="relative px-6 pt-5 pb-4 border-b border-black/6 bg-[linear-gradient(135deg,#f0fbfd_0%,#ffffff_55%)]">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="text-[11px] font-semibold uppercase tracking-wide text-b24-cyan-deep mb-1">
                Задачи
              </div>
              <h2 id={titleId} className="text-[22px] font-semibold text-b24-text-dark tracking-tight">
                Новая задача
              </h2>
              <p className="mt-1 text-[13px] text-b24-text-muted">
                Название и срок — главное. Остальное можно заполнить позже.
              </p>
            </div>
            <button
              type="button"
              onClick={onClose}
              className="size-9 inline-flex items-center justify-center rounded-full text-b24-text-muted hover:bg-black/5"
              aria-label="Закрыть"
            >
              <X size={18} />
            </button>
          </div>
        </div>

        <div className="px-6 py-5 flex flex-col gap-5 max-h-[min(70vh,640px)] overflow-y-auto b24-scrollbar">
          <section className="flex flex-col gap-3">
            <h3 className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted">
              Основное
            </h3>
            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-medium text-b24-text">Название</span>
              <div className="relative">
                <ListTodo
                  size={16}
                  className="absolute left-3.5 top-1/2 -translate-y-1/2 text-b24-text-muted"
                />
                <input
                  autoFocus
                  value={title}
                  onChange={(e) => {
                    setTitle(e.target.value)
                    if (error) setError('')
                  }}
                  placeholder="Что нужно сделать?"
                  className={`${fieldClass} pl-10`}
                />
              </div>
            </label>

            <div className="flex flex-col gap-2">
              <span className="text-[12px] font-medium text-b24-text">Тип</span>
              <div className="flex flex-wrap gap-2">
                {[
                  { important: false, label: 'Обычная' },
                  { important: true, label: 'Важная' },
                ].map((opt) => {
                  const active = isImportant === opt.important
                  return (
                    <button
                      key={opt.label}
                      type="button"
                      onClick={() => {
                        setIsImportant(opt.important)
                        if (opt.important && deadline && !deadline.includes('T')) {
                          setDeadline(`${deadline}T12:00`)
                        }
                        if (error) setError('')
                      }}
                      className={[
                        'h-9 px-3.5 rounded-full text-[13px] font-medium border transition',
                        active
                          ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep shadow-[0_0_0_1px_rgba(47,198,246,0.25)]'
                          : 'bg-[#f5f7f9] border-transparent text-b24-text hover:bg-[#eef1f4]',
                      ].join(' ')}
                    >
                      {opt.label}
                    </button>
                  )
                })}
              </div>
            </div>
          </section>

          <section className="flex flex-col gap-3">
            <h3 className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted">
              Сроки
            </h3>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <DateTimePicker
                label={isImportant ? 'Дата и время' : 'Крайний срок'}
                value={deadline}
                onChange={(v) => {
                  setDeadline(v)
                  if (error) setError('')
                }}
                withTime={isImportant}
                required={isImportant}
                placeholder={isImportant ? 'Когда связаться' : 'Выберите день'}
              />
              <DateTimePicker
                label="В плане"
                value={plannedDate}
                onChange={setPlannedDate}
                placeholder="Когда делать"
              />
            </div>
            {isImportant ? (
              <p className="text-[13px] text-b24-text-muted">
                Перед сроком появится уведомление — нужно будет зафиксировать результат.
              </p>
            ) : null}
          </section>

          <section className="flex flex-col gap-3">
            <h3 className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted">
              Участники
            </h3>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-medium text-b24-text">Исполнитель</span>
                <input
                  value={assignee}
                  onChange={(e) => setAssignee(e.target.value)}
                  placeholder="Имя сотрудника"
                  className={fieldClass}
                />
              </label>
              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-medium text-b24-text">Теги</span>
                <input
                  value={tags}
                  onChange={(e) => setTags(e.target.value)}
                  placeholder="через запятую"
                  className={fieldClass}
                />
              </label>
            </div>
          </section>

          {error ? <p className="text-[13px] text-red-500">{error}</p> : null}
        </div>

        <div className="flex items-center justify-between gap-3 px-6 py-4 border-t border-black/6 bg-[#fafbfc]">
          <p className="text-[12px] text-b24-text-muted hidden sm:block">
            Задача появится в списке сразу после создания
          </p>
          <div className="flex items-center gap-2 ml-auto">
            <Button type="button" variant="filter" className="rounded-[10px] h-10 px-4" onClick={onClose}>
              Отмена
            </Button>
            <Button type="submit" variant="green" className="rounded-[10px] h-10 px-6">
              {isImportant ? 'Создать важную' : 'Создать задачу'}
            </Button>
          </div>
        </div>
      </form>
    </div>
  )
}

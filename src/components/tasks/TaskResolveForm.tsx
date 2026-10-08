import { useEffect, useState } from 'react'
import {
  TASK_RESOLVE_OPTIONS,
  toDatetimeLocalValue,
  type TaskResolveResult,
} from '../../data/tasks'
import { Button } from '../ui/Button'
import { DateTimePicker } from '../ui/DateTimePicker'

const fieldClass =
  'w-full rounded-[12px] border border-black/10 bg-[#f5f7f9] px-3.5 py-2.5 text-[14px] text-b24-text-dark outline-none transition focus:border-b24-cyan focus:bg-white focus:ring-2 focus:ring-b24-cyan/20 resize-y min-h-[84px]'

type TaskResolveFormProps = {
  variant?: 'panel' | 'gate'
  isMissed?: boolean
  pending?: boolean
  error?: string
  noteRequired?: boolean
  onSubmit: (payload: {
    result: TaskResolveResult
    note: string
    rescheduleAt?: string
  }) => void
}

export function TaskResolveForm({
  variant = 'panel',
  isMissed,
  pending,
  error,
  noteRequired,
  onSubmit,
}: TaskResolveFormProps) {
  const [result, setResult] = useState<TaskResolveResult | ''>('')
  const [note, setNote] = useState('')
  const [rescheduleAt, setRescheduleAt] = useState('')
  const [localError, setLocalError] = useState('')

  useEffect(() => {
    setResult('')
    setNote('')
    setRescheduleAt('')
    setLocalError('')
  }, [isMissed])

  const requireNote = Boolean(noteRequired || isMissed || result === 'other')

  const submit = () => {
    if (!result) {
      setLocalError('Выберите результат')
      return
    }
    if (requireNote && !note.trim()) {
      setLocalError('Укажите комментарий')
      return
    }
    if (result === 'reschedule' && !rescheduleAt) {
      setLocalError('Укажите новую дату и время')
      return
    }
    setLocalError('')
    onSubmit({
      result,
      note: note.trim(),
      rescheduleAt: result === 'reschedule' ? rescheduleAt : undefined,
    })
  }

  return (
    <div
      className={[
        'flex flex-col gap-3',
        variant === 'panel'
          ? 'rounded-[12px] border border-black/8 bg-white p-3.5 shadow-sm'
          : '',
      ].join(' ')}
    >
      <div>
        <div className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted">
          Результат
        </div>
        {variant === 'panel' ? (
          <p className="mt-0.5 text-[12px] text-b24-text-muted">
            {isMissed
              ? 'Откройте карточку и зафиксируйте результат или комментарий.'
              : 'Важная задача закрывается только с результатом.'}
          </p>
        ) : null}
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
        {TASK_RESOLVE_OPTIONS.map((opt) => (
          <button
            key={opt.value}
            type="button"
            onClick={() => {
              setResult(opt.value)
              setLocalError('')
              if (opt.value === 'reschedule' && !rescheduleAt) {
                setRescheduleAt(toDatetimeLocalValue(new Date(Date.now() + 86400000).toISOString()))
              }
            }}
            className={[
              'rounded-[12px] border px-3.5 py-2.5 text-left text-[13px] font-medium transition',
              result === opt.value
                ? 'border-b24-cyan bg-[#eef9fc] text-b24-text-dark shadow-[inset_0_0_0_1px_rgba(46,178,204,0.35)]'
                : 'border-black/8 bg-[#f5f7f9] text-b24-text hover:bg-white',
            ].join(' ')}
          >
            {opt.label}
          </button>
        ))}
      </div>

      {result === 'reschedule' ? (
        <DateTimePicker
          label="Новая дата и время"
          value={rescheduleAt}
          onChange={setRescheduleAt}
          withTime
          required
          placeholder="Когда перенести"
        />
      ) : null}

      <label className="flex flex-col gap-1.5">
        <span className="text-[12px] font-medium text-b24-text">
          {requireNote ? 'Комментарий' : 'Комментарий (необязательно)'}
          {requireNote ? <span className="text-red-500"> *</span> : null}
        </span>
        <textarea
          value={note}
          onChange={(e) => {
            setNote(e.target.value)
            if (localError) setLocalError('')
          }}
          rows={3}
          placeholder={
            isMissed
              ? 'Что сделали / почему не перезвонили / иной результат…'
              : 'Краткий комментарий к результату'
          }
          className={fieldClass}
        />
      </label>

      {localError || error ? (
        <p className="text-[13px] text-red-600">{localError || error}</p>
      ) : null}

      <Button
        type="button"
        variant="green"
        className="rounded-[10px] h-10 px-5 w-full sm:w-auto sm:self-end justify-center"
        disabled={pending}
        onClick={submit}
      >
        {pending ? 'Сохранение…' : 'Зафиксировать'}
      </Button>
    </div>
  )
}

import { X } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { Button } from '../ui/Button'
import { EntitySearch } from './EntitySearch'

export type CrmField = {
  name: string
  label: string
  type?: 'text' | 'email' | 'tel' | 'number' | 'select' | 'search'
  required?: boolean
  placeholder?: string
  options?: { value: string; label: string }[]
  searchPath?: string
  searchTitle?: string
}

type CrmFormModalProps = {
  open: boolean
  title: string
  fields: CrmField[]
  initial?: Record<string, string>
  pending?: boolean
  error?: string
  submitLabel?: string
  onClose: () => void
  onSubmit: (values: Record<string, string>) => void
}

const inputClass =
  'h-10 rounded-b24-sm border border-black/10 px-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan focus:ring-2 focus:ring-b24-cyan/20 bg-white'

export function CrmFormModal({
  open,
  title,
  fields,
  initial,
  pending,
  error,
  submitLabel = 'Сохранить',
  onClose,
  onSubmit,
}: CrmFormModalProps) {
  const titleId = useId()
  const [values, setValues] = useState<Record<string, string>>({})
  const [localError, setLocalError] = useState('')
  const wasOpen = useRef(false)

  useEffect(() => {
    if (open && !wasOpen.current) {
      const next: Record<string, string> = {}
      for (const field of fields) {
        next[field.name] = initial?.[field.name] ?? ''
        if (field.type === 'search') {
          next[`${field.name}_label`] = initial?.[`${field.name}_label`] ?? ''
        }
      }
      setValues(next)
      setLocalError('')
    }
    wasOpen.current = open
  }, [open, fields, initial])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  const set = (name: string, value: string) => {
    setValues((prev) => ({ ...prev, [name]: value }))
    if (localError) setLocalError('')
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    for (const field of fields) {
      if (field.required && !String(values[field.name] ?? '').trim()) {
        setLocalError(`Укажите: ${field.label}`)
        return
      }
    }
    onSubmit(values)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[8vh] px-4">
      <button
        type="button"
        className="absolute inset-0 bg-black/50 backdrop-blur-[2px]"
        aria-label="Закрыть"
        onClick={onClose}
      />
      <form
        onSubmit={handleSubmit}
        className="relative z-10 w-full max-w-[520px] rounded-b24 bg-white shadow-2xl border border-black/5 overflow-hidden"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
      >
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-black/6">
          <h2 id={titleId} className="text-[16px] font-semibold text-b24-text-dark">
            {title}
          </h2>
          <button
            type="button"
            onClick={onClose}
            className="size-8 inline-flex items-center justify-center rounded-full text-b24-text-muted hover:bg-black/5"
            aria-label="Закрыть"
          >
            <X size={18} strokeWidth={1.75} />
          </button>
        </div>

        <div className="px-5 py-4 grid grid-cols-2 gap-3.5">
          {fields.map((field, index) => (
            <label
              key={field.name}
              className={['flex flex-col gap-1.5', field.type === 'select' || field.type === 'search' || index === 0 ? 'col-span-2' : ''].join(' ')}
            >
              <span className="text-[12px] font-medium text-b24-text">{field.label}</span>
              {field.type === 'search' && field.searchPath ? (
                <EntitySearch
                  path={field.searchPath}
                  titleKey={field.searchTitle ?? 'title'}
                  value={values[field.name] ?? ''}
                  label={values[`${field.name}_label`] ?? ''}
                  placeholder={field.placeholder}
                  onChange={(id, title) => {
                    setValues((prev) => ({
                      ...prev,
                      [field.name]: id,
                      [`${field.name}_label`]: title,
                    }))
                    if (localError) setLocalError('')
                  }}
                />
              ) : field.type === 'select' ? (
                <select
                  value={values[field.name] ?? ''}
                  onChange={(e) => set(field.name, e.target.value)}
                  className={inputClass}
                >
                  <option value="">Не выбран</option>
                  {(field.options ?? []).map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  autoFocus={index === 0}
                  type={field.type ?? 'text'}
                  value={values[field.name] ?? ''}
                  onChange={(e) => set(field.name, e.target.value)}
                  placeholder={field.placeholder}
                  className={inputClass}
                />
              )}
            </label>
          ))}
          {localError || error ? (
            <p className="col-span-2 text-[12px] text-red-500">{localError || error}</p>
          ) : null}
        </div>

        <div className="flex items-center justify-end gap-2 px-5 py-3.5 border-t border-black/6 bg-[#f8f9fb]">
          <Button type="button" variant="filter" onClick={onClose} className="rounded-b24-sm">
            Отмена
          </Button>
          <Button type="submit" variant="green" disabled={pending} className="rounded-b24-sm px-5">
            {pending ? 'Сохранение…' : submitLabel}
          </Button>
        </div>
      </form>
    </div>
  )
}

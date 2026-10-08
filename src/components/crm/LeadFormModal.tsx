import { MapPin, MessageSquare, Phone, User, X } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { Button } from '../ui/Button'

type LeadFormModalProps = {
  open: boolean
  editing?: boolean
  pending?: boolean
  error?: string
  initial?: Record<string, string>
  headline?: string
  onClose: () => void
  onSubmit: (values: Record<string, string>) => void
}

function formatPhoneInput(raw: string) {
  const d = raw.replace(/\D/g, '').slice(0, 11)
  if (!d) return ''
  let n = d
  if (n[0] === '8') n = '7' + n.slice(1)
  if (n[0] !== '7') n = '7' + n
  n = n.slice(0, 11)
  const rest = n.slice(1)
  if (rest.length <= 3) return `+7 ${rest}`
  if (rest.length <= 6) return `+7 ${rest.slice(0, 3)} ${rest.slice(3)}`
  if (rest.length <= 8) return `+7 ${rest.slice(0, 3)} ${rest.slice(3, 6)}-${rest.slice(6)}`
  return `+7 ${rest.slice(0, 3)} ${rest.slice(3, 6)}-${rest.slice(6, 8)}-${rest.slice(8, 10)}`
}

const fieldClass =
  'h-11 w-full rounded-[12px] border border-black/10 bg-[#f5f7f9] px-3.5 text-[14px] text-b24-text-dark outline-none transition focus:border-b24-cyan focus:bg-white focus:ring-2 focus:ring-b24-cyan/20'

export function LeadFormModal({
  open,
  editing,
  pending,
  error,
  initial,
  headline,
  onClose,
  onSubmit,
}: LeadFormModalProps) {
  const titleId = useId()
  const [title, setTitle] = useState('')
  const [candidate, setCandidate] = useState('')
  const [phone, setPhone] = useState('')
  const [city, setCity] = useState('')
  const [comment, setComment] = useState('')
  const [localError, setLocalError] = useState('')
  const wasOpen = useRef(false)

  useEffect(() => {
    if (open && !wasOpen.current) {
      setTitle(initial?.title ?? '')
      setCandidate(initial?.candidate ?? initial?.contact ?? '')
      setPhone(
        initial?.phone
          ? formatPhoneInput(initial.phone)
          : initial?.contact_phone
            ? formatPhoneInput(String(initial.contact_phone))
            : '',
      )
      setCity(initial?.city ?? '')
      setComment(initial?.card_note ?? initial?.comment ?? '')
      setLocalError('')
    }
    wasOpen.current = open
  }, [open, initial])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    const name = title.trim()
    if (!name) {
      setLocalError('Укажите название лида')
      return
    }
    onSubmit({
      title: name,
      candidate: candidate.trim(),
      phone: phone.replace(/\D/g, ''),
      city: city.trim(),
      card_note: comment.trim(),
      status: 'new',
      source: 'manual',
    })
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
        onSubmit={submit}
        className="relative z-10 w-full max-w-[520px] rounded-[18px] bg-white shadow-[0_24px_80px_rgba(26,21,69,0.35)] border border-black/5 overflow-hidden"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
      >
        <div className="relative px-6 pt-5 pb-4 border-b border-black/6 bg-[linear-gradient(135deg,#f0fbfd_0%,#ffffff_55%)]">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="text-[11px] font-semibold uppercase tracking-wide text-b24-cyan-deep mb-1">
                CRM · Лид
              </div>
              <h2 id={titleId} className="text-[22px] font-semibold text-b24-text-dark tracking-tight">
                {headline || (editing ? 'Изменить лид' : 'Новый лид')}
              </h2>
              <p className="mt-1 text-[13px] text-b24-text-muted">
                Название, кандидат, телефон и город — комментарий по желанию.
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

        <div className="px-6 py-5 flex flex-col gap-4">
          <label className="flex flex-col gap-1.5">
            <span className="text-[12px] font-medium text-b24-text">Имя (название лида)</span>
            <div className="relative">
              <User size={16} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-b24-text-muted" />
              <input
                autoFocus
                value={title}
                onChange={(e) => {
                  setTitle(e.target.value)
                  setLocalError('')
                }}
                placeholder="Название заявки"
                className={`${fieldClass} pl-10`}
              />
            </div>
          </label>

          <label className="flex flex-col gap-1.5">
            <span className="text-[12px] font-medium text-b24-text">Кандидат</span>
            <input
              value={candidate}
              onChange={(e) => setCandidate(e.target.value)}
              placeholder="ФИО кандидата"
              className={fieldClass}
            />
          </label>

          <label className="flex flex-col gap-1.5">
            <span className="text-[12px] font-medium text-b24-text">Номер</span>
            <div className="relative">
              <Phone size={16} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-b24-green" />
              <input
                type="tel"
                value={phone}
                readOnly={Boolean(editing)}
                onChange={(e) => setPhone(formatPhoneInput(e.target.value))}
                placeholder="+7 900 123-45-67"
                className={`${fieldClass} pl-10 tabular-nums ${editing ? 'opacity-70' : ''}`}
              />
            </div>
          </label>

          <label className="flex flex-col gap-1.5">
            <span className="text-[12px] font-medium text-b24-text">Город</span>
            <div className="relative">
              <MapPin size={16} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-b24-text-muted" />
              <input
                value={city}
                onChange={(e) => setCity(e.target.value)}
                placeholder="Москва"
                className={`${fieldClass} pl-10`}
              />
            </div>
          </label>

          <label className="flex flex-col gap-1.5">
            <span className="text-[12px] font-medium text-b24-text">
              Комментарий <span className="font-normal text-b24-text-muted">(необязательно)</span>
            </span>
            <div className="relative">
              <MessageSquare
                size={16}
                className="absolute left-3.5 top-3.5 text-b24-text-muted pointer-events-none"
              />
              <textarea
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder="Краткий комментарий к кандидату"
                rows={3}
                className="min-h-[88px] w-full resize-y rounded-[12px] border border-black/10 bg-[#f5f7f9] px-3.5 py-2.5 pl-10 text-[14px] text-b24-text-dark outline-none transition focus:border-b24-cyan focus:bg-white focus:ring-2 focus:ring-b24-cyan/20"
              />
            </div>
          </label>

          {localError || error ? (
            <p className="text-[13px] text-red-500">{localError || error}</p>
          ) : null}
        </div>

        <div className="flex items-center justify-between gap-3 px-6 py-4 border-t border-black/6 bg-[#fafbfc]">
          <p className="text-[12px] text-b24-text-muted hidden sm:block">
            После создания откроется карточка
          </p>
          <div className="flex items-center gap-2 ml-auto">
            <Button type="button" variant="filter" className="rounded-[10px] h-10 px-4" onClick={onClose}>
              Отмена
            </Button>
            <Button type="submit" variant="green" disabled={pending} className="rounded-[10px] h-10 px-6">
              {pending ? 'Сохранение…' : editing ? 'Сохранить' : 'Создать'}
            </Button>
          </div>
        </div>
      </form>
    </div>
  )
}

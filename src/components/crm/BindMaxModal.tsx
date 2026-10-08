import { ChevronRight, X } from 'lucide-react'
import { useEffect, useId, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../../lib/api'
import { Button } from '../ui/Button'

export type MaxNumberItem = {
  id: string
  line_id?: string
  channel_id?: string
  phone: string
  label: string
  entities?: number
}

function formatMaxPhone(raw: string) {
  const d = raw.replace(/\D/g, '')
  if (d.length === 11 && (d[0] === '7' || d[0] === '8')) {
    const n = '7' + d.slice(1)
    return `+${n[0]} ${n.slice(1, 4)} ${n.slice(4, 7)}-${n.slice(7, 9)}-${n.slice(9, 11)}`
  }
  if (d.length === 10) {
    return `+7 ${d.slice(0, 3)} ${d.slice(3, 6)}-${d.slice(6, 8)}-${d.slice(8, 10)}`
  }
  return raw || '—'
}

function dialogsLabel(n: number) {
  const abs = Math.abs(n) % 100
  const last = abs % 10
  if (abs > 10 && abs < 20) return `${n} диалогов`
  if (last === 1) return `${n} диалог`
  if (last >= 2 && last <= 4) return `${n} диалога`
  return `${n} диалогов`
}

type BindMaxModalProps = {
  open: boolean
  leadId: string
  clientName: string
  clientPhone: string
  ownerId?: string
  onClose: () => void
  onBound?: (threadId: string) => void
}

export function BindMaxModal({
  open,
  leadId,
  clientName,
  clientPhone,
  onClose,
  onBound,
}: BindMaxModalProps) {
  const titleId = useId()
  const navigate = useNavigate()
  const [numbers, setNumbers] = useState<MaxNumberItem[]>([])
  const [selected, setSelected] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!open) return
    setError('')
    setPending(false)
    setLoading(true)
    api<{ items?: MaxNumberItem[] }>('/inbox/max/numbers')
      .then((nums) => {
        const items = nums.items ?? []
        setNumbers(items)
        const withDialogs = items.find((n) => (n.entities ?? 0) > 0)
        setSelected(withDialogs?.id || items[0]?.id || '')
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
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

  const write = async () => {
    const digits = clientPhone.replace(/\D/g, '')
    const phone = digits.length >= 10 && !/[•*]/.test(clientPhone) ? digits : ''
    if (!phone && !leadId) {
      setError('У лида нет телефона контакта')
      return
    }
    if (!selected && numbers.length > 0) {
      setError('Выберите аккаунт MAX')
      return
    }
    const num = numbers.find((n) => n.id === selected)
    setPending(true)
    setError('')
    try {
      const res = num?.line_id
        ? await api<{ thread_id?: string; line_id?: string }>('/inbox/max/open', {
            method: 'POST',
            body: JSON.stringify({
              ...(phone ? { phone } : {}),
              name: clientName || phone || 'Клиент',
              line_id: num.line_id,
              lead_id: leadId,
            }),
          })
        : await api<{ thread_id?: string; line_id?: string }>('/inbox/wappi/open', {
            method: 'POST',
            body: JSON.stringify({
              ...(phone ? { phone } : {}),
              name: clientName || phone || 'Клиент',
              profile_id: num?.channel_id || num?.id || '',
              lead_id: leadId,
            }),
          })
      if (!res.thread_id) throw new Error('Не удалось открыть чат')
      const openedLine = res.line_id || num?.line_id || ''
      onBound?.(res.thread_id)
      onClose()
      const qs = new URLSearchParams({ channel: 'max', open: res.thread_id })
      if (openedLine) qs.set('line_id', openedLine)
      navigate(`/inbox?${qs}`)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось открыть чат')
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="fixed inset-0 z-[140] flex items-start justify-center px-4 pt-[12vh]">
      <button
        type="button"
        className="absolute inset-0 z-0 bg-black/40"
        aria-label="Закрыть"
        onClick={onClose}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="relative z-10 w-full max-w-[480px] overflow-hidden rounded-[16px] border border-black/5 bg-white shadow-2xl"
      >
        <div className="flex items-center justify-between px-5 pb-3 pt-5">
          <h2 id={titleId} className="text-[18px] font-semibold tracking-tight text-b24-text-dark">
            Написать в MAX
          </h2>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex size-8 items-center justify-center rounded-full text-b24-text-muted hover:bg-black/5"
            aria-label="Закрыть"
          >
            <X size={18} />
          </button>
        </div>

        <div className="flex flex-col gap-3.5 px-5 pb-4">
          <div className="flex items-center gap-3 rounded-[12px] border border-black/6 bg-[#f7f8fa] px-3 py-2.5">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-full bg-[#e8eef3] text-[12px] font-semibold text-b24-text-dark">
              {(clientName || '?').trim().slice(0, 1).toUpperCase()}
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-[13px] font-semibold text-b24-text-dark">
                {clientName || 'Клиент'}
              </div>
              {clientPhone ? (
                <div className="truncate text-[12px] tabular-nums text-b24-text-muted">
                  {/[•*]/.test(clientPhone) ? clientPhone : formatMaxPhone(clientPhone)}
                </div>
              ) : null}
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <span className="text-[12px] font-medium text-b24-text-muted">С аккаунта</span>
            {loading ? (
              <div className="py-6 text-center text-[13px] text-b24-text-muted">Загрузка…</div>
            ) : numbers.length === 0 ? (
              <div className="rounded-[12px] border border-dashed border-black/10 px-4 py-5 text-center text-[13px] text-b24-text-muted">
                Нет аккаунтов MAX
              </div>
            ) : (
              <ul className="b24-scrollbar flex max-h-[280px] flex-col overflow-y-auto rounded-[12px] border border-black/6">
                {numbers.map((n) => {
                  const active = selected === n.id
                  const dialogs = n.entities ?? 0
                  const phoneLabel =
                    n.phone && n.phone.replace(/\D/g, '').length <= 11
                      ? formatMaxPhone(n.phone)
                      : n.label || 'MAX'
                  return (
                    <li key={n.id} className="border-b border-black/[0.05] last:border-b-0">
                      <button
                        type="button"
                        onClick={() => setSelected(n.id)}
                        className={[
                          'flex w-full items-center gap-3 border-l-4 px-3 py-3 text-left transition-colors',
                          active
                            ? 'border-[#2b7de9] bg-[#e8f1ff]'
                            : 'border-transparent hover:bg-[#f7f8fa]',
                        ].join(' ')}
                      >
                        <div className="relative shrink-0">
                          <div className="flex size-10 items-center justify-center rounded-lg bg-[#efeaff] text-[#7b61ff]">
                            <span className="text-[11px] font-black">M</span>
                          </div>
                          {dialogs > 0 ? (
                            <span className="absolute -bottom-0.5 -right-0.5 size-2.5 rounded-full bg-[#2fc06e] ring-2 ring-white" />
                          ) : null}
                        </div>
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-[13px] font-semibold tabular-nums text-b24-text-dark">
                            {phoneLabel}
                          </span>
                          <span className="block truncate text-[11px] text-b24-text-muted">
                            {dialogs > 0 ? dialogsLabel(dialogs) : 'Нет диалогов'}
                          </span>
                        </span>
                        <ChevronRight
                          size={16}
                          className={active ? 'text-[#2b7de9]' : 'text-b24-text-muted/60'}
                        />
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}
          </div>

          {error ? <p className="text-[12px] text-red-500">{error}</p> : null}
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-black/6 bg-[#fafbfc] px-5 py-4">
          <Button
            type="button"
            variant="filter"
            className="h-10 rounded-[10px] px-4"
            onClick={onClose}
            disabled={pending}
          >
            Отмена
          </Button>
          <button
            type="button"
            disabled={pending || loading || (!selected && numbers.length > 0)}
            onClick={() => void write()}
            className="h-10 rounded-[10px] bg-[#2b7de9] px-5 text-[14px] font-semibold text-white shadow-[0_2px_8px_rgba(43,125,233,0.35)] hover:bg-[#246fce] disabled:opacity-50"
          >
            {pending ? '…' : 'Написать'}
          </button>
        </div>
      </div>
    </div>
  )
}

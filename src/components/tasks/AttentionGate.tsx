import { AlertTriangle, ExternalLink, PhoneMissed, Star } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { formatTaskDate, type AttentionItem } from '../../data/tasks'
import { resolveLeadIdFromEntity } from '../../lib/openLeadFromEntity'
import { useCrmLeadDrawer } from '../../store/crmLeadDrawer'
import { useTasks } from '../../store/tasks'
import { Button } from '../ui/Button'
import { TaskResolveForm } from './TaskResolveForm'

function openedKey(id: string) {
  return `attn-card-opened:${id}`
}

export function AttentionGate() {
  const navigate = useNavigate()
  const { openLead } = useCrmLeadDrawer()
  const { attention, attentionBlocked, resolve, attentionLoading } = useTasks()
  const current = attention[0] ?? null

  const [cardOpened, setCardOpened] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!current) return
    setError('')
    setCardOpened(sessionStorage.getItem(openedKey(current.id)) === '1')
  }, [current?.id])

  useEffect(() => {
    if (!attentionBlocked) return
    const blockEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
      }
    }
    window.addEventListener('keydown', blockEsc, true)
    return () => window.removeEventListener('keydown', blockEsc, true)
  }, [attentionBlocked])

  if (!attentionBlocked || !current) return null

  const isMissed = current.attentionType === 'missed_call'
  const hasCard = Boolean(current.entityId)
  // Для пропущенного — карточку нужно открыть до фиксации; для важной — по желанию.
  const requiresCard = Boolean(isMissed && hasCard)
  const docked = Boolean(hasCard && cardOpened)

  const openCard = async () => {
    const entityType = (current.entityType || '').toLowerCase()
    const entityId = current.entityId || ''
    if (!entityId) {
      setError('К карточке нет привязки — зафиксируйте результат комментарием')
      setCardOpened(true)
      sessionStorage.setItem(openedKey(current.id), '1')
      return
    }

    try {
      const leadId = await resolveLeadIdFromEntity(entityType, entityId)
      if (leadId) {
        openLead(leadId)
        navigate(`/crm/leads?open=${encodeURIComponent(leadId)}`)
      } else if (entityType === 'deal') {
        navigate(`/crm?open=${encodeURIComponent(entityId)}`)
      } else {
        navigate(`/crm?open=${encodeURIComponent(entityId)}`)
      }
      setCardOpened(true)
      sessionStorage.setItem(openedKey(current.id), '1')
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось открыть карточку')
    }
  }

  return (
    <div
      className={[
        'fixed inset-0 z-[120] flex',
        docked ? 'items-end justify-center pointer-events-none' : 'items-center justify-center px-4',
      ].join(' ')}
    >
      {!docked ? (
        <div className="absolute inset-0 bg-[#1a1545]/55 backdrop-blur-[3px]" aria-hidden />
      ) : null}

      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="attention-title"
        className={[
          'relative z-10 w-full bg-white border border-black/5 overflow-hidden pointer-events-auto',
          docked
            ? 'max-w-[640px] mb-3 mx-3 rounded-[18px] shadow-[0_16px_50px_rgba(26,21,69,0.35)] max-h-[min(52vh,520px)] overflow-y-auto'
            : 'max-w-[560px] rounded-[18px] shadow-[0_24px_80px_rgba(26,21,69,0.35)]',
        ].join(' ')}
      >
        <div className="px-6 pt-5 pb-4 border-b border-black/6 bg-[linear-gradient(135deg,#f0fbfd_0%,#ffffff_55%)]">
          <div className="flex items-start gap-3">
            <span className="size-11 shrink-0 inline-flex items-center justify-center rounded-full bg-[#e8f8fe] text-b24-cyan-deep">
              {isMissed ? (
                <PhoneMissed size={20} strokeWidth={2.25} />
              ) : (
                <Star size={20} strokeWidth={2.25} fill="currentColor" />
              )}
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2 mb-1">
                <span className="text-[11px] font-semibold uppercase tracking-wide text-b24-cyan-deep">
                  Требует внимания
                </span>
                <span
                  className={[
                    'inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium',
                    isMissed ? 'bg-rose-50 text-rose-700' : 'bg-sky-50 text-sky-700',
                  ].join(' ')}
                >
                  {isMissed ? 'Пропущенный звонок' : 'Важная задача'}
                </span>
                {attention.length > 1 ? (
                  <span className="rounded-full bg-black/5 px-2 py-0.5 text-[11px] font-medium text-b24-text">
                    ещё {attention.length - 1}
                  </span>
                ) : null}
              </div>
              <h2
                id="attention-title"
                className="text-[20px] font-semibold text-b24-text-dark tracking-tight leading-snug"
              >
                {current.title}
              </h2>
              {current.entityLabel ? (
                <p className="mt-1.5 text-[14px] font-medium text-b24-text-dark truncate">
                  Карточка: {current.entityLabel}
                </p>
              ) : null}
              <p className="mt-1.5 text-[13px] leading-relaxed text-b24-text-muted">
                {isMissed
                  ? 'Откройте карточку и зафиксируйте результат. Конкретное действие система не навязывает.'
                  : hasCard
                    ? 'Откройте карточку лида, чтобы вспомнить контекст, затем зафиксируйте результат.'
                    : 'Пока не зафиксируете результат, обычная работа в CRM заблокирована.'}
              </p>
              {current.deadline ? (
                <div className="mt-2 inline-flex items-center gap-1.5 text-[13px] font-medium text-b24-text-dark">
                  <AlertTriangle size={14} className="text-orange-500" />
                  Срок: {formatTaskDate(current.deadline)}
                </div>
              ) : null}
            </div>
          </div>
        </div>

        <div className="px-6 py-5 flex flex-col gap-4">
          {hasCard ? (
            <div
              className={[
                'rounded-[12px] border px-3.5 py-3 flex flex-wrap items-center justify-between gap-3',
                cardOpened
                  ? 'border-emerald-200 bg-emerald-50/70'
                  : 'border-black/8 bg-[#f5f7f9]',
              ].join(' ')}
            >
              <div className="min-w-0 text-[13px] text-b24-text-dark">
                {cardOpened
                  ? 'Карточка открыта — можно зафиксировать результат'
                  : requiresCard
                    ? 'Сначала откройте карточку лида'
                    : current.entityLabel
                      ? `Открыть: ${current.entityLabel}`
                      : 'Открыть карточку лида'}
              </div>
              <Button
                type="button"
                variant={cardOpened ? 'filter' : 'green'}
                className="rounded-[10px] h-9 shrink-0"
                leftIcon={<ExternalLink size={14} strokeWidth={2} />}
                onClick={() => void openCard()}
              >
                Открыть карточку
              </Button>
            </div>
          ) : null}

          <TaskResolveForm
            key={current.id}
            variant="gate"
            isMissed={isMissed}
            noteRequired={isMissed}
            pending={pending || attentionLoading}
            error={error}
            onSubmit={async (payload) => {
              if (requiresCard && !cardOpened) {
                setError('Сначала откройте карточку лида')
                return
              }
              setPending(true)
              setError('')
              try {
                await resolve(current.id, payload)
                sessionStorage.removeItem(openedKey(current.id))
              } catch (e) {
                setError(e instanceof Error ? e.message : 'Не удалось зафиксировать результат')
              } finally {
                setPending(false)
              }
            }}
          />
        </div>

        <div className="px-6 py-3 border-t border-black/6 bg-[#fafbfc]">
          <p className="text-[12px] text-b24-text-muted">Закрыть без результата нельзя</p>
        </div>
      </div>
    </div>
  )
}

export type { AttentionItem }

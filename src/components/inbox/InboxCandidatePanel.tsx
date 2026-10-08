import { ExternalLink, Phone } from 'lucide-react'
import type { ReactNode } from 'react'
import { sourceLabel } from '../../lib/crm'
import { maskPhone } from '../../lib/roles'

export type InboxCandidateInfo = {
  name: string
  phone?: string
  channelLabel: string
  channelGlyph?: ReactNode
  leadTitle?: string
  owner?: string
  source?: string
  stage?: string
  leadId?: string
  dealId?: string
}

type Props = {
  candidate: InboxCandidateInfo
  hidePhones?: boolean
  callDisabled?: boolean
  callPending?: boolean
  onOpenCard?: () => void
  onCall?: () => void
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[minmax(96px,36%)_1fr] gap-2 text-[13px] py-2 border-b border-black/[0.05] last:border-0">
      <div className="text-b24-text-muted leading-snug pt-0.5">{label}</div>
      <div className="min-w-0 break-words text-b24-text-dark leading-snug font-medium">
        {children || '—'}
      </div>
    </div>
  )
}

function initials(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

export function InboxCandidatePanel({
  candidate,
  hidePhones,
  callDisabled,
  callPending,
  onOpenCard,
  onCall,
}: Props) {
  const phone = candidate.phone
    ? hidePhones
      ? maskPhone(candidate.phone)
      : candidate.phone
    : ''
  const hasCard = Boolean(candidate.leadId || candidate.dealId)
  const sourceText = candidate.source ? sourceLabel(candidate.source) : ''

  return (
    <div className="flex h-full min-h-0 w-full flex-col bg-[#fafbfc]">
      <div className="shrink-0 border-b border-black/6 bg-white px-4 py-4">
        <div className="flex items-start gap-3">
          <div className="flex size-14 shrink-0 items-center justify-center rounded-full bg-[#e8f1ff] text-[17px] font-semibold text-[#2b7de9]">
            {initials(candidate.name)}
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-[16px] font-semibold leading-snug text-b24-text-dark break-words">
              {candidate.name}
            </div>
            {phone ? (
              <div className="mt-1 font-mono text-[13px] tabular-nums text-b24-text-muted">
                {phone}
              </div>
            ) : null}
            <div className="mt-2 inline-flex items-center gap-1.5 rounded-full border border-black/8 bg-[#f5f6f8] px-2.5 py-1 text-[12px] font-medium text-b24-text-dark">
              {candidate.channelGlyph}
              {candidate.channelLabel}
              <span className="text-b24-text-muted font-normal">· основной</span>
            </div>
          </div>
        </div>
      </div>

      <div className="b24-scrollbar min-h-0 flex-1 overflow-y-auto px-4 py-4">
        <div className="rounded-[12px] border border-black/6 bg-white px-3.5 py-1 shadow-sm">
          {candidate.stage ? <Field label="Стадия">{candidate.stage}</Field> : null}
          <Field label="Канал">{candidate.channelLabel}</Field>
          {sourceText ? <Field label="Источник">{sourceText}</Field> : null}
          <Field label="Заявка">{candidate.leadTitle || '—'}</Field>
          <Field label="Телефон">{phone || '—'}</Field>
          <Field label="Ответственный">{candidate.owner || '—'}</Field>
        </div>

        {!hasCard ? (
          <p className="mt-4 rounded-[10px] border border-dashed border-black/10 bg-white px-3 py-3 text-[12px] leading-snug text-b24-text-muted">
            Кандидат ещё не привязан к заявке — карточку открыть нельзя.
          </p>
        ) : (
          <p className="mt-4 text-[12px] leading-snug text-b24-text-muted">
            Откройте карточку, чтобы сменить стадию, оставить комментарий или посмотреть историю.
          </p>
        )}
      </div>

      <div className="shrink-0 border-t border-black/6 bg-white px-4 py-3 flex flex-col gap-2">
        <button
          type="button"
          disabled={!hasCard}
          onClick={onOpenCard}
          className="inline-flex h-11 w-full items-center justify-center gap-1.5 rounded-[10px] bg-[#2b7de9] px-3 text-[13px] font-semibold text-white hover:bg-[#246fce] disabled:opacity-40"
        >
          <ExternalLink className="size-3.5" strokeWidth={2.25} />
          Открыть карточку
        </button>
        <button
          type="button"
          disabled={callDisabled || callPending || !phone}
          onClick={onCall}
          className="inline-flex h-11 w-full items-center justify-center gap-1.5 rounded-[10px] border border-black/10 bg-[#f5f6f8] px-3 text-[13px] font-semibold text-b24-text-dark hover:bg-[#eef1f4] disabled:opacity-40"
        >
          <Phone className="size-3.5" strokeWidth={2.25} />
          {callPending ? '…' : 'Позвонить'}
        </button>
      </div>
    </div>
  )
}

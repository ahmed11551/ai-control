import { Phone, PhoneOff, RotateCcw, UserMinus, UserPlus } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { api } from '../../lib/api'
import { canSeePhones, maskPhone } from '../../lib/roles'
import { formatDuration } from '../../telephony/callHistory'
import { useAuth } from '../../store/auth'
import { Button } from '../ui/Button'

export type AutodialDisposition = 'bad' | 'return' | 'take'

type Props = {
  title: string
  dealId?: string
  callId?: string
  ringing: boolean
  talking?: boolean
  /** После разговора — выбор §8.2 */
  disposition?: boolean
  busy?: boolean
  durationSec?: number
  onAnswer: () => void
  onDecline: () => void
  onHangup?: () => void
  onOpenCard?: () => void
  onDispositionDone?: () => void
}

type DealBrief = {
  title?: string
  contact?: string
  contact_phone?: string
  phone?: string
  stage?: string
}

function initials(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

function formatWait(sec: number) {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

export function AutodialIncomingModal({
  title,
  dealId,
  callId,
  ringing,
  talking = false,
  disposition = false,
  busy,
  durationSec = 0,
  onAnswer,
  onDecline,
  onHangup,
  onOpenCard,
  onDispositionDone,
}: Props) {
  const { user } = useAuth()
  const [brief, setBrief] = useState<DealBrief | null>(null)
  const [waitSec, setWaitSec] = useState(0)
  const [justAccepted, setJustAccepted] = useState(false)
  const [dispBusy, setDispBusy] = useState(false)
  const [dispErr, setDispErr] = useState('')
  const [badReason, setBadReason] = useState('')
  const [showBadReason, setShowBadReason] = useState(false)
  const wasTalking = useRef(false)

  useEffect(() => {
    if (!dealId) {
      setBrief(null)
      return
    }
    let alive = true
    void api<DealBrief>(`/deals/${dealId}`)
      .then((d) => {
        if (alive) setBrief(d)
      })
      .catch(() => {
        if (alive) setBrief(null)
      })
    return () => {
      alive = false
    }
  }, [dealId])

  useEffect(() => {
    if (talking || disposition) return
    setWaitSec(0)
    const id = window.setInterval(() => setWaitSec((n) => n + 1), 1000)
    return () => window.clearInterval(id)
  }, [dealId, ringing, talking, disposition])

  useEffect(() => {
    if (talking && !wasTalking.current) {
      setJustAccepted(true)
      const t = window.setTimeout(() => setJustAccepted(false), 900)
      wasTalking.current = true
      return () => window.clearTimeout(t)
    }
    if (!talking && !disposition) wasTalking.current = false
  }, [talking, disposition])

  const displayName = (brief?.contact || brief?.title || title || 'Кандидат').trim()
  const phoneRaw = brief?.contact_phone || brief?.phone || ''
  const phone = canSeePhones(user) ? phoneRaw : maskPhone(phoneRaw)
  const showPhone = Boolean(phone)
  const stageLabel = (brief?.stage || '').trim()
  const canAnswer = ringing && !busy && !talking && !disposition
  const inCall = talking
  const deciding = disposition

  const submitDisposition = async (action: AutodialDisposition) => {
    if (!dealId && !callId) {
      onDispositionDone?.()
      return
    }
    if (action === 'bad' && !showBadReason) {
      setShowBadReason(true)
      return
    }
    setDispBusy(true)
    setDispErr('')
    try {
      await api('/telephony/autodial/disposition', {
        method: 'POST',
        body: JSON.stringify({
          action,
          deal_id: dealId || '',
          call_id: callId || '',
          reason: action === 'bad' ? badReason.trim() || 'Некачественный лид после скорозвона' : '',
        }),
      })
      onDispositionDone?.()
    } catch (e) {
      setDispErr(e instanceof Error ? e.message : 'Не удалось сохранить решение')
    } finally {
      setDispBusy(false)
    }
  }

  const statusLabel = deciding
    ? 'Решение'
    : inCall
      ? 'Разговор'
      : ringing
        ? 'Звонит'
        : 'Ожидание'
  const statusHint = deciding ? 'после звонка' : inCall ? 'на связи' : 'на линии'
  const subtitle = deciding
    ? 'Что сделать с кандидатом?'
    : inCall
      ? 'Вы на линии с кандидатом'
      : ringing
        ? 'Кандидат на линии · первый забирает'
        : 'Ждём входящий на softphone…'

  return (
    <div className="fixed inset-0 z-[210] flex items-center justify-center px-4">
      <div className="absolute inset-0 bg-[#1a1545]/45 backdrop-blur-[2px]" aria-hidden />

      <div
        role="dialog"
        aria-modal="true"
        aria-label="Скорозвон"
        className={[
          'relative z-10 w-full max-w-[440px] rounded-[20px] border border-white/10 bg-[#221c52] p-4 shadow-[0_24px_64px_rgba(10,8,40,0.55)]',
          justAccepted ? 'autodial-accepted-pop' : '',
        ].join(' ')}
      >
        {justAccepted ? <div className="autodial-accepted-flash" aria-hidden /> : null}

        <div className="mb-3 px-0.5">
          <h2 className="text-[26px] font-semibold leading-none tracking-tight text-white">
            Скорозвон
          </h2>
          <p className="mt-1.5 text-[13px] text-white/65">{subtitle}</p>
        </div>

        <div className="grid grid-cols-2 gap-2.5">
          <div
            className={[
              'rounded-[14px] border border-white/10 bg-white/12 px-4 py-3 transition-colors duration-300',
              inCall || deciding ? 'border-emerald-400/35 bg-emerald-400/15' : '',
            ].join(' ')}
          >
            <div className="text-[11px] tracking-wide text-white/55 uppercase">Статус</div>
            <div className="mt-1 text-[22px] font-semibold leading-none text-white">{statusLabel}</div>
            <div className="mt-1 text-[11px] text-white/45">{statusHint}</div>
          </div>
          <div className="rounded-[14px] border border-white/10 bg-white/12 px-4 py-3">
            <div className="text-[11px] tracking-wide text-white/55 uppercase">
              {inCall || deciding ? 'Длительность' : 'Ожидание'}
            </div>
            <div className="mt-1 font-mono text-[22px] font-semibold leading-none tabular-nums text-white">
              {inCall || deciding ? formatDuration(durationSec) : formatWait(waitSec)}
            </div>
            <div className="mt-1 text-[11px] text-white/45">таймер</div>
          </div>
        </div>

        <div
          className={[
            'mt-2.5 rounded-[14px] border border-white/10 bg-white/12 px-4 py-4 transition-colors duration-300',
            inCall || deciding ? 'border-emerald-400/25' : '',
          ].join(' ')}
        >
          <div className="flex items-center gap-3.5">
            <div className="relative shrink-0">
              {ringing && !inCall && !deciding ? (
                <>
                  <span className="autodial-pulse-ring absolute inset-0 rounded-full border border-white/35" />
                  <span className="autodial-pulse-ring autodial-pulse-ring--delay absolute inset-[-8px] rounded-full border border-white/20" />
                </>
              ) : null}
              {inCall || deciding ? (
                <span className="absolute inset-[-4px] rounded-full border border-emerald-400/40" />
              ) : null}
              <div
                className={[
                  'relative z-10 flex size-12 items-center justify-center rounded-full text-[15px] font-semibold text-white transition-colors duration-300',
                  inCall || deciding ? 'bg-emerald-500/35' : 'bg-white/20',
                ].join(' ')}
              >
                {initials(displayName)}
              </div>
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-[18px] font-semibold tracking-tight text-white">
                {displayName}
              </div>
              {showPhone ? (
                <div className="mt-1 font-mono text-[13px] tabular-nums text-white/65">{phone}</div>
              ) : null}
              {stageLabel ? (
                <div className="mt-1.5 text-[12px] text-white/45">{stageLabel}</div>
              ) : null}
            </div>
          </div>
        </div>

        <div className="mt-3.5 flex flex-col gap-2">
          {deciding ? (
            <>
              {showBadReason ? (
                <textarea
                  value={badReason}
                  onChange={(e) => setBadReason(e.target.value)}
                  placeholder="Причина (необязательно)"
                  rows={2}
                  className="w-full resize-none rounded-[12px] border border-white/15 bg-white/10 px-3 py-2 text-[13px] text-white placeholder:text-white/40 outline-none focus:border-white/30"
                />
              ) : null}
              <button
                type="button"
                disabled={dispBusy}
                onClick={() => void submitDisposition('take')}
                className="flex h-11 w-full items-center justify-center gap-1.5 rounded-[12px] bg-b24-green text-[14px] font-semibold text-white shadow-[0_8px_24px_rgba(47,192,110,0.4)] transition hover:brightness-110 disabled:opacity-50"
              >
                <UserPlus size={16} strokeWidth={2.5} />
                Забрать себе
              </button>
              <button
                type="button"
                disabled={dispBusy}
                onClick={() => void submitDisposition('return')}
                className="flex h-11 w-full items-center justify-center gap-1.5 rounded-[12px] border border-white/15 bg-white/10 text-[14px] font-semibold text-white transition hover:bg-white/16 disabled:opacity-50"
              >
                <RotateCcw size={16} strokeWidth={2.5} />
                Вернуть владельцу
              </button>
              <button
                type="button"
                disabled={dispBusy}
                onClick={() => void submitDisposition('bad')}
                className="flex h-11 w-full items-center justify-center gap-1.5 rounded-[12px] border border-rose-400/30 bg-rose-500/15 text-[14px] font-semibold text-rose-100 transition hover:bg-rose-500/25 disabled:opacity-50"
              >
                <UserMinus size={16} strokeWidth={2.5} />
                {showBadReason ? 'Подтвердить: некачественный' : 'Некачественный лид'}
              </button>
              {dispErr ? <p className="text-center text-[12px] text-rose-200">{dispErr}</p> : null}
            </>
          ) : inCall ? (
            <button
              type="button"
              onClick={onHangup}
              className="flex h-11 w-full items-center justify-center gap-1.5 rounded-[12px] bg-[#e5484d] text-[14px] font-semibold text-white shadow-[0_8px_24px_rgba(229,72,77,0.4)] transition hover:bg-[#d63d42]"
            >
              <PhoneOff size={16} strokeWidth={2.5} />
              Завершить звонок
            </button>
          ) : (
            <Button
              type="button"
              variant="green"
              disabled={!canAnswer}
              onClick={onAnswer}
              className={[
                'h-11 w-full rounded-[12px] text-[14px] font-semibold shadow-[0_8px_24px_rgba(47,192,110,0.4)]',
                canAnswer ? 'autodial-accept-glow' : '',
              ].join(' ')}
              leftIcon={<Phone size={16} strokeWidth={2.5} />}
            >
              {busy ? 'Подключаем…' : ringing ? 'Принять звонок' : 'Ожидание линии'}
            </Button>
          )}

          {!deciding ? (
            <button
              type="button"
              disabled={!dealId}
              onClick={onOpenCard}
              className="h-10 rounded-[12px] border border-white/15 bg-white/10 text-[13px] font-medium text-white/85 transition hover:bg-white/16 disabled:opacity-40"
            >
              Карточка
            </button>
          ) : null}

          {!inCall && !deciding ? (
            <button
              type="button"
              disabled={busy}
              onClick={onDecline}
              className="flex h-10 items-center justify-center gap-1.5 rounded-[12px] border border-white/12 bg-white/6 text-[13px] font-medium text-white/65 transition hover:bg-white/12 hover:text-white"
            >
              <PhoneOff size={14} strokeWidth={2} />
              Отклонить
            </button>
          ) : null}
        </div>
      </div>
    </div>
  )
}

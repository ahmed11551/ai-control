import { Delete as Backspace, Phone, PhoneOff } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { canSeePhones, isManagerRole, maskPhone } from '../../lib/roles'
import { formatDuration } from '../../telephony/callHistory'
import { softphonePhaseHint, softphonePhaseLabel } from '../../telephony/callPhase'
import type { TelephonyProvider } from '../../telephony/config'
import { AUTODIAL_ENABLED, EXOLVE_ENABLED } from '../../telephony/config'
import { isOurExolveDid } from '../../telephony/autodialSuppress'
import { useAuth } from '../../store/auth'
import { useCrmLeadDrawer } from '../../store/crmLeadDrawer'
import { useTelephony } from '../../store/telephony'
import { hasLiveCallCard, openOrCreateCallCard } from '../../telephony/openLiveCallCard'
import { Button } from '../ui/Button'
import { VoxInboundPanel } from './VoxInboundPanel'

const KEYS = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '*', '0', '#'] as const

export function SoftphonePanel() {
  const navigate = useNavigate()
  const { openLead } = useCrmLeadDrawer()
  const { user } = useAuth()
  const hidePhones = !canSeePhones(user)
  const role = String(user?.role || '').toLowerCase()
  // Набор в телефонии — как у админа. Номера в карточках и журнале по-прежнему скрыты.
  const canTypeNumber =
    !hidePhones || role === 'manager' || role === 'sales' || role === 'rop'
  const showPeer = (raw?: string) => (hidePhones ? maskPhone(raw) || 'Клиент' : raw || '')
  const {
    provider,
    setProvider,
    defaultDest,
    snap,
    history,
    connect,
    disconnect,
    call,
    answer,
    rejectIncoming,
    hangup,
    toggleMute,
    toggleHold,
    sendDtmf,
    liveDealId,
    liveLeadId,
    journalError,
    activeCli,
    exolveMode,
  } = useTelephony()

  const [dest, setDest] = useState(defaultDest)
  const [dialed, setDialed] = useState('')
  const [busy, setBusy] = useState(false)
  const [cardBusy, setCardBusy] = useState(false)
  const cardBusyRef = useRef(false)
  const [localError, setLocalError] = useState('')

  useEffect(() => {
    setDest(defaultDest)
  }, [defaultDest, provider])

  useEffect(() => {
    if (snap.status === 'idle' || snap.status === 'ready' || snap.status === 'error') {
      setDialed('')
    }
  }, [snap.status])

  const statusLabel = useMemo(() => softphonePhaseLabel(snap.status), [snap.status])
  const statusHint = useMemo(() => softphonePhaseHint(snap.status), [snap.status])

  const inCall = snap.status === 'active' || snap.status === 'dialing'
  const canDial = snap.status === 'ready' || snap.status === 'error'
  const connected = snap.status !== 'idle' && snap.status !== 'error'
  const canSwitch = EXOLVE_ENABLED && (snap.status === 'idle' || snap.status === 'error')
  const livePulse =
    snap.status === 'ringing' ||
    snap.status === 'active' ||
    snap.status === 'dialing' ||
    snap.status === 'connecting'
  const canAutodialUI = AUTODIAL_ENABLED && isManagerRole(user?.role) && user?.can_call !== false
  const autodialRinging = snap.status === 'ringing' && isOurExolveDid(snap.incomingPeer || snap.peer)
  const showIncomingAnswer = snap.status === 'ringing' && (!autodialRinging || canAutodialUI)

  const onConnect = async () => {
    setBusy(true)
    setLocalError('')
    try {
      await connect()
    } catch (e) {
      setLocalError(e instanceof Error ? e.message : 'Ошибка подключения')
    } finally {
      setBusy(false)
    }
  }

  const onCall = async () => {
    const number = dest.trim()
    setBusy(true)
    setLocalError('')
    setDialed(number)
    try {
      await call(number)
    } catch (e) {
      setDialed('')
      setLocalError(e instanceof Error ? e.message : 'Не удалось позвонить')
    } finally {
      setBusy(false)
    }
  }

  const onProvider = async (next: TelephonyProvider) => {
    if (next === provider || !canSwitch) return
    setBusy(true)
    setLocalError('')
    try {
      await setProvider(next)
    } catch (e) {
      setLocalError(e instanceof Error ? e.message : 'Не удалось сменить провайдера')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="b24-glass-panel overflow-hidden">
        <div className="flex flex-wrap items-center gap-2 px-4 py-3 border-b border-black/6 bg-b24-panel-header">
          <div className="flex gap-1.5">
            {(
              [
                { id: 'vox' as const, label: 'Vox' },
                ...(EXOLVE_ENABLED
                  ? [{ id: 'exolve' as const, label: 'МТС' }]
                  : []),
              ] as { id: TelephonyProvider; label: string }[]
            ).map((item) => {
              const active = provider === item.id
              return (
                <button
                  key={item.id}
                  type="button"
                  disabled={!canSwitch || busy}
                  onClick={() => onProvider(item.id)}
                  className={[
                    'h-8 px-3 rounded-b24-pill text-[12px] font-semibold tracking-wide transition-colors duration-200',
                    active
                      ? 'bg-b24-cyan/15 text-[#0b7ea4]'
                      : 'bg-white/70 text-b24-text hover:bg-white',
                    !canSwitch ? 'opacity-50 cursor-not-allowed' : '',
                  ].join(' ')}
                >
                  {item.label}
                </button>
              )
            })}
            {!EXOLVE_ENABLED ? (
              <span
                className="h-8 px-2.5 inline-flex items-center rounded-b24-pill text-[11px] text-b24-text-muted bg-white/50"
                title="МТС сейчас не работает"
              >
                МТС не работает
              </span>
            ) : null}
          </div>

          <div className="flex items-center gap-2 ml-auto min-w-0">
            <span
              className={[
                'inline-flex items-center gap-1.5 h-8 px-3 rounded-b24-pill text-[12px] font-medium transition-colors duration-300',
                snap.status === 'ready'
                  ? 'bg-emerald-50 text-emerald-700'
                  : snap.status === 'error' || snap.status === 'idle'
                    ? 'bg-[#eef1f4] text-b24-text-muted'
                    : 'bg-b24-cyan/15 text-[#0b7ea4]',
              ].join(' ')}
            >
              <span
                className={[
                  'size-1.5 rounded-full bg-current',
                  livePulse ? 'animate-pulse' : 'opacity-60',
                ].join(' ')}
              />
              {statusLabel}
            </span>
            {statusHint ? (
              <span className="text-[11px] text-b24-text-muted">{statusHint}</span>
            ) : null}
            {!connected ? (
              <Button
                variant="green"
                className="rounded-b24-pill h-8 px-3 text-[12px]"
                disabled={busy}
                onClick={onConnect}
              >
                Подключить
              </Button>
            ) : (
              <Button
                variant="filter"
                className="rounded-b24-pill h-8 px-3 text-[12px] text-red-600"
                disabled={busy}
                onClick={() => disconnect()}
              >
                Откл.
              </Button>
            )}
          </div>
        </div>

        {(provider === 'exolve' && (activeCli || exolveMode === 'pool')) ? (
          <div className="px-4 pt-2 pb-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-b24-text-muted">
            {activeCli ? (
              <span className="font-mono text-b24-text-dark">CLI {activeCli}</span>
            ) : null}
            {exolveMode === 'pool' ? <span>пул</span> : null}
          </div>
        ) : null}

        {(localError || snap.error) && (
          <div className="mx-4 mt-2 rounded-b24-sm bg-red-50 text-red-700 text-[13px] px-3 py-2">
            {localError || snap.error}
          </div>
        )}
        {journalError ? (
          <div className="mx-4 mt-2 rounded-b24-sm bg-red-50 text-red-700 text-[13px] px-3 py-2">
            {journalError}
          </div>
        ) : null}
        {hasLiveCallCard({ leadId: liveLeadId, dealId: liveDealId }) ||
        ((snap.status === 'ringing' || snap.status === 'dialing' || snap.status === 'active') &&
          Boolean(snap.status === 'ringing' ? snap.incomingPeer || snap.peer : snap.peer)) ? (
          <button
            type="button"
            disabled={cardBusy}
            className="mx-4 mt-2 w-[calc(100%-2rem)] rounded-b24-sm bg-emerald-50 text-emerald-800 text-[13px] px-3 py-2 text-left transition-colors hover:bg-emerald-100/80 disabled:opacity-60"
            onClick={() => {
              if (cardBusyRef.current) return
              cardBusyRef.current = true
              setCardBusy(true)
              setLocalError('')
              const peer = snap.status === 'ringing' ? snap.incomingPeer || snap.peer : snap.peer
              void openOrCreateCallCard(navigate, {
                leadId: liveLeadId,
                dealId: liveDealId,
                openLead,
                peer,
                requireOwnerId: isManagerRole(user?.role) ? user?.id : undefined,
              })
                .then((ok) => {
                  if (!ok) setLocalError('Не удалось открыть карточку')
                })
                .catch((e: unknown) => {
                  setLocalError(e instanceof Error ? e.message : 'Не удалось открыть карточку')
                })
                .finally(() => {
                  cardBusyRef.current = false
                  setCardBusy(false)
                })
            }}
          >
            {cardBusy ? 'Открываем…' : 'Открыть карточку звонка'}
          </button>
        ) : null}

        {showIncomingAnswer ? (
          <div className="mx-4 mt-3 rounded-b24 p-4 bg-b24-cyan/10 border border-b24-cyan/25 flex flex-col gap-3 animate-pulse">
            <div className="text-[18px] font-semibold text-b24-text-dark tracking-tight">
              {autodialRinging ? 'Скорозвон' : showPeer(snap.incomingPeer)}
            </div>
            <div className="flex gap-2">
              <Button variant="green" className="rounded-b24-pill flex-1 h-11" onClick={() => answer()}>
                Ответить
              </Button>
              <Button
                variant="filter"
                className="rounded-b24-pill flex-1 h-11 text-red-600"
                onClick={() => rejectIncoming()}
              >
                Отклонить
              </Button>
            </div>
          </div>
        ) : null}

        <div className="px-4 pt-4 pb-5 flex flex-col gap-4">
          {!inCall ? (
            !canTypeNumber ? (
              <p className="text-[13px] text-b24-text-muted text-center px-2 py-6">
                Набор номера скрыт. Звоните из карточки лида или контакта.
              </p>
            ) : (
            <>
              <input
                value={dest}
                onChange={(e) => setDest(e.target.value)}
                placeholder="Номер"
                inputMode="tel"
                autoComplete="tel"
                className="h-14 w-full rounded-b24 border-0 bg-transparent px-2 text-center text-[34px] font-semibold tracking-[0.04em] text-b24-text-dark outline-none placeholder:text-b24-text-muted/40"
              />

              <div className="grid grid-cols-3 gap-2.5 max-w-[300px] mx-auto w-full">
                {KEYS.map((key) => (
                  <button
                    key={key}
                    type="button"
                    className="h-14 rounded-2xl bg-[#f0f3f6] text-[22px] font-semibold text-b24-text-dark transition-all duration-150 hover:bg-[#e4e9ee] active:scale-[0.96] active:bg-[#dce2e8]"
                    onClick={() => setDest((v) => v + key)}
                  >
                    {key}
                  </button>
                ))}
              </div>

              <div className="flex items-center justify-center gap-3 pt-1">
                <Button
                  variant="green"
                  className="rounded-b24-pill h-14 px-10 text-[16px] shadow-sm"
                  disabled={!canDial || busy || !dest.trim()}
                  onClick={onCall}
                  leftIcon={<Phone size={18} strokeWidth={2.25} />}
                >
                  Позвонить
                </Button>
                <button
                  type="button"
                  className="size-14 rounded-full bg-[#f0f3f6] inline-flex items-center justify-center text-b24-text transition-all duration-150 hover:bg-[#e4e9ee] active:scale-95"
                  onClick={() => setDest((v) => v.slice(0, -1))}
                  aria-label="Стереть"
                >
                  <Backspace size={20} strokeWidth={1.75} />
                </button>
              </div>
            </>
            )
          ) : (
            <div className="flex flex-col items-center gap-4 py-6">
              <div className="text-[40px] font-semibold tabular-nums tracking-tight text-b24-text-dark">
                {formatDuration(snap.durationSec)}
              </div>
              <div className="text-[15px] text-b24-text truncate max-w-full">
                {dialed || showPeer(snap.peer)}
              </div>
              <div className="grid grid-cols-3 gap-2.5 max-w-[300px] w-full">
                {KEYS.map((key) => (
                  <button
                    key={key}
                    type="button"
                    className="h-12 rounded-2xl bg-[#f0f3f6] text-[18px] font-semibold text-b24-text-dark transition-all duration-150 hover:bg-[#e4e9ee] active:scale-[0.96]"
                    onClick={() => sendDtmf(key)}
                  >
                    {key}
                  </button>
                ))}
              </div>
              <div className="flex flex-wrap justify-center gap-2 pt-2">
                <Button
                  variant="filter"
                  className={`rounded-b24-pill h-11 px-4 transition-colors ${snap.muted ? 'bg-b24-cyan/15 text-[#0b7ea4]' : ''}`}
                  onClick={toggleMute}
                >
                  {snap.muted ? 'Микрофон' : 'Mute'}
                </Button>
                <Button
                  variant="filter"
                  className={`rounded-b24-pill h-11 px-4 transition-colors ${snap.onHold ? 'bg-orange-50 text-orange-700' : ''}`}
                  onClick={() => toggleHold()}
                >
                  Hold
                </Button>
                <Button
                  variant="filter"
                  className="rounded-b24-pill h-11 px-5 text-red-600 bg-red-50"
                  onClick={hangup}
                  leftIcon={<PhoneOff size={16} strokeWidth={2} />}
                >
                  Сброс
                </Button>
              </div>
            </div>
          )}
        </div>

        <div className="border-t border-black/6">
          <div className="px-4 py-2 text-[11px] font-semibold uppercase tracking-wider text-b24-text-muted">
            Недавние
          </div>
          <div className="max-h-[160px] overflow-y-auto b24-scrollbar divide-y divide-black/5">
            {history.length === 0 ? (
              <div className="px-4 py-5 text-[13px] text-b24-text-muted">Нет звонков</div>
            ) : (
              history.slice(0, 8).map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className="w-full text-left px-4 py-2.5 hover:bg-[#f5f8fa] flex items-center justify-between gap-3 transition-colors"
                  onClick={() => {
                    if (!hidePhones) setDest(item.peer)
                  }}
                >
                  <div className="min-w-0">
                    <div className="text-[13px] font-medium text-b24-text-dark truncate font-mono">
                      {item.direction === 'incoming' ? '← ' : '→ '}
                      {showPeer(item.peer)}
                    </div>
                  </div>
                  <div className="text-[12px] tabular-nums text-b24-text-muted shrink-0">
                    {formatDuration(item.durationSec)}
                  </div>
                </button>
              ))
            )}
          </div>
        </div>
      </div>

      {provider === 'vox' ? (
        <details className="b24-glass-panel group open:shadow-sm transition-shadow">
          <summary className="cursor-pointer list-none px-4 py-3 text-[13px] font-semibold text-b24-text-dark flex items-center justify-between select-none [&::-webkit-details-marker]:hidden">
            <span>Ещё</span>
            <span className="text-b24-text-muted text-[12px] font-normal group-open:hidden">входящие</span>
            <span className="text-b24-text-muted text-[12px] font-normal hidden group-open:inline">свернуть</span>
          </summary>
          <div className="px-3 pb-3 flex flex-col gap-3 border-t border-black/6 pt-3">
            <VoxInboundPanel softphoneReady={snap.status === 'ready' || inCall || snap.status === 'ringing'} />
          </div>
        </details>
      ) : null}
    </div>
  )
}

import { Phone, PhoneOff } from 'lucide-react'
import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { api, wsURL } from '../../lib/api'
import { canSeePhones, isManagerRole, maskPhone } from '../../lib/roles'
import {
  clearAutodialAwaitingOffer,
  clearAutodialPrompt,
  clearAutodialUserDeclined,
  getLastAutodialOffer,
  isAutodialDismissed,
  isAutodialAwaitingOffer,
  isAutodialPromptReady,
  isAutodialSuppressing,
  isAutodialUserDeclined,
  isOurExolveDid,
  markAutodialAwaitingOffer,
  markAutodialDismissed,
  markAutodialEnded,
  markAutodialPromptReady,
  markAutodialSuppress,
  rememberAutodialCli,
  rememberOurExolveDids,
  setActiveAutodialOffer,
  subscribeAutodialOffer,
  getAutodialDebugFlags,
} from '../../telephony/autodialSuppress'
import { formatDuration } from '../../telephony/callHistory'
import { softphonePhaseHint, softphonePhaseLabel } from '../../telephony/callPhase'
import { AUTODIAL_ENABLED, TELEPHONY_ENABLED } from '../../telephony/config'
import { shouldHideNormalIncomingBar } from '../../telephony/incomingBarVisibility'
import { useAuth } from '../../store/auth'
import { useCrmLeadDrawer } from '../../store/crmLeadDrawer'
import { useTelephony } from '../../store/telephony'
import { hasLiveCallCard, navigateToLiveCallCard, openOrCreateCallCard } from '../../telephony/openLiveCallCard'
import { Button } from '../ui/Button'
import { AutodialIncomingModal } from './AutodialIncomingModal'

function DemoSkorozvonModal({
  onClose,
  onOpenCard,
}: {
  onClose: () => void
  onOpenCard: () => void
}) {
  const [talking, setTalking] = useState(false)
  const [disposition, setDisposition] = useState(false)
  const [durationSec, setDurationSec] = useState(0)

  useEffect(() => {
    if (!talking) return
    const id = window.setInterval(() => setDurationSec((n) => n + 1), 1000)
    return () => window.clearInterval(id)
  }, [talking])

  return (
    <AutodialIncomingModal
      title="Алексей Кандидатов"
      dealId={undefined}
      ringing={!talking && !disposition}
      talking={talking}
      disposition={disposition}
      busy={false}
      durationSec={durationSec}
      onAnswer={() => {
        setTalking(true)
        setDurationSec(0)
      }}
      onDecline={onClose}
      onHangup={() => {
        setTalking(false)
        setDisposition(true)
      }}
      onOpenCard={onOpenCard}
      onDispositionDone={onClose}
    />
  )
}

export function IncomingCallBar() {
  const location = useLocation()
  const navigate = useNavigate()
  const { user } = useAuth()
  const { openLead } = useCrmLeadDrawer()
  const { snap, liveDealId, liveLeadId, journalError, answer, rejectIncoming, hangup } = useTelephony()
  const [autodialActive, setAutodialActive] = useState(false)
  const [, bump] = useState(0)
  const [answerBusy, setAnswerBusy] = useState(false)
  const [needsDisposition, setNeedsDisposition] = useState(false)
  const [dispositionMeta, setDispositionMeta] = useState<{
    dealId?: string
    callId?: string
    title?: string
    duration: number
  }>({ duration: 0 })
  const wasAutodialTalking = useRef(false)
  const iAmAccepter = useRef(false)
  const wasInCall = useRef(false)
  const lastPeerRef = useRef('')
  const [endedFlash, setEndedFlash] = useState<{ peer: string; until: number } | null>(null)
  const [cardBusy, setCardBusy] = useState(false)
  const [cardError, setCardError] = useState('')
  const cardBusyRef = useRef(false)
  const panelRef = useRef<HTMLDivElement>(null)
  const dragRef = useRef<{ pointerId: number; dx: number; dy: number } | null>(null)
  const [dragPos, setDragPos] = useState<{ x: number; y: number } | null>(null)

  const canAutodialUI = AUTODIAL_ENABLED && isManagerRole(user?.role) && user?.can_call !== false
  const demoSkorozvon = AUTODIAL_ENABLED && new URLSearchParams(location.search).get('demo') === 'skorozvon'

  useEffect(() => {
    if (!TELEPHONY_ENABLED) return
    void api<{ numbers?: string[] }>('/telephony/exolve/dids')
      .then((r) => rememberOurExolveDids(r.numbers || []))
      .catch(() => {})
  }, [])

  useEffect(() => subscribeAutodialOffer(() => bump((n) => n + 1)), [])

  useEffect(() => {
    const live =
      snap.status === 'ringing' ||
      snap.status === 'active' ||
      snap.status === 'dialing' ||
      snap.status === 'connecting'
    if (live) return
    if (endedFlash && Date.now() < endedFlash.until) return
    setDragPos(null)
  }, [snap.status, endedFlash])

  useEffect(() => {
    if (!TELEPHONY_ENABLED || !canAutodialUI) return
    const ws = new WebSocket(wsURL())
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(String(ev.data)) as {
          type?: string
          payload?: { cli?: string; deal_id?: string; title?: string; call_id?: string; user_id?: string }
        }
        if (msg.type === 'autodial.dialing') {
          rememberAutodialCli(msg.payload?.cli)
          markAutodialAwaitingOffer(180_000)
          clearAutodialPrompt()
          setAutodialActive(true)
          bump((n) => n + 1)
        }
        if (msg.type === 'autodial.offer') {
          rememberAutodialCli(msg.payload?.cli)
          clearAutodialAwaitingOffer()
          clearAutodialPrompt()
          clearAutodialUserDeclined()
          markAutodialSuppress(180_000)
          if (msg.payload?.deal_id) setActiveAutodialOffer(msg.payload)
          setAutodialActive(true)
          bump((n) => n + 1)
        }
        if (msg.type === 'autodial.prompt') {
          rememberAutodialCli(msg.payload?.cli)
          clearAutodialUserDeclined()
          if (msg.payload?.deal_id) setActiveAutodialOffer(msg.payload)
          markAutodialPromptReady()
          setAutodialActive(true)
          bump((n) => n + 1)
        }
        if (msg.type === 'autodial.taken') {
          const mine = Boolean(msg.payload?.user_id && user?.id && msg.payload.user_id === user.id)
          if (mine) {
            iAmAccepter.current = true
          } else if (msg.payload?.user_id) {
            clearAutodialPrompt()
            if (msg.payload?.call_id || msg.payload?.deal_id) {
              markAutodialDismissed(msg.payload.call_id, msg.payload.deal_id)
            }
          }
          bump((n) => n + 1)
        }
        if (msg.type === 'autodial.accepted') {
          if (msg.payload?.user_id && user?.id && msg.payload.user_id === user.id) {
            iAmAccepter.current = true
          }
          bump((n) => n + 1)
        }
        if (msg.type === 'autodial.ended') {
          clearAutodialPrompt()
          if (iAmAccepter.current && wasAutodialTalking.current) {
            const o = getLastAutodialOffer()
            setDispositionMeta({
              dealId: o?.deal_id || msg.payload?.deal_id,
              callId: o?.call_id || msg.payload?.call_id,
              title: o?.title,
              duration: 0,
            })
            setNeedsDisposition(true)
          } else {
            markAutodialEnded()
            markAutodialSuppress(45_000)
            setAutodialActive(false)
          }
          wasAutodialTalking.current = false
          bump((n) => n + 1)
        }
        if (msg.type === 'autodial.disposition') {
          setNeedsDisposition(false)
          iAmAccepter.current = false
          markAutodialEnded()
          setAutodialActive(false)
          bump((n) => n + 1)
        }
      } catch {
        /* ignore */
      }
    }
    return () => {
      try {
        ws.close()
      } catch {
        /* ignore */
      }
    }
  }, [canAutodialUI, user?.id])

  useEffect(() => {
    if (!autodialActive && !isAutodialSuppressing()) return
    const t = window.setInterval(() => bump((n) => n + 1), 1000)
    return () => window.clearInterval(t)
  }, [autodialActive, snap.status])

  // Временный превью UI: /crm?demo=skorozvon
  if (demoSkorozvon) {
    return (
      <DemoSkorozvonModal
        onClose={() => navigate(location.pathname)}
        onOpenCard={() => navigate('/crm')}
      />
    )
  }

  if (!TELEPHONY_ENABLED) return null

  const peerRaw = snap.status === 'ringing' ? snap.incomingPeer || snap.peer : snap.peer
  if (peerRaw) lastPeerRef.current = peerRaw
  const ringing = snap.status === 'ringing'
  const dialing = snap.status === 'dialing'
  const talking = snap.status === 'active' || dialing
  const inCallNow = talking || ringing

  useEffect(() => {
    if (wasInCall.current && !inCallNow && (snap.status === 'ready' || snap.status === 'idle')) {
      const peer = peerRaw || lastPeerRef.current || 'Звонок'
      setEndedFlash({ peer, until: Date.now() + 2500 })
    }
    wasInCall.current = inCallNow
  }, [inCallNow, snap.status, peerRaw])

  useEffect(() => {
    if (!endedFlash) return
    const left = endedFlash.until - Date.now()
    if (left <= 0) {
      setEndedFlash(null)
      return
    }
    const t = window.setTimeout(() => setEndedFlash(null), left)
    return () => window.clearTimeout(t)
  }, [endedFlash])

  const autodialOffer = getLastAutodialOffer()
  const hasSkOffer = Boolean(autodialOffer?.deal_id || autodialOffer?.call_id)
  const ourDidHit = isOurExolveDid(peerRaw)
  const suppressing = isAutodialSuppressing()
  const awaitingOffer = isAutodialAwaitingOffer()
  const autodialWindow = autodialActive || suppressing || awaitingOffer || hasSkOffer
  // DID-шум только в окне скорозвона (иначе peer=DID прятал бы любой входящий).
  const suppressNoise = canAutodialUI && ourDidHit && autodialWindow
  const skorozvonTalk =
    canAutodialUI && talking && iAmAccepter.current && (autodialActive || hasSkOffer)
  const autodialContext =
    canAutodialUI && (needsDisposition || skorozvonTalk || suppressNoise || (autodialActive && hasSkOffer))
  const declinedBlock =
    isAutodialUserDeclined() || isAutodialDismissed(autodialOffer?.call_id)
  const promptReady = isAutodialPromptReady()
  const showAutodialRing =
    autodialContext && promptReady && !declinedBlock && !needsDisposition && ringing && !talking
  const showAutodialTalk = skorozvonTalk && (!declinedBlock || iAmAccepter.current) && !needsDisposition

  if (skorozvonTalk) {
    wasAutodialTalking.current = true
  }

  const hideDecision = shouldHideNormalIncomingBar({
    canAutodialUI,
    ringing,
    talking,
    autodialActive,
    suppressing,
    awaitingOffer,
    peerIsOurDid: ourDidHit,
    needsDisposition,
    hasSkOffer,
    iAmAccepter: iAmAccepter.current,
    promptReady,
    declinedBlock,
    onTelephonyPage: location.pathname.startsWith('/telephony'),
  })

  // #region agent log
  if (ringing || talking || autodialContext || suppressing) {
    fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'post-fix',hypothesisId:'D',location:'IncomingCallBar.tsx:decide',message:'bar visibility after fix',data:{status:snap.status,ringing,talking,peerRaw:String(peerRaw||'').slice(0,60),canAutodialUI,autodialActive,suppressing,ourDidHit,suppressNoise,autodialContext,showAutodialRing,hideDecision,flags:getAutodialDebugFlags()},timestamp:Date.now()})}).catch(()=>{});
  }
  // #endregion

  const finishDisposition = () => {
    setNeedsDisposition(false)
    iAmAccepter.current = false
    wasAutodialTalking.current = false
    markAutodialEnded()
    markAutodialSuppress(45_000)
    setAutodialActive(false)
  }

  // После разговора — три выбора
  if (needsDisposition && canAutodialUI) {
    return (
      <AutodialIncomingModal
        title={dispositionMeta.title || autodialOffer?.title || 'Кандидат'}
        dealId={dispositionMeta.dealId || autodialOffer?.deal_id || liveDealId || undefined}
        callId={dispositionMeta.callId || autodialOffer?.call_id}
        ringing={false}
        talking={false}
        disposition
        durationSec={dispositionMeta.duration || snap.durationSec}
        onAnswer={() => {}}
        onDecline={finishDisposition}
        onOpenCard={() => {
          void navigateToLiveCallCard(navigate, {
            leadId: liveLeadId,
            dealId: dispositionMeta.dealId || autodialOffer?.deal_id || liveDealId,
            openLead,
          })
        }}
        onDispositionDone={finishDisposition}
      />
    )
  }

  // Скорозвон — одна модалка: входящий → разговор
  if ((showAutodialRing || showAutodialTalk) && canAutodialUI) {
    return (
      <AutodialIncomingModal
        title={autodialOffer?.title || 'Кандидат'}
        dealId={autodialOffer?.deal_id || liveDealId || undefined}
        callId={autodialOffer?.call_id}
        ringing={ringing && !talking}
        talking={talking}
        busy={answerBusy}
        durationSec={snap.durationSec}
        onAnswer={() => {
          setAnswerBusy(true)
          iAmAccepter.current = true
          void answer()
            .catch(() => {
              iAmAccepter.current = false
            })
            .finally(() => setAnswerBusy(false))
        }}
        onDecline={rejectIncoming}
        onHangup={() => {
          setDispositionMeta({
            dealId: autodialOffer?.deal_id || liveDealId || undefined,
            callId: autodialOffer?.call_id,
            title: autodialOffer?.title,
            duration: snap.durationSec,
          })
          setNeedsDisposition(true)
          hangup()
        }}
        onOpenCard={() => {
          void navigateToLiveCallCard(navigate, {
            leadId: liveLeadId,
            dealId: autodialOffer?.deal_id || liveDealId,
            openLead,
          })
        }}
      />
    )
  }

  // Скорозвон без prompt / DID-шум — молчим; реальный входящий (не наш DID) показываем.
  if (hideDecision.hide && hideDecision.reason !== 'telephony_page') {
    // #region agent log
    fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'post-fix',hypothesisId:'D',location:'IncomingCallBar.tsx:hide',message:'HIDDEN after fix',data:{reason:hideDecision.reason,status:snap.status,peerRaw:String(peerRaw||'').slice(0,60),ourDidHit,suppressing,ringing},timestamp:Date.now()})}).catch(()=>{});
    // #endregion
    return null
  }

  if (location.pathname.startsWith('/telephony')) {
    // #region agent log
    if (ringing) {
      fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'post-fix',hypothesisId:'E',location:'IncomingCallBar.tsx:hide',message:'HIDDEN: on /telephony page',data:{path:location.pathname,status:snap.status},timestamp:Date.now()})}).catch(()=>{});
    }
    // #endregion
    return null
  }

  const showEnded = Boolean(endedFlash && Date.now() < endedFlash.until && !ringing && !talking)
  if (!ringing && !talking && !journalError && !showEnded) return null

  // #region agent log
  if (ringing) {
    fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'post-fix',hypothesisId:'OK',location:'IncomingCallBar.tsx:show',message:'SHOWING normal IncomingCallBar',data:{peerRaw:String(peerRaw||'').slice(0,60),status:snap.status,suppressing,ourDidHit},timestamp:Date.now()})}).catch(()=>{});
  }
  // #endregion

  const peer = canSeePhones(user)
    ? showEnded
      ? endedFlash!.peer
      : peerRaw
    : maskPhone(showEnded ? endedFlash!.peer : peerRaw) || 'Клиент'

  const cardDealId = liveDealId || autodialOffer?.deal_id || ''
  const canOpenCard = hasLiveCallCard({ leadId: liveLeadId, dealId: cardDealId })
  const rawPeer = (showEnded ? endedFlash?.peer : peerRaw) || ''
  const openCard = () => {
    if (cardBusyRef.current) return
    cardBusyRef.current = true
    setCardError('')
    setCardBusy(true)
    void openOrCreateCallCard(navigate, {
      leadId: liveLeadId,
      dealId: cardDealId,
      openLead,
      peer: rawPeer,
      // Softphone stale lastLeadId — не чужой; autodial-пул открываем без проверки.
      requireOwnerId:
        isManagerRole(user?.role) && !autodialOffer?.deal_id ? user?.id : undefined,
    })
      .then((ok) => {
        if (!ok) setCardError('Не удалось открыть карточку')
      })
      .catch((e: unknown) => {
        setCardError(e instanceof Error ? e.message : 'Не удалось открыть карточку')
      })
      .finally(() => {
        cardBusyRef.current = false
        setCardBusy(false)
      })
  }

  const phaseLabel = softphonePhaseLabel(snap.status, showEnded)
  const phaseHint = softphonePhaseHint(snap.status)

  const clampBar = (x: number, y: number) => {
    const el = panelRef.current
    const w = el?.offsetWidth ?? 320
    const h = el?.offsetHeight ?? 140
    const margin = 8
    const maxX = Math.max(margin, window.innerWidth - w - margin)
    const maxY = Math.max(margin, window.innerHeight - h - margin)
    return {
      x: Math.min(maxX, Math.max(margin, x)),
      y: Math.min(maxY, Math.max(margin, y)),
    }
  }

  const onDragPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return
    const rect = panelRef.current?.getBoundingClientRect()
    if (!rect) return
    dragRef.current = {
      pointerId: e.pointerId,
      dx: e.clientX - rect.left,
      dy: e.clientY - rect.top,
    }
    setDragPos(clampBar(rect.left, rect.top))
    e.currentTarget.setPointerCapture(e.pointerId)
    e.preventDefault()
  }

  const onDragPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== e.pointerId) return
    setDragPos(clampBar(e.clientX - drag.dx, e.clientY - drag.dy))
  }

  const onDragPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (dragRef.current?.pointerId !== e.pointerId) return
    dragRef.current = null
    if (e.currentTarget.hasPointerCapture(e.pointerId)) {
      e.currentTarget.releasePointerCapture(e.pointerId)
    }
  }

  return (
    <div
      ref={panelRef}
      className={`pointer-events-none fixed z-[200] w-[min(100%-2rem,22rem)] ${dragPos ? '' : 'bottom-4 right-4'}`}
      style={dragPos ? { left: dragPos.x, top: dragPos.y } : undefined}
    >
      <div className="pointer-events-auto rounded-b24-sm bg-white shadow-[0_12px_40px_rgba(15,23,42,0.22)] border border-black/8 p-3 flex flex-col gap-2">
        <div
          className="cursor-grab touch-none select-none active:cursor-grabbing"
          title="Перетащить"
          onPointerDown={onDragPointerDown}
          onPointerMove={onDragPointerMove}
          onPointerUp={onDragPointerUp}
          onPointerCancel={onDragPointerUp}
        >
          <div className="text-[12px] text-b24-text-muted">{phaseLabel}</div>
          <div className="text-[16px] font-semibold text-b24-text-dark truncate">
            {peer || (journalError ? 'Журнал CRM' : '…')}
          </div>
          {snap.status === 'active' ? (
            <div className="text-[13px] tabular-nums text-b24-text-muted">
              {formatDuration(snap.durationSec)}
            </div>
          ) : phaseHint ? (
            <div className="text-[12px] text-b24-text-muted">{phaseHint}</div>
          ) : null}
        </div>
        {journalError ? (
          <div className="text-[12px] text-red-600">Журнал CRM: {journalError}</div>
        ) : null}
        {cardError ? <div className="text-[12px] text-red-600">{cardError}</div> : null}
        <div className="flex flex-wrap gap-2">
          {ringing ? (
            <>
              <Button variant="green" className="rounded-b24-sm flex-1" onClick={() => void answer()}>
                <Phone size={14} strokeWidth={2} />
                Ответить
              </Button>
              <Button
                variant="filter"
                className="rounded-b24-sm flex-1 text-red-600"
                onClick={() => rejectIncoming()}
              >
                Отклонить
              </Button>
            </>
          ) : null}
          {talking ? (
            <Button
              variant="filter"
              className="rounded-b24-sm flex-1 text-red-600 bg-red-50"
              onClick={hangup}
            >
              <PhoneOff size={14} strokeWidth={2} />
              Завершить
            </Button>
          ) : null}
          {canOpenCard || rawPeer.trim() ? (
            <Button
              variant="filter"
              className="rounded-b24-sm flex-1"
              disabled={cardBusy}
              onClick={openCard}
            >
              {cardBusy ? 'Открываем…' : 'Карточка'}
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  )
}

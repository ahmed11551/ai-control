import { useEffect, useRef, useState } from 'react'
import { api, wsURL } from '../../lib/api'
import {
  clearAutodialAwaitingOffer,
  clearAutodialUserDeclined,
  getActiveAutodialOffer,
  getAutodialDebugFlags,
  isAutodialDismissed,
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
} from '../../telephony/autodialSuppress'
import { TELEPHONY_ENABLED, AUTODIAL_ENABLED } from '../../telephony/config'
import { isManagerRole } from '../../lib/roles'
import { useAuth } from '../../store/auth'
import { useTelephony } from '../../store/telephony'

type Offer = {
  deal_id: string
  title?: string
  call_id?: string
  cli?: string
}

function dbg(hypothesisId: string, location: string, message: string, data: Record<string, unknown>) {
  // #region agent log
  fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'post-fix21',hypothesisId,location,message,data:{...data,flags:getAutodialDebugFlags()},timestamp:Date.now()})}).catch(()=>{});
  // #endregion
}

export function AutodialOfferBar() {
  const { user } = useAuth()
  const { hangupCallOnly, snap } = useTelephony()
  const hangupCallOnlyRef = useRef(hangupCallOnly)
  hangupCallOnlyRef.current = hangupCallOnly

  const paused = !AUTODIAL_ENABLED || !TELEPHONY_ENABLED
  const canAutodialUI = !paused && isManagerRole(user?.role) && user?.can_call !== false

  const [offer, setOffer] = useState<Offer | null>(null)
  const [talking, setTalking] = useState(false)
  const talkingRef = useRef(false)
  talkingRef.current = talking
  const offerRef = useRef<Offer | null>(null)
  offerRef.current = offer

  useEffect(() => {
    if (paused || !TELEPHONY_ENABLED) return
    void api<{ numbers?: string[] }>('/telephony/exolve/dids')
      .then((r) => rememberOurExolveDids(r.numbers || []))
      .catch(() => {})
  }, [paused])

  useEffect(() => {
    if (paused) return
    return subscribeAutodialOffer(() => {
      if (!getActiveAutodialOffer() && !talkingRef.current) {
        setOffer(null)
        setTalking(false)
      }
    })
  }, [paused])

  useEffect(() => {
    if (paused || !TELEPHONY_ENABLED || !canAutodialUI) return
    if (snap.status !== 'active' && snap.status !== 'dialing') return
    const peer = snap.peer || snap.incomingPeer
    // Только живой offer скорозвона + From=связующий DID — не любой исходящий в окне suppress.
    const stored = getActiveAutodialOffer()
    if (!stored?.deal_id) return
    if (stored.call_id && isAutodialDismissed(stored.call_id)) return
    // Обычный исходящий: peer = клиент, не связующий DID скорозвона.
    if (!isOurExolveDid(peer)) return

    const next: Offer = {
      deal_id: stored.deal_id,
      title: stored.title,
      call_id: stored.call_id,
      cli: stored.cli,
    }

    clearAutodialUserDeclined()
    setActiveAutodialOffer(next)
    setOffer((cur) => ({ ...(cur || {}), ...next }))
    setTalking(true)
    dbg('BE', 'AutodialOfferBar.tsx:softphone', 'softphone active → SHOW talk bar', {
      status: snap.status,
      peer: peer || null,
      dealId: next.deal_id,
      callId: next.call_id || null,
    })
  }, [paused, snap.status, snap.peer, snap.incomingPeer, canAutodialUI])

  useEffect(() => {
    if (paused || !TELEPHONY_ENABLED || !canAutodialUI) return
    const ws = new WebSocket(wsURL())
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(String(ev.data)) as {
          type?: string
          payload?: Offer & { status?: string; user_id?: string }
        }
        const p = msg.payload

        if (msg.type === 'autodial.dialing') {
          if (talkingRef.current) return
          rememberAutodialCli(p?.cli)
          markAutodialAwaitingOffer(180_000)
          return
        }

        if (msg.type === 'autodial.offer' && p?.deal_id) {
          if (p.call_id && isAutodialDismissed(p.call_id)) return
          if (talkingRef.current) return
          rememberAutodialCli(p.cli)
          clearAutodialAwaitingOffer()
          markAutodialSuppress(180_000)
          setActiveAutodialOffer(p)
          setOffer(p)
          setTalking(false)
          return
        }

        if (msg.type === 'autodial.prompt' && p?.deal_id) {
          rememberAutodialCli(p.cli)
          setActiveAutodialOffer(p)
          setOffer((cur) => ({ ...(cur || {}), ...p }))
          markAutodialPromptReady()
          return
        }

        if (msg.type === 'autodial.taken') {
          if (!p?.user_id) return
          if (user?.id && p.user_id === user.id) return
          if (talkingRef.current) return
          if (p?.call_id || p?.deal_id) markAutodialDismissed(p.call_id, p.deal_id)
          setActiveAutodialOffer(null)
          setOffer(null)
          setTalking(false)
          return
        }

        if (msg.type === 'autodial.accepted' && p?.deal_id) {
          if (p.call_id && isAutodialDismissed(p.call_id)) return
          if (p.user_id && user?.id && p.user_id !== user.id) return
          clearAutodialUserDeclined()
          const next = { ...(getActiveAutodialOffer() || {}), ...p }
          setActiveAutodialOffer(next)
          setOffer((cur) =>
            cur ? { ...cur, ...p } : { deal_id: p.deal_id!, call_id: p.call_id, title: p.title },
          )
          setTalking(true)
          return
        }

        if (msg.type === 'autodial.ended') {
          const cur = getActiveAutodialOffer() || offerRef.current
          const endedId = p?.call_id
          const ours = cur?.call_id

          if (endedId && ours && endedId !== ours) {
            if (endedId) markAutodialDismissed(endedId, p?.deal_id)
            return
          }

          if (!talkingRef.current) {
            if (endedId || p?.deal_id) markAutodialDismissed(endedId, p?.deal_id)
            if (!ours || (endedId && endedId === ours)) {
              setActiveAutodialOffer(null)
              setOffer(null)
            }
            return
          }

          if (endedId || p?.deal_id) markAutodialDismissed(endedId, p?.deal_id)
          setActiveAutodialOffer(null)
          setOffer(null)
          setTalking(false)
          markAutodialEnded()
          hangupCallOnlyRef.current()
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
  }, [paused, canAutodialUI, user?.id])

  if (paused || !canAutodialUI) return null
  // Talk UI — в AutodialIncomingModal; здесь только WS/состояние
  return null
}

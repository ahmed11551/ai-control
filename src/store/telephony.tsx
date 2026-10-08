import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { api, wsURL } from '../lib/api'
import {
  AUTODIAL_ENABLED,
  EXOLVE_ENABLED,
  TELEPHONY_ENABLED,
  getTelephonyConfig,
  loadProviderPreference,
  saveProviderPreference,
  type TelephonyProvider,
} from '../telephony/config'
import { loadCallHistory, setCallHistoryUser, historyFromServerCalls } from '../telephony/callHistory'
import { dialProvider } from '../telephony/dest'
import {
  dismissActiveAutodialOffer,
  getActiveAutodialOffer,
  getLastAutodialOffer,
  isAutodialPromptReady,
  isAutodialSuppressing,
  isOurExolveDid,
  markAutodialDismissed,
  markAutodialEnded,
} from '../telephony/autodialSuppress'
import { exolveSoftphone } from '../telephony/exolveClient'
import {
  mergeLiveCrmIds,
  setSoftphoneActiveCli,
  setSoftphoneCrmContext,
  syncSoftphoneJournal,
} from '../telephony/reportCall'
import type { CallHistoryItem, SoftphoneSnapshot } from '../telephony/types'
import { softphone as voxSoftphone } from '../telephony/voxClient'
import { isManagerRole } from '../lib/roles'
import { useAuth } from './auth'

type ExolveSession = {
  mode: 'pool' | 'fallback'
  sip_login: string
  sip_password: string
  pool_count?: number
}

type TelephonyContextValue = {
  provider: TelephonyProvider
  setProvider: (p: TelephonyProvider) => Promise<void>
  configReady: boolean
  username: string
  nodeLabel: string
  defaultDest: string
  snap: SoftphoneSnapshot
  history: CallHistoryItem[]
  connect: () => Promise<void>
  disconnect: () => Promise<void>
  call: (destination: string) => Promise<void>
  answer: () => Promise<void>
  rejectIncoming: (opts?: { localOnly?: boolean }) => void
  hangup: () => void
  /** Только SIP, без dismiss offer (для autodial.ended). */
  hangupCallOnly: () => void
  toggleMute: () => void
  toggleHold: () => Promise<void>
  sendDtmf: (tone: string) => void
  dial: (destination: string, crm?: { leadId?: string; dealId?: string }) => Promise<void>
  liveDealId: string
  liveLeadId: string
  journalError: string
  activeCli: string
  exolveMode: 'pool' | 'fallback' | ''
}

const TelephonyContext = createContext<TelephonyContextValue | null>(null)

const idleSnap: SoftphoneSnapshot = {
  status: 'idle',
  muted: false,
  onHold: false,
  durationSec: 0,
}

function clientFor(provider: TelephonyProvider) {
  return provider === 'exolve' ? exolveSoftphone : voxSoftphone
}

export function TelephonyProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  const config = useMemo(() => getTelephonyConfig(), [])
  const [provider, setProviderState] = useState<TelephonyProvider>(() =>
    loadProviderPreference(config.provider),
  )
  const [snap, setSnap] = useState<SoftphoneSnapshot>(idleSnap)
  const [history, setHistory] = useState<CallHistoryItem[]>([])
  const [liveDealId, setLiveDealId] = useState('')
  const [liveLeadId, setLiveLeadId] = useState('')
  const [journalError, setJournalError] = useState('')
  const [sessionUsername, setSessionUsername] = useState('')
  const [activeCli, setActiveCli] = useState('')
  const [exolveMode, setExolveMode] = useState<'pool' | 'fallback' | ''>('')
  const providerRef = useRef(provider)
  providerRef.current = provider
  const activeCliRef = useRef('')
  activeCliRef.current = activeCli
  const liveLeadRef = useRef('')
  const liveDealRef = useRef('')
  liveLeadRef.current = liveLeadId
  liveDealRef.current = liveDealId

  const reloadRecent = useCallback(() => {
    const uid = user?.id || ''
    setCallHistoryUser(uid)
    if (!uid) {
      setHistory([])
      return
    }
    void api<{ calls?: Parameters<typeof historyFromServerCalls>[0] }>('/telephony')
      .then((d) => {
        const fromServer = historyFromServerCalls(d.calls || [])
        if (fromServer.length) {
          setHistory(fromServer)
          return
        }
        setHistory(loadCallHistory())
      })
      .catch(() => setHistory(loadCallHistory()))
  }, [user?.id])

  useEffect(() => {
    reloadRecent()
  }, [reloadRecent])

  useEffect(() => {
    const client = clientFor(provider)
    return client.subscribe(setSnap)
  }, [provider])

  useEffect(() => {
    if (TELEPHONY_ENABLED) return
    void voxSoftphone.disconnect()
    void exolveSoftphone.disconnect()
    setSnap(idleSnap)
  }, [])

  useEffect(() => {
    if (!TELEPHONY_ENABLED || !EXOLVE_ENABLED) {
      void exolveSoftphone.disconnect()
    }
    if (!EXOLVE_ENABLED && providerRef.current === 'exolve') {
      saveProviderPreference('vox')
      setProviderState('vox')
      setSnap(idleSnap)
    }
  }, [])

  useEffect(() => {
    void syncSoftphoneJournal(providerRef.current, snap).then((res) => {
      const merged = mergeLiveCrmIds(
        { leadId: liveLeadRef.current, dealId: liveDealRef.current },
        { leadId: res.leadId || '', dealId: res.dealId || '' },
        snap.status,
      )
      setLiveLeadId(merged.leadId)
      setLiveDealId(merged.dealId)
      setJournalError(res.error)
      if (res.ended) {
        reloadRecent()
        if (providerRef.current === 'exolve' && activeCliRef.current) {
          void api('/telephony/exolve/cli/release', {
            method: 'POST',
            body: JSON.stringify({ cli: activeCliRef.current }),
          }).catch(() => undefined)
          setActiveCli('')
          activeCliRef.current = ''
          setSoftphoneActiveCli('')
        }
      }
    })
  }, [snap, reloadRecent])

  const activeCfg = provider === 'exolve' ? config.exolve : config.vox
  const configReady = activeCfg.configured
  const username =
    provider === 'exolve'
      ? sessionUsername || config.exolve.sipLogin || 'Exolve'
      : config.vox.username
  const nodeLabel = provider === 'exolve' ? 'МТС Exolve' : config.vox.nodeLabel
  const defaultDest =
    provider === 'exolve' ? config.exolve.defaultDest : config.vox.defaultDest

  const setProvider = useCallback(
    async (next: TelephonyProvider) => {
      if (next === 'exolve' && !EXOLVE_ENABLED) {
        throw new Error('МТС Exolve временно отключён в коде')
      }
      if (next === providerRef.current) return
      await clientFor(providerRef.current).disconnect()
      saveProviderPreference(next)
      setProviderState(next)
      setSnap(idleSnap)
      setSessionUsername('')
      setExolveMode('')
      setActiveCli('')
      setSoftphoneActiveCli('')
    },
    [],
  )

  const releaseCli = useCallback(async () => {
    const cli = activeCliRef.current
    if (!cli) return
    try {
      await api('/telephony/exolve/cli/release', {
        method: 'POST',
        body: JSON.stringify({ cli }),
      })
    } catch {
      /* best-effort */
    }
    setActiveCli('')
    activeCliRef.current = ''
    setSoftphoneActiveCli('')
  }, [])

  const connect = useCallback(async () => {
    if (!TELEPHONY_ENABLED) {
      throw new Error('Телефония временно отключена')
    }
    if (provider === 'exolve') {
      if (!EXOLVE_ENABLED) {
        throw new Error('МТС Exolve временно отключён в коде')
      }
      let sipLogin = config.exolve.sipLogin
      let sipPassword = config.exolve.sipPassword
      try {
        const session = await api<ExolveSession>('/telephony/exolve/session')
        sipLogin = session.sip_login
        sipPassword = session.sip_password
        setExolveMode(session.mode)
        setSessionUsername(session.sip_login)
      } catch (e) {
        if (!sipLogin || !sipPassword) {
          throw e instanceof Error
            ? e
            : new Error('Нет SIP сессии Exolve — импортируйте пул или задайте VITE_EXOLVE_SIP_*')
        }
        setExolveMode('fallback')
        setSessionUsername(sipLogin)
      }
      await exolveSoftphone.connect({ sipLogin, sipPassword })
      void api('/telephony/exolve/pool/ensure', { method: 'POST', body: '{}' }).catch(() => {})
      return
    }
    if (!config.vox.configured) {
      // всё равно пробуем сессию с API
    }
    const ownVoxOnly = String(user?.role || '').toLowerCase() === 'night_logist'
    try {
      const session = await api<{
        username: string
        password: string
        mode?: string
        label?: string
      }>('/telephony/vox/session')
      setSessionUsername(session.username)
      await voxSoftphone.connect({
        node: config.vox.node,
        username: session.username,
        password: session.password,
      })
      return
    } catch (e) {
      if (ownVoxOnly || !config.vox.configured) {
        throw e instanceof Error
          ? e
          : new Error(
              ownVoxOnly
                ? 'Нет отдельного Vox — привяжите аккаунт в карточке сотрудника'
                : 'Нет Vox — привяжите аккаунт в Сотрудниках или задайте VITE_VOX_*',
            )
      }
    }
    await voxSoftphone.connect({
      node: config.vox.node,
      username: config.vox.username,
      password: config.vox.password,
    })
    setSessionUsername(config.vox.username)
  }, [config, provider, user?.role])

  const disconnect = useCallback(async () => {
    if (provider === 'exolve') await releaseCli()
    await clientFor(provider).disconnect()
  }, [provider, releaseCli])

  const call = useCallback(async (destination: string) => {
    const dest = destination.trim()
    const via = dialProvider(providerRef.current, dest)
    if (via === 'exolve') {
      const next = await api<{ cli?: string; mode?: string }>('/telephony/exolve/cli/next', {
        method: 'POST',
        body: '{}',
      })
      const cli = (next.cli || '').trim()
      if (cli) {
        setActiveCli(cli)
        activeCliRef.current = cli
        setSoftphoneActiveCli(cli)
      }
      try {
        await exolveSoftphone.call(dest)
      } catch (e) {
        await releaseCli()
        throw e
      }
      return
    }
    await clientFor(via).call(dest)
  }, [releaseCli])

  const dial = useCallback(
    async (destination: string, crm?: { leadId?: string; dealId?: string }) => {
      const dest = destination.trim()
      if (!dest) throw new Error('Нет номера')
      if (crm?.leadId || crm?.dealId) {
        setSoftphoneCrmContext({ leadId: crm.leadId, dealId: crm.dealId })
      }
      const st = clientFor(providerRef.current).snapshot().status
      if (st === 'idle' || st === 'error') {
        await connect()
      }
      await call(dest)
    },
    [call, connect],
  )

  useEffect(() => {
    if (!TELEPHONY_ENABLED || !configReady) return
    if (user && user.can_call === false) return
    void connect().catch(() => {
      /* ошибка останется в snap */
    })
  }, [configReady, connect, user?.can_call, user?.id])

  useEffect(() => {
    return () => {
      if (import.meta.hot) return
      void voxSoftphone.disconnect()
      void exolveSoftphone.disconnect()
    }
  }, [])

  const answer = useCallback(async () => {
    const client = clientFor(provider)
    const cur = client.snapshot()
    const peer = cur.incomingPeer || cur.peer
    const offer = getActiveAutodialOffer() || getLastAutodialOffer()
    const autodial =
      AUTODIAL_ENABLED &&
      (isOurExolveDid(peer) ||
        Boolean(offer?.call_id || offer?.deal_id) ||
        isAutodialSuppressing() ||
        isAutodialPromptReady())

    if (autodial && cur.status !== 'ringing') {
      setJournalError('Нет входящего на softphone — Exolve на линии?')
      return
    }

    if (autodial && isManagerRole(user?.role)) {
      const q = offer?.call_id
        ? `call_id=${encodeURIComponent(offer.call_id)}`
        : offer?.deal_id
          ? `deal_id=${encodeURIComponent(offer.deal_id)}`
          : ''
      if (q) {
        try {
          await api(`/telephony/autodial/claim?${q}`, { method: 'POST' })
        } catch (err) {
          const msg = err instanceof Error ? err.message : ''
          if (msg === 'already_taken' || msg.includes('already_taken')) {
            markAutodialDismissed(offer?.call_id, offer?.deal_id)
            markAutodialEnded()
            client.rejectIncoming()
            setJournalError('Уже принял другой менеджер')
            return
          }
          throw err
        }
      }
    }

    await client.answer()
  }, [provider, user?.role])

  const hangup = useCallback(() => {
    // Только при явном сбросе — не трогаем offer на Accept
    const o = dismissActiveAutodialOffer()
    if (AUTODIAL_ENABLED && (o?.call_id || o?.deal_id)) {
      const q = o.call_id
        ? `call_id=${encodeURIComponent(o.call_id)}`
        : `deal_id=${encodeURIComponent(o.deal_id!)}`
      void api(`/telephony/autodial/dismiss?${q}`, { method: 'POST' }).catch(() => {})
    }
    clientFor(provider).hangup()
    if (provider === 'exolve') void releaseCli()
  }, [provider, releaseCli])

  const hangupCallOnly = useCallback(() => {
    clientFor(provider).hangup()
    if (provider === 'exolve') void releaseCli()
  }, [provider, releaseCli])

  const rejectIncoming = useCallback(
    (opts?: { localOnly?: boolean }) => {
      const o = dismissActiveAutodialOffer()
      // #region agent log
      fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'decline-trace',hypothesisId:'N',location:'telephony.tsx:rejectIncoming',message:'rejectIncoming called',data:{localOnly:Boolean(opts?.localOnly),hasOffer:Boolean(o),callId:o?.call_id||null,dealId:o?.deal_id||null,status:clientFor(provider).snapshot().status},timestamp:Date.now()})}).catch(()=>{});
      // #endregion
      if (AUTODIAL_ENABLED && !opts?.localOnly && (o?.call_id || o?.deal_id)) {
        const q = o.call_id
          ? `call_id=${encodeURIComponent(o.call_id)}`
          : `deal_id=${encodeURIComponent(o.deal_id!)}`
        void api(`/telephony/autodial/dismiss?${q}`, { method: 'POST' }).catch(() => {})
      }
      clientFor(provider).rejectIncoming()
    },
    [provider],
  )

  // Другой менеджер принял — локально сбрасываем SIP, без dismiss attempt.
  useEffect(() => {
    if (!TELEPHONY_ENABLED || !AUTODIAL_ENABLED) return
    const ws = new WebSocket(wsURL())
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(String(ev.data)) as {
          type?: string
          payload?: { call_id?: string; deal_id?: string; user_id?: string }
        }
        if (msg.type !== 'autodial.taken') return
        const p = msg.payload
        if (!p) return
        // Без user_id нельзя решать «чужой» — иначе победитель сам себе rejectIncoming.
        if (!p.user_id) return
        if (user?.id && p.user_id === user.id) return

        const cur = clientFor(providerRef.current).snapshot()
        const offer = getActiveAutodialOffer() || getLastAutodialOffer()
        const sameCall =
          (p.call_id && offer?.call_id && p.call_id === offer.call_id) ||
          (p.deal_id && offer?.deal_id && p.deal_id === offer.deal_id) ||
          isOurExolveDid(cur.incomingPeer || cur.peer)

        if (!sameCall) return
        if (cur.status !== 'ringing') {
          markAutodialDismissed(p.call_id, p.deal_id, offer?.call_id, offer?.deal_id)
          markAutodialEnded()
          return
        }
        markAutodialDismissed(p.call_id, p.deal_id, offer?.call_id, offer?.deal_id)
        markAutodialEnded()
        clientFor(providerRef.current).rejectIncoming()
        setJournalError('Уже принял другой менеджер')
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
  }, [user?.id])

  const value = useMemo(
    () => ({
      provider,
      setProvider,
      configReady,
      username,
      nodeLabel,
      defaultDest,
      snap,
      history,
      connect,
      disconnect,
      call,
      dial,
      liveDealId,
      liveLeadId,
      journalError,
      answer,
      rejectIncoming,
      hangup,
      hangupCallOnly,
      toggleMute: () => clientFor(provider).toggleMute(),
      toggleHold: () => clientFor(provider).toggleHold(),
      sendDtmf: (tone: string) => clientFor(provider).sendDtmf(tone),
      activeCli,
      exolveMode,
    }),
    [
      provider,
      setProvider,
      configReady,
      username,
      nodeLabel,
      defaultDest,
      snap,
      history,
      connect,
      disconnect,
      call,
      dial,
      liveDealId,
      liveLeadId,
      journalError,
      answer,
      rejectIncoming,
      hangup,
      hangupCallOnly,
      activeCli,
      exolveMode,
    ],
  )

  return (
    <TelephonyContext.Provider value={value}>{children}</TelephonyContext.Provider>
  )
}

export function useTelephony() {
  const ctx = useContext(TelephonyContext)
  if (!ctx) throw new Error('useTelephony must be used within TelephonyProvider')
  return ctx
}

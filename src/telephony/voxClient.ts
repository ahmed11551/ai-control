import { Core, type ConnectionNode } from '@voximplant/websdk'
import {
  CallLoader,
  CallManagerEvent,
  CallEvent,
  RejectMode,
  callToken,
  type Call,
} from '@voximplant/websdk/modules/call-manager'
import {
  StreamHelper,
  StreamLoader,
  streamToken,
  type AudioRenderer,
  type DeviceTrackerHelper,
  type StreamModule,
} from '@voximplant/websdk/modules/stream'
import { applyCallFailedIsolation, inboundRejectMode } from './callPhase'
import { normalizeVoxDest } from './dest'
import { explainCallFail } from './failReason'
import type { SoftphoneSnapshot, SoftphoneStatus } from './types'

type Listener = (snap: SoftphoneSnapshot) => void

/** Caller for inbound bar — не подставлять destination (часто наш DID / agentN). */
function pickIncomingPeer(call: Call): string {
  const display = String(call.remoteDisplayName?.value || '').trim()
  const sip = String(call.remoteSipUri?.value || '').trim()
  const dest = String(call.destination || '').trim()
  const looksPhone = (s: string) => {
    const d = s.replace(/\D/g, '')
    return d.length >= 10 && !/^agent\d+/i.test(s)
  }
  if (looksPhone(display)) return display
  if (looksPhone(sip)) {
    const m = sip.match(/(\+?\d{10,15})/)
    return m ? m[1] : sip
  }
  if (display) return display
  if (sip) return sip
  if (dest && !/^agent\d+/i.test(dest) && looksPhone(dest)) return dest
  return display || sip || 'Входящий'
}

export class VoxSoftphone {
  private core: ReturnType<typeof Core.init> | null = null
  private streamModule: StreamModule | null = null
  private tracker: DeviceTrackerHelper | null = null
  private activeCall: Call | null = null
  private incomingCall: Call | null = null
  private status: SoftphoneStatus = 'idle'
  private error?: string
  private peer?: string
  private incomingPeer?: string
  private muted = false
  private onHold = false
  private durationSec = 0
  private durationTimer: number | null = null
  private listeners = new Set<Listener>()
  private unsubs: Array<() => void> = []
  private audioRenderers = new Map<string, AudioRenderer>()
  private audioHost: HTMLDivElement | null = null
  /** call.id → уже повесили watchers (не дублируем на answer). */
  private watchedCallIds = new Set<string>()
  private connectKey: string | null = null
  private connectPromise: Promise<void> | null = null
  private connectGen = 0
  private onIncomingHandler: ((event: { payload: { callId: string } }) => void) | null = null

  subscribe(listener: Listener) {
    this.listeners.add(listener)
    listener(this.snapshot())
    return () => {
      this.listeners.delete(listener)
    }
  }

  snapshot(): SoftphoneSnapshot {
    return {
      status: this.status,
      error: this.error,
      peer: this.peer,
      muted: this.muted,
      onHold: this.onHold,
      durationSec: this.durationSec,
      incomingPeer: this.incomingPeer,
    }
  }

  private emit() {
    const snap = this.snapshot()
    this.listeners.forEach((l) => l(snap))
  }

  private setStatus(status: SoftphoneStatus, error?: string) {
    this.status = status
    this.error = error
    this.emit()
  }

  private ensureAudioHost() {
    if (this.audioHost || typeof document === 'undefined') return
    const el = document.createElement('div')
    el.id = 'vox-remote-audio'
    el.setAttribute('aria-hidden', 'true')
    el.style.cssText = 'position:fixed;left:-9999px;width:1px;height:1px;overflow:hidden'
    document.body.appendChild(el)
    this.audioHost = el
  }

  private clearAudioRenderers() {
    this.audioRenderers.forEach((renderer) => {
      try {
        renderer.clear()
      } catch {
        /* ignore */
      }
    })
    this.audioRenderers.clear()
    if (this.audioHost) this.audioHost.innerHTML = ''
  }

  private playRemoteStream(stream: { id: string; type?: string }) {
    if (!this.streamModule) return
    this.ensureAudioHost()
    try {
      if (this.audioRenderers.has(stream.id)) return
      const renderer = this.streamModule.rendererManager.createAudioRenderer(
        stream as Parameters<StreamModule['rendererManager']['createAudioRenderer']>[0],
      )
      const audioEl = renderer.getElement()
      audioEl.autoplay = true
      audioEl.setAttribute('playsinline', 'true')
      audioEl.volume = 1
      this.audioHost?.appendChild(audioEl)
      void audioEl.play().catch(() => {
        /* autoplay may require gesture — call button already is a gesture */
      })
      this.audioRenderers.set(stream.id, renderer)
    } catch {
      /* non-audio streams ignored */
    }
  }

  async connect(opts: { node: ConnectionNode; username: string; password: string }) {
    const key = `${opts.username}|${String(opts.node)}`
    if (this.status === 'ready' && this.connectKey === key && this.core) return
    if (this.connectPromise && this.connectKey === key) return this.connectPromise

    const gen = ++this.connectGen
    this.connectKey = key
    this.connectPromise = this.doConnect(opts, gen).finally(() => {
      if (this.connectGen === gen) this.connectPromise = null
    })
    return this.connectPromise
  }

  private async doConnect(
    opts: { node: ConnectionNode; username: string; password: string },
    gen: number,
  ) {
    this.cleanupCallWatchers()
    this.clearAudioRenderers()
    this.setStatus('connecting')

    try {
      // Unlock mic early (browser permission)
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false })
        stream.getTracks().forEach((t) => t.stop())
      } catch {
        throw new Error('Разрешите доступ к микрофону в браузере')
      }

      if (gen !== this.connectGen) return

      if (!this.core) {
        this.core = Core.init({})
        this.core.registerModules([StreamLoader(), CallLoader()])
      }

      const { client } = this.core
      await client.connect({ node: opts.node })
      if (gen !== this.connectGen) return
      await client.login({
        username: opts.username,
        password: opts.password,
      })
      if (gen !== this.connectGen) return

      const streamModule = this.core.getModule(streamToken)
      const callManager = this.core.getModule(callToken)
      if (!streamModule || !callManager) {
        throw new Error('Call modules failed to initialize')
      }

      this.streamModule = streamModule
      this.tracker = streamModule.createHelper(StreamHelper.DeviceTracker)
      this.tracker.enableTracker()

      if (this.onIncomingHandler) {
        try {
          callManager.removeEventListener(CallManagerEvent.IncomingCall, this.onIncomingHandler)
        } catch {
          /* ignore */
        }
      }

      this.onIncomingHandler = (event: { payload: { callId: string } }) => {
        const call = callManager.getCalls().get(event.payload.callId)
        if (!call) {
          // #region agent log
          fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'inbound-bar',hypothesisId:'B',location:'voxClient.ts:onIncoming',message:'IncomingCall but call missing from map',data:{callId:event.payload.callId,status:this.status},timestamp:Date.now()})}).catch(()=>{});
          // #endregion
          return
        }
        // Дубль события по тому же call — не трогаем (иначе 603 Decline сам себе).
        if (this.incomingCall?.id === call.id || this.activeCall?.id === call.id) {
          // #region agent log
          fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'inbound-bar',hypothesisId:'C',location:'voxClient.ts:onIncoming',message:'duplicate IncomingCall ignored',data:{callId:call.id,status:this.status},timestamp:Date.now()})}).catch(()=>{});
          // #endregion
          return
        }

        // Зависший ringing без объекта звонка — лечим, иначе вечный Decline.
        if (this.status === 'ringing' && !this.incomingCall) {
          this.setStatus('ready')
        }

        const rejectMode = inboundRejectMode({
          status: this.status,
          hasActiveCall: Boolean(this.activeCall),
          hasIncomingCall: Boolean(this.incomingCall),
          incomingCallId: this.incomingCall?.id ?? null,
          newCallId: call.id,
        })
        const peerPreview = pickIncomingPeer(call)
        // #region agent log
        fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'post-fix',hypothesisId:rejectMode?'C':'B',location:'voxClient.ts:onIncoming',message:rejectMode?'inbound rejected by softphone':'inbound accepted → ringing',data:{rejectMode,status:this.status,hasActive:Boolean(this.activeCall),hasIncoming:Boolean(this.incomingCall),peerPreview:String(peerPreview).slice(0,80),dest:String(call.destination||'').slice(0,40),display:String(call.remoteDisplayName.value||'').slice(0,40),sip:String(call.remoteSipUri.value||'').slice(0,60),callId:call.id},timestamp:Date.now()})}).catch(()=>{});
        // #endregion
        if (rejectMode) {
          const mode = rejectMode === 'busy' ? RejectMode.Busy : RejectMode.Decline
          console.info('[vox] inbound rejected', {
            mode: rejectMode,
            status: this.status,
            activeId: this.activeCall?.id,
            incomingId: this.incomingCall?.id,
            rejectedId: call.id,
          })
          try {
            call.reject(mode)
          } catch {
            try {
              call.reject(RejectMode.Decline)
            } catch {
              /* ignore */
            }
          }
          return
        }
        this.incomingCall = call
        this.incomingPeer = peerPreview
        this.setStatus('ringing')
        this.watchCall(call, 'incoming')
      }

      callManager.addEventListener(CallManagerEvent.IncomingCall, this.onIncomingHandler)
      this.unsubs.push(() => {
        if (!this.onIncomingHandler) return
        try {
          callManager.removeEventListener(CallManagerEvent.IncomingCall, this.onIncomingHandler)
        } catch {
          /* ignore */
        }
      })

      if (gen !== this.connectGen) return
      this.setStatus('ready')
    } catch (err) {
      if (gen !== this.connectGen) return
      const message = formatVoxConnectError(err, opts.node)
      this.setStatus('error', message)
      throw new Error(message)
    }
  }

  async disconnect() {
    this.connectGen += 1
    this.connectPromise = null
    this.connectKey = null
    try {
      if (this.activeCall) this.activeCall.hangup()
      if (this.incomingCall && this.incomingCall !== this.activeCall) {
        this.incomingCall.reject(RejectMode.Decline)
      }
    } catch {
      /* ignore */
    }
    this.cleanupCallWatchers()
    this.onIncomingHandler = null
    this.clearAudioRenderers()
    try {
      await this.tracker?.clear()
    } catch {
      /* ignore */
    }
    this.activeCall = null
    this.incomingCall = null
    this.peer = undefined
    this.incomingPeer = undefined
    this.muted = false
    this.onHold = false
    this.stopTimer()

    try {
      await this.core?.client.disconnect()
    } catch {
      /* ignore */
    }

    this.setStatus('idle')
  }

  async call(destination: string) {
    if (!this.core) throw new Error('Сначала подключитесь к Voximplant')
    const dest = normalizeVoxDest(destination)
    if (!dest) throw new Error('Укажите номер или пользователя')

    const callManager = this.core.getModule(callToken)
    if (!callManager || !this.tracker) throw new Error('Call manager not ready')

    this.clearAudioRenderers()
    const call = callManager.createCall(dest)
    this.activeCall = call
    this.peer = dest
    this.muted = false
    this.onHold = false
    this.setStatus('dialing')

    this.watchCall(call, 'outgoing')
    // CRITICAL: wait until mic stream is attached before start()
    await this.tracker.attachCall(call)
    await call.start()
  }

  async answer() {
    if (!this.incomingCall || !this.tracker) return
    const call = this.incomingCall
    this.activeCall = call
    this.peer = this.incomingPeer
    this.incomingCall = null
    this.incomingPeer = undefined
    this.clearAudioRenderers()
    this.setStatus('dialing')
    // watchers уже повешены в onIncoming — не дублируем (иначе двойной Failed)
    if (!this.watchedCallIds.has(call.id)) {
      this.watchCall(call, 'incoming')
    }
    await this.tracker.attachCall(call)
    await call.answer()
  }

  rejectIncoming() {
    if (this.incomingCall) {
      try {
        this.incomingCall.reject(RejectMode.Decline)
      } catch {
        /* ignore */
      }
    }
    this.incomingCall = null
    this.incomingPeer = undefined
    if (!this.activeCall) this.setStatus('ready')
    else this.emit()
  }

  hangup() {
    if (this.activeCall) {
      this.activeCall.hangup()
      return
    }
    this.rejectIncoming()
  }

  toggleMute() {
    if (!this.activeCall) return
    this.activeCall.toggleMicrophone()
    this.muted = this.activeCall.isMicrophoneMuted.value
    this.emit()
  }

  async toggleHold() {
    if (!this.activeCall) return
    const next = !this.activeCall.isOnHold.value
    await this.activeCall.hold(next)
    this.onHold = this.activeCall.isOnHold.value
    this.emit()
  }

  sendDtmf(tone: string) {
    this.activeCall?.sendDTMF(tone)
  }

  private watchCall(call: Call, kind: 'outgoing' | 'incoming') {
    if (this.watchedCallIds.has(call.id)) return
    this.watchedCallIds.add(call.id)

    const onConnected = () => {
      this.activeCall = call
      this.setStatus('active')
      this.startTimer()
      call.remoteStreams.value.forEach((stream) => this.playRemoteStream(stream))
    }
    const onRemoteMedia = (event: { payload: { stream: { id: string } } }) => {
      this.playRemoteStream(event.payload.stream)
    }
    const onFailed = (event: { payload?: { code?: number; reason?: string } }) => {
      const next = applyCallFailedIsolation(
        {
          activeCallId: this.activeCall?.id ?? null,
          incomingCallId: this.incomingCall?.id ?? null,
          status: this.status,
          peer: this.peer,
          incomingPeer: this.incomingPeer,
        },
        call.id,
      )
      // Failed не про наш call — игнор (чужой/stale watcher)
      if (
        next.activeCallId === (this.activeCall?.id ?? null) &&
        next.incomingCallId === (this.incomingCall?.id ?? null) &&
        next.status === this.status
      ) {
        return
      }

      const lostActive = this.activeCall?.id === call.id
      if (this.incomingCall?.id === call.id) {
        this.incomingCall = null
        this.incomingPeer = undefined
      }
      if (lostActive) {
        const duration = this.activeCall
          ? Math.floor(this.activeCall.duration / 1000)
          : this.durationSec
        this.stopTimer()
        this.durationSec = duration
        this.activeCall = null
        this.peer = undefined
        this.muted = false
        this.onHold = false
        this.clearAudioRenderers()
        void this.tracker?.clear()
        this.tracker?.enableTracker()
        const code = event?.payload?.code
        const reason = event?.payload?.reason
        this.setStatus(
          'ready',
          kind === 'outgoing' ? explainCallFail(code, reason) : undefined,
        )
        this.durationSec = 0
        this.watchedCallIds.delete(call.id)
        return
      }

      // Упал только входящий — исходящий жив
      if (this.activeCall) {
        this.watchedCallIds.delete(call.id)
        if (this.status === 'ringing') this.setStatus('active')
        else this.emit()
        return
      }
      this.watchedCallIds.delete(call.id)
      this.setStatus('ready')
    }
    const onDisconnected = () => {
      const isActive = this.activeCall?.id === call.id
      const isIncoming = this.incomingCall?.id === call.id
      if (!isActive && !isIncoming) return

      if (isIncoming) {
        this.incomingCall = null
        this.incomingPeer = undefined
      }
      if (isActive) {
        const duration = this.activeCall
          ? Math.floor(this.activeCall.duration / 1000)
          : this.durationSec
        this.stopTimer()
        this.durationSec = duration
        this.activeCall = null
        this.peer = undefined
        this.muted = false
        this.onHold = false
        this.clearAudioRenderers()
        void this.tracker?.clear()
        this.tracker?.enableTracker()
        this.setStatus('ready')
        this.durationSec = 0
      } else if (this.activeCall) {
        if (this.status === 'ringing') this.setStatus('active')
        else this.emit()
      } else {
        this.setStatus('ready')
      }
      this.watchedCallIds.delete(call.id)
    }

    call.addEventListener(CallEvent.Connected, onConnected)
    call.addEventListener(CallEvent.RemoteMediaAdded, onRemoteMedia)
    call.addEventListener(CallEvent.Failed, onFailed)
    call.addEventListener(CallEvent.Disconnected, onDisconnected)

    this.unsubs.push(() => {
      call.removeEventListener(CallEvent.Connected, onConnected)
      call.removeEventListener(CallEvent.RemoteMediaAdded, onRemoteMedia)
      call.removeEventListener(CallEvent.Failed, onFailed)
      call.removeEventListener(CallEvent.Disconnected, onDisconnected)
      this.watchedCallIds.delete(call.id)
    })
  }

  private startTimer() {
    this.stopTimer()
    this.durationSec = 0
    this.durationTimer = window.setInterval(() => {
      if (this.activeCall) {
        this.durationSec = Math.floor(this.activeCall.duration / 1000)
      } else {
        this.durationSec += 1
      }
      this.emit()
    }, 1000)
  }

  private stopTimer() {
    if (this.durationTimer != null) {
      window.clearInterval(this.durationTimer)
      this.durationTimer = null
    }
    this.durationSec = 0
  }

  private cleanupCallWatchers() {
    this.unsubs.splice(0).forEach((fn) => {
      try {
        fn()
      } catch {
        /* ignore */
      }
    })
    this.watchedCallIds.clear()
  }
}

function formatVoxConnectError(err: unknown, node: ConnectionNode | string): string {
  const raw = err instanceof Error ? err.message : String(err || '')
  if (/timeout|408|TransportTimeout|awaiter|type id/i.test(raw)) {
    return (
      `Voximplant не ответил за 40 сек (нода ${String(node)}). ` +
      'В кабинете: главная → «Credentials for working with API, SDK, SIP» — там должна быть та же нода. ' +
      'Или сеть/VPN режет wss. Ключи из .env уже подставлены.'
    )
  }
  if (/401|InvalidPassword/i.test(raw)) {
    return 'Неверный пароль пользователя Voximplant.'
  }
  if (/404|InvalidUsername/i.test(raw)) {
    return 'Пользователь Voximplant не найден. Проверьте VITE_VOX_USERNAME.'
  }
  return raw || 'Не удалось подключиться к Voximplant'
}

export const softphone = new VoxSoftphone()

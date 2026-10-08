import { createSipInstance, type SIPInstance } from '@mts-exolve/web-voice-sdk'
import type { RTCSession } from 'jssip/lib/RTCSession'
import { shouldAutodialDeclineInvite } from './autodialSuppress'
import { explainCallFail } from './failReason'
import type { SoftphoneListener } from './softphoneTypes'
import type { SoftphoneSnapshot, SoftphoneStatus } from './types'

type ActiveHandle = Awaited<ReturnType<SIPInstance['call']>>

type IncomingHandle = {
  accept: () => void
  decline: () => void
  peer: string
  session: RTCSession
}

type CallControls = {
  terminate: () => void
  mute: () => void
  unmute: () => void
  hold: () => void
  unhold: () => void
  sendDTMF: (tone: string) => void
  isMuted: () => boolean
  isOnHold: () => boolean
}

/** Exolve expects digits like 79991234567 (no +). */
export function normalizeExolveNumber(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) return ''
  if (trimmed.includes('@')) return trimmed
  const digits = trimmed.replace(/\D/g, '')
  if (digits.length === 11 && digits.startsWith('8')) {
    return `7${digits.slice(1)}`
  }
  return digits
}

const VIRTUAL_MIC_RE =
  /blackhole|soundflower|loopback|vb-?audio|cable|aggregate|multi-output|zoomaudio|microsoft\s*teams\s*audio|virtual|discord|obs\s*virtual/i

function isVirtualAudioLabel(label: string): boolean {
  return VIRTUAL_MIC_RE.test(label || '')
}

async function pickRealMicrophone(): Promise<{ deviceId: string | null; label: string }> {
  try {
    const warmup = await navigator.mediaDevices.getUserMedia({ audio: true, video: false })
    warmup.getTracks().forEach((t) => t.stop())
  } catch {
    throw new Error('Разрешите доступ к микрофону в браузере')
  }

  const devices = await navigator.mediaDevices.enumerateDevices()
  const mics = devices.filter((d) => d.kind === 'audioinput' && d.deviceId)
  const real = mics.filter((d) => !isVirtualAudioLabel(d.label))

  const preferred =
    real.find((m) => /built-?in|macbook|internal|microphone|микрофон|airpods|headset|usb/i.test(m.label)) ||
    real.find((m) => !/default/i.test(m.label)) ||
    real[0]

  if (!preferred) {
    throw new Error(
      'Не найден обычный микрофон. Сейчас в системе выбран виртуальный (BlackHole и т.п.) — выберите MacBook/гарнитуру в настройках звука macOS',
    )
  }

  return { deviceId: preferred.deviceId, label: preferred.label || preferred.deviceId }
}

export class ExolveSoftphone {
  private sdk: SIPInstance | null = null
  private active: CallControls | null = null
  private incoming: IncomingHandle | null = null
  private status: SoftphoneStatus = 'idle'
  private error?: string
  private peer?: string
  private incomingPeer?: string
  private muted = false
  private onHold = false
  private durationSec = 0
  private durationTimer: number | null = null
  private ringWatchTimer: number | null = null
  private listeners = new Set<SoftphoneListener>()
  private callStartedAt = 0
  private audioEls = new Map<string, HTMLAudioElement>()
  private micWatchTimers: number[] = []
  private micDeviceId: string | null = null
  private micLabel: string | null = null
  /** Инкремент при hangup — отменяет исходящий, который ещё в await sdk.call. */
  private callEpoch = 0

  subscribe(listener: SoftphoneListener) {
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
      micLabel: this.micLabel || undefined,
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

  async connect(opts: { sipLogin: string; sipPassword: string }) {
    await this.disconnect()
    this.setStatus('connecting')

    try {
      // Warm permission + pick a real mic (skip BlackHole / virtual loopbacks)
      const picked = await pickRealMicrophone()
      this.micDeviceId = picked.deviceId
      this.micLabel = picked.label
      console.info('[exolve] using microphone', picked.label)

      const audioConstraint: MediaTrackConstraints = {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        // exact — иначе Chrome часто отдаёт системный default (BlackHole) и собеседник вас не слышит
        ...(picked.deviceId ? { deviceId: { exact: picked.deviceId } } : {}),
      }

      const sdk = createSipInstance({
        sipLogin: opts.sipLogin.trim(),
        sipPassword: opts.sipPassword,
        ssl: true,
        maxLines: 1,
        // SDK default :8443 is often blocked; working path is :443 (as in older Exolve SDK)
        WSUrl: 'wss://webrtc.exolve.ru:443',
        realm: 'webrtc.exolve.ru',
        stunServers: ['stun:webrtc.exolve.ru:3478', 'stun:webrtc.exolve.ru:3479'],
        callOptions: {
          mediaConstraints: { audio: audioConstraint, video: false },
          rtcOfferConstraints: {
            offerToReceiveAudio: true,
            offerToReceiveVideo: false,
          },
          pcConfig: {
            iceServers: [
              { urls: 'stun:webrtc.exolve.ru:3478' },
              { urls: 'stun:webrtc.exolve.ru:3479' },
              { urls: 'stun:stun.l.google.com:19302' },
            ],
            iceCandidatePoolSize: 4,
          },
        },
      })

      let socketConnected = false
      sdk.onConnectionEvent({
        connected: () => {
          socketConnected = true
        },
        disconnected: () => {
          if (this.status !== 'idle' && this.status !== 'connecting') {
            this.setStatus('error', 'SIP-соединение разорвано')
          }
        },
      })

      sdk.onIncomingCall(({ accept, decline, event }) => {
        const session = event.session
        const peer =
          session?.remote_identity?.uri?.user ||
          event?.request?.from?.uri?.user ||
          'Входящий'

        const declineBusy = () => {
          try {
            decline()
          } catch {
            try {
              session?.terminate?.({ status_code: 486, reason_phrase: 'Busy Here' })
            } catch {
              /* ignore */
            }
          }
        }

        // Уже на линии / набор / другой входящий — не рвём текущий разговор.
        if (
          this.active ||
          this.incoming ||
          this.status === 'dialing' ||
          this.status === 'active' ||
          this.status === 'ringing' ||
          this.status === 'connecting'
        ) {
          console.info('[exolve] inbound rejected Busy — already in call', {
            status: this.status,
            peer: String(peer),
          })
          declineBusy()
          return
        }

        // Только после явного «Отклонить»
        if (shouldAutodialDeclineInvite()) {
          // #region agent log
          fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'post-fix14',hypothesisId:'N',location:'exolveClient.ts:onIncoming',message:'SIP INVITE auto-declined (userDeclined)',data:{peer:String(peer)},timestamp:Date.now()})}).catch(()=>{});
          // #endregion
          declineBusy()
          return
        }
        // Не auto-accept: иначе вебхук «менеджер ответил» + бар «Разговор» на автоответчике.
        this.incoming = { accept, decline, peer: String(peer), session }
        this.incomingPeer = String(peer)
        this.setStatus('ringing')
        // #region agent log
        fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'post-fix14',hypothesisId:'A',location:'exolveClient.ts:onIncoming',message:'SIP INVITE → ringing (manual answer only)',data:{peer:String(peer)},timestamp:Date.now()})}).catch(()=>{});
        // #endregion

        this.attachRemoteAudio(session)

        const onDead = () => {
          if (this.incoming?.session && this.incoming.session !== session) return
          if (this.active) {
            // Входящий умер — исходящий разговор оставляем.
            this.incoming = null
            this.incomingPeer = undefined
            if (this.status === 'ringing') this.setStatus('active')
            else this.emit()
            return
          }
          this.finishCall()
        }
        session.on('failed', onDead)
        session.on('ended', onDead)
        session.on('confirmed', () => {
          this.attachRemoteAudio(session, true)
          void this.forceSendMic(session)
          this.setStatus('active')
          this.startTimer()
        })
      })

      try {
        await registerWithTimeout(sdk, 20_000)
      } catch (err) {
        if (!socketConnected) {
          throw new Error(
            'Нет связи с SIP WebSocket (wss://webrtc.exolve.ru:443). Проверь сеть/файрвол',
          )
        }
        throw err
      }
      this.sdk = sdk
      if (picked.deviceId) {
        try {
          await sdk.setMicrophone(picked.deviceId)
        } catch {
          /* optional — mediaConstraints already pin the device */
        }
      }
      this.startRingWatch()
      this.setStatus('ready')
    } catch (err) {
      const message = formatExolveConnectError(err)
      this.setStatus('error', message)
      throw new Error(message)
    }
  }

  async disconnect() {
    this.stopRingWatch()
    try {
      this.active?.terminate()
    } catch {
      /* ignore */
    }
    try {
      this.incoming?.decline()
    } catch {
      /* ignore */
    }
    this.clearAudio()
    this.clearMicWatch()
    this.stopTimer()
    this.active = null
    this.incoming = null
    this.peer = undefined
    this.incomingPeer = undefined
    this.muted = false
    this.onHold = false
    this.micDeviceId = null
    this.micLabel = null

    try {
      if (this.sdk?.isRegistered()) await this.sdk.unregister()
    } catch {
      /* ignore */
    }
    this.sdk = null
    this.setStatus('idle')
  }

  async call(destination: string) {
    if (!this.sdk) throw new Error('Сначала подключитесь к МТС Exolve')
    const dest = normalizeExolveNumber(destination)
    if (!dest) throw new Error('Укажите номер')

    const epoch = ++this.callEpoch
    this.peer = dest
    this.muted = false
    this.onHold = false
    this.clearAudio()
    this.clearMicWatch()
    this.setStatus('dialing')

    try {
      // Re-pick mic each call — system default may have flipped to BlackHole
      await this.refreshPreferredMic()
      if (epoch !== this.callEpoch) {
        this.finishCall()
        return
      }
      const handle = await this.sdk.call(dest, {
        progress: () => {
          if (epoch !== this.callEpoch) return
          this.setStatus('dialing')
        },
        confirmed: () => {
          if (epoch !== this.callEpoch) {
            try {
              handle.terminate()
            } catch {
              /* ignore */
            }
            return
          }
          this.attachRemoteAudio(handle.session, true)
          void this.forceSendMic(handle.session, handle)
          this.setStatus('active')
          this.startTimer()
        },
        muted: () => {
          this.muted = true
          this.emit()
        },
        unmuted: () => {
          this.muted = false
          this.emit()
        },
        held: () => {
          this.onHold = true
          this.emit()
        },
        unheld: () => {
          this.onHold = false
          this.emit()
        },
        failed: (event) => {
          if (epoch !== this.callEpoch) return
          const cause = event?.cause ? String(event.cause) : undefined
          const code = Number.parseInt(cause || '', 10)
          this.finishCall(explainCallFail(Number.isFinite(code) ? code : undefined, cause))
        },
        ended: () => {
          if (epoch !== this.callEpoch) return
          this.finishCall()
        },
      })
      if (epoch !== this.callEpoch) {
        try {
          handle.terminate()
        } catch {
          /* ignore */
        }
        this.finishCall()
        return
      }
      this.active = asControls(handle)
      this.attachRemoteAudio(handle.session)
      // Peer connection may already exist before confirmed
      void this.forceSendMic(handle.session, handle)
    } catch (err) {
      if (epoch !== this.callEpoch) {
        this.finishCall()
        return
      }
      const message = err instanceof Error ? err.message : 'Не удалось позвонить'
      this.finishCall(message)
      throw err
    }
  }

  async answer() {
    if (!this.incoming) return
    const incoming = this.incoming
    this.peer = incoming.peer
    this.incomingPeer = undefined
    this.active = sessionAsControls(incoming.session)
    this.clearMicWatch()
    this.setStatus('dialing')
    try {
      await this.refreshPreferredMic()
      // Warm real mic before answer — reduces both-way silence races
      await this.openPreferredMic().then((s) => s.getTracks().forEach((t) => t.stop())).catch(() => {})
      this.attachRemoteAudio(incoming.session, true)
      incoming.accept()
      this.incoming = null
      this.attachRemoteAudio(incoming.session, true)
      await this.forceSendMic(incoming.session)
      window.setTimeout(() => {
        this.attachRemoteAudio(incoming.session, true)
        void this.forceSendMic(incoming.session)
      }, 400)
      window.setTimeout(() => {
        this.attachRemoteAudio(incoming.session, true)
        void this.forceSendMic(incoming.session)
      }, 1200)
    } catch (err) {
      this.incoming = null
      this.active = null
      const message = err instanceof Error ? err.message : 'Не удалось ответить'
      this.setStatus('ready', message)
    }
  }

  rejectIncoming() {
    const incoming = this.incoming
    if (incoming) {
      try {
        incoming.decline()
      } catch {
        try {
          incoming.session.terminate({ status_code: 486, reason_phrase: 'Busy Here' })
        } catch {
          /* ignore */
        }
      }
    }
    this.incoming = null
    this.incomingPeer = undefined
    // Даже если session уже пропала — не оставляем UI в «ringing» (скорозвон-офер)
    if (!this.active) this.setStatus('ready')
    else this.emit()
  }

  hangup() {
    // Сразу инвалидируем исходящий, который ещё в await sdk.call (иначе «Сброс» во время набора мёртв).
    this.callEpoch += 1
    try {
      this.active?.terminate()
    } catch {
      /* ignore */
    }
    try {
      if (this.incoming) {
        try {
          this.incoming.decline()
        } catch {
          try {
            this.incoming.session?.terminate?.({ status_code: 486, reason_phrase: 'Busy Here' })
          } catch {
            /* ignore */
          }
        }
      }
    } catch {
      /* ignore */
    }
    // Всегда сбрасываем UI: ended от SDK иногда не приходит — в телефонии «Сброс» зависал,
    // а в скорозвоне модалка переключалась сама на «Решение».
    if (
      this.active ||
      this.incoming ||
      this.status === 'dialing' ||
      this.status === 'active' ||
      this.status === 'ringing'
    ) {
      this.finishCall()
    }
  }

  toggleMute() {
    if (!this.active) return
    if (this.active.isMuted()) this.active.unmute()
    else this.active.mute()
    this.muted = Boolean(this.active.isMuted())
    this.emit()
  }

  async toggleHold() {
    if (!this.active) return
    if (this.active.isOnHold()) this.active.unhold()
    else this.active.hold()
    this.onHold = this.active.isOnHold()
    this.emit()
  }

  sendDtmf(tone: string) {
    this.active?.sendDTMF(tone)
  }

  /** New SDK never creates <audio> — without this remote side is silent. */
  private attachRemoteAudio(session: RTCSession, force = false) {
    const id = session.id
    let audio = this.audioEls.get(id)
    if (audio && !force) {
      this.bindRemotePlayback(session, audio)
      return
    }
    if (!audio) {
      audio = document.createElement('audio')
      audio.autoplay = true
      audio.setAttribute('playsinline', 'true')
      audio.setAttribute('data-session-id', id)
      audio.volume = 1
      audio.style.cssText = 'position:fixed;left:-9999px;width:1px;height:1px;overflow:hidden'
      document.body.appendChild(audio)
      this.audioEls.set(id, audio)
    }
    this.bindRemotePlayback(session, audio)
  }

  private bindRemotePlayback(session: RTCSession, audio: HTMLAudioElement) {
    const playTrack = (track: MediaStreamTrack) => {
      if (track.kind !== 'audio') return
      try {
        track.enabled = true
      } catch {
        /* ignore */
      }
      audio.srcObject = new MediaStream([track])
      void audio.play().catch(() => {})
    }

    const onTrack = (event: RTCTrackEvent) => {
      if (event.track.kind !== 'audio') return
      playTrack(event.track)
    }

    const bindPc = (pc: RTCPeerConnection) => {
      pc.removeEventListener('track', onTrack)
      pc.addEventListener('track', onTrack)
      pc.getReceivers().forEach((receiver) => {
        if (receiver.track?.kind === 'audio') playTrack(receiver.track)
      })
      // Late tracks / ICE
      window.setTimeout(() => {
        pc.getReceivers().forEach((receiver) => {
          if (receiver.track?.kind === 'audio') playTrack(receiver.track)
        })
        void audio.play().catch(() => {})
      }, 500)
    }

    if (session.connection) {
      bindPc(session.connection)
    } else {
      session.on('peerconnection', (data: { peerconnection: RTCPeerConnection }) => {
        bindPc(data.peerconnection)
      })
    }
  }

  /**
   * Ensure mic is actually sent: unmute/unhold, enable sender tracks,
   * and if the browser muted the track (silent but "live"), replace it.
   */
  private async forceSendMic(
    session: RTCSession,
    handle?: { unmute: () => void; unhold: () => void; isMuted: () => boolean; isOnHold: () => boolean },
  ) {
    const apply = async (pc: RTCPeerConnection) => {
      try {
        if (handle) {
          if (handle.isOnHold()) handle.unhold()
          if (handle.isMuted()) handle.unmute()
        } else {
          try {
            session.unhold()
          } catch {
            /* ignore */
          }
          try {
            session.unmute({ audio: true })
          } catch {
            /* ignore */
          }
        }
      } catch {
        /* ignore */
      }

      const sender =
        pc.getSenders().find((s) => s.track?.kind === 'audio') ??
        pc.getSenders().find((s) => s.track == null)
      const track = sender?.track
      const virtual = track ? isVirtualAudioLabel(track.label) : false
      const wrongMic =
        Boolean(track && this.micLabel && track.label && track.label !== this.micLabel)

      // Always push our real mic — JsSIP/default often grabs BlackHole even when "live"
      if (sender) {
        try {
          const stream = await this.openPreferredMic()
          const fresh = stream.getAudioTracks()[0]
          if (fresh) {
            fresh.enabled = true
            if (!track || virtual || wrongMic || track.readyState !== 'live' || track.enabled === false || track !== fresh) {
              await sender.replaceTrack(fresh)
              try {
                if (track && track !== fresh) track.stop()
              } catch {
                /* ignore */
              }
              console.info('[exolve] mic on call', {
                from: track?.label,
                to: fresh.label,
                virtual,
                wrongMic,
              })
            }
          }
        } catch (err) {
          console.warn('[exolve] mic refresh failed', err)
        }
      }

      pc.getSenders().forEach((s) => {
        if (s.track?.kind === 'audio') s.track.enabled = true
      })

      this.watchOutboundAudio(pc)
    }

    if (session.connection) {
      await apply(session.connection)
      return
    }

    await new Promise<void>((resolve) => {
      const timer = window.setTimeout(() => resolve(), 3000)
      session.on('peerconnection', (data: { peerconnection: RTCPeerConnection }) => {
        window.clearTimeout(timer)
        void apply(data.peerconnection).finally(() => resolve())
      })
    })
  }

  /** If outbound RTP stays at 0 bytes, force a fresh mic track once. */
  private watchOutboundAudio(pc: RTCPeerConnection) {
    this.clearMicWatch()
    let attempts = 0
    const tick = window.setInterval(() => {
      void (async () => {
        attempts += 1
        try {
          const stats = await pc.getStats()
          let bytesSent = 0
          stats.forEach((report) => {
            if (report.type === 'outbound-rtp' && (report as { kind?: string }).kind === 'audio') {
              bytesSent += Number((report as { bytesSent?: number }).bytesSent || 0)
            }
          })
          const sender = pc.getSenders().find((s) => s.track?.kind === 'audio')
          const track = sender?.track
          console.debug('[exolve] outbound audio', {
            bytesSent,
            trackState: track?.readyState,
            enabled: track?.enabled,
            muted: track?.muted,
            label: track?.label,
          })

          if (bytesSent > 0) {
            this.clearMicWatch()
            return
          }

          // Virtual mic or no RTP — force the real device we picked on connect
          if (attempts <= 4) {
            const badVirtual = Boolean(track && isVirtualAudioLabel(track.label))
            if (badVirtual || bytesSent === 0) {
              const stream = await this.openPreferredMic()
              const fresh = stream.getAudioTracks()[0]
              if (fresh && sender) {
                const old = sender.track
                fresh.enabled = true
                await sender.replaceTrack(fresh)
                try {
                  old?.stop()
                } catch {
                  /* ignore */
                }
                console.warn('[exolve] replaced mic', {
                  from: old?.label,
                  to: fresh.label,
                  muted: fresh.muted,
                })
                if (!isVirtualAudioLabel(fresh.label) && attempts >= 2 && bytesSent === 0) {
                  // keep watching a bit more for RTP
                }
              }
            }
          }

          if (attempts >= 6) this.clearMicWatch()
        } catch {
          /* ignore */
        }
      })()
    }, 1000)
    this.micWatchTimers.push(tick)
  }

  private clearMicWatch() {
    this.micWatchTimers.forEach((id) => window.clearInterval(id))
    this.micWatchTimers = []
  }

  private async refreshPreferredMic() {
    try {
      const picked = await pickRealMicrophone()
      this.micDeviceId = picked.deviceId
      this.micLabel = picked.label
      this.emit()
      if (picked.deviceId && this.sdk) {
        try {
          await this.sdk.setMicrophone(picked.deviceId)
        } catch {
          /* ignore */
        }
      }
      console.info('[exolve] microphone', picked.label)
    } catch (err) {
      console.warn('[exolve] mic pick failed', err)
    }
  }

  private async openPreferredMic() {
    if (!this.micDeviceId) await this.refreshPreferredMic()
    const audio: MediaTrackConstraints = {
      echoCancellation: true,
      noiseSuppression: true,
      autoGainControl: true,
    }
    try {
      if (this.micDeviceId) audio.deviceId = { exact: this.micDeviceId }
      const stream = await navigator.mediaDevices.getUserMedia({ audio, video: false })
      const label = stream.getAudioTracks()[0]?.label || ''
      if (isVirtualAudioLabel(label)) {
        stream.getTracks().forEach((t) => t.stop())
        throw new Error('virtual')
      }
      return stream
    } catch {
      await this.refreshPreferredMic()
      const fallback: MediaTrackConstraints = {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        ...(this.micDeviceId ? { deviceId: { exact: this.micDeviceId } } : {}),
      }
      return navigator.mediaDevices.getUserMedia({ audio: fallback, video: false })
    }
  }

  private clearAudio() {
    this.audioEls.forEach((audio) => {
      try {
        audio.pause()
        audio.srcObject = null
        audio.remove()
      } catch {
        /* ignore */
      }
    })
    this.audioEls.clear()
  }

  private finishCall(error?: string) {
    this.clearAudio()
    this.clearMicWatch()
    this.stopTimer()
    this.active = null
    this.incoming = null
    this.peer = undefined
    this.incomingPeer = undefined
    this.muted = false
    this.onHold = false
    if (this.sdk?.isRegistered()) this.setStatus('ready', error)
    else this.setStatus('idle', error)
    this.durationSec = 0
  }

  /** Пока ringing: если session уже CANCELED/TERMINATED, а событие lost — сбрасываем UI. */
  private startRingWatch() {
    this.stopRingWatch()
    this.ringWatchTimer = window.setInterval(() => {
      if (this.status !== 'ringing') return
      const session = this.incoming?.session
      if (!session || session.isEnded()) {
        if (this.active) {
          this.incoming = null
          this.incomingPeer = undefined
          this.setStatus('active')
          return
        }
        this.finishCall()
      }
    }, 400)
  }

  private stopRingWatch() {
    if (this.ringWatchTimer != null) {
      window.clearInterval(this.ringWatchTimer)
      this.ringWatchTimer = null
    }
  }

  private startTimer() {
    this.stopTimer()
    this.callStartedAt = Date.now()
    this.durationSec = 0
    this.durationTimer = window.setInterval(() => {
      this.durationSec = Math.floor((Date.now() - this.callStartedAt) / 1000)
      this.emit()
    }, 1000)
  }

  private stopTimer() {
    if (this.durationTimer != null) {
      window.clearInterval(this.durationTimer)
      this.durationTimer = null
    }
  }
}

async function registerWithTimeout(sdk: SIPInstance, ms: number) {
  const ua = sdk.SIPInstance
  if (sdk.isRegistered()) return

  await new Promise<void>((resolve, reject) => {
    let done = false
    const timer = window.setTimeout(() => {
      if (done) return
      done = true
      reject(new Error('Таймаут регистрации SIP — проверь логин/пароль и сеть'))
    }, ms)

    ua.on('registered', () => {
      if (done) return
      done = true
      window.clearTimeout(timer)
      resolve()
    })
    ua.on(
      'registrationFailed',
      (event: { cause?: string; response?: { status_code?: number; reason_phrase?: string } }) => {
        if (done) return
        done = true
        window.clearTimeout(timer)
        const code = event?.response?.status_code
        const reason = event?.response?.reason_phrase || event?.cause
        const detail = [code != null ? `код ${code}` : null, reason].filter(Boolean).join(' — ')
        reject(
          new Error(
            code === 403
              ? 'Регистрация SIP отклонена (403). Нужен SIP ID из ЛК Exolve → «SIP-подключения», не городской номер и не логин Vox. Пароль — из той же карточки SIP.'
              : detail
                ? `Регистрация SIP не удалась (${detail})`
                : 'Регистрация SIP не удалась',
          ),
        )
      },
    )
    ua.start()
  })
}

function formatExolveConnectError(err: unknown): string {
  if (err instanceof Error) return err.message
  if (typeof err === 'string') return err
  if (err && typeof err === 'object') {
    const e = err as {
      cause?: string
      message?: string
      response?: { status_code?: number; reason_phrase?: string }
    }
    if (e.message) return e.message
    const code = e.response?.status_code
    const reason = e.response?.reason_phrase || e.cause
    const detail = [code != null ? `код ${code}` : null, reason].filter(Boolean).join(' — ')
    if (detail) return `Регистрация SIP не удалась (${detail})`
  }
  return 'Не удалось подключиться к Exolve'
}

function asControls(handle: ActiveHandle): CallControls {
  return {
    terminate: () => handle.terminate(),
    mute: () => handle.mute(),
    unmute: () => handle.unmute(),
    hold: () => handle.hold(),
    unhold: () => handle.unhold(),
    sendDTMF: (tone) => handle.sendDTMF(tone),
    isMuted: () => Boolean(handle.isMuted()),
    isOnHold: () => handle.isOnHold(),
  }
}

function sessionAsControls(session: RTCSession): CallControls {
  return {
    terminate: () => session.terminate(),
    mute: () => session.mute({ audio: true }),
    unmute: () => session.unmute({ audio: true }),
    hold: () => {
      void session.hold()
    },
    unhold: () => {
      void session.unhold()
    },
    sendDTMF: (tone) => session.sendDTMF(tone),
    isMuted: () => Boolean(session.isMuted()?.audio),
    isOnHold: () => Boolean(session.isOnHold()?.local || session.isOnHold()?.remote),
  }
}

export const exolveSoftphone = new ExolveSoftphone()

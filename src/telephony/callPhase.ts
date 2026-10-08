import type { SoftphoneStatus } from './types'

/** Как отклонять лишний входящий: Busy = 486 «user busy» на телефоне звонящего. */
export type InboundRejectMode = 'busy' | 'decline'

/**
 * Лишний входящий при живом разговоре → Busy.
 * Другой входящий уже висит → Decline.
 * Нельзя отклонять по одному status=connecting/ringing: иначе 603 Decline
 * на каждый PSTN во время reconnect / дубль IncomingCall.
 */
export function inboundRejectMode(opts: {
  status: SoftphoneStatus
  hasActiveCall: boolean
  hasIncomingCall: boolean
  incomingCallId?: string | null
  newCallId?: string | null
}): InboundRejectMode | null {
  if (opts.newCallId && opts.incomingCallId && opts.newCallId === opts.incomingCallId) {
    return null
  }
  if (opts.hasActiveCall || opts.status === 'dialing' || opts.status === 'active') {
    return 'busy'
  }
  if (opts.hasIncomingCall) {
    return 'decline'
  }
  return null
}

/** @deprecated см. inboundRejectMode — true, если входящий нужно отклонить. */
export function shouldRejectInboundBusy(opts: {
  status: SoftphoneStatus
  hasActiveCall: boolean
  hasIncomingCall: boolean
}): boolean {
  return inboundRejectMode(opts) !== null
}

export type CallLegState = {
  activeCallId: string | null
  incomingCallId: string | null
  status: SoftphoneStatus
  peer?: string
  incomingPeer?: string
}

/**
 * Изоляция Failed: чистим только ногу с failedCallId.
 * Если упал входящий при живом исходящем — исходящий остаётся.
 */
export function applyCallFailedIsolation(
  state: CallLegState,
  failedCallId: string,
): CallLegState {
  const isActive = state.activeCallId === failedCallId
  const isIncoming = state.incomingCallId === failedCallId
  if (!isActive && !isIncoming) {
    return state
  }

  let activeCallId = state.activeCallId
  let incomingCallId = state.incomingCallId
  let peer = state.peer
  let incomingPeer = state.incomingPeer
  let status = state.status

  if (isIncoming) {
    incomingCallId = null
    incomingPeer = undefined
  }
  if (isActive) {
    activeCallId = null
    peer = undefined
    status = 'ready'
  } else if (activeCallId) {
    // упал только входящий — сохраняем разговор
    status = status === 'ringing' ? 'active' : status
  } else {
    status = 'ready'
  }

  return { activeCallId, incomingCallId, status, peer, incomingPeer }
}

/** Подпись фазы для плашки / TopBar. */
export function softphonePhaseLabel(status: SoftphoneStatus, endedFlash = false): string {
  if (endedFlash) return 'Завершён'
  switch (status) {
    case 'idle':
      return 'Офлайн'
    case 'connecting':
      return 'Подключение…'
    case 'ready':
      return 'На линии'
    case 'dialing':
      return 'Набор…'
    case 'ringing':
      return 'Входящий звонок'
    case 'active':
      return 'Разговор'
    case 'error':
      return 'Ошибка'
    default:
      return 'Софтфон'
  }
}

export function softphonePhaseHint(status: SoftphoneStatus): string | null {
  if (status === 'dialing') return 'гудки / соединение'
  if (status === 'connecting') return 'подключение softphone'
  return null
}

/** Иконка в TopBar считается «живой» (не серая). */
export function softphoneIconLive(status: SoftphoneStatus): boolean {
  return (
    status === 'active' ||
    status === 'dialing' ||
    status === 'ringing' ||
    status === 'connecting' ||
    status === 'ready'
  )
}

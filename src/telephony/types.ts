export type CallDirection = 'outgoing' | 'incoming'

export type CallHistoryItem = {
  id: string
  direction: CallDirection
  peer: string
  startedAt: string
  endedAt?: string
  durationSec: number
  status: 'answered' | 'missed' | 'failed' | 'cancelled'
}

export type SoftphoneStatus =
  | 'idle'
  | 'connecting'
  | 'ready'
  | 'ringing'
  | 'dialing'
  | 'active'
  | 'error'

export type SoftphoneSnapshot = {
  status: SoftphoneStatus
  error?: string
  peer?: string
  muted: boolean
  onHold: boolean
  durationSec: number
  incomingPeer?: string
  micLabel?: string
}

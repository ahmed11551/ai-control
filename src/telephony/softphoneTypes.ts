import type { SoftphoneSnapshot } from './types'

export type SoftphoneListener = (snap: SoftphoneSnapshot) => void

/** Common softphone surface used by Voximplant and MTS Exolve clients. */
export type SoftphoneClient = {
  subscribe(listener: SoftphoneListener): () => void
  snapshot(): SoftphoneSnapshot
  connect(opts: Record<string, unknown>): Promise<void>
  disconnect(): Promise<void>
  call(destination: string): Promise<void>
  answer(): Promise<void>
  rejectIncoming(): void
  hangup(): void
  toggleMute(): void
  toggleHold(): Promise<void>
  sendDtmf(tone: string): void
}

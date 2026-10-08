/** Минимальная длительность разговора после поднятия линии для «Дозвон».
 *  Короче — автоответчик, сброс, AMD Exolve, недозвон. */
export const MIN_REACHED_TALK_SEC = 15

export type CallReachResult = 'reached' | 'no_reach'

/** Дозвон только если линия реально «пожила» достаточно для разговора. */
export function resolveCallReach(talkSec: number): CallReachResult {
  return talkSec >= MIN_REACHED_TALK_SEC ? 'reached' : 'no_reach'
}

export function callReachLabel(disposition: string, talkSec = 0): 'Дозвон' | 'Недозвон' {
  const v = disposition.toLowerCase().trim()
  if (v === 'reached') return 'Дозвон'
  // старые записи: answered/connected — пересчитываем по длительности
  if (v === 'connected' || v === 'answered' || v === 'answer') {
    return resolveCallReach(talkSec) === 'reached' ? 'Дозвон' : 'Недозвон'
  }
  return 'Недозвон'
}

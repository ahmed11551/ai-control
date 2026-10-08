/** Общий флаг «идёт скорозвон / только что закончился» — глушим обычный IncomingCallBar. */

const STORAGE_KEY = 'crm_autodial_decline_v1'

let suppressUntil = 0
/**
 * Короткое окно после явного «Отклонить»: глушим re-INVITE того же звонка.
 * НЕ блокирует следующих кандидатов кампании навечно.
 */
let userDeclinedUntil = 0
/** Скрипт ещё звонит кандидату — ложный SIP INVITE (From=DID) не поднимать в ringing. */
let awaitingOfferUntil = 0
/** Наши DID Exolve (CLI) — From на softphone при MakeCallback. */
const ourDids = new Set<string>()
/** Отклонённые call_id / deal_id — модульный Set, переживает ремаунт компонента. */
const dismissedIds = new Set<string>()
/**
 * Текущий + последний offer с вебхука.
 * lastOffer нужен: reject softphone часто раньше, чем успели setActive — иначе dismiss не уходит.
 */
let activeOffer: { call_id?: string; deal_id?: string; title?: string; cli?: string } | null = null
let lastOffer: { call_id?: string; deal_id?: string; title?: string; cli?: string } | null = null
/** Бэкенд подтвердил: AMD не сбросил — можно показать «Ответить». */
let promptReady = false
const offerListeners = new Set<() => void>()

export function markAutodialPromptReady() {
  promptReady = true
  notifyOfferListeners()
}

export function clearAutodialPrompt() {
  promptReady = false
}

export function isAutodialPromptReady() {
  return promptReady
}

function persistDecline() {
  try {
    sessionStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        until: userDeclinedUntil,
        dismissed: [...dismissedIds].slice(-80),
      }),
    )
  } catch {
    /* ignore */
  }
}

function loadPersistedDecline() {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return
    const j = JSON.parse(raw) as { until?: number; dismissed?: string[] }
    // Восстанавливаем только короткий хвост (≤45с), старые 120с-локи сбрасываем
    if (typeof j.until === 'number') {
      const remain = j.until - Date.now()
      if (remain > 0 && remain <= 45_000) userDeclinedUntil = j.until
    }
    if (Array.isArray(j.dismissed)) {
      for (const id of j.dismissed) {
        const s = String(id || '').trim()
        if (s) dismissedIds.add(s)
      }
    }
    persistDecline()
    // #region agent log
    fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'post-fix8',hypothesisId:'Q',location:'autodialSuppress.ts:load',message:'restored decline from sessionStorage',data:{userDeclined:Date.now()<userDeclinedUntil,until:userDeclinedUntil||null,dismissedCount:dismissedIds.size},timestamp:Date.now()})}).catch(()=>{});
    // #endregion
  } catch {
    /* ignore */
  }
}

loadPersistedDecline()

function notifyOfferListeners() {
  for (const fn of offerListeners) {
    try {
      fn()
    } catch {
      /* ignore */
    }
  }
}

export function setActiveAutodialOffer(
  offer: { call_id?: string; deal_id?: string; title?: string; cli?: string } | null,
) {
  activeOffer = offer
  if (offer?.deal_id) lastOffer = offer
  if (!offer) promptReady = false
  notifyOfferListeners()
}

export function getActiveAutodialOffer() {
  return activeOffer
}

export function getLastAutodialOffer() {
  return activeOffer || lastOffer
}

export function subscribeAutodialOffer(fn: () => void) {
  offerListeners.add(fn)
  return () => {
    offerListeners.delete(fn)
  }
}

/** Softphone «Отклонить» / hangup — гасит бар и возвращает id для POST dismiss. */
export function dismissActiveAutodialOffer() {
  const o = activeOffer || lastOffer
  if (o) markAutodialDismissed(o.call_id, o.deal_id)
  markAutodialUserDeclined(45_000)
  activeOffer = null
  lastOffer = null
  promptReady = false
  notifyOfferListeners()
  return o
}

function digits(raw: string) {
  return String(raw || '').replace(/\D/g, '')
}

function normalizeRuPhone(raw: string) {
  let d = digits(raw)
  if (d.length === 11 && d.startsWith('8')) d = `7${d.slice(1)}`
  return d
}

export function rememberOurExolveDids(numbers: string[]) {
  for (const n of numbers) {
    const d = normalizeRuPhone(n)
    if (d.length >= 10) ourDids.add(d)
  }
}

export function rememberAutodialCli(cli?: string) {
  const d = normalizeRuPhone(cli || '')
  if (d.length >= 10) ourDids.add(d)
}

/** INVITE с From = наш городской DID (скорозвон), не реальный входящий. */
export function isOurExolveDid(peer?: string) {
  const d = normalizeRuPhone(peer || '')
  if (!d) return false
  if (ourDids.has(d)) return true
  if (d.length >= 10) {
    const tail = d.slice(-10)
    for (const x of ourDids) {
      if (x.endsWith(tail) || tail.endsWith(x.slice(-10))) return true
    }
  }
  return false
}

/** Только id — без продления userDeclined (иначе каждый новый dialing вечно блокирует Accept). */
export function markAutodialDismissed(...ids: Array<string | undefined>) {
  for (const id of ids) {
    const s = String(id || '').trim()
    if (s) dismissedIds.add(s)
  }
  persistDecline()
}

/** Снимок флагов для отладки бара после Отклонить. */
export function getAutodialDebugFlags() {
  return {
    userDeclined: isAutodialUserDeclined(),
    suppressing: isAutodialSuppressing(),
    awaitingOffer: isAutodialAwaitingOffer(),
    activeCallId: activeOffer?.call_id || null,
    activeDealId: activeOffer?.deal_id || null,
    lastCallId: lastOffer?.call_id || null,
    lastTitle: lastOffer?.title || null,
    dismissedCount: dismissedIds.size,
  }
}

export function isAutodialDismissed(...ids: Array<string | undefined>) {
  for (const id of ids) {
    const s = String(id || '').trim()
    if (s && dismissedIds.has(s)) return true
  }
  return false
}

export function clearAutodialUserDeclined() {
  // #region agent log
  fetch('http://127.0.0.1:7611/ingest/f0ae33db-4389-47d2-9582-575d02a2a49a',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'a5b483'},body:JSON.stringify({sessionId:'a5b483',runId:'post-fix8',hypothesisId:'Q',location:'autodialSuppress.ts:clear',message:'CLEARED userDeclined',data:{hadUntil:userDeclinedUntil||null},timestamp:Date.now()})}).catch(()=>{});
  // #endregion
  userDeclinedUntil = 0
  persistDecline()
}

export function markAutodialSuppress(ms = 120_000) {
  const until = Date.now() + ms
  if (until > suppressUntil) suppressUntil = until
}

export function markAutodialEnded() {
  awaitingOfferUntil = 0
  promptReady = false
  activeOffer = null
  lastOffer = null
  notifyOfferListeners()
  markAutodialSuppress(45_000)
}

export function markAutodialAwaitingOffer(ms = 180_000) {
  // Новый обзвон — снимаем прошлый «Отклонить», иначе INVITE снова глушится.
  clearAutodialUserDeclined()
  const until = Date.now() + ms
  if (until > awaitingOfferUntil) awaitingOfferUntil = until
  markAutodialSuppress(ms)
}

export function clearAutodialAwaitingOffer() {
  awaitingOfferUntil = 0
}

export function isAutodialAwaitingOffer() {
  return Date.now() < awaitingOfferUntil
}

export function markAutodialUserDeclined(ms = 45_000) {
  awaitingOfferUntil = 0
  const until = Date.now() + ms
  if (until > userDeclinedUntil) userDeclinedUntil = until
  markAutodialSuppress(ms)
  persistDecline()
}

export function isAutodialUserDeclined() {
  return Date.now() < userDeclinedUntil
}

export function isAutodialSuppressing() {
  return Date.now() < suppressUntil
}

export function shouldAutodialDeclineInvite() {
  return isAutodialUserDeclined()
}

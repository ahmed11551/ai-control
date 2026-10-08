import geo from '../data/phoneGeo.json'

type GeoFile = {
  v: number
  regions: [string, number][]
  ranges: number[]
}

const data = geo as GeoFile

/** 10 цифр без кода страны (7/8). Маски и обрезки не принимаем — иначе утечка по DEF. */
export function nationalPhoneDigits(raw: string): string | null {
  if (!raw || /[•*xх]/i.test(raw)) return null
  const d = raw.replace(/\D/g, '')
  // только полный номер РФ: не угадываем по хвосту/префиксу
  if (d.length === 11 && (d[0] === '7' || d[0] === '8')) return d.slice(1)
  if (d.length === 10 && d[0] !== '7' && d[0] !== '8') return d
  return null
}

function lookupRange(n: number): [string, number] | null {
  const flat = data.ranges
  let lo = 0
  let hi = flat.length / 3 - 1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    const i = mid * 3
    const a = flat[i]
    const b = flat[i + 1]
    const ri = flat[i + 2]
    if (n < a) hi = mid - 1
    else if (n > b) lo = mid + 1
    else {
      const reg = data.regions[ri]
      return reg ? [reg[0], reg[1]] : null
    }
  }
  return null
}

export function lookupPhoneRegion(raw: string): { region: string; utcOffset: number } | null {
  const digits = nationalPhoneDigits(raw)
  if (!digits) return null
  const hit = lookupRange(Number(digits))
  if (!hit) return null
  return { region: hit[0], utcOffset: hit[1] }
}

export function mskOffsetLabel(utcOffset: number): string {
  const msk = utcOffset - 3
  if (msk === 0) return 'МСК'
  if (msk > 0) return `МСК+${msk}`
  return `МСК${msk}`
}

export type CallAdvice = 'ok' | 'caution' | 'avoid'

export type PhoneCallHint = {
  region: string
  utcOffset: number
  mskLabel: string
  localTime: string
  localHour: number
  advice: CallAdvice
  adviceText: string
}

function localParts(now: Date, utcOffset: number): { hour: number; minute: number } {
  const utcMs = now.getTime() + now.getTimezoneOffset() * 60_000
  const local = new Date(utcMs + utcOffset * 3_600_000)
  return { hour: local.getHours(), minute: local.getMinutes() }
}

export function callAdviceForHour(hour: number): { advice: CallAdvice; adviceText: string } {
  if (hour >= 9 && hour < 21) {
    return { advice: 'ok', adviceText: 'Сейчас удобно звонить' }
  }
  if ((hour >= 8 && hour < 9) || (hour >= 21 && hour < 22)) {
    return { advice: 'caution', adviceText: 'Пограничное время — лучше уточнить' }
  }
  if (hour < 8) {
    return { advice: 'avoid', adviceText: 'Рано у кандидата — лучше не звонить' }
  }
  return { advice: 'avoid', adviceText: 'Поздно у кандидата — лучше не звонить' }
}

export function phoneCallHint(raw: string, now = new Date()): PhoneCallHint | null {
  const found = lookupPhoneRegion(raw)
  if (!found) return null
  const { hour, minute } = localParts(now, found.utcOffset)
  const { advice, adviceText } = callAdviceForHour(hour)
  return {
    region: found.region,
    utcOffset: found.utcOffset,
    mskLabel: mskOffsetLabel(found.utcOffset),
    localTime: `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`,
    localHour: hour,
    advice,
    adviceText,
  }
}

export type DialTab = 'vox' | 'exolve'

/** City numbers stay on the tab the user opened. Never auto-switch Vox → Exolve. */
export function dialProvider(active: DialTab, _destination: string): DialTab {
  return active
}

export function isCityPhone(raw: string): boolean {
  const v = raw.trim()
  if (!v || v.includes('@') || /[a-zA-Z]/.test(v)) return false
  const digits = v.replace(/\D/g, '')
  return digits.length >= 10 && digits.length <= 15
}

/** Vox Web SDK: E.164 for city numbers, username as-is for agents. Spaces/dashes stripped. */
export function normalizeVoxDest(raw: string): string {
  const v = raw.trim()
  if (!v || v.includes('@') || /[a-zA-Z]/.test(v)) return v
  const digits = v.replace(/\D/g, '')
  if (digits.length === 11 && digits.startsWith('8')) return '+7' + digits.slice(1)
  if (digits.length === 11 && digits.startsWith('7')) return '+' + digits
  if (digits.length === 10) return '+7' + digits
  if (digits.length >= 8) return '+' + digits
  return v
}

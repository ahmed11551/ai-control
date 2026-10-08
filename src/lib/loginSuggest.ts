const CYR: Record<string, string> = {
  а: 'a', б: 'b', в: 'v', г: 'g', д: 'd', е: 'e', ё: 'e', ж: 'zh', з: 'z',
  и: 'i', й: 'y', к: 'k', л: 'l', м: 'm', н: 'n', о: 'o', п: 'p', р: 'r',
  с: 's', т: 't', у: 'u', ф: 'f', х: 'h', ц: 'ts', ч: 'ch', ш: 'sh', щ: 'sch',
  ъ: '', ы: 'y', ь: '', э: 'e', ю: 'yu', я: 'ya',
}

export function suggestLoginFromName(fullName: string): string {
  const parts = fullName.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (!parts.length) return ''
  const tr = (s: string) =>
    [...s]
      .map((ch) => {
        if (CYR[ch] !== undefined) return CYR[ch]
        if (/[a-z0-9]/.test(ch)) return ch
        if (ch === '.' || ch === '_' || ch === '-') return ch
        return ''
      })
      .join('')
      .replace(/\.+/g, '.')
      .replace(/^[._-]+|[._-]+$/g, '')
  let base = tr(parts[0]) || 'user'
  if (parts[1]) {
    const first = tr(parts[1])
    if (first) base = `${base}.${first[0]}`
  }
  return base.slice(0, 48)
}

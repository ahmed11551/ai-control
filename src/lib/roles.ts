/** Маскировка номера на клиенте (если API вдруг отдал полный). */
export function maskPhone(raw: string | undefined | null): string {
  const s = String(raw || '').trim()
  if (!s) return ''
  const d = s.replace(/\D/g, '')
  if (d.length < 4) return '••••'
  const tail = d.slice(-4)
  if (d.length >= 11 && (d[0] === '7' || d[0] === '8')) {
    return `7${'•'.repeat(Math.max(0, d.length - 5))}${tail}`
  }
  return `${'•'.repeat(Math.max(0, d.length - 4))}${tail}`
}

/** Маскирует номера внутри текста ошибок / note для менеджера. */
export function maskPhonesInText(raw: string | undefined | null): string {
  const s = String(raw || '')
  if (!s) return ''
  return s.replace(/(?:\+?\d[\d\s\-()]{8,}\d|\d{10,15})/g, (m) => maskPhone(m) || '••••')
}

export function isManagerRole(role?: string) {
  const r = String(role || '').toLowerCase()
  return r === 'manager' || r === 'sales'
}

/** Логист и ночной логист — один кабинет. Номера видит только ночной. */
export function isLogistRole(role?: string) {
  const r = String(role || '').toLowerCase()
  return r === 'logist' || r === 'night_logist'
}

export function isCustomerRole(role?: string) {
  return String(role || '').toLowerCase() === 'customer'
}

export function canSeePhones(user?: { role?: string; see_phones?: boolean } | null) {
  if (!user) return false
  if (typeof user.see_phones === 'boolean') return user.see_phones
  return String(user.role || '').toLowerCase() === 'admin'
}

export function canManageMaxAccounts(user?: { role?: string } | null) {
  return String(user?.role || '').toLowerCase() === 'admin'
}

export function roleLabel(role: string) {
  switch (String(role || '').toLowerCase()) {
    case 'admin':
      return 'Администратор'
    case 'rop':
      return 'РОП'
    case 'manager':
    case 'sales':
      return 'Менеджер'
    case 'logist':
      return 'Логист'
    case 'night_logist':
      return 'Ночной логист'
    case 'customer':
      return 'Заказчик'
    case 'support':
      return 'Поддержка'
    case 'marketing':
      return 'Маркетинг'
    default:
      return role || '—'
  }
}

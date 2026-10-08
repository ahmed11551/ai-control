export type CrmRow = Record<string, unknown>

export const LEAD_STATUSES = [
  { value: 'new', label: 'Новый' },
  { value: 'in_progress', label: 'В работе' },
  { value: 'qualified', label: 'Квалифицирован' },
  { value: 'rejected', label: 'Отказ' },
] as const

export const CRM_SOURCES = [
  { value: 'manual', label: 'Вручную' },
  { value: 'site', label: 'Сайт' },
  { value: 'phone', label: 'Телефон' },
  { value: 'vk_ads', label: 'VK Ads' },
  { value: 'avito', label: 'Avito' },
  { value: 'google', label: 'Google Forms' },
  { value: 'hh', label: 'HH' },
  { value: 'skorozvon', label: 'Скорозвон' },
  { value: 'referral', label: 'Рекомендация' },
  { value: 'other', label: 'Другое' },
] as const

export function cell(row: CrmRow, key: string) {
  const value = row[key]
  if (value == null || value === '') return '—'
  return String(value)
}

export function leadStatusLabel(status: string) {
  return LEAD_STATUSES.find((s) => s.value === status)?.label ?? status
}

export function sourceLabel(source: string) {
  return CRM_SOURCES.find((s) => s.value === source)?.label ?? source
}

export function money(amount: unknown, currency = 'RUB') {
  const n = typeof amount === 'number' ? amount : Number(String(amount ?? '').replace(',', '.'))
  if (!Number.isFinite(n)) return '—'
  return new Intl.NumberFormat('ru-RU', {
    style: 'currency',
    currency: currency || 'RUB',
    maximumFractionDigits: 0,
  }).format(n)
}

export function optionList(rows: CrmRow[], valueKey: string, labelKey: string) {
  return rows.map((row) => ({
    value: String(row[valueKey] ?? ''),
    label: String(row[labelKey] ?? row[valueKey] ?? ''),
  })).filter((o) => o.value)
}

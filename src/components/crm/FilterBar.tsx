import type { ReactNode } from 'react'
import { FilterChip } from '../ui/Badge'
import { SearchPill } from '../ui/SearchPill'

export function FilterBar({
  query,
  onQuery,
  placeholder = 'Фильтр + поиск',
  children,
}: {
  query: string
  onQuery: (value: string) => void
  placeholder?: string
  children?: ReactNode
}) {
  return (
    <div className="inline-flex items-center gap-2 min-h-8 rounded-b24-sm bg-white/90 border border-black/5 shadow-sm px-2.5">
      <SearchPill
        tone="light"
        placeholder={placeholder}
        value={query}
        onChange={(e) => onQuery(e.target.value)}
        className="!bg-transparent !border-0 !shadow-none !px-0 !h-7 !min-w-[140px] !rounded-none"
      />
      {children}
    </div>
  )
}

export function FilterToggle({
  active,
  label,
  onOn,
  onOff,
}: {
  active: boolean
  label: string
  onOn: () => void
  onOff: () => void
}) {
  if (active) {
    return <FilterChip onClear={onOff}>{label}</FilterChip>
  }
  return (
    <button
      type="button"
      onClick={onOn}
      className="text-[12px] text-b24-text hover:text-b24-text-dark whitespace-nowrap"
    >
      + {label}
    </button>
  )
}

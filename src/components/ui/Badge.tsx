import type { ReactNode } from 'react'

type BadgeProps = {
  children: ReactNode
  onClear?: () => void
}

export function FilterChip({ children, onClear }: BadgeProps) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-b24-sm bg-[#e8f9ff] text-[#1a8bb8] text-[12px] font-medium px-2 py-0.5 border border-[#c5ebf7]">
      {children}
      {onClear ? (
        <button
          type="button"
          onClick={onClear}
          aria-label="Убрать фильтр"
          className="text-[#1a8bb8]/70 hover:text-[#1a8bb8] text-[14px] leading-none"
        >
          ×
        </button>
      ) : null}
    </span>
  )
}

export function CounterBadge({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1 text-[12px] text-b24-white-mute">
      {children}
    </span>
  )
}

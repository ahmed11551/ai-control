import { useMemo, useRef, useEffect } from 'react'

export type StageChevronItem = {
  id: string
  name: string
  short_name?: string
  is_won?: boolean
  is_lost?: boolean
}

type StageChevronBarProps = {
  stages: StageChevronItem[]
  currentId: string
  /** запасной матч, если id не совпал (имя / short_name) */
  currentLabel?: string
  disabled?: boolean
  pendingId?: string
  onSelect: (stageId: string) => void
  className?: string
}

function labelOf(s: StageChevronItem) {
  return (s.short_name || s.name || '').trim() || s.name
}

function norm(s: string) {
  return s
    .trim()
    .toLowerCase()
    .replace(/\u00a0/g, ' ')
    .replace(/\s+/g, ' ')
    .replace(/[.…]+$/g, '')
}

function idsEqual(a: string, b: string) {
  const x = a.trim().toLowerCase()
  const y = b.trim().toLowerCase()
  if (!x || !y) return false
  if (x === y) return true
  // uuid без дефисов
  const nx = x.replace(/-/g, '')
  const ny = y.replace(/-/g, '')
  return nx.length === 32 && ny.length === 32 && nx === ny
}

/**
 * Bitrix-style chevron stage strip: click → move deal in kanban.
 * Passed + current = cyan; future = grey; lost/won tinted.
 */
export function StageChevronBar({
  stages,
  currentId,
  currentLabel = '',
  disabled,
  pendingId,
  onSelect,
  className = '',
}: StageChevronBarProps) {
  const scroller = useRef<HTMLDivElement>(null)

  const currentIndex = useMemo(() => {
    if (!stages.length) return -1
    if (currentId) {
      const byId = stages.findIndex((s) => idsEqual(s.id, currentId))
      if (byId >= 0) return byId
    }
    const label = norm(currentLabel)
    if (!label) return -1
    const exact = stages.findIndex((s) => {
      const short = norm(s.short_name || '')
      const name = norm(s.name || '')
      return (short && short === label) || (name && name === label)
    })
    if (exact >= 0) return exact
    // мягкий матч: «Недозвон 2» ↔ «Недозвоны 2» / обрезки
    return stages.findIndex((s) => {
      const short = norm(s.short_name || '')
      const name = norm(s.name || '')
      return (
        (short && (short.startsWith(label) || label.startsWith(short))) ||
        (name && (name.startsWith(label) || label.startsWith(name)))
      )
    })
  }, [stages, currentId, currentLabel])

  const resolvedId = currentIndex >= 0 ? stages[currentIndex]?.id || currentId : currentId

  useEffect(() => {
    const root = scroller.current
    if (!root || currentIndex < 0) return
    const id = resolvedId.replace(/"/g, '')
    if (!id) return
    const el = root.querySelector<HTMLElement>(`[data-stage-id="${id}"]`)
    el?.scrollIntoView({ behavior: 'smooth', inline: 'center', block: 'nearest' })
  }, [resolvedId, currentIndex, stages.length])

  if (!stages.length) return null

  return (
    <div className={`relative bg-white border-b border-black/8 ${className}`}>
      <div
        ref={scroller}
        className="flex items-stretch gap-0 overflow-x-auto b24-scrollbar px-3 py-2.5 scroll-smooth [scrollbar-width:thin]"
        role="listbox"
        aria-label="Стадии воронки"
      >
        {stages.map((stage, index) => {
          const active = currentIndex >= 0 && index === currentIndex
          const passed = currentIndex >= 0 && index < currentIndex
          const busy = pendingId === stage.id
          const isLost = Boolean(stage.is_lost)
          const isWon = Boolean(stage.is_won)

          let fill = 'bg-[#e8ecf0] text-[#6a737c] hover:bg-[#dde3e8]'
          if (isLost && active) fill = 'bg-[#ff5752] text-white hover:bg-[#f04843]'
          else if (isWon && active) fill = 'bg-[#9dcf00] text-white hover:bg-[#8cbc00]'
          else if (active) fill = 'bg-[#2fc6f6] text-white shadow-[0_1px_0_rgba(0,0,0,0.06)]'
          else if (passed) fill = 'bg-[#a4e5f8] text-[#0b6e8a] hover:bg-[#8fdcf5]'

          const first = index === 0
          const clip = first
            ? 'polygon(0 0, calc(100% - 10px) 0, 100% 50%, calc(100% - 10px) 100%, 0 100%)'
            : 'polygon(0 0, calc(100% - 10px) 0, 100% 50%, calc(100% - 10px) 100%, 0 100%, 10px 50%)'

          return (
            <button
              key={stage.id}
              type="button"
              data-stage-id={stage.id}
              role="option"
              aria-selected={active}
              title={stage.name}
              disabled={disabled || busy || active}
              onClick={() => onSelect(stage.id)}
              className={[
                'relative shrink-0 h-9 min-w-[7.5rem] max-w-[11rem] px-4 text-[12px] font-semibold leading-tight',
                'transition-[filter,transform,background-color] duration-150',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#2fc6f6]/50 focus-visible:z-10',
                'disabled:cursor-default',
                !disabled && !active ? 'hover:brightness-[1.03] active:scale-[0.99] cursor-pointer' : '',
                fill,
                busy ? 'opacity-70' : '',
                first ? '' : '-ml-2',
              ].join(' ')}
              style={{
                clipPath: clip,
                WebkitClipPath: clip,
                zIndex: active ? 5 : stages.length - index,
                paddingLeft: first ? undefined : '1.15rem',
                paddingRight: '1.15rem',
              }}
            >
              <span className="block truncate text-center">{busy && !active ? '…' : labelOf(stage)}</span>
            </button>
          )
        })}
      </div>
      <div
        aria-hidden
        className="pointer-events-none absolute inset-y-0 left-0 w-4 bg-gradient-to-r from-white to-transparent"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-y-0 right-0 w-4 bg-gradient-to-l from-white to-transparent"
      />
    </div>
  )
}

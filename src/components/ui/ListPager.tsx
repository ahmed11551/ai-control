import { ChevronLeft, ChevronRight } from 'lucide-react'
import { pageRange } from '../../lib/page'

/** Размеры страницы. «Все» только если allowAll. */
const SIZES: { value: number; label: string }[] = [
  { value: 10, label: '10' },
  { value: 20, label: '20' },
  { value: 50, label: '50' },
]

const ALL_SIZE = { value: 0, label: 'Все' }

export function ListPager({
  total,
  limit,
  offset,
  onOffset,
  onLimit,
  allowAll = false,
}: {
  total: number
  limit: number
  offset: number
  onOffset: (next: number) => void
  onLimit?: (next: number) => void
  /** Показывать опцию «Все» (по умолчанию нет — тяжело на больших списках). */
  allowAll?: boolean
}) {
  const sizes = allowAll ? [...SIZES, ALL_SIZE] : SIZES
  const effective = limit > 0 ? limit : Math.max(total, 1)
  const canPrev = offset > 0
  const canNext = limit > 0 && offset + effective < total

  return (
    <div className="flex flex-wrap items-center gap-2 px-3 py-2.5 border-t border-black/6 bg-[#f8f9fb] text-[12px] text-b24-text-muted">
      <span className="mr-auto tabular-nums text-b24-text">{pageRange(total, offset, effective)}</span>
      {onLimit ? (
        <div className="inline-flex items-center gap-1 rounded-[8px] bg-white border border-black/8 p-0.5">
          {sizes.map((size) => {
            const active = limit === size.value
            return (
              <button
                key={size.label}
                type="button"
                onClick={() => onLimit(size.value)}
                className={[
                  'h-7 min-w-[2rem] px-2 rounded-[6px] text-[12px] font-medium transition',
                  active
                    ? 'bg-b24-text-dark text-white'
                    : 'text-b24-text hover:bg-[#f3f5f7]',
                ].join(' ')}
              >
                {size.label}
              </button>
            )
          })}
        </div>
      ) : null}
      <button
        type="button"
        className="size-7 inline-flex items-center justify-center rounded-b24-sm border border-black/8 bg-white disabled:opacity-40"
        disabled={!canPrev}
        aria-label="Предыдущая страница"
        onClick={() => onOffset(Math.max(0, offset - effective))}
      >
        <ChevronLeft size={16} />
      </button>
      <button
        type="button"
        className="size-7 inline-flex items-center justify-center rounded-b24-sm border border-black/8 bg-white disabled:opacity-40"
        disabled={!canNext}
        aria-label="Следующая страница"
        onClick={() => onOffset(offset + effective)}
      >
        <ChevronRight size={16} />
      </button>
    </div>
  )
}

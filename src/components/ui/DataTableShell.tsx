import { Settings2 } from 'lucide-react'
import type { ReactNode } from 'react'

type DataTableShellProps = {
  columns: string[]
  emptyTitle: string
  emptyDescription: string
  children?: ReactNode
}

export function DataTableShell({
  columns,
  emptyTitle,
  emptyDescription,
  children,
}: DataTableShellProps) {
  return (
    <div className="b24-glass-panel flex flex-col min-h-[420px] overflow-hidden">
      <div className="overflow-x-auto b24-scrollbar">
        <div className="grid grid-cols-[40px_36px_minmax(180px,1.6fr)_repeat(6,minmax(100px,1fr))] gap-0 border-b border-b24-border bg-b24-panel-header text-[12px] text-b24-text-muted font-medium min-w-[900px]">
          <div className="flex items-center justify-center py-2.5">
            <input
              type="checkbox"
              className="size-3.5 rounded border-gray-300 accent-b24-cyan"
              aria-label="Выбрать все"
            />
          </div>
          <div className="flex items-center justify-center py-2.5 text-b24-text-muted">
            <Settings2 size={14} strokeWidth={1.75} />
          </div>
          {columns.map((col, index) => (
            <div
              key={col}
              className={[
                'flex items-center py-2.5 px-2 truncate',
                index === 1 ? 'gap-1' : '',
              ].join(' ')}
            >
              <span className="truncate">{col}</span>
              {index === 1 ? (
                <span className="text-[10px] opacity-60" aria-hidden>
                  ↓
                </span>
              ) : null}
            </div>
          ))}
        </div>
      </div>

      <div className="flex-1 flex flex-col items-center justify-center px-8 py-16 text-center">
        {children ?? (
          <>
            <h3 className="text-[28px] font-normal text-[#a0a5ab] tracking-tight mb-2">
              {emptyTitle}
            </h3>
            <p className="max-w-[420px] text-[14px] leading-relaxed text-[#b0b5bb]">
              {emptyDescription}
            </p>
          </>
        )}
      </div>
    </div>
  )
}

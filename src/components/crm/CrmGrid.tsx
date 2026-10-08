import { Settings2 } from 'lucide-react'
import { type CrmRow, cell } from '../../lib/crm'
import { ListPager } from '../ui/ListPager'

export type GridColumn = {
  key: string
  label: string
  sort?: string
  primary?: boolean
}

export function formatGridDate(value: unknown) {
  const raw = String(value ?? '')
  if (!raw) return '—'
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return cell({ v: value }, 'v')
  return date.toLocaleDateString('ru-RU')
}

export function CrmGrid({
  columns,
  rows,
  total,
  limit,
  offset,
  sort,
  dir,
  selectedIds,
  empty,
  onSort,
  onOffset,
  onLimit,
  onOpen,
  onToggle,
  onToggleAll,
  formatCell,
}: {
  columns: GridColumn[]
  rows: CrmRow[]
  total: number
  limit: number
  offset: number
  sort: string
  dir: 'ASC' | 'DESC'
  selectedIds: Set<string>
  empty: string
  onSort: (key: string) => void
  onOffset: (next: number) => void
  onLimit: (next: number) => void
  onOpen: (id: string) => void
  onToggle: (id: string) => void
  onToggleAll: () => void
  formatCell: (key: string, row: CrmRow) => string
}) {
  const allSelected = rows.length > 0 && rows.every((row) => selectedIds.has(String(row.id)))

  return (
    <div className="b24-glass-panel overflow-hidden min-h-[420px] flex flex-col">
      <div className="overflow-x-auto b24-scrollbar flex-1">
        <table className="w-full min-w-[860px] text-left text-[13px]">
          <thead className="bg-b24-panel-header text-[12px] text-b24-text-muted sticky top-0 z-10">
            <tr>
              <th className="w-10 px-2 py-2.5">
                <input
                  type="checkbox"
                  className="size-3.5 rounded border-gray-300 accent-b24-cyan"
                  aria-label="Выбрать все"
                  checked={allSelected}
                  onChange={onToggleAll}
                />
              </th>
              <th className="w-9 px-1 py-2.5 text-center">
                <Settings2 size={14} strokeWidth={1.75} className="inline opacity-70" />
              </th>
              {columns.map((col) => {
                const active = sort === (col.sort ?? col.key)
                return (
                  <th key={col.key} className="px-3 py-2.5 font-medium">
                    {col.sort ? (
                      <button
                        type="button"
                        className="inline-flex items-center gap-1 hover:text-b24-text-dark"
                        onClick={() => onSort(col.sort ?? col.key)}
                      >
                        {col.label}
                        <span className="text-[10px] opacity-60">{active ? (dir === 'ASC' ? '↑' : '↓') : ''}</span>
                      </button>
                    ) : (
                      col.label
                    )}
                  </th>
                )
              })}
            </tr>
          </thead>
          <tbody className="divide-y divide-b24-border text-b24-text-dark">
            {rows.length === 0 ? (
              <tr>
                <td colSpan={columns.length + 2} className="px-3 py-16 text-center text-b24-text-muted">
                  {empty}
                </td>
              </tr>
            ) : (
              rows.map((row) => {
                const id = String(row.id)
                const selected = selectedIds.has(id)
                return (
                  <tr
                    key={id}
                    className={['cursor-pointer', selected ? 'bg-[#eef8fc]' : 'hover:bg-[#f5f8fa]'].join(' ')}
                    onClick={() => onOpen(id)}
                  >
                    <td className="px-2 py-2.5" onClick={(e) => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        className="size-3.5 rounded border-gray-300 accent-b24-cyan"
                        aria-label="Выбрать"
                        checked={selected}
                        onChange={() => onToggle(id)}
                      />
                    </td>
                    <td className="px-1" />
                    {columns.map((col) => (
                      <td key={col.key} className="px-3 py-2.5 truncate max-w-[260px]">
                        {col.primary ? (
                          <span className="text-[#2067b0] hover:underline">{formatCell(col.key, row)}</span>
                        ) : (
                          formatCell(col.key, row)
                        )}
                      </td>
                    ))}
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>
      <ListPager total={total} limit={limit} offset={offset} onOffset={onOffset} onLimit={onLimit} />
    </div>
  )
}

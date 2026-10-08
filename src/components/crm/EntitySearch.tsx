import { useEffect, useRef, useState } from 'react'
import { api } from '../../lib/api'
import { type CrmRow } from '../../lib/crm'
import { type Page, qs } from '../../lib/page'

export function EntitySearch({
  path,
  titleKey,
  value,
  label,
  disabled,
  placeholder = 'Начните вводить…',
  onChange,
}: {
  path: string
  titleKey: string
  value: string
  label?: string
  disabled?: boolean
  placeholder?: string
  onChange: (id: string, title: string) => void
}) {
  const [q, setQ] = useState('')
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<{ id: string; title: string }[]>([])
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const t = window.setTimeout(() => {
      void api<Page<CrmRow>>(`${path}${qs({ q: q.trim(), limit: 20 })}`)
        .then((page) => {
          setItems(
            page.items.map((row) => ({
              id: String(row.id ?? ''),
              title: String(row[titleKey] ?? row.full_name ?? row.name ?? row.title ?? row.id ?? ''),
            })),
          )
        })
        .catch(() => setItems([]))
    }, 220)
    return () => window.clearTimeout(t)
  }, [open, q, path, titleKey])

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  const shown = open ? q : label || ''

  return (
    <div ref={box} className="relative">
      <input
        type="search"
        disabled={disabled}
        value={shown}
        placeholder={placeholder}
        onFocus={() => {
          setOpen(true)
          setQ('')
        }}
        onChange={(e) => {
          setOpen(true)
          setQ(e.target.value)
          if (!e.target.value && value) onChange('', '')
        }}
        className="h-10 w-full rounded-b24-sm border border-black/10 px-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan focus:ring-2 focus:ring-b24-cyan/20 bg-white"
      />
      {open ? (
        <div className="absolute z-20 left-0 right-0 mt-1 max-h-56 overflow-auto rounded-b24-sm border border-black/8 bg-white shadow-lg">
          {items.length === 0 ? (
            <div className="px-3 py-2 text-[12px] text-b24-text-muted">Ничего не найдено</div>
          ) : (
            items.map((item) => (
              <button
                key={item.id}
                type="button"
                className="w-full text-left px-3 py-2 text-[13px] text-b24-text-dark hover:bg-[#f5f8fa]"
                onClick={() => {
                  onChange(item.id, item.title)
                  setQ('')
                  setOpen(false)
                }}
              >
                {item.title}
              </button>
            ))
          )}
        </div>
      ) : null}
    </div>
  )
}

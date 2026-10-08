type Tab = {
  id: string
  label: string
}

type TabsProps = {
  items: Tab[]
  activeId: string
  onChange?: (id: string) => void
  tone?: 'dark' | 'light'
}

export function Tabs({ items, activeId, onChange, tone = 'dark' }: TabsProps) {
  const isDark = tone === 'dark'

  return (
    <div className="flex items-center gap-0.5 flex-wrap">
      {items.map((item) => {
        const active = item.id === activeId
        return (
          <button
            key={item.id}
            type="button"
            onClick={() => onChange?.(item.id)}
            className={[
              'px-3 py-1.5 text-[13px] transition-colors relative',
              isDark
                ? active
                  ? 'text-white font-medium after:absolute after:left-2 after:right-2 after:bottom-0 after:h-[2px] after:rounded-full after:bg-white/85'
                  : 'text-b24-white-soft hover:text-white'
                : active
                  ? 'bg-[#eef2f4] text-b24-text-dark font-medium rounded-b24-sm'
                  : 'text-b24-text hover:bg-[#f5f7f8] rounded-b24-sm',
            ].join(' ')}
          >
            {item.label}
          </button>
        )
      })}
    </div>
  )
}

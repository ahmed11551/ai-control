import { X } from 'lucide-react'
import { useEffect, type ReactNode } from 'react'

type CrmDrawerProps = {
  title: string
  subtitle?: string
  badge?: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
}

export function CrmDrawer({ title, subtitle, badge, onClose, children, footer }: CrmDrawerProps) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="fixed inset-0 z-40 flex justify-end">
      <button
        type="button"
        className="absolute inset-0 bg-black/40"
        aria-label="Закрыть"
        onClick={onClose}
      />
      <aside className="relative z-10 h-full w-full max-w-[440px] bg-white shadow-2xl flex flex-col">
        <div className="flex items-start justify-between gap-3 px-5 py-4 border-b border-black/6">
          <div className="min-w-0">
            {subtitle ? (
              <div className="text-[11px] uppercase tracking-wide text-b24-text-muted mb-1">{subtitle}</div>
            ) : null}
            <h2 className="text-[18px] font-semibold text-b24-text-dark leading-snug break-words">{title}</h2>
            {badge ? (
              <span className="mt-2 inline-flex items-center rounded-b24-pill px-2.5 py-1 text-[12px] font-medium bg-sky-50 text-sky-700">
                {badge}
              </span>
            ) : null}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="size-8 shrink-0 inline-flex items-center justify-center rounded-full text-b24-text-muted hover:bg-black/5"
            aria-label="Закрыть"
          >
            <X size={18} strokeWidth={1.75} />
          </button>
        </div>
        <div className="flex-1 overflow-y-auto b24-scrollbar px-5 py-4 flex flex-col gap-4">{children}</div>
        {footer ? (
          <div className="px-5 py-3.5 border-t border-black/6 bg-[#f8f9fb] flex flex-col gap-2">{footer}</div>
        ) : null}
      </aside>
    </div>
  )
}

export function CrmFieldRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[minmax(108px,38%)_1fr] gap-3 items-start text-[13px]">
      <div className="text-b24-text-muted pt-0.5">{label}</div>
      <div className="text-b24-text-dark min-w-0 break-words">{children}</div>
    </div>
  )
}

export function CrmRelated({
  title,
  items,
  empty,
  onOpen,
}: {
  title: string
  items: { id: string; label: string; extra?: string }[]
  empty: string
  onOpen: (id: string) => void
}) {
  return (
    <section>
      <h3 className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted mb-2">{title}</h3>
      {items.length === 0 ? (
        <p className="text-[13px] text-b24-text-muted">{empty}</p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {items.map((item) => (
            <li key={item.id}>
              <button
                type="button"
                onClick={() => onOpen(item.id)}
                className="w-full text-left rounded-[10px] border border-black/6 px-3 py-2 hover:bg-[#f5f8fa]"
              >
                <div className="text-[13px] font-medium text-b24-text-dark">{item.label}</div>
                {item.extra ? <div className="text-[12px] text-b24-text-muted mt-0.5">{item.extra}</div> : null}
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

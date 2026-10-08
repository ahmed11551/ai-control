import type { ReactNode } from 'react'

/** Единый заголовок страницы (как у Адаптации / лидов). */
export function PageHeader({
  title,
  subtitle,
  actions,
}: {
  title: string
  subtitle?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-3 shrink-0">
      <div className="min-w-0">
        <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">{title}</h1>
        {subtitle ? (
          <p className="mt-1.5 text-[13px] text-white/65 leading-snug">{subtitle}</p>
        ) : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  )
}

/** Общая оболочка контента страницы. */
export function PageShell({
  children,
  className = '',
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <div className={`flex flex-col gap-4 pt-1 pb-4 min-h-[calc(100%-2rem)] ${className}`.trim()}>
      {children}
    </div>
  )
}

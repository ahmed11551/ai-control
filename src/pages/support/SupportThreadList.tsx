import { CheckCircle2, Search, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { roleLabel } from '../../lib/roles'
import { avatarColor, initials, listTime, type SupportThread } from './supportTypes'

export type ThreadTab = 'all' | 'unread' | 'resolved'

export function SupportAvatar({ name, seed, size = 40 }: { name?: string; seed?: string; size?: number }) {
  return (
    <span
      className="grid shrink-0 select-none place-items-center rounded-full font-semibold text-white"
      style={{ width: size, height: size, background: avatarColor(seed || name), fontSize: Math.round(size * 0.36) }}
      aria-hidden
    >
      {initials(name)}
    </span>
  )
}

export function threadSubtitle(t?: SupportThread | null) {
  if (!t) return ''
  return [t.department, t.role ? roleLabel(t.role) : ''].filter(Boolean).join(' · ')
}

export function SupportThreadList({
  threads,
  loading,
  selected,
  onOpen,
}: {
  threads: SupportThread[]
  loading: boolean
  selected: string
  onOpen: (id: string) => void
}) {
  const [query, setQuery] = useState('')
  const [tab, setTab] = useState<ThreadTab>('all')

  const counts = useMemo(
    () => ({
      all: threads.filter((t) => !t.resolved_at).length,
      unread: threads.filter((t) => (t.unread || 0) > 0).length,
      resolved: threads.filter((t) => t.resolved_at).length,
    }),
    [threads],
  )

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    return threads.filter((t) => {
      if (tab === 'all' && t.resolved_at) return false
      if (tab === 'unread' && !(t.unread || 0)) return false
      if (tab === 'resolved' && !t.resolved_at) return false
      if (!q) return true
      return [t.name, t.department, t.role ? roleLabel(t.role) : '', t.last_body]
        .filter(Boolean)
        .some((s) => String(s).toLowerCase().includes(q))
    })
  }, [threads, query, tab])

  const tabs: { id: ThreadTab; label: string; count: number }[] = [
    { id: 'all', label: 'Все', count: counts.all },
    { id: 'unread', label: 'Непрочитанные', count: counts.unread },
    { id: 'resolved', label: 'Решённые', count: counts.resolved },
  ]

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 space-y-2 border-b border-black/6 p-3">
        <label className="flex h-10 items-center gap-2 rounded-[12px] bg-black/[0.045] px-3 text-b24-text-muted focus-within:bg-white focus-within:ring-2 focus-within:ring-[#1f6feb]/30">
          <Search size={16} className="shrink-0" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Поиск по имени, отделу, тексту"
            className="min-w-0 flex-1 bg-transparent text-[14px] text-b24-text-dark outline-none placeholder:text-b24-text-muted"
          />
          {query ? (
            <button type="button" onClick={() => setQuery('')} aria-label="Очистить поиск" className="rounded-full p-0.5 hover:bg-black/5">
              <X size={14} />
            </button>
          ) : null}
        </label>
        <div className="flex gap-1 rounded-[12px] bg-black/[0.045] p-1" role="tablist">
          {tabs.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={tab === t.id}
              onClick={() => setTab(t.id)}
              className={[
                'flex flex-1 items-center justify-center gap-1 rounded-[9px] px-1.5 py-1.5 text-[12px] font-medium transition',
                tab === t.id ? 'bg-white text-b24-text-dark shadow-sm' : 'text-b24-text-muted hover:text-b24-text-dark',
              ].join(' ')}
            >
              <span className="truncate">{t.label}</span>
              {t.count > 0 ? (
                <span
                  className={[
                    'rounded-full px-1.5 text-[10px] leading-[16px]',
                    t.id === 'unread' ? 'bg-[#f04438] text-white' : 'bg-black/8 text-b24-text-dark',
                  ].join(' ')}
                >
                  {t.count}
                </span>
              ) : null}
            </button>
          ))}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        {loading && threads.length === 0 ? (
          <div className="space-y-1 p-2">
            {Array.from({ length: 7 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 rounded-[12px] px-2 py-2.5">
                <div className="size-10 animate-pulse rounded-full bg-black/8" />
                <div className="flex-1 space-y-2">
                  <div className="h-3 w-2/3 animate-pulse rounded bg-black/8" />
                  <div className="h-2.5 w-5/6 animate-pulse rounded bg-black/6" />
                </div>
              </div>
            ))}
          </div>
        ) : visible.length === 0 ? (
          <div className="px-6 py-12 text-center">
            <div className="text-[14px] font-medium text-b24-text-dark">
              {threads.length === 0
                ? 'Пока никто не писал'
                : query
                  ? 'Ничего не нашли'
                  : tab === 'unread'
                    ? 'Всё прочитано'
                    : tab === 'resolved'
                      ? 'Решённых пока нет'
                      : 'Все обращения решены'}
            </div>
            <div className="mt-1 text-[12px] text-b24-text-muted">
              {query ? 'Попробуйте другое имя или слово' : threads.length === 0 ? 'Сообщения сотрудников появятся здесь' : ''}
            </div>
          </div>
        ) : (
          <ul className="p-1.5">
            {visible.map((t) => {
              const unread = t.unread || 0
              const active = t.id === selected
              return (
                <li key={t.id}>
                  <button
                    type="button"
                    onClick={() => onOpen(t.id)}
                    className={[
                      'flex w-full items-center gap-3 rounded-[12px] px-2.5 py-2.5 text-left transition',
                      active ? 'bg-[#1f6feb]/10 ring-1 ring-[#1f6feb]/25' : 'hover:bg-black/[0.04]',
                    ].join(' ')}
                  >
                    <SupportAvatar name={t.name} seed={t.user_id} />
                    <span className="min-w-0 flex-1">
                      <span className="flex items-baseline gap-2">
                        <span
                          className={[
                            'min-w-0 flex-1 truncate text-[14px] text-b24-text-dark',
                            unread ? 'font-semibold' : 'font-medium',
                          ].join(' ')}
                        >
                          {t.name || 'Сотрудник'}
                        </span>
                        <span className={['shrink-0 text-[11px]', unread ? 'font-semibold text-[#1f6feb]' : 'text-b24-text-muted'].join(' ')}>
                          {listTime(t.last_at)}
                        </span>
                      </span>
                      {threadSubtitle(t) ? (
                        <span className="block truncate text-[11px] text-b24-text-muted">{threadSubtitle(t)}</span>
                      ) : null}
                      <span className="mt-0.5 flex items-center gap-2">
                        <span
                          className={[
                            'min-w-0 flex-1 truncate text-[12.5px]',
                            unread ? 'font-medium text-b24-text-dark' : 'text-b24-text-muted',
                          ].join(' ')}
                        >
                          {t.last_body || '—'}
                        </span>
                        {unread ? (
                          <span className="min-w-[20px] shrink-0 rounded-full bg-[#f04438] px-1.5 text-center text-[11px] font-semibold leading-[20px] text-white">
                            {unread > 99 ? '99+' : unread}
                          </span>
                        ) : t.resolved_at ? (
                          <CheckCircle2 size={15} className="shrink-0 text-[#1a7f37]" aria-label="Решено" />
                        ) : null}
                      </span>
                    </span>
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </div>
  )
}


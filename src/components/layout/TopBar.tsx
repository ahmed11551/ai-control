import { Phone } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { crmTabsForRole } from '../../data/nav'
import { formatDuration } from '../../telephony/callHistory'
import { softphoneIconLive } from '../../telephony/callPhase'
import { TELEPHONY_ENABLED } from '../../telephony/config'
import { api } from '../../lib/api'
import { useAuth } from '../../store/auth'
import { useTelephony } from '../../store/telephony'
import { SearchPill } from '../ui/SearchPill'
import { NotificationBell } from './NotificationBell'
import { MobileMenuButton } from './Sidebar'

type TopBarProps = {
  onOpenMobile: () => void
  module?: 'tasks' | 'crm' | null
}

type SearchHit = { id: string; title?: string; full_name?: string }
type SearchResult = {
  contacts: SearchHit[]
  companies: SearchHit[]
  deals: SearchHit[]
  leads: SearchHit[]
}

function formatClock(date: Date) {
  return date.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
}

export function TopBar({ onOpenMobile, module = null }: TopBarProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const { user } = useAuth()
  const { snap } = useTelephony()
  const [now, setNow] = useState(() => new Date())
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<SearchResult | null>(null)

  const phoneActive = TELEPHONY_ENABLED && softphoneIconLive(snap.status)

  useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), 15_000)
    return () => window.clearInterval(id)
  }, [])

  useEffect(() => {
    const q = query.trim()
    if (q.length < 2) {
      setHits(null)
      return
    }
    const handle = window.setTimeout(() => {
      api<SearchResult>(`/search?q=${encodeURIComponent(q)}`)
        .then(setHits)
        .catch(() => setHits(null))
    }, 250)
    return () => window.clearTimeout(handle)
  }, [query])

  const tabs = module === 'crm' ? crmTabsForRole(user?.role, { canSeeOldLeads: user?.can_see_old_leads }) : []
  const activeTab = useMemo(() => {
    if (module === 'crm') {
      if (location.pathname.startsWith('/crm/old-leads')) return 'old-leads'
      if (location.pathname.startsWith('/crm/leads')) return 'leads'
      return 'crm'
    }
    return tabs.find((t) => location.pathname.startsWith(t.path))?.id ?? tabs[0]?.id
  }, [location.pathname, module, tabs])

  const groups: { label: string; items: SearchHit[]; to: (id: string) => string }[] = [
    { label: 'Кандидаты', items: hits?.deals ?? [], to: (id) => `/crm?open=${id}` },
    { label: 'Лиды', items: hits?.leads ?? [], to: (id) => `/crm/leads?open=${id}` },
  ]

  return (
    <header className="shrink-0">
      <div className="flex items-center gap-3 px-4 lg:px-5 h-12">
        <MobileMenuButton onClick={onOpenMobile} />

        {tabs.length > 0 ? (
          <nav className="hidden md:flex items-center gap-0.5 min-w-0 flex-1 overflow-x-auto b24-scrollbar">
            {tabs.map((tab) => {
              const active = tab.id === activeTab
              return (
                <NavLink
                  key={tab.id}
                  to={tab.path}
                  end={tab.path === '/crm'}
                  className={[
                    'px-2.5 py-1.5 text-[13px] rounded-b24-sm whitespace-nowrap transition-colors',
                    active
                      ? 'text-white font-medium'
                      : 'text-b24-white-soft hover:text-white hover:bg-b24-glass',
                  ].join(' ')}
                >
                  {tab.label}
                </NavLink>
              )
            })}
          </nav>
        ) : (
          <div className="flex-1" />
        )}

        <div className="flex items-center gap-2 ml-auto shrink-0">
          <NotificationBell />
          <div className="relative hidden sm:block">
            <SearchPill
              tone="dark"
              placeholder="Поиск"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="w-[180px]"
            />
            {hits && query.trim().length >= 2 ? (
              <div className="absolute right-0 top-9 z-30 w-[280px] rounded-b24-sm bg-white shadow-lg border border-black/8 py-2 max-h-[360px] overflow-y-auto">
                {groups.every((g) => g.items.length === 0) ? (
                  <div className="px-3 py-4 text-[12px] text-b24-text-muted">Ничего не найдено</div>
                ) : (
                  groups.map((group) =>
                    group.items.length === 0 ? null : (
                      <div key={group.label} className="px-1 pb-1">
                        <div className="px-2 py-1 text-[11px] uppercase text-b24-text-muted">
                          {group.label}
                        </div>
                        {group.items.map((item) => (
                          <button
                            key={item.id}
                            type="button"
                            className="w-full text-left px-2 py-1.5 rounded text-[13px] text-b24-text-dark hover:bg-[#f5f8fa]"
                            onClick={() => {
                              navigate(group.to(item.id))
                              setQuery('')
                              setHits(null)
                            }}
                          >
                            {item.title || item.full_name || item.id}
                          </button>
                        ))}
                      </div>
                    ),
                  )
                )}
              </div>
            ) : null}
          </div>

          <Link
            to="/telephony"
            className={[
              'inline-flex items-center gap-1.5 rounded-b24-pill px-2.5 h-8 text-white shadow-[0_2px_10px_rgba(47,198,246,0.4)]',
              snap.status === 'ringing'
                ? 'bg-orange-500 animate-pulse'
                : snap.status === 'active'
                  ? 'bg-b24-green'
                  : snap.status === 'dialing' || snap.status === 'connecting'
                    ? 'bg-b24-cyan animate-pulse'
                    : phoneActive
                      ? 'bg-b24-cyan'
                      : 'bg-white/20 hover:bg-white/30',
            ].join(' ')}
            title={
              snap.status === 'dialing'
                ? 'Набор…'
                : snap.status === 'connecting'
                  ? 'Подключение…'
                  : snap.status === 'active'
                    ? 'Разговор'
                    : snap.status === 'ringing'
                      ? 'Входящий'
                      : 'Телефония'
            }
          >
            <Phone size={14} strokeWidth={2} />
            <span className="text-[13px] font-semibold tabular-nums tracking-wide">
              {snap.status === 'active'
                ? formatDuration(snap.durationSec)
                : snap.status === 'dialing' || snap.status === 'connecting'
                  ? '…'
                  : formatClock(now)}
            </span>
          </Link>
        </div>
      </div>
    </header>
  )
}

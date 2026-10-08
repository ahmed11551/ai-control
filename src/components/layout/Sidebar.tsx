import { ChevronDown, Menu, Settings, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { navForRole } from '../../data/nav'
import { api, wsURL } from '../../lib/api'
import { isLogistRole } from '../../lib/roles'
import { useAuth } from '../../store/auth'
import { BitrixLogo } from './BitrixLogo'
import { NOTIF_BADGE_EVENT } from './NotificationBell'

type SidebarProps = {
  mobileOpen: boolean
  onCloseMobile: () => void
}

export function Sidebar({ mobileOpen, onCloseMobile }: SidebarProps) {
  const { user, initials, logout } = useAuth()
  const location = useLocation()
  const role = String(user?.role || '').toLowerCase()
  const items = navForRole(role, {
    canSeeOldLeads: Boolean(user?.can_see_old_leads) || role === 'admin',
  })
  const [dialogUnread, setDialogUnread] = useState(0)
  const [supportUnread, setSupportUnread] = useState(0)
  const isLogist = isLogistRole(user?.role)
  const canSupport = role === 'admin' || role === 'rop' || role === 'manager' || role === 'sales'
  const showSupportBanner = role === 'rop' || role === 'manager' || role === 'sales'

  // Бейдж диалогов — из NotificationBell (один WS/poll на всё приложение).
  useEffect(() => {
    if (!isLogist) {
      setDialogUnread(0)
      return
    }
    const onBadge = (ev: Event) => {
      const detail = (ev as CustomEvent<{ dialogUnread?: number }>).detail
      setDialogUnread(Number(detail?.dialogUnread) || 0)
    }
    window.addEventListener(NOTIF_BADGE_EVENT, onBadge)
    return () => window.removeEventListener(NOTIF_BADGE_EVENT, onBadge)
  }, [isLogist])

  useEffect(() => {
    if (location.pathname.startsWith('/dialogs')) setDialogUnread(0)
  }, [location.pathname])

  useEffect(() => {
    if (!canSupport) {
      setSupportUnread(0)
      return
    }
    let closed = false
    let ws: WebSocket | null = null
    const load = () => {
      void api<{ unread?: number }>('/support/summary')
        .then((res) => {
          if (!closed) setSupportUnread(Number(res.unread) || 0)
        })
        .catch(() => {})
    }
    load()
    const connect = () => {
      if (closed) return
      ws = new WebSocket(wsURL())
      ws.onmessage = (ev) => {
        try {
          const msg = JSON.parse(String(ev.data)) as { type?: string }
          if (msg.type === 'support.updated') load()
        } catch {
          /* ignore malformed frames */
        }
      }
      ws.onclose = () => {
        if (!closed) window.setTimeout(connect, 2000)
      }
    }
    connect()
    return () => {
      closed = true
      ws?.close()
    }
  }, [canSupport])

  return (
    <>
      {mobileOpen ? (
        <button
          type="button"
          aria-label="Закрыть меню"
          className="fixed inset-0 z-30 bg-black/45 lg:hidden"
          onClick={onCloseMobile}
        />
      ) : null}

      <aside
        className={[
          'fixed lg:static inset-y-0 left-0 z-40 flex flex-col',
          'w-[var(--sidebar-width)] shrink-0',
          'bg-b24-sidebar backdrop-blur-xl border-r border-white/5',
          'transition-transform duration-200 ease-out',
          mobileOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0',
        ].join(' ')}
      >
        <div className="flex items-center justify-between px-4 h-14 shrink-0">
          <BitrixLogo />
          <button
            type="button"
            className="lg:hidden text-b24-white-soft p-1"
            onClick={onCloseMobile}
            aria-label="Закрыть"
          >
            <X size={18} />
          </button>
        </div>

        {showSupportBanner ? (
          <NavLink
            to="/support"
            onClick={onCloseMobile}
            className="mx-3 mb-3 block rounded-[14px] bg-[#ffe14a] px-3 py-3 text-[#1b1f23] shadow-[0_8px_24px_rgba(0,0,0,0.25)] hover:bg-[#ffd000]"
          >
            <div className="text-[18px] font-bold leading-tight">Нашли ошибку?</div>
            <div className="mt-1 text-[14px] font-medium leading-snug">Напишите сюда. Текст, фото или файл.</div>
            <div className="mt-2.5 flex h-10 w-full items-center justify-center rounded-[10px] bg-[#1f6feb] text-[15px] font-semibold text-white">
              Написать
            </div>
          </NavLink>
        ) : null}

        <nav className="flex-1 overflow-y-auto b24-scrollbar px-2 pb-3">
          <ul className="flex flex-col gap-0.5">
            {items.map((item) => {
              const Icon = item.icon
              const active =
                item.path === '/crm'
                  ? location.pathname.startsWith('/crm') &&
                    !location.pathname.startsWith('/crm/old-leads')
                  : location.pathname === item.path || location.pathname.startsWith(`${item.path}/`)
              const badge = item.id === 'dialogs' ? dialogUnread : item.id === 'support' ? supportUnread : 0
              return (
                <li key={item.id}>
                  <NavLink
                    to={item.path}
                    onClick={onCloseMobile}
                    className={[
                      'group flex items-center gap-2.5 rounded-b24-sm px-2.5 py-[7px] text-[13.5px]',
                      'transition-colors',
                      active
                        ? 'bg-b24-sidebar-active text-white font-medium'
                        : 'text-b24-white-soft hover:bg-b24-sidebar-hover hover:text-white',
                    ].join(' ')}
                  >
                    <Icon size={18} strokeWidth={1.6} className="shrink-0 opacity-90" />
                    <span className="flex-1 truncate">{item.label}</span>
                    {badge > 0 ? (
                      <span className="min-w-[18px] h-[18px] px-1 rounded-full bg-[#f04438] text-[10px] font-semibold text-white leading-[18px] text-center">
                        {badge > 99 ? '99+' : badge}
                      </span>
                    ) : null}
                    {item.expandable ? (
                      <ChevronDown
                        size={14}
                        strokeWidth={1.75}
                        className="opacity-50 group-hover:opacity-80"
                      />
                    ) : null}
                  </NavLink>
                </li>
              )
            })}
          </ul>
        </nav>

        <div className="shrink-0 px-3 pb-4 pt-2 flex flex-col gap-2.5 border-t border-white/5">
          <div className="flex items-center justify-between">
            <NavLink
              to="/settings"
              onClick={onCloseMobile}
              className="inline-flex items-center justify-center text-b24-white-mute hover:text-white p-1.5 rounded-full hover:bg-b24-glass transition-colors"
              aria-label="Настройки"
            >
              <Settings size={18} strokeWidth={1.6} />
            </NavLink>
            <button
              type="button"
              onClick={logout}
              className="text-[11px] text-b24-white-mute hover:text-white px-2 py-1"
            >
              Выйти
            </button>
          </div>

          <div className="flex items-center gap-2.5 px-1 pt-1">
            <div
              className="size-8 rounded-full bg-[linear-gradient(135deg,#5b8def,#2fc6f6)] flex items-center justify-center text-[12px] font-semibold text-white shadow-sm"
              aria-hidden
            >
              {initials}
            </div>
            <div className="min-w-0">
              <div className="text-[12.5px] text-white truncate font-medium">
                {user?.full_name || 'Пользователь'}
              </div>
              <div className="text-[11px] text-b24-white-mute truncate">
                {user?.login || user?.email || 'forgecrm.local'}
              </div>
            </div>
          </div>
        </div>
      </aside>
    </>
  )
}

export function MobileMenuButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="lg:hidden inline-flex items-center justify-center size-8 rounded-full text-white hover:bg-b24-glass"
      aria-label="Открыть меню"
    >
      <Menu size={18} strokeWidth={1.75} />
    </button>
  )
}

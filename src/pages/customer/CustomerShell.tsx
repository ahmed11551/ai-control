import { Menu, X } from 'lucide-react'
import { useState } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import { BitrixLogo } from '../../components/layout/BitrixLogo'
import { useAuth } from '../../store/auth'

export function CustomerShell() {
  const { user, initials, logout } = useAuth()
  const [mobileOpen, setMobileOpen] = useState(false)
  const city = user?.customer_site || user?.full_name || 'Заказчик'

  return (
    <div className="relative h-full min-h-0 overflow-hidden">
      <div className="b24-theme-bg" aria-hidden />
      <div className="relative z-10 flex h-full min-h-0">
        {mobileOpen ? (
          <button
            type="button"
            aria-label="Закрыть меню"
            className="fixed inset-0 z-30 bg-black/45 lg:hidden"
            onClick={() => setMobileOpen(false)}
          />
        ) : null}
        <aside
          className={[
            'fixed lg:static inset-y-0 left-0 z-40 flex w-[var(--sidebar-width)] shrink-0 flex-col',
            'border-r border-white/5 bg-b24-sidebar backdrop-blur-xl',
            'transition-transform duration-200',
            mobileOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0',
          ].join(' ')}
        >
          <div className="flex h-14 items-center justify-between px-4">
            <BitrixLogo />
            <button type="button" className="p-1 text-white lg:hidden" onClick={() => setMobileOpen(false)} aria-label="Закрыть">
              <X size={18} />
            </button>
          </div>
          <nav className="flex-1 px-2">
            <NavLink
              to="/customer"
              onClick={() => setMobileOpen(false)}
              className="flex items-center rounded-b24-sm bg-b24-sidebar-active px-2.5 py-[7px] text-[13.5px] font-medium text-white"
            >
              Прибывающие
            </NavLink>
          </nav>
          <div className="border-t border-white/5 px-3 pb-4 pt-3">
            <button type="button" className="mb-3 text-[12px] text-b24-white-mute hover:text-white" onClick={logout}>
              Выйти
            </button>
            <div className="flex items-center gap-2.5">
              <div className="flex size-8 items-center justify-center rounded-full bg-[linear-gradient(135deg,#5b8def,#2fc6f6)] text-[12px] font-semibold text-white">
                {initials}
              </div>
              <div className="min-w-0">
                <div className="truncate text-[12.5px] font-medium text-white">{city}</div>
                <div className="truncate text-[11px] text-b24-white-mute">{user?.login || 'заказчик'}</div>
              </div>
            </div>
          </div>
        </aside>
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <header className="flex h-12 items-center gap-3 px-4 lg:px-5">
            <button
              type="button"
              className="inline-flex size-8 items-center justify-center rounded-full text-white hover:bg-b24-glass lg:hidden"
              onClick={() => setMobileOpen(true)}
              aria-label="Открыть меню"
            >
              <Menu size={18} />
            </button>
            <div className="truncate text-[14px] text-white/80">{city}</div>
          </header>
          <main className="b24-scrollbar min-h-0 flex-1 overflow-y-auto px-3 pb-6 sm:px-4 lg:px-5">
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  )
}

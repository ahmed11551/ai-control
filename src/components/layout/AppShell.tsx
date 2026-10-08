import { useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '../../store/auth'
import { FooterLinks } from './FooterLinks'
import { Sidebar } from './Sidebar'
import { TopBar } from './TopBar'
import { EntryReminder } from './EntryReminder'
import { AttentionGate } from '../tasks/AttentionGate'
import { AutodialOfferBar } from '../telephony/AutodialOfferBar'
import { IncomingCallBar } from '../telephony/IncomingCallBar'

const TASKS_MODULE_PATHS = [
  '/tasks',
  '/projects',
  '/flows',
  '/scrum',
  '/efficiency',
]

const CRM_MODULE_PATHS = ['/crm']

export function AppShell() {
  const [mobileOpen, setMobileOpen] = useState(false)
  const { user } = useAuth()
  const location = useLocation()
  const isTasksModule = TASKS_MODULE_PATHS.some((p) => location.pathname.startsWith(p))
  const isCrmModule = CRM_MODULE_PATHS.some((p) => location.pathname.startsWith(p))

  return (
    <div className="relative h-full min-h-0 overflow-hidden">
      <div className="b24-theme-bg" aria-hidden />

      <div className="relative z-10 flex h-full min-h-0">
        <Sidebar mobileOpen={mobileOpen} onCloseMobile={() => setMobileOpen(false)} />

        <div className="flex min-w-0 flex-1 flex-col min-h-0">
          <TopBar
            onOpenMobile={() => setMobileOpen(true)}
            module={isCrmModule ? 'crm' : isTasksModule ? 'tasks' : null}
          />

          <main className="flex-1 min-h-0 overflow-y-auto b24-scrollbar px-3 sm:px-4 lg:px-5 pb-3">
            {user?.session_restricted ? (
              <div className="mt-3 rounded-[12px] bg-amber-50 px-4 py-3 text-[13px] text-amber-950">
                Сессия ограничена: сообщения кандидатам недоступны. Можно писать РОПу и администратору в поддержке и внутренних диалогах.
              </div>
            ) : null}
            <Outlet />
            <FooterLinks />
          </main>
        </div>
      </div>
      <EntryReminder />
      <IncomingCallBar />
      <AutodialOfferBar />
      <AttentionGate />
    </div>
  )
}

import { useState } from 'react'
import { Button } from '../ui/Button'
import { useAuth } from '../../store/auth'

const STORAGE_KEY = 'entry_reminder_v1'

function Key({ children }: { children: string }) {
  return (
    <kbd className="inline-flex min-w-7 items-center justify-center rounded-md border border-black/15 bg-[#f4f6f8] px-1.5 py-0.5 text-[12px] font-semibold text-b24-text-dark shadow-[0_1px_0_rgba(0,0,0,0.12)]">
      {children}
    </kbd>
  )
}

export function EntryReminder() {
  const { user } = useAuth()
  const role = String(user?.role || '').toLowerCase()
  const [open, setOpen] = useState(() => {
    if (role === 'admin' || !user) return false
    try {
      return sessionStorage.getItem(STORAGE_KEY) !== '1'
    } catch {
      return true
    }
  })

  if (!open || !user || role === 'admin') return null

  const close = () => {
    try {
      sessionStorage.setItem(STORAGE_KEY, '1')
    } catch {
      /* ignore */
    }
    setOpen(false)
  }

  return (
    <div className="fixed inset-0 z-[300] flex items-center justify-center bg-black/45 p-4">
      <div className="w-full max-w-md rounded-[14px] bg-white p-5 shadow-xl">
        <h2 className="text-[18px] font-semibold text-b24-text-dark">Перед работой</h2>
        <ol className="mt-4 flex flex-col gap-4 text-[14px] leading-relaxed text-b24-text-dark">
          <li>
            <span className="font-semibold">1. Выключите VPN.</span>
            <p className="mt-0.5 text-[13px] text-b24-text-muted">
              Пока VPN включён, портал может не открыться или работать с ошибками.
            </p>
          </li>
          <li>
            <span className="font-semibold">2. Полностью обновите страницу.</span>
            <p className="mt-0.5 text-[13px] text-b24-text-muted">
              Обычное обновление не подходит. Нажмите сочетание клавиш:
            </p>
            <div className="mt-2 flex flex-col gap-2 text-[13px]">
              <p className="flex flex-wrap items-center gap-1.5">
                <span className="w-16 text-b24-text-muted">Mac</span>
                <Key>⌘</Key>
                <span>+</span>
                <Key>Shift</Key>
                <span>+</span>
                <Key>R</Key>
              </p>
              <p className="flex flex-wrap items-center gap-1.5">
                <span className="w-16 text-b24-text-muted">Windows</span>
                <Key>Ctrl</Key>
                <span>+</span>
                <Key>Shift</Key>
                <span>+</span>
                <Key>R</Key>
              </p>
            </div>
          </li>
        </ol>
        <div className="mt-5 flex justify-end">
          <Button type="button" variant="green" onClick={close}>
            Понятно
          </Button>
        </div>
      </div>
    </div>
  )
}

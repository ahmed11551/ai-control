import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../../lib/api'

type Dashboard = {
  open_leads: number
  open_deals: number
  pipeline_amount: number
  open_tasks: number
  calls_24h: number
}

const cards: { key: keyof Dashboard; label: string; to: string }[] = [
  { key: 'open_leads', label: 'Открытые лиды', to: '/crm/leads' },
  { key: 'open_deals', label: 'Сделки в работе', to: '/crm' },
  { key: 'pipeline_amount', label: 'Сумма воронки', to: '/crm' },
  { key: 'open_tasks', label: 'Открытые задачи', to: '/tasks' },
  { key: 'calls_24h', label: 'Звонки за 24 ч', to: '/calls' },
]

function formatValue(key: keyof Dashboard, value: number | undefined) {
  if (value == null) return '…'
  if (key === 'pipeline_amount') {
    return new Intl.NumberFormat('ru-RU', {
      style: 'currency',
      currency: 'RUB',
      maximumFractionDigits: 0,
    }).format(value)
  }
  return String(value)
}

export function DashboardPage() {
  const [data, setData] = useState<Dashboard | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    api<Dashboard>('/dashboard')
      .then(setData)
      .catch((e: Error) => setError(e.message))
  }, [])

  return (
    <div className="flex flex-col gap-4 pt-1">
      <h1 className="text-[26px] font-normal text-[#191919] tracking-tight">Сводка</h1>
      {error ? <p className="text-[13px] text-orange-200">{error}</p> : null}
      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-5 gap-3">
        {cards.map((card) => (
          <Link
            key={card.key}
            to={card.to}
            className="b24-glass-panel px-4 py-4 hover:brightness-[1.03] transition-[filter]"
          >
            <div className="text-[12px] text-b24-text-muted">{card.label}</div>
            <div className="mt-2 text-[26px] font-semibold text-b24-text-dark tabular-nums">
              {formatValue(card.key, data?.[card.key])}
            </div>
          </Link>
        ))}
      </div>
    </div>
  )
}

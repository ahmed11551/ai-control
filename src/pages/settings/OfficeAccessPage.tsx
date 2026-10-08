import { useEffect, useState } from 'react'
import { PageHeader, PageShell } from '../../components/layout/PageHeader'
import { Button } from '../../components/ui/Button'
import { api } from '../../lib/api'

type Item = { id?: string; cidr: string; note: string }

export function OfficeAccessPage() {
  const [enabled, setEnabled] = useState(false)
  const [items, setItems] = useState<Item[]>([])
  const [yourIP, setYourIP] = useState('')
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  const [pending, setPending] = useState(false)

  const load = async () => {
    const data = await api<{ enabled: boolean; items: Item[]; your_ip: string }>('/settings/office-access')
    setEnabled(Boolean(data.enabled))
    setItems((data.items || []).map((it) => ({ cidr: it.cidr || '', note: it.note || '' })))
    setYourIP(data.your_ip || '')
  }

  useEffect(() => {
    void load().catch((e: Error) => setError(e.message))
  }, [])

  const save = async () => {
    setPending(true)
    setError('')
    setMsg('')
    try {
      const data = await api<{ enabled: boolean; items: Item[]; your_ip: string }>('/settings/office-access', {
        method: 'PUT',
        body: JSON.stringify({
          enabled,
          items: items
            .map((it) => ({ cidr: it.cidr.trim(), note: it.note.trim() }))
            .filter((it) => it.cidr),
        }),
      })
      setEnabled(Boolean(data.enabled))
      setItems((data.items || []).map((it) => ({ cidr: it.cidr || '', note: it.note || '' })))
      setYourIP(data.your_ip || yourIP)
      setMsg('Сохранено')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'ошибка')
    } finally {
      setPending(false)
    }
  }

  return (
    <PageShell>
      <PageHeader
        title="Доступ по IP"
        subtitle="Сотрудники и РОП входят только с этих адресов. Администратор, логист, ночной логист и заказчик не ограничиваются."
      />
      <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
        <label className="flex items-center gap-3 text-[14px]">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          <span>Ограничение включено</span>
        </label>
        <p className="mt-2 text-[13px] text-b24-text-muted">
          Пока выключено или список пуст — вход как обычно, ничего не блокируется. Ваш текущий IP:{' '}
          <span className="font-medium text-b24-text-dark">{yourIP || '—'}</span>
        </p>
        <div className="mt-3">
          <Button
            type="button"
            variant="filter"
            className="rounded-b24-sm h-8"
            disabled={!yourIP || items.some((it) => it.cidr.trim() === yourIP)}
            onClick={() => setItems((list) => [...list, { cidr: yourIP, note: 'офис' }])}
          >
            Добавить мой IP
          </Button>
        </div>

        <div className="mt-4 flex flex-col gap-2">
          {items.map((it, i) => (
            <div key={i} className="flex flex-wrap gap-2">
              <input
                className="h-9 min-w-[180px] flex-1 rounded-b24-sm border border-black/10 px-2 text-[13px]"
                placeholder="203.0.113.10 или 203.0.113.0/24"
                value={it.cidr}
                onChange={(e) =>
                  setItems((list) => list.map((row, j) => (j === i ? { ...row, cidr: e.target.value } : row)))
                }
              />
              <input
                className="h-9 w-[160px] rounded-b24-sm border border-black/10 px-2 text-[13px]"
                placeholder="подпись"
                value={it.note}
                onChange={(e) =>
                  setItems((list) => list.map((row, j) => (j === i ? { ...row, note: e.target.value } : row)))
                }
              />
              <button
                type="button"
                className="h-9 px-2 text-[12px] text-red-600"
                onClick={() => setItems((list) => list.filter((_, j) => j !== i))}
              >
                убрать
              </button>
            </div>
          ))}
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-3">
          <Button
            type="button"
            variant="filter"
            className="rounded-b24-sm h-8"
            onClick={() => setItems((list) => [...list, { cidr: '', note: '' }])}
          >
            Добавить адрес
          </Button>
          <Button
            type="button"
            variant="green"
            className="rounded-b24-sm h-8"
            disabled={pending}
            onClick={() => void save()}
          >
            {pending ? 'Сохраняем…' : 'Сохранить'}
          </Button>
        </div>
        {error ? <p className="mt-3 text-[13px] text-red-600">{error}</p> : null}
        {msg ? <p className="mt-3 text-[13px] text-emerald-700">{msg}</p> : null}
      </section>
    </PageShell>
  )
}

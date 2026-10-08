import { useEffect, useMemo, useState } from 'react'
import { Button } from '../../components/ui/Button'
import { api } from '../../lib/api'
import { useAuth } from '../../store/auth'

type Count = { total?: number; imported?: number; error?: string }
type Job = {
  status: string
  phase?: string
  portal?: string
  error?: string
  counts?: Record<string, Count>
}

const ENTITIES: { key: string; label: string; heavy?: boolean; wave?: string }[] = [
  { key: 'users', label: 'Сотрудники' },
  { key: 'departments', label: 'Отделы' },
  { key: 'pipelines', label: 'Воронки и стадии' },
  { key: 'companies', label: 'Компании' },
  { key: 'contacts', label: 'Контакты', wave: '2c' },
  { key: 'leads', label: 'Лиды', wave: '2c' },
  { key: 'deals', label: 'Сделки', wave: '2c' },
  { key: 'tasks', label: 'Задачи', heavy: true },
  { key: 'fields', label: 'Поля карточек сделок', wave: '2' },
  { key: 'task_roles', label: 'Роли в задачах', wave: '2d' },
  { key: 'task_comments', label: 'Комментарии задач', wave: '2d' },
]

const FIRST_WAVE = new Set(['users', 'departments', 'pipelines', 'companies'])
const WAVE_USERS = new Set(['users'])
const WAVE_TWO = new Set(['fields'])
const WAVE_DELTA = new Set(['users', 'departments', 'pipelines', 'companies', 'contacts', 'leads', 'deals', 'fields'])
const WAVE_TASKS = new Set(['task_roles', 'task_comments'])

export function ImportPage() {
  const { user } = useAuth()
  const [webhook, setWebhook] = useState('')
  const [job, setJob] = useState<Job | null>(null)
  const [error, setError] = useState('')
  const [pending, setPending] = useState<'inv' | 'run' | null>(null)
  const [selected, setSelected] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(ENTITIES.map((e) => [e.key, WAVE_DELTA.has(e.key)])),
  )

  const loadStatus = () =>
    api<Job>('/import/bitrix/status')
      .then(setJob)
      .catch(() => undefined)

  useEffect(() => {
    void loadStatus()
  }, [])

  useEffect(() => {
    if (job?.status !== 'running') return
    const id = window.setInterval(() => void loadStatus(), 2000)
    return () => window.clearInterval(id)
  }, [job?.status])

  const running = job?.status === 'running' || pending === 'run'
  const chosen = ENTITIES.filter((e) => selected[e.key]).map((e) => e.key)
  const heavyOn = ENTITIES.some((e) => e.heavy && selected[e.key])

  const toggle = (key: string) => {
    setSelected((prev) => ({ ...prev, [key]: !prev[key] }))
  }

  const inventory = async () => {
    setError('')
    setPending('inv')
    try {
      const data = await api<Job>('/import/bitrix/inventory', {
        method: 'POST',
        body: JSON.stringify({ webhook: webhook.trim() }),
      })
      setJob(data)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось получить инвентарь')
    } finally {
      setPending(null)
    }
  }

  const run = async () => {
    if (chosen.length === 0) {
      setError('Отметьте хотя бы один раздел')
      return
    }
    setError('')
    setPending('run')
    try {
      await api('/import/bitrix/run', {
        method: 'POST',
        body: JSON.stringify({ webhook: webhook.trim(), entities: chosen }),
      })
      await loadStatus()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось запустить импорт')
    } finally {
      setPending(null)
    }
  }

  const rows = useMemo(() => {
    const counts = job?.counts ?? {}
    return ENTITIES.map((e) => ({ ...e, ...(counts[e.key] ?? {}) }))
  }, [job])

  if (user?.role !== 'admin') {
    return (
      <div className="flex flex-col gap-4 pt-1 pb-4 min-h-[calc(100%-2rem)]">
        <div className="min-w-0">
          <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">
            Импорт Bitrix24
          </h1>
          <p className="mt-1.5 text-[13px] text-white/65">
            Запуск импорта доступен только администратору.
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4 pt-1 pb-4 max-w-[860px] min-h-[calc(100%-2rem)]">
      <div className="min-w-0">
        <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">
          Импорт Bitrix24
        </h1>
        <p className="mt-1.5 text-[13px] text-white/65 leading-snug">
          Сейчас дельта: новые контакты, лиды и сделки с ID выше уже лежащих, затем поля карточек.
          Полный дамп 260 тысяч контактов и 328 тысяч сделок не повторяется.
        </p>
      </div>

      <div className="b24-glass-panel p-4 flex flex-col gap-3">
        <label className="flex flex-col gap-1.5">
          <span className="text-[12px] font-medium text-b24-text">Входящий вебхук</span>
          <input
            type="password"
            autoComplete="off"
            value={webhook}
            onChange={(e) => setWebhook(e.target.value)}
            placeholder="https://portal.bitrix24.ru/rest/1/xxxxx/"
            className="h-10 rounded-b24-sm border border-black/10 px-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan"
          />
        </label>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="filter"
            className="rounded-b24-sm"
            disabled={running}
            onClick={() => setSelected(Object.fromEntries(ENTITIES.map((e) => [e.key, FIRST_WAVE.has(e.key)])))}
          >
            Только первая волна
          </Button>
          <Button
            type="button"
            variant="filter"
            className="rounded-b24-sm"
            disabled={running}
            onClick={() => setSelected(Object.fromEntries(ENTITIES.map((e) => [e.key, WAVE_USERS.has(e.key)])))}
          >
            Только сотрудники
          </Button>
          <Button
            type="button"
            variant="green"
            className="rounded-b24-sm"
            disabled={running}
            onClick={() => setSelected(Object.fromEntries(ENTITIES.map((e) => [e.key, WAVE_DELTA.has(e.key)])))}
          >
            Дельта: новые карточки
          </Button>
          <Button
            type="button"
            variant="filter"
            className="rounded-b24-sm"
            disabled={running}
            onClick={() => setSelected(Object.fromEntries(ENTITIES.map((e) => [e.key, WAVE_TWO.has(e.key)])))}
          >
            Только поля карточек
          </Button>
          <Button
            type="button"
            variant="filter"
            className="rounded-b24-sm"
            disabled={running}
            onClick={() => setSelected(Object.fromEntries(ENTITIES.map((e) => [e.key, WAVE_TASKS.has(e.key)])))}
          >
            Роли в задачах
          </Button>
          <Button
            type="button"
            variant="filter"
            className="rounded-b24-sm"
            disabled={running}
            onClick={() => setSelected(Object.fromEntries(ENTITIES.map((e) => [e.key, false])))}
          >
            Снять все
          </Button>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="filter"
            className="rounded-b24-sm"
            disabled={running || pending !== null}
            onClick={() => void inventory()}
          >
            {pending === 'inv' ? 'Считаем…' : 'Инвентарь портала'}
          </Button>
          <Button
            type="button"
            variant="green"
            className="rounded-b24-sm"
            disabled={running || pending !== null || chosen.length === 0}
            onClick={() => void run()}
          >
            {running ? 'Импорт идёт…' : `Перенести выбранное (${chosen.length})`}
          </Button>
        </div>
        {job?.portal ? (
          <p className="text-[12px] text-b24-text-muted">Портал: {job.portal}</p>
        ) : null}
        {job?.phase && job.status === 'running' ? (
          <p className="text-[12px] text-b24-text">Сейчас: {job.phase}</p>
        ) : null}
        {heavyOn ? (
          <p className="text-[12px] text-orange-700">
            Выбраны тяжёлые разделы. На этом объёме перенос может занять часы или упасть. Снимите
            контакты, лиды, сделки и задачи.
          </p>
        ) : null}
        {error ? <p className="text-[13px] text-red-500">{error}</p> : null}
        {job?.error ? <p className="text-[13px] text-red-500">{job.error}</p> : null}
      </div>

      <div className="b24-glass-panel overflow-hidden">
        <table className="w-full text-left text-[13px]">
          <thead className="bg-b24-panel-header text-[12px] text-b24-text-muted">
            <tr>
              <th className="px-3 py-2.5 font-medium w-10" />
              <th className="px-3 py-2.5 font-medium">Раздел</th>
              <th className="px-3 py-2.5 font-medium">На портале</th>
              <th className="px-3 py-2.5 font-medium">У нас</th>
              <th className="px-3 py-2.5 font-medium">Статус</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-b24-border text-b24-text-dark">
            {rows.map((row) => (
              <tr key={row.key} className={row.heavy ? 'bg-orange-50/40' : undefined}>
                <td className="px-3 py-2.5">
                  <input
                    type="checkbox"
                    checked={Boolean(selected[row.key])}
                    disabled={running}
                    onChange={() => toggle(row.key)}
                    aria-label={row.label}
                  />
                </td>
                <td className="px-3 py-2.5">
                  {row.label}
                  {row.heavy ? <span className="ml-2 text-[11px] text-orange-700">полный дамп</span> : null}
                  {row.wave === '2c' ? (
                    <span className="ml-2 text-[11px] text-sky-800">только новые ID</span>
                  ) : null}
                  {row.wave === '2' ? (
                    <span className="ml-2 text-[11px] text-sky-800">значения в текущие сделки</span>
                  ) : null}
                  {row.wave === '2d' ? (
                    <span className="ml-2 text-[11px] text-sky-800">делаю / помогаю / поручил / наблюдаю</span>
                  ) : null}
                </td>
                <td className="px-3 py-2.5 tabular-nums">{row.total ?? '—'}</td>
                <td className="px-3 py-2.5 tabular-nums">{row.imported ?? 0}</td>
                <td className="px-3 py-2.5 text-b24-text-muted">{row.error || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { PageHeader, PageShell } from '../../components/layout/PageHeader'
import { api } from '../../lib/api'
import { incidentTypeLabel } from '../analytics/AnalyticsPage'

type Incident = {
  id: string
  created_at: string
  event_type: string
  rule_code: string
  severity: string
  status: string
  summary: string
  comment: string
  actor_id: string
  actor_name: string
  restricted: boolean
  excerpt: string
  channel: string
  detail_kind: string
  object_type: string
  object_id: string
  reviewed_by: string
  reviewer_name: string
  reviewed_at: string | null
}

type DlpRule = {
  id: string
  code: string
  kind: 'phrase' | 'domain'
  pattern: string
  action: 'block' | 'log'
  severity: 'low' | 'medium' | 'high'
  enabled: boolean
}

type DlpAudit = {
  id: string
  rule_id: string
  rule_code: string
  action: string
  reason: string
  actor_name: string
  created_at: string
}

type StaffRisk = { id: string; full_name: string; email: string; role: string; department: string; is_active: boolean }

type StaffSecuritySummary = {
  user_id: string
  restricted: boolean
  reason: string
  open: number
  confirmed: number
  recent: Array<{ id: string; created_at: string; event_type: string; summary: string; status: string; severity: string; rule_code: string }>
}

type SecurityFeedback = {
  confirmed: number
  dismissed: number
  by_rule: Array<{ rule_code: string; event_type: string; confirmed: number; dismissed: number; total: number }>
}

const maxRuleLabels: Record<string, string> = {
  personal_redirect: 'Личная ссылка или номер',
  personal_messenger: 'Личный мессенджер',
  candidate_phone_request: 'Запрос телефона кандидата',
  passport_request: 'Текстовый запрос паспорта',
  passport_share: 'Передача паспортных данных',
}

const statuses = [
  { id: 'open', label: 'Открытые' },
  { id: 'confirmed', label: 'Подтверждённые' },
  { id: 'dismissed', label: 'Отклонённые' },
  { id: 'noted', label: 'Журнал' },
  { id: 'all', label: 'Все' },
]

const types = [
  { id: 'all', label: 'Все типы' },
  { id: 'message_blocked', label: 'Сообщения' },
  { id: 'call_phrase', label: 'Звонки' },
  { id: 'sensitive_denied', label: 'Отказ в документе' },
  { id: 'sensitive_view', label: 'Просмотр документа' },
  { id: 'mass_view', label: 'Массовый просмотр' },
]

function when(raw: string) {
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function severityLabel(value: string) {
  if (value === 'high') return 'высокая'
  if (value === 'low') return 'низкая'
  return 'средняя'
}

function statusLabel(value: string) {
  if (value === 'confirmed') return 'подтверждён'
  if (value === 'dismissed') return 'отклонён'
  if (value === 'noted') return 'запись'
  return 'открыт'
}

function objectLabel(type: string) {
  if (type === 'deal_ticket_file') return 'документ сделки'
  if (type === 'call') return 'звонок'
  if (type === 'max_dialog') return 'диалог MAX'
  if (type === 'user') return 'сотрудник'
  return type || '—'
}

export function SecurityPage() {
  const [status, setStatus] = useState('open')
  const [type, setType] = useState('all')
  const [items, setItems] = useState<Incident[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [selected, setSelected] = useState('')
  const [comment, setComment] = useState('')
  const [pending, setPending] = useState(false)
  const [callId, setCallId] = useState('')
  const [transcript, setTranscript] = useState('')
  const [checkMsg, setCheckMsg] = useState('')
  const [rules, setRules] = useState<DlpRule[]>([])
  const [ruleAudit, setRuleAudit] = useState<DlpAudit[]>([])
  const [ruleForm, setRuleForm] = useState({ code: '', kind: 'phrase', pattern: '', action: 'block', severity: 'medium' })
  const [view, setView] = useState<'incidents' | 'max' | 'rules' | 'employees' | 'feedback' | 'analytics'>('incidents')
  const [staff, setStaff] = useState<StaffRisk[]>([])
  const [riskIncidents, setRiskIncidents] = useState<Incident[]>([])
  const [maxIncidents, setMaxIncidents] = useState<Incident[]>([])
  const [selectedStaffId, setSelectedStaffId] = useState('')
  const [staffSummary, setStaffSummary] = useState<StaffSecuritySummary | null>(null)
  const [feedback, setFeedback] = useState<SecurityFeedback | null>(null)

  const loadRules = () => Promise.all([
    api<{ items: DlpRule[] }>('/security/rules'),
    api<{ items: DlpAudit[] }>('/security/rules/audit'),
  ]).then(([rulesRes, auditRes]) => {
    setRules(Array.isArray(rulesRes.items) ? rulesRes.items : [])
    setRuleAudit(Array.isArray(auditRes.items) ? auditRes.items : [])
  })

  const load = () => {
    const params = new URLSearchParams({ status, type })
    setLoading(true)
    return api<{ items: Incident[] }>(`/security/incidents?${params.toString()}`)
      .then((res) => setItems(Array.isArray(res.items) ? res.items : []))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    setError('')
    void load()
  }, [status, type])

  useEffect(() => {
    void loadRules().catch((e: Error) => setError(e.message))
  }, [])

  useEffect(() => {
    if (view !== 'employees') return
    Promise.all([
      api<StaffRisk[]>('/users'),
      api<{ items: Incident[] }>('/security/incidents?status=all&type=all'),
    ]).then(([users, incidents]) => {
      setStaff(Array.isArray(users) ? users : [])
      setRiskIncidents(Array.isArray(incidents.items) ? incidents.items : [])
    }).catch((e: Error) => setError(e.message))
  }, [view])

  useEffect(() => {
    if (view !== 'feedback') return
    api<SecurityFeedback>('/security/feedback')
      .then(setFeedback)
      .catch((e: Error) => setError(e.message))
  }, [view])

  useEffect(() => {
    if (view !== 'employees' || !selectedStaffId) {
      setStaffSummary(null)
      return
    }
    api<StaffSecuritySummary>(`/security/summary?user_id=${encodeURIComponent(selectedStaffId)}`)
      .then(setStaffSummary)
      .catch((e: Error) => setError(e.message))
  }, [view, selectedStaffId])

  useEffect(() => {
    if (view !== 'max') return
    api<{ items: Incident[] }>('/security/incidents?status=all&type=message_blocked')
      .then((res) => setMaxIncidents(Array.isArray(res.items) ? res.items : []))
      .catch((e: Error) => setError(e.message))
  }, [view])

  const current = items.find((item) => item.id === selected) || null

  const review = async (next: 'confirmed' | 'dismissed' | 'open') => {
    if (!current) return
    setPending(true)
    setError('')
    try {
      await api(`/security/incidents/${current.id}`, {
        method: 'POST',
        body: JSON.stringify({ status: next, comment }),
      })
      setComment('')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось сохранить')
    } finally {
      setPending(false)
    }
  }

  const release = async (userId: string) => {
    setPending(true)
    setError('')
    try {
      await api(`/security/users/${userId}/release`, { method: 'POST' })
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось снять ограничение')
    } finally {
      setPending(false)
    }
  }

  const checkCall = async () => {
    const id = callId.trim()
    if (!id || !transcript.trim()) return
    setPending(true)
    setCheckMsg('')
    setError('')
    try {
      const res = await api<{ status: string; rule: string }>(`/security/calls/${id}/transcript`, {
        method: 'POST',
        body: JSON.stringify({ transcript }),
      })
      setCheckMsg(res.status === 'flagged' ? `Нарушение: ${res.rule || 'правило'}` : 'Запрещённых фраз нет')
      setTranscript('')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось проверить звонок')
    } finally {
      setPending(false)
    }
  }

  const addRule = async () => {
    if (!ruleForm.code.trim() || !ruleForm.pattern.trim()) return
    const reason = window.prompt('Основание добавления правила:')?.trim()
    if (!reason) return
    setPending(true)
    try {
      await api('/security/rules', { method: 'POST', body: JSON.stringify({ ...ruleForm, reason }) })
      setRuleForm({ code: '', kind: 'phrase', pattern: '', action: 'block', severity: 'medium' })
      await loadRules()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось создать правило')
    } finally {
      setPending(false)
    }
  }

  const toggleRule = async (rule: DlpRule) => {
    const reason = window.prompt(`${rule.enabled ? 'Основание выключения' : 'Основание включения'} правила:`)?.trim()
    if (!reason) return
    setPending(true)
    try {
      await api(`/security/rules/${rule.id}`, { method: 'PATCH', body: JSON.stringify({ enabled: !rule.enabled, reason }) })
      await loadRules()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось изменить правило')
    } finally {
      setPending(false)
    }
  }

  const deleteRule = async (id: string) => {
    const reason = window.prompt('Основание удаления правила:')?.trim()
    if (!reason) return
    setPending(true)
    try {
      await api(`/security/rules/${id}`, { method: 'DELETE', body: JSON.stringify({ reason }) })
      await loadRules()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось удалить правило')
    } finally {
      setPending(false)
    }
  }

  const approvePassportText = async (userId: string) => {
    const reason = window.prompt('Основание согласования текстового запроса паспорта:')?.trim()
    if (!reason) return
    setPending(true)
    try {
      await api(`/security/users/${userId}/passport-text-approval`, {
        method: 'POST',
        body: JSON.stringify({ reason, hours: 24 }),
      })
      setError('Разрешение выдано на 24 часа и записано в аудит')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось выдать разрешение')
    } finally {
      setPending(false)
    }
  }

  return (
    <PageShell>
      <PageHeader
        title="Безопасность"
        subtitle="Инциденты по сообщениям, звонкам и документам. Менеджер по-прежнему не видит паспорта и полные номера."
      />

      <nav className="flex flex-wrap gap-2 rounded-[14px] bg-white p-2 text-[13px] shadow-sm">
        {[
          ['incidents', 'Инциденты'],
          ['max', 'Контроль MAX'],
          ['rules', 'Правила DLP'],
          ['employees', 'Сотрудники и риск'],
          ['feedback', 'Обратная связь'],
          ['analytics', 'Аналитика'],
        ].map(([id, label]) => <button key={id} type="button" onClick={() => setView(id as typeof view)} className={`rounded-[10px] px-3 py-2 ${view === id ? 'bg-b24-blue text-white' : 'text-b24-text-muted hover:bg-b24-panel-header'}`}>{label}</button>)}
      </nav>

      {view === 'max' ? (() => {
        const counts = Object.keys(maxRuleLabels).map((rule) => ({ rule, count: maxIncidents.filter((item) => item.rule_code === rule).length }))
        const blocked = maxIncidents.filter((item) => item.status !== 'dismissed').length
        return <section className="space-y-3">
          <div className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
            <h2 className="text-[16px] font-semibold">Контроль переписки MAX</h2>
            <p className="mt-1 text-[13px] text-b24-text-muted">Исходящие сообщения менеджеров проверяются до отправки. Контроль не меняет CRM, распределение лидов или рабочие диалоги.</p>
            <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              <div className="rounded-[12px] border border-rose-100 bg-rose-50 p-3"><div className="text-[12px] text-rose-700">Активных нарушений</div><div className="mt-1 text-[24px] font-semibold text-rose-900">{blocked}</div></div>
              <div className="rounded-[12px] border border-emerald-100 bg-emerald-50 p-3"><div className="text-[12px] text-emerald-700">Паспортные файлы</div><div className="mt-1 text-[13px] font-medium text-emerald-900">Видны только администратору и логисту</div></div>
              <div className="rounded-[12px] border border-sky-100 bg-sky-50 p-3"><div className="text-[12px] text-sky-700">Телефоны кандидатов</div><div className="mt-1 text-[13px] font-medium text-sky-900">Полный номер только у администратора</div></div>
            </div>
          </div>
          <div className="grid gap-3 lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
            <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm"><h3 className="text-[15px] font-semibold">Срабатывания по правилам</h3><div className="mt-3 space-y-2">{counts.map(({ rule, count }) => <div key={rule} className="flex items-center justify-between rounded-[10px] bg-[#f8fafc] px-3 py-2 text-[13px]"><span>{maxRuleLabels[rule]}</span><span className="font-semibold">{count}</span></div>)}</div><p className="mt-3 text-[12px] text-b24-text-muted">Пользовательские правила DLP отображаются в журнале инцидентов с их кодом.</p></section>
            <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm"><h3 className="text-[15px] font-semibold">Последние события</h3>{maxIncidents.length ? <div className="mt-3 divide-y divide-b24-border">{maxIncidents.slice(0, 8).map((item) => <div key={item.id} className="py-2 text-[12px]"><div className="flex flex-wrap gap-2"><span className="text-b24-text-muted">{when(item.created_at)}</span><span className="font-medium">{item.actor_name || 'Сотрудник'}</span><span className="ml-auto">{statusLabel(item.status)} · {severityLabel(item.severity)}</span></div><div className="mt-1">{maxRuleLabels[item.rule_code] || item.rule_code || 'Пользовательское правило'} — {item.summary}</div></div>)}</div> : <p className="mt-3 text-[13px] text-b24-text-muted">Заблокированных сообщений пока нет.</p>}</section>
          </div>
        </section>
      })() : null}

      {view === 'feedback' ? <section className="space-y-3">
        <div className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
          <h2 className="text-[16px] font-semibold">Обратная связь по DLP</h2>
          <p className="mt-1 text-[13px] text-b24-text-muted">Сводка строится только по решениям администратора. Тексты сообщений, номера телефонов и документы сюда не попадают.</p>
          <div className="mt-4 grid gap-3 sm:grid-cols-2"><div className="rounded-[12px] border border-emerald-100 bg-emerald-50 p-3"><div className="text-[12px] text-emerald-700">Подтверждённых нарушений</div><div className="mt-1 text-[24px] font-semibold text-emerald-900">{feedback?.confirmed ?? 0}</div></div><div className="rounded-[12px] border border-amber-100 bg-amber-50 p-3"><div className="text-[12px] text-amber-700">Ложных срабатываний</div><div className="mt-1 text-[24px] font-semibold text-amber-900">{feedback?.dismissed ?? 0}</div></div></div>
        </div>
        <div className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm"><h3 className="text-[15px] font-semibold">По правилам</h3><p className="mt-1 text-[12px] text-b24-text-muted">Эти данные используются для ручной настройки правил. Автоматическое переобучение модели не выполняется.</p><div className="mt-3 overflow-x-auto"><table className="w-full text-left text-[13px]"><thead className="bg-b24-panel-header text-[12px] text-b24-text-muted"><tr><th className="px-3 py-2">Правило</th><th className="px-3 py-2">Тип события</th><th className="px-3 py-2">Подтверждено</th><th className="px-3 py-2">Ложные</th><th className="px-3 py-2">Всего</th></tr></thead><tbody className="divide-y divide-b24-border">{feedback?.by_rule?.length ? feedback.by_rule.map((row) => <tr key={`${row.rule_code}-${row.event_type}`}><td className="px-3 py-3 font-medium">{row.rule_code || 'без кода'}</td><td className="px-3 py-3">{incidentTypeLabel(row.event_type)}</td><td className="px-3 py-3 text-emerald-700">{row.confirmed}</td><td className="px-3 py-3 text-amber-700">{row.dismissed}</td><td className="px-3 py-3">{row.total}</td></tr>) : <tr><td colSpan={5} className="px-3 py-8 text-center text-b24-text-muted">Решений администратора пока нет.</td></tr>}</tbody></table></div></div>
      </section> : null}

      {view === 'incidents' ? <>
      <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm flex flex-wrap gap-2">
        {statuses.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => setStatus(item.id)}
            className={[
              'h-9 px-3 rounded-[10px] text-[13px] border',
              status === item.id ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep' : 'border-black/10',
            ].join(' ')}
          >
            {item.label}
          </button>
        ))}
        <select
          value={type}
          onChange={(e) => setType(e.target.value)}
          className="h-9 rounded-[10px] border border-black/10 px-2 text-[13px]"
        >
          {types.map((item) => (
            <option key={item.id} value={item.id}>
              {item.label}
            </option>
          ))}
        </select>
      </section>

      {error ? <p className="text-[13px] text-rose-700">{error}</p> : null}

      <div className="grid grid-cols-1 xl:grid-cols-[minmax(0,1.4fr)_minmax(280px,0.8fr)] gap-3">
        <section className="rounded-[14px] bg-white text-b24-text-dark shadow-sm overflow-hidden">
          <table className="w-full text-left text-[13px]">
            <thead className="bg-b24-panel-header text-[12px] text-b24-text-muted">
              <tr>
                <th className="px-4 py-2.5 font-medium">Когда</th>
                <th className="px-3 py-2.5 font-medium">Кто</th>
                <th className="px-3 py-2.5 font-medium">Событие</th>
                <th className="px-3 py-2.5 font-medium">Статус</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-b24-border">
              {loading && items.length === 0 ? (
                <tr>
                  <td colSpan={4} className="px-4 py-10 text-center text-b24-text-muted">
                    Загрузка…
                  </td>
                </tr>
              ) : items.length === 0 ? (
                <tr>
                  <td colSpan={4} className="px-4 py-10 text-center text-b24-text-muted">
                    Инцидентов нет
                  </td>
                </tr>
              ) : (
                items.map((item) => (
                  <tr
                    key={item.id}
                    onClick={() => {
                      setSelected(item.id)
                      setComment(item.comment || '')
                    }}
                    className={selected === item.id ? 'bg-[#e8f8fe] cursor-pointer' : 'cursor-pointer hover:bg-[#f7fafc]'}
                  >
                    <td className="px-4 py-3 whitespace-nowrap">{when(item.created_at)}</td>
                    <td className="px-3 py-3">{item.actor_name || '—'}</td>
                    <td className="px-3 py-3">
                      <div>{incidentTypeLabel(item.event_type)}</div>
                      <div className="text-[12px] text-b24-text-muted">{item.summary}</div>
                    </td>
                    <td className="px-3 py-3">
                      {statusLabel(item.status)}
                      <div className="text-[12px] text-b24-text-muted">{severityLabel(item.severity)}</div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </section>

        <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
          {current ? (
            <>
              <h2 className="text-[16px] font-semibold">{current.actor_name || 'Инцидент'}</h2>
              <p className="mt-1 text-[13px]">{current.summary}</p>
              {current.excerpt ? (
                <p className="mt-2 rounded-[10px] bg-[#f4f7f8] px-3 py-2 text-[13px]">{current.excerpt}</p>
              ) : null}
              <p className="mt-2 text-[12px] text-b24-text-muted">
                Правило: {current.rule_code || '—'} · {severityLabel(current.severity)}
              </p>
              <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[12px]">
                <dt className="text-b24-text-muted">Событие</dt>
                <dd>{incidentTypeLabel(current.event_type)}</dd>
                <dt className="text-b24-text-muted">Объект</dt>
                <dd>{objectLabel(current.object_type)}{current.object_id ? ` · ${current.object_id}` : ''}</dd>
                {current.channel ? <><dt className="text-b24-text-muted">Канал</dt><dd>{current.channel}</dd></> : null}
                {current.detail_kind ? <><dt className="text-b24-text-muted">Тип данных</dt><dd>{current.detail_kind}</dd></> : null}
                {current.reviewed_at ? <><dt className="text-b24-text-muted">Решение</dt><dd>{current.reviewer_name || 'Администратор'} · {when(current.reviewed_at)}</dd></> : null}
              </dl>
              {current.restricted && current.actor_id ? (
                <button
                  type="button"
                  disabled={pending}
                  onClick={() => void release(current.actor_id)}
                  className="mt-3 h-9 px-3 rounded-[10px] border border-amber-300 bg-amber-50 text-[13px]"
                >
                  Снять ограничение сессии
                </button>
              ) : null}
              <label className="mt-3 block text-[12px] text-b24-text-muted">
                Комментарий
                <textarea
                  value={comment}
                  onChange={(e) => setComment(e.target.value)}
                  rows={4}
                  className="mt-1 w-full rounded-[10px] border border-black/10 px-3 py-2 text-[13px] text-b24-text-dark"
                />
              </label>
              <div className="mt-3 flex flex-wrap gap-2">
                <button
                  type="button"
                  disabled={pending}
                  onClick={() => void review('confirmed')}
                  className="h-9 px-3 rounded-[10px] bg-[#e8f8ef] text-[13px] text-[#1a7a45]"
                >
                  Подтвердить
                </button>
                <button
                  type="button"
                  disabled={pending}
                  onClick={() => void review('dismissed')}
                  className="h-9 px-3 rounded-[10px] bg-[#f5f0f0] text-[13px] text-[#9a5050]"
                >
                  Отклонить
                </button>
                <button
                  type="button"
                  disabled={pending}
                  onClick={() => void review('open')}
                  className="h-9 px-3 rounded-[10px] border border-black/10 text-[13px]"
                >
                  Вернуть в открытые
                </button>
              </div>
            </>
          ) : (
            <p className="text-[13px] text-b24-text-muted">Выберите инцидент, чтобы подтвердить, отклонить или оставить комментарий.</p>
          )}
        </section>
      </div>
      </> : null}

      {view === 'rules' ? <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
        <h2 className="text-[16px] font-semibold">Правила DLP</h2>
        <p className="mt-1 text-[13px] text-b24-text-muted">Администратор добавляет собственные фразы и домены. Правило работает до отправки сообщения.</p>
        <div className="mt-3 grid gap-2 md:grid-cols-5">
          <input value={ruleForm.code} onChange={(e) => setRuleForm((v) => ({ ...v, code: e.target.value }))} placeholder="Код правила" className="h-9 rounded-[10px] border border-black/10 px-3 text-[13px]" />
          <select value={ruleForm.kind} onChange={(e) => setRuleForm((v) => ({ ...v, kind: e.target.value }))} className="h-9 rounded-[10px] border border-black/10 px-2 text-[13px]"><option value="phrase">Фраза</option><option value="domain">Домен или ссылка</option></select>
          <input value={ruleForm.pattern} onChange={(e) => setRuleForm((v) => ({ ...v, pattern: e.target.value }))} placeholder="Например: напиши в телеграм" className="h-9 rounded-[10px] border border-black/10 px-3 text-[13px] md:col-span-2" />
          <select value={ruleForm.action} onChange={(e) => setRuleForm((v) => ({ ...v, action: e.target.value }))} className="h-9 rounded-[10px] border border-black/10 px-2 text-[13px]"><option value="block">Блокировать</option><option value="log">Только журнал</option></select>
        </div>
        <div className="mt-2 flex flex-wrap gap-2">
          <select value={ruleForm.severity} onChange={(e) => setRuleForm((v) => ({ ...v, severity: e.target.value }))} className="h-9 rounded-[10px] border border-black/10 px-2 text-[13px]"><option value="low">Низкий риск</option><option value="medium">Средний риск</option><option value="high">Высокий риск</option></select>
          <button type="button" disabled={pending || !ruleForm.code.trim() || !ruleForm.pattern.trim()} onClick={() => void addRule()} className="h-9 rounded-[10px] bg-b24-blue px-3 text-[13px] text-white disabled:opacity-50">Добавить правило</button>
        </div>
        <div className="mt-4 divide-y divide-b24-border rounded-[10px] border border-black/5">
          {rules.length === 0 ? <p className="px-3 py-4 text-[13px] text-b24-text-muted">Пользовательских правил пока нет. Базовые правила защиты работают всегда.</p> : rules.map((rule) => <div key={rule.id} className="flex flex-wrap items-center gap-3 px-3 py-3 text-[13px]"><span className={`size-2 rounded-full ${rule.enabled ? 'bg-emerald-500' : 'bg-slate-300'}`} /><span className="font-medium">{rule.code}</span><span className="text-b24-text-muted">{rule.kind === 'domain' ? 'домен' : 'фраза'}: {rule.pattern}</span><span className="ml-auto text-b24-text-muted">{rule.action === 'block' ? 'блокировка' : 'журнал'} · {severityLabel(rule.severity)}</span><button type="button" disabled={pending} onClick={() => void toggleRule(rule)} className="rounded-[8px] border border-black/10 px-2 py-1 text-[12px]">{rule.enabled ? 'Выключить' : 'Включить'}</button><button type="button" disabled={pending} onClick={() => void deleteRule(rule.id)} className="rounded-[8px] border border-rose-200 px-2 py-1 text-[12px] text-rose-700">Удалить</button></div>)}
        </div>
        <div className="mt-5"><h3 className="text-[15px] font-semibold">Журнал изменений</h3><p className="mt-1 text-[12px] text-b24-text-muted">Каждое действие требует основания и сохраняется с администратором и временем.</p><div className="mt-3 divide-y divide-b24-border rounded-[10px] border border-black/5">{ruleAudit.length === 0 ? <p className="px-3 py-4 text-[13px] text-b24-text-muted">Изменений пока нет.</p> : ruleAudit.slice(0, 12).map((entry) => <div key={entry.id} className="flex flex-wrap items-center gap-2 px-3 py-2 text-[12px]"><span className="text-b24-text-muted">{when(entry.created_at)}</span><span className="font-medium">{entry.action}</span><span>{entry.rule_code || 'удалённое правило'}</span><span className="text-b24-text-muted">{entry.actor_name || 'администратор'}</span><span className="ml-auto text-b24-text-muted">{entry.reason || 'без основания'}</span></div>)}</div></div>
      </section> : null}

      {view === 'incidents' ? <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
        <h2 className="text-[16px] font-semibold">Проверка звонка</h2>
        <p className="mt-1 text-[13px] text-b24-text-muted">
          Постобработка: вставьте транскрипт звонка. Те же правила, что в чате. Если фраза запрещена, появится инцидент.
        </p>
        <div className="mt-3 grid gap-2">
          <input
            value={callId}
            onChange={(e) => setCallId(e.target.value)}
            placeholder="ID звонка"
            className="h-9 rounded-[10px] border border-black/10 px-3 text-[13px]"
          />
          <textarea
            value={transcript}
            onChange={(e) => setTranscript(e.target.value)}
            rows={3}
            placeholder="Текст разговора"
            className="rounded-[10px] border border-black/10 px-3 py-2 text-[13px]"
          />
          <div className="flex items-center gap-3">
            <button
              type="button"
              disabled={pending || !callId.trim() || !transcript.trim()}
              onClick={() => void checkCall()}
              className="h-9 px-3 rounded-[10px] border border-black/10 text-[13px] disabled:opacity-50"
            >
              Проверить
            </button>
            {checkMsg ? <span className="text-[13px]">{checkMsg}</span> : null}
          </div>
        </div>
      </section> : null}

      {view === 'employees' ? <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
        <h2 className="text-[16px] font-semibold">Сотрудники и риск</h2>
        <p className="mt-1 text-[13px] text-b24-text-muted">Статус рассчитывается по открытым и подтверждённым событиям DLP. Это контрольный сигнал, а не автоматическое увольнение или перераспределение.</p>
        <div className="mt-4 overflow-x-auto"><table className="w-full text-left text-[13px]"><thead className="bg-b24-panel-header text-[12px] text-b24-text-muted"><tr><th className="px-3 py-2">Сотрудник</th><th className="px-3 py-2">Роль</th><th className="px-3 py-2">Отдел</th><th className="px-3 py-2">Нарушения</th><th className="px-3 py-2">Статус</th><th className="px-3 py-2">Действие</th></tr></thead><tbody className="divide-y divide-b24-border">{staff.map((person) => { const events = riskIncidents.filter((item) => item.actor_id === person.id && item.status !== 'dismissed'); const high = events.filter((item) => item.severity === 'high').length; const label = high >= 3 ? 'подозрительный' : events.length ? 'под наблюдением' : 'норма'; return <tr key={person.id} onClick={() => setSelectedStaffId(person.id)} className={`cursor-pointer ${selectedStaffId === person.id ? 'bg-[#e8f8fe]' : 'hover:bg-[#f7fafc]'}`}><td className="px-3 py-3">{person.full_name || person.email}<div className="text-[11px] text-b24-text-muted">{person.email}</div></td><td className="px-3 py-3">{person.role}</td><td className="px-3 py-3">{person.department || '—'}</td><td className="px-3 py-3">{events.length}</td><td className="px-3 py-3"><span className={`rounded-full px-2 py-1 text-[11px] ${label === 'подозрительный' ? 'bg-rose-100 text-rose-700' : label === 'под наблюдением' ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'}`}>{label}</span></td><td className="px-3 py-3">{['manager', 'sales'].includes(String(person.role).toLowerCase()) ? <button type="button" disabled={pending} onClick={(event) => { event.stopPropagation(); void approvePassportText(person.id) }} className="rounded-[8px] border border-amber-300 bg-amber-50 px-2 py-1 text-[12px] text-amber-800">Разрешить запрос паспорта на 24 ч</button> : <span className="text-[12px] text-b24-text-muted">—</span>}</td></tr> })}</tbody></table></div>
        {selectedStaffId && staffSummary ? (() => { const person = staff.find((item) => item.id === selectedStaffId); return <div className="mt-4 rounded-[12px] border border-black/5 bg-[#f8fafc] p-4"><div className="flex flex-wrap items-start justify-between gap-3"><div><h3 className="text-[15px] font-semibold">{person?.full_name || person?.email || 'Сотрудник'}</h3><p className="mt-1 text-[12px] text-b24-text-muted">{person?.role || '—'} · {person?.department || 'без отдела'} · {person?.is_active ? 'активен' : 'неактивен'}</p></div><span className={`rounded-full px-2 py-1 text-[11px] ${staffSummary.restricted ? 'bg-rose-100 text-rose-700' : 'bg-emerald-100 text-emerald-700'}`}>{staffSummary.restricted ? 'сессия ограничена' : 'сессия активна'}</span></div><div className="mt-3 grid gap-2 sm:grid-cols-3"><div className="rounded-[10px] bg-white p-3"><div className="text-[11px] text-b24-text-muted">Открытых</div><div className="mt-1 text-[18px] font-semibold">{staffSummary.open}</div></div><div className="rounded-[10px] bg-white p-3"><div className="text-[11px] text-b24-text-muted">Подтверждено</div><div className="mt-1 text-[18px] font-semibold">{staffSummary.confirmed}</div></div><div className="rounded-[10px] bg-white p-3"><div className="text-[11px] text-b24-text-muted">Основание ограничения</div><div className="mt-1 text-[12px]">{staffSummary.reason || 'нет'}</div></div></div>{staffSummary.recent.length ? <div className="mt-3"><div className="text-[12px] font-medium">Последние события</div><div className="mt-2 space-y-2">{staffSummary.recent.map((event) => <div key={event.id} className="flex flex-wrap items-center gap-2 text-[12px]"><span className="text-b24-text-muted">{when(event.created_at)}</span><span>{event.summary}</span><span className="ml-auto text-b24-text-muted">{statusLabel(event.status)} · {severityLabel(event.severity)}</span></div>)}</div></div> : <p className="mt-3 text-[12px] text-b24-text-muted">Событий для отображения нет.</p>}{staffSummary.restricted ? <button type="button" disabled={pending} onClick={() => void release(selectedStaffId)} className="mt-4 h-9 rounded-[10px] border border-amber-300 bg-amber-50 px-3 text-[13px]">Снять ограничение сессии</button> : null}</div> })() : <p className="mt-4 text-[12px] text-b24-text-muted">Выберите сотрудника в таблице, чтобы открыть его профиль безопасности.</p>}
      </section> : null}

      {view === 'analytics' ? <section className="rounded-[14px] bg-white p-4 text-b24-text-dark shadow-sm">
        <h2 className="text-[16px] font-semibold">Аналитика сотрудников</h2>
        <p className="mt-1 text-[13px] text-b24-text-muted">Количество звонков, дозвоны, обработка лидов и результативность находятся в существующем аналитическом модуле CRM.</p>
        <Link to="/analytics" className="mt-4 inline-flex rounded-[10px] bg-b24-blue px-3 py-2 text-[13px] text-white">Открыть аналитику CRM →</Link>
      </section> : null}
    </PageShell>
  )
}

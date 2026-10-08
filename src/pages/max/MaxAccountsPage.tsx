import { RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { PageHeader, PageShell } from '../../components/layout/PageHeader'
import { Button } from '../../components/ui/Button'
import { api } from '../../lib/api'

type PhoneRow = {
  phone: string
  owner_id: string
  owner_name: string
  bound: boolean
  session: string
  send_down: boolean
  cold_limit: boolean
  check?: string
  can_new: boolean
  bucket: string
  profiles: number
  extra_profile: boolean
}

type PersonNum = { phone: string; label: string }

type PersonRow = {
  id: string
  name: string
  role: string
  has_working: boolean
  numbers: PersonNum[]
}

type CheckTarget = { profile_id: string; phone: string }

type Board = {
  phones: PhoneRow[]
  people: PersonRow[]
  counts: Record<string, number>
  targets?: CheckTarget[]
  wappi_error?: string
}

type Filter = 'all' | 'live' | 'down' | 'free' | 'cold' | 'people_gap'

const PAGE = 10

const FILTERS: { id: Filter; label: string; hint: string }[] = [
  { id: 'all', label: 'Все', hint: 'по 10' },
  { id: 'live', label: 'Работают', hint: 'успешно' },
  { id: 'down', label: 'Не работают', hint: 'не авторизован' },
  { id: 'free', label: 'Свободные', hint: 'ничей и успешно' },
  { id: 'cold', label: 'Лимит на новых', hint: 'проверка «нет»' },
  { id: 'people_gap', label: 'Без рабочего MAX', hint: 'нечем писать новым' },
]

function phoneMatches(row: PhoneRow, filter: Filter) {
  if (filter === 'all' || filter === 'people_gap') return true
  if (filter === 'live') return row.session === 'live' && !row.send_down && !row.cold_limit
  if (filter === 'down') return row.send_down || row.session !== 'live'
  if (filter === 'free') return !row.bound && row.session === 'live' && !row.send_down && !row.cold_limit
  if (filter === 'cold') return row.cold_limit && !row.send_down && row.session === 'live'
  return false
}

function formatPhone(raw: string) {
  const d = String(raw || '').replace(/\D/g, '')
  if (d.length === 11 && d[0] === '7') {
    return `+7 ${d.slice(1, 4)} ${d.slice(4, 7)}-${d.slice(7, 9)}-${d.slice(9)}`
  }
  return raw || 'без номера'
}

function sessionLabel(session: string) {
  if (session === 'live') return 'жива'
  if (session === 'down') return 'не работает'
  return 'нет в списке'
}

function newLabel(row: PhoneRow) {
  if (row.can_new) return 'да'
  if (!row.bound) return '—'
  return 'нет'
}

function checkLabel(row: PhoneRow) {
  if (row.check === 'ok') return 'успешно'
  if (row.check === 'no') return 'нет'
  if (row.check === 'unauthorized') return 'не авторизован'
  return 'не проверяли'
}

function probeDigits(raw: string) {
  let d = raw.replace(/\D/g, '')
  if (d.length === 11 && d[0] === '8') d = `7${d.slice(1)}`
  if (d.length === 10) d = `7${d}`
  return d.length === 11 && d[0] === '7' ? d : ''
}

function wait(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function samePhone(a: string, b: string) {
  const da = a.replace(/\D/g, '')
  const db = b.replace(/\D/g, '')
  if (da.length >= 10 && db.length >= 10) return da.slice(-10) === db.slice(-10)
  return da !== '' && da === db
}

export function MaxAccountsPage() {
  const navigate = useNavigate()
  const [data, setData] = useState<Board | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<Filter>('all')
  const [query, setQuery] = useState('')
  const [peopleMode, setPeopleMode] = useState<'gap' | 'all'>('gap')
  const [phonePage, setPhonePage] = useState(0)
  const [peoplePage, setPeoplePage] = useState(0)
  const [grantId, setGrantId] = useState('')
  const [grantNote, setGrantNote] = useState('')
  const [probe, setProbe] = useState('')
  const [checking, setChecking] = useState(false)
  const [checkedN, setCheckedN] = useState(0)
  const [checkTotal, setCheckTotal] = useState(0)
  const [checkNote, setCheckNote] = useState('')
  const [checkingPhone, setCheckingPhone] = useState('')
  const stopCheck = useRef(false)

  async function load() {
    setLoading(true)
    setError('')
    try {
      const res = await api<Board>('/max-accounts')
      setData(res)
      return res
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось загрузить')
      return null
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  async function checkAll() {
    const phone = probeDigits(probe)
    const targets = data?.targets ?? []
    if (!phone) {
      setCheckNote('Впишите номер, на который проверяем все MAX')
      return
    }
    if (targets.length === 0) {
      setCheckNote('Список номеров пуст')
      return
    }
    stopCheck.current = false
    setChecking(true)
    setCheckNote('')
    setCheckedN(0)
    setCheckTotal(targets.length)
    let done = 0
    try {
      for (const target of targets) {
        if (stopCheck.current) {
          setCheckNote(`Остановлено. Проверено ${done} из ${targets.length}`)
          break
        }
        try {
          await api('/max-accounts/check', {
            method: 'POST',
            body: JSON.stringify({ phone, profile_id: target.profile_id }),
          })
        } catch (e) {
          const msg = e instanceof Error ? e.message : 'Проверка не прошла'
          if (/лимит проверок|flood/i.test(msg) || /нет доступа/i.test(msg)) {
            setCheckNote(`${msg}. Проверено ${done} из ${targets.length}`)
            break
          }
        }
        done += 1
        setCheckedN(done)
        if (done < targets.length && !stopCheck.current) await wait(1100)
      }
      if (!stopCheck.current && done === targets.length) {
        setCheckNote(`Проверено ${done} из ${targets.length}`)
      }
      await load()
    } finally {
      setChecking(false)
    }
  }

  async function checkOne(row: PhoneRow) {
    const phone = probeDigits(probe)
    const targets = (data?.targets ?? []).filter((target) => samePhone(target.phone, row.phone))
    if (!phone) {
      setCheckNote('Впишите номер, на который проверяем')
      return
    }
    if (targets.length === 0) {
      setCheckNote('У этого номера нет профиля для проверки')
      return
    }
    setCheckingPhone(row.phone)
    setCheckNote('')
    let last = ''
    try {
      for (let i = 0; i < targets.length; i++) {
        const res = await api<{ result?: string }>('/max-accounts/check', {
          method: 'POST',
          body: JSON.stringify({ phone, profile_id: targets[i].profile_id }),
        })
        last = res.result || last
        if (i < targets.length - 1) await wait(1100)
      }
      const fresh = await load()
      const updated = fresh?.phones.find((item) => samePhone(item.phone, row.phone))
      const label = updated ? checkLabel(updated) : last === 'ok' ? 'успешно' : last === 'no' ? 'нет' : last === 'unauthorized' ? 'не авторизован' : 'проверен'
      setCheckNote(`${formatPhone(row.phone)}: ${label}`)
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Проверка не прошла'
      setCheckNote(msg)
      await load()
    } finally {
      setCheckingPhone('')
    }
  }

  async function grant(userId: string) {
    setGrantId(userId)
    setGrantNote('')
    try {
      const res = await api<{ phone?: string }>('/max-accounts/grant', {
        method: 'POST',
        body: JSON.stringify({ user_id: userId }),
      })
      setGrantNote(res.phone ? `Добавлен ${formatPhone(res.phone)}. Старые чаты с него не подтянутся.` : 'Свободного рабочего номера нет')
      await load()
    } catch (e) {
      setGrantNote(e instanceof Error ? e.message : 'Не удалось выдать номер')
    } finally {
      setGrantId('')
    }
  }

  const q = query.trim().toLowerCase()
  const qDigits = q.replace(/\D/g, '')

  const phones = useMemo(() => {
    const rows = data?.phones ?? []
    return rows.filter((row) => {
      if (!phoneMatches(row, filter)) return false
      if (!q) return true
      const blob = `${row.phone} ${row.owner_name}`.toLowerCase()
      if (qDigits.length >= 3 && row.phone.replace(/\D/g, '').includes(qDigits)) return true
      return blob.includes(q)
    })
  }, [data, filter, q, qDigits])

  const people = useMemo(() => {
    const rows = data?.people ?? []
    return rows.filter((row) => {
      if (peopleMode === 'gap' && row.has_working) return false
      if (!q) return true
      const nums = row.numbers.map((n) => `${n.phone} ${n.label}`).join(' ')
      const blob = `${row.name} ${nums}`.toLowerCase()
      if (qDigits.length >= 3 && nums.replace(/\D/g, '').includes(qDigits)) return true
      return blob.includes(q)
    })
  }, [data, peopleMode, q, qDigits])

  useEffect(() => {
    setPhonePage(0)
  }, [filter, q])

  useEffect(() => {
    setPeoplePage(0)
  }, [peopleMode, q])

  const phonePages = Math.max(1, Math.ceil(phones.length / PAGE))
  const peoplePages = Math.max(1, Math.ceil(people.length / PAGE))
  const phoneSlice = phones.slice(phonePage * PAGE, phonePage * PAGE + PAGE)
  const peopleSlice = people.slice(peoplePage * PAGE, peoplePage * PAGE + PAGE)
  const allPhones = data?.phones ?? []
  const counts: Record<string, number> = {
    all: allPhones.length,
    live: allPhones.filter((row) => phoneMatches(row, 'live')).length,
    down: allPhones.filter((row) => phoneMatches(row, 'down')).length,
    free: allPhones.filter((row) => phoneMatches(row, 'free')).length,
    cold: allPhones.filter((row) => phoneMatches(row, 'cold')).length,
    people_gap: data?.counts?.people_gap ?? 0,
  }

  return (
    <PageShell>
      <PageHeader
        title="MAX"
        subtitle="Кто на каком номере, где лимит и у кого нечем писать новым. Привязка по-прежнему на карточке сотрудника."
        actions={
          <Button
            type="button"
            variant="glass"
            className="h-10 rounded-[10px] px-4"
            disabled={loading}
            onClick={() => void load()}
            leftIcon={<RefreshCw size={16} className={loading ? 'animate-spin' : ''} />}
          >
            Обновить
          </Button>
        }
      />

      <div className="grid grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-2.5">
        {FILTERS.map((item) => {
          const active = filter === item.id
          const n = item.id === 'all' ? counts.all ?? 0 : counts[item.id] ?? 0
          return (
            <button
              key={item.id}
              type="button"
              onClick={() => {
                setFilter(item.id)
                if (item.id === 'people_gap') setPeopleMode('gap')
              }}
              className={[
                'rounded-[14px] border px-4 py-3 text-left backdrop-blur-sm transition',
                active ? 'bg-white text-b24-text-dark border-white' : 'bg-white/12 border-white/10 text-white',
              ].join(' ')}
            >
              <div className={['text-[11px] uppercase tracking-wide', active ? 'text-b24-text-muted' : 'text-white/55'].join(' ')}>
                {item.label}
              </div>
              <div className="mt-1 text-[26px] font-semibold tabular-nums leading-none">{n}</div>
              <div className={['mt-1 text-[11px] truncate', active ? 'text-b24-text-muted' : 'text-white/45'].join(' ')}>
                {item.hint}
              </div>
            </button>
          )
        })}
      </div>

      <div className="b24-glass-panel px-4 py-3 flex flex-wrap items-end gap-2">
        <label className="flex min-w-[220px] flex-1 flex-col gap-1 text-[12px] text-b24-text-muted">
          Номер для проверки
          <input
            value={probe}
            onChange={(e) => setProbe(e.target.value)}
            placeholder="79014171261"
            disabled={checking}
            className="h-10 rounded-[10px] border border-black/10 bg-white px-3 text-[14px] text-b24-text-dark outline-none placeholder:text-b24-text-muted focus:border-[#1f6feb]"
          />
        </label>
        <button
          type="button"
          disabled={checking || loading || checkingPhone !== ''}
          onClick={() => void checkAll()}
          className="h-10 rounded-[10px] bg-[#1f6feb] px-4 text-[13px] font-medium text-white disabled:opacity-50"
        >
          {checking ? 'Проверяю…' : 'Проверить все'}
        </button>
        {checking ? (
          <button
            type="button"
            onClick={() => {
              stopCheck.current = true
            }}
            className="h-10 rounded-[10px] border border-black/10 bg-white px-4 text-[13px] font-medium text-b24-text-dark"
          >
            Стоп
          </button>
        ) : null}
        {checkTotal > 0 ? (
          <span className="pb-2 text-[13px] text-b24-text-dark">проверено {checkedN} из {checkTotal}</span>
        ) : null}
        {checkNote ? <span className="pb-2 text-[13px] text-b24-text-dark">{checkNote}</span> : null}
      </div>

      {error ? <p className="text-[13px] text-orange-200">{error}</p> : null}
      {data?.wappi_error ? <p className="text-[13px] text-orange-200">{data.wappi_error}</p> : null}

      <div className="relative">
        <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-white/55 pointer-events-none" />
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Номер или имя"
          className="h-10 w-full rounded-[10px] border border-white/15 bg-white/10 pl-9 pr-3 text-[14px] text-white outline-none placeholder:text-white/40 focus:border-white/40"
        />
      </div>

      {filter !== 'people_gap' ? (
        <div className="b24-glass-panel overflow-hidden flex flex-col min-h-[320px]">
          <div className="overflow-auto">
            <table className="w-full text-left text-[13px]">
              <thead className="text-[11px] uppercase tracking-wide text-b24-text-muted">
                <tr className="border-b border-black/6">
                  <th className="px-4 py-2 font-medium">Номер</th>
                  <th className="px-4 py-2 font-medium">Кому</th>
                  <th className="px-4 py-2 font-medium">Новым</th>
                  <th className="px-4 py-2 font-medium">Сессия</th>
                  <th className="px-4 py-2 font-medium">Проверка</th>
                  <th className="px-4 py-2 font-medium">Профили</th>
                </tr>
              </thead>
              <tbody>
                {phoneSlice.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-b24-text-muted">
                      {loading ? 'Загрузка…' : 'Ничего не нашлось'}
                    </td>
                  </tr>
                ) : (
                  phoneSlice.map((row) => (
                    <tr key={`${row.phone}-${row.owner_id}`} className="border-b border-black/5 last:border-0">
                      <td className="px-4 py-2.5 font-medium tabular-nums text-b24-text-dark">{formatPhone(row.phone)}</td>
                      <td className="px-4 py-2.5">
                        {row.owner_id ? (
                          <button
                            type="button"
                            className="text-b24-cyan font-medium hover:underline"
                            onClick={() => navigate(`/employees/${row.owner_id}`)}
                          >
                            {row.owner_name || 'Сотрудник'}
                          </button>
                        ) : (
                          <span className="text-b24-text-muted">свободен</span>
                        )}
                      </td>
                      <td className="px-4 py-2.5 text-b24-text-dark">{newLabel(row)}</td>
                      <td className="px-4 py-2.5">
                        <span className={row.session === 'live' && !row.send_down ? 'text-b24-text-dark' : 'text-[#7a3030]'}>
                          {row.send_down ? 'не работает' : row.cold_limit ? 'лимит на новых' : sessionLabel(row.session)}
                        </span>
                      </td>
                      <td className="px-4 py-2.5">
                        <div className="flex items-center gap-2">
                          <span className="text-b24-text-dark">{checkLabel(row)}</span>
                          <button
                            type="button"
                            disabled={checking || checkingPhone !== '' || loading}
                            onClick={() => void checkOne(row)}
                            className="h-7 rounded-[8px] border border-black/10 bg-white px-2 text-[12px] font-medium text-b24-text-dark disabled:opacity-50"
                          >
                            {checkingPhone === row.phone ? '…' : 'Проверить'}
                          </button>
                        </div>
                      </td>
                      <td className="px-4 py-2.5 text-b24-text-muted">
                        {row.profiles || '—'}
                        {row.extra_profile ? ' · второй не свободный' : ''}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
          <Pager page={phonePage} pages={phonePages} total={phones.length} onPage={setPhonePage} />
        </div>
      ) : null}

      <div className="b24-glass-panel overflow-hidden">
        <div className="px-4 py-3 border-b border-black/6 bg-white/80 flex flex-wrap items-center gap-2">
          <div className="text-[15px] font-semibold text-b24-text-dark">Сотрудники</div>
          <button
            type="button"
            onClick={() => setPeopleMode('gap')}
            className={[
              'h-8 rounded-[8px] px-3 text-[12px] font-medium',
              peopleMode === 'gap' ? 'bg-[#1f6feb] text-white' : 'bg-[#f5f7f9] text-b24-text-dark',
            ].join(' ')}
          >
            Без рабочего · {counts.people_gap ?? 0}
          </button>
          <button
            type="button"
            onClick={() => setPeopleMode('all')}
            className={[
              'h-8 rounded-[8px] px-3 text-[12px] font-medium',
              peopleMode === 'all' ? 'bg-[#1f6feb] text-white' : 'bg-[#f5f7f9] text-b24-text-dark',
            ].join(' ')}
          >
            Все
          </button>
        </div>
        <div className="overflow-auto">
          <table className="w-full text-left text-[13px]">
            <thead className="text-[11px] uppercase tracking-wide text-b24-text-muted">
              <tr className="border-b border-black/6">
                <th className="px-4 py-2 font-medium">Имя</th>
                <th className="px-4 py-2 font-medium">Новым можно</th>
                <th className="px-4 py-2 font-medium">Номера</th>
                <th className="px-4 py-2 font-medium" />
              </tr>
            </thead>
            <tbody>
              {peopleSlice.length === 0 ? (
                <tr>
                  <td colSpan={4} className="px-4 py-8 text-center text-b24-text-muted">
                    {loading ? 'Загрузка…' : 'Никого нет'}
                  </td>
                </tr>
              ) : (
                peopleSlice.map((row) => (
                  <tr key={row.id} className="border-b border-black/5 last:border-0 align-top">
                    <td className="px-4 py-2.5">
                      <button
                        type="button"
                        className="text-b24-cyan font-medium hover:underline"
                        onClick={() => navigate(`/employees/${row.id}`)}
                      >
                        {row.name || 'Сотрудник'}
                      </button>
                    </td>
                    <td className="px-4 py-2.5 text-b24-text-dark">{row.has_working ? 'да' : 'нет'}</td>
                    <td className="px-4 py-2.5 text-b24-text-dark">
                      {row.numbers.length === 0 ? (
                        <span className="text-b24-text-muted">нет номеров</span>
                      ) : (
                        row.numbers.map((n) => (
                          <div key={`${row.id}-${n.phone}`} className="tabular-nums">
                            {formatPhone(n.phone)}
                            <span className="text-b24-text-muted"> · {n.label}</span>
                          </div>
                        ))
                      )}
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      {!row.has_working ? (
                        <button
                          type="button"
                          disabled={grantId !== ''}
                          onClick={() => void grant(row.id)}
                          className="h-8 rounded-[8px] bg-[#1f6feb] px-3 text-[12px] font-medium text-white disabled:opacity-50"
                        >
                          {grantId === row.id ? 'Выдаю…' : 'Дать рабочий'}
                        </button>
                      ) : null}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
        <Pager page={peoplePage} pages={peoplePages} total={people.length} onPage={setPeoplePage} />
        {grantNote ? <p className="border-t border-black/6 bg-white/80 px-4 py-2 text-[12px] text-b24-text-dark">{grantNote}</p> : null}
      </div>
    </PageShell>
  )
}

function Pager({
  page,
  pages,
  total,
  onPage,
}: {
  page: number
  pages: number
  total: number
  onPage: (n: number) => void
}) {
  if (total <= PAGE) return null
  const from = page * PAGE + 1
  const to = Math.min(total, (page + 1) * PAGE)
  return (
    <div className="flex items-center justify-between gap-3 border-t border-black/6 bg-white/80 px-4 py-2.5">
      <span className="text-[12px] text-b24-text-muted tabular-nums">
        {from}–{to} из {total}
      </span>
      <div className="flex items-center gap-2">
        <button
          type="button"
          disabled={page <= 0}
          onClick={() => onPage(page - 1)}
          className="h-8 rounded-[8px] bg-[#f5f7f9] px-3 text-[12px] font-medium text-b24-text-dark disabled:opacity-40"
        >
          Назад
        </button>
        <button
          type="button"
          disabled={page + 1 >= pages}
          onClick={() => onPage(page + 1)}
          className="h-8 rounded-[8px] bg-[#f5f7f9] px-3 text-[12px] font-medium text-b24-text-dark disabled:opacity-40"
        >
          Дальше
        </button>
      </div>
    </div>
  )
}

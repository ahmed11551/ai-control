import { useCallback, useEffect, useMemo, useState } from 'react'
import { CrmFormModal, type CrmField } from '../../components/crm/CrmFormModal'
import { CrmGrid, formatGridDate, type GridColumn } from '../../components/crm/CrmGrid'
import { FilterBar, FilterToggle } from '../../components/crm/FilterBar'
import { Button } from '../../components/ui/Button'
import { api } from '../../lib/api'
import {
  CRM_SOURCES,
  type CrmRow,
  cell,
  leadStatusLabel,
  money,
  sourceLabel,
} from '../../lib/crm'
import { type Page, qs } from '../../lib/page'
import { CompanyDrawer, ContactDrawer } from './EntityDrawers'
import { useOpenId } from './useOpenId'

function formatCell(key: string, row: CrmRow) {
  if (key === 'status') return leadStatusLabel(cell(row, key) === '—' ? '' : String(row.status ?? ''))
  if (key === 'source') return sourceLabel(cell(row, key) === '—' ? '' : String(row.source ?? ''))
  if (key === 'amount') return money(row.amount)
  if (key === 'created_at') return formatGridDate(row.created_at)
  return cell(row, key)
}

const companySearch: CrmField = {
  name: 'company_id',
  label: 'Компания',
  type: 'search',
  searchPath: '/companies',
  searchTitle: 'name',
  placeholder: 'Поиск компании',
}

export function ContactsPage() {
  const fields: CrmField[] = useMemo(
    () => [
      { name: 'full_name', label: 'Имя', required: true, placeholder: 'Иван Иванов' },
      { name: 'phone', label: 'Телефон', type: 'tel' },
      { name: 'email', label: 'Email', type: 'email' },
      { name: 'position', label: 'Должность' },
      { name: 'source', label: 'Источник', type: 'select', options: [...CRM_SOURCES] },
      companySearch,
    ],
    [],
  )

  return (
    <EntityTable
      kind="contact"
      title="Контакты"
      path="/contacts"
      columns={[
        { key: 'full_name', label: 'Имя', sort: 'full_name', primary: true },
        { key: 'phone', label: 'Телефон', sort: 'phone' },
        { key: 'email', label: 'Email', sort: 'email' },
        { key: 'company', label: 'Компания', sort: 'company' },
        { key: 'source', label: 'Источник', sort: 'source' },
        { key: 'created_at', label: 'Создан', sort: 'created_at' },
      ]}
      fields={fields}
      createLabel="Контакт"
      defaultValues={{ source: 'manual' }}
    />
  )
}

export function CompaniesPage() {
  const fields: CrmField[] = [
    { name: 'name', label: 'Название', required: true, placeholder: 'ООО Пример' },
    { name: 'inn', label: 'ИНН' },
    { name: 'phone', label: 'Телефон', type: 'tel' },
    { name: 'email', label: 'Email', type: 'email' },
    { name: 'website', label: 'Сайт', placeholder: 'https://' },
    { name: 'address', label: 'Адрес' },
  ]

  return (
    <EntityTable
      kind="company"
      title="Компании"
      path="/companies"
      columns={[
        { key: 'name', label: 'Название', sort: 'name', primary: true },
        { key: 'inn', label: 'ИНН', sort: 'inn' },
        { key: 'phone', label: 'Телефон', sort: 'phone' },
        { key: 'email', label: 'Email', sort: 'email' },
        { key: 'website', label: 'Сайт', sort: 'website' },
        { key: 'created_at', label: 'Создан', sort: 'created_at' },
      ]}
      fields={fields}
      createLabel="Компания"
    />
  )
}

function EntityTable({
  kind,
  title,
  path,
  columns,
  fields,
  createLabel,
  defaultValues,
}: {
  kind: 'contact' | 'company'
  title: string
  path: string
  columns: GridColumn[]
  fields: CrmField[]
  createLabel: string
  defaultValues?: Record<string, string>
}) {
  const [openId, setOpenId] = useOpenId()
  const [rows, setRows] = useState<CrmRow[]>([])
  const [total, setTotal] = useState(0)
  const [limit, setLimit] = useState(20)
  const [offset, setOffset] = useState(0)
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')
  const [mine, setMine] = useState(false)
  const [sort, setSort] = useState('created_at')
  const [dir, setDir] = useState<'ASC' | 'DESC'>('DESC')
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [error, setError] = useState('')
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<CrmRow | null>(null)
  const [pending, setPending] = useState(false)
  const [formError, setFormError] = useState('')

  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(query.trim()), 280)
    return () => window.clearTimeout(t)
  }, [query])

  useEffect(() => {
    setOffset(0)
  }, [debounced, path, mine, sort, dir, limit])

  const load = useCallback(async () => {
    const data = await api<Page<CrmRow>>(
      `${path}${qs({ q: debounced, limit, offset, sort, dir, mine: mine ? 1 : undefined })}`,
    )
    setRows(data.items ?? [])
    setTotal(data.total ?? 0)
  }, [path, debounced, limit, offset, sort, dir, mine])

  useEffect(() => {
    load().catch((e: Error) => setError(e.message))
  }, [load])

  const selected = rows.find((r) => String(r.id) === openId) ?? null

  const toggleSort = (key: string) => {
    if (sort === key) setDir((prev) => (prev === 'ASC' ? 'DESC' : 'ASC'))
    else {
      setSort(key)
      setDir('ASC')
    }
  }

  const toggle = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleAll = () => {
    setSelectedIds((prev) => {
      if (rows.length === 0) return prev
      if (rows.every((row) => prev.has(String(row.id)))) return new Set()
      return new Set(rows.map((row) => String(row.id)))
    })
  }

  const openCreate = () => {
    setEditing(null)
    setFormError('')
    setFormOpen(true)
  }

  const openEdit = (row: CrmRow) => {
    setEditing(row)
    setFormError('')
    setFormOpen(true)
  }

  const submit = async (values: Record<string, string>) => {
    setPending(true)
    setFormError('')
    const body = { ...values }
    delete body.contact_id_label
    delete body.company_id_label
    try {
      if (editing) {
        await api(`${path}/${editing.id}`, { method: 'PATCH', body: JSON.stringify(body) })
      } else {
        await api(path, { method: 'POST', body: JSON.stringify(body) })
      }
      setFormOpen(false)
      setEditing(null)
      await load()
    } catch (e) {
      setFormError(e instanceof Error ? e.message : 'Ошибка сохранения')
    } finally {
      setPending(false)
    }
  }

  const initial = useMemo(() => {
    if (!editing) return defaultValues
    const next: Record<string, string> = {}
    for (const field of fields) {
      next[field.name] = editing[field.name] == null ? '' : String(editing[field.name])
      if (field.name === 'contact_id') next.contact_id_label = String(editing.contact ?? '')
      if (field.name === 'company_id') next.company_id_label = String(editing.company ?? '')
    }
    return next
  }, [editing, fields, defaultValues])

  return (
    <div className="flex flex-col gap-4 pt-1 pb-4 min-h-[calc(100%-2rem)]">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">{title}</h1>
        </div>
        <Button type="button" variant="green" className="h-9 rounded-b24-sm" onClick={openCreate}>
          {createLabel}
        </Button>
      </div>

      <FilterBar query={query} onQuery={setQuery}>
        <FilterToggle active={mine} label="Мои" onOn={() => setMine(true)} onOff={() => setMine(false)} />
      </FilterBar>

      {error ? <p className="text-[13px] text-orange-200">{error}</p> : null}

      <CrmGrid
        columns={columns}
        rows={rows}
        total={total}
        limit={limit}
        offset={offset}
        sort={sort}
        dir={dir}
        selectedIds={selectedIds}
        empty={
          debounced || mine
            ? 'Ничего не найдено — измените фильтр.'
            : 'Пока пусто — создайте запись или импортируйте из Bitrix24.'
        }
        onSort={toggleSort}
        onOffset={setOffset}
        onLimit={(next) => {
          setLimit(next)
          setOffset(0)
        }}
        onOpen={setOpenId}
        onToggle={toggle}
        onToggleAll={toggleAll}
        formatCell={formatCell}
      />

      <CrmFormModal
        open={formOpen}
        title={editing ? 'Изменить' : `Новый ${createLabel.toLowerCase()}`}
        fields={fields}
        initial={initial}
        pending={pending}
        error={formError}
        submitLabel={editing ? 'Сохранить' : 'Создать'}
        onClose={() => setFormOpen(false)}
        onSubmit={(values) => void submit(values)}
      />

      {openId && kind === 'contact' ? (
        <ContactDrawer
          id={openId}
          onClose={() => setOpenId(null)}
          onChanged={() => openEdit(selected ?? { id: openId })}
        />
      ) : null}
      {openId && kind === 'company' ? (
        <CompanyDrawer
          id={openId}
          onClose={() => setOpenId(null)}
          onChanged={() => openEdit(selected ?? { id: openId })}
        />
      ) : null}
    </div>
  )
}

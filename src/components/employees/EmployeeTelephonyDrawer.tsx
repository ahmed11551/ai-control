import { Eye, EyeOff, Phone, Plus, Save, Star, Trash2, X } from 'lucide-react'
import { useCallback, useEffect, useId, useState } from 'react'
import { Button } from '../ui/Button'
import { api } from '../../lib/api'
import { roleLabel } from '../../lib/roles'

type EmpUser = {
  id: string
  email: string
  full_name: string
  role: string
}

type VoxRow = {
  id: string
  label?: string
  username: string
  is_primary?: boolean
  has_password?: boolean
}

type TelephonyData = {
  user: EmpUser
  exolve: {
    bound?: boolean
    sip_login?: string
    has_password?: boolean
    numbers_text?: string
    numbers?: string[]
  }
  vox: VoxRow[]
}

const inputClass =
  'h-11 w-full rounded-[12px] border border-black/10 bg-[#f5f7f9] px-3.5 text-[14px] text-[#1a2332] placeholder:text-[#8b949e] outline-none transition focus:border-b24-cyan focus:bg-white focus:ring-2 focus:ring-b24-cyan/20'
const monoInputClass = `${inputClass} font-mono text-[13px]`
const areaClass =
  'w-full rounded-[12px] border border-black/10 bg-[#f5f7f9] px-3.5 py-2.5 text-[13px] font-mono text-[#1a2332] placeholder:text-[#8b949e] outline-none transition focus:border-b24-cyan focus:bg-white focus:ring-2 focus:ring-b24-cyan/20 resize-y min-h-[96px]'

function initial(name: string) {
  return (name.trim().slice(0, 1) || '?').toUpperCase()
}

export function EmployeeTelephonyDrawer({
  user,
  onClose,
}: {
  user: EmpUser
  onClose: () => void
}) {
  const titleId = useId()
  const [data, setData] = useState<TelephonyData | null>(null)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState('')

  const [sipLogin, setSipLogin] = useState('')
  const [sipPass, setSipPass] = useState('')
  const [sipPassShow, setSipPassShow] = useState(false)
  const [numbersText, setNumbersText] = useState('')

  const [voxForm, setVoxForm] = useState({ label: '', username: '', password: '' })
  const [voxPassShow, setVoxPassShow] = useState(false)
  const [editVox, setEditVox] = useState<string | null>(null)
  const [editDraft, setEditDraft] = useState({ label: '', username: '', password: '' })

  const load = useCallback(async () => {
    setError('')
    try {
      const d = await api<TelephonyData>(`/employees/${user.id}/telephony`)
      setData(d)
      setSipLogin(d.exolve?.sip_login || '')
      setSipPass('')
      setNumbersText(
        d.exolve?.numbers_text || (d.exolve?.numbers ?? []).join('\n') || '',
      )
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось загрузить')
    }
  }, [user.id])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const saveExolve = async () => {
    setBusy('exolve')
    setError('')
    setNote('')
    try {
      await api(`/employees/${user.id}/exolve`, {
        method: 'PUT',
        body: JSON.stringify({
          sip_login: sipLogin.trim(),
          sip_password: sipPass,
          numbers_text: numbersText,
        }),
      })
      setSipPass('')
      setNote('Exolve SIP сохранён')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'ошибка сохранения Exolve')
    } finally {
      setBusy('')
    }
  }

  const clearExolve = async () => {
    if (!window.confirm('Отвязать Exolve SIP у этого сотрудника?')) return
    setBusy('exolve-del')
    setError('')
    setNote('')
    try {
      await api(`/employees/${user.id}/exolve`, { method: 'DELETE' })
      setNote('Exolve отвязан')
      setSipLogin('')
      setSipPass('')
      setNumbersText('')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось отвязать')
    } finally {
      setBusy('')
    }
  }

  const addVox = async () => {
    setBusy('vox-add')
    setError('')
    setNote('')
    try {
      await api(`/employees/${user.id}/vox`, {
        method: 'POST',
        body: JSON.stringify({
          label: voxForm.label.trim(),
          username: voxForm.username.trim(),
          password: voxForm.password,
          is_primary: (data?.vox?.length ?? 0) === 0,
        }),
      })
      setVoxForm({ label: '', username: '', password: '' })
      setNote('Vox-аккаунт добавлен')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось добавить Vox')
    } finally {
      setBusy('')
    }
  }

  const saveVoxEdit = async (vid: string) => {
    setBusy(`vox-${vid}`)
    setError('')
    setNote('')
    try {
      await api(`/employees/${user.id}/vox/${vid}`, {
        method: 'PATCH',
        body: JSON.stringify({
          label: editDraft.label.trim(),
          username: editDraft.username.trim(),
          ...(editDraft.password.trim() ? { password: editDraft.password.trim() } : {}),
        }),
      })
      setEditVox(null)
      setNote('Vox обновлён')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось обновить')
    } finally {
      setBusy('')
    }
  }

  const setPrimary = async (vid: string) => {
    setBusy(`vox-p-${vid}`)
    setError('')
    try {
      await api(`/employees/${user.id}/vox/${vid}`, {
        method: 'PATCH',
        body: JSON.stringify({ is_primary: true }),
      })
      setNote('Основной Vox выбран')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'ошибка')
    } finally {
      setBusy('')
    }
  }

  const removeVox = async (vid: string, label: string) => {
    if (!window.confirm(`Удалить Vox «${label}»?`)) return
    setBusy(`vox-d-${vid}`)
    setError('')
    try {
      await api(`/employees/${user.id}/vox/${vid}`, { method: 'DELETE' })
      setNote('Vox удалён')
      if (editVox === vid) setEditVox(null)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось удалить')
    } finally {
      setBusy('')
    }
  }

  const voxCount = data?.vox?.length ?? 0
  const exolveOn = Boolean(data?.exolve?.bound)

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[4vh] sm:pt-[6vh] px-3 sm:px-4 pb-6 overflow-y-auto">
      <button
        type="button"
        className="fixed inset-0 bg-[#12182a]/60 backdrop-blur-[4px]"
        aria-label="Закрыть"
        onClick={onClose}
      />

      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="relative z-10 w-full max-w-[920px] rounded-[20px] bg-white shadow-[0_28px_90px_rgba(18,24,42,0.45)] border border-black/5 overflow-hidden my-auto"
      >
        <header className="px-5 sm:px-7 pt-5 pb-4 border-b border-black/6 bg-[linear-gradient(135deg,#e8f7fc_0%,#ffffff_48%,#f7fafc_100%)]">
          <div className="flex items-start gap-3.5">
            <div className="size-12 rounded-2xl bg-[linear-gradient(135deg,#2fc6f6,#1a9fd0)] text-white text-[18px] font-semibold inline-flex items-center justify-center shrink-0 shadow-[0_8px_20px_rgba(47,198,246,0.35)]">
              {initial(user.full_name)}
            </div>
            <div className="min-w-0 flex-1">
              <div className="text-[11px] font-semibold uppercase tracking-wide text-b24-cyan-deep">
                Телефония менеджера
              </div>
              <h2 id={titleId} className="mt-0.5 text-[22px] font-semibold text-[#12182a] leading-tight truncate">
                {user.full_name}
              </h2>
              <div className="mt-1.5 flex flex-wrap items-center gap-2 text-[13px] text-[#5b6575]">
                <span className="truncate">{user.email}</span>
                <span className="inline-flex h-6 items-center rounded-full bg-emerald-50 text-emerald-800 ring-1 ring-emerald-200 px-2 text-[11px] font-medium">
                  {roleLabel(user.role)}
                </span>
                <span
                  className={[
                    'inline-flex h-6 items-center rounded-full px-2 text-[11px] font-medium ring-1',
                    exolveOn
                      ? 'bg-sky-50 text-sky-800 ring-sky-200'
                      : 'bg-slate-100 text-slate-600 ring-slate-200',
                  ].join(' ')}
                >
                  Exolve {exolveOn ? '✓' : '—'}
                </span>
                <span className="inline-flex h-6 items-center rounded-full bg-violet-50 text-violet-800 ring-1 ring-violet-200 px-2 text-[11px] font-medium">
                  Vox {voxCount}
                </span>
              </div>
            </div>
            <button
              type="button"
              onClick={onClose}
              className="size-9 shrink-0 inline-flex items-center justify-center rounded-full text-[#5b6575] hover:bg-black/5 hover:text-[#12182a]"
              aria-label="Закрыть"
            >
              <X size={18} />
            </button>
          </div>
        </header>

        <div className="px-5 sm:px-7 py-4 flex flex-col gap-3">
          {error ? (
            <div className="rounded-[12px] bg-rose-50 text-rose-800 ring-1 ring-rose-200 px-3.5 py-2.5 text-[13px]">
              {error}
            </div>
          ) : null}
          {note ? (
            <div className="rounded-[12px] bg-emerald-50 text-emerald-800 ring-1 ring-emerald-200 px-3.5 py-2.5 text-[13px]">
              {note}
            </div>
          ) : null}

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            {/* Exolve */}
            <section className="rounded-[16px] border border-black/8 bg-[#fbfcfd] p-4 sm:p-5 flex flex-col gap-3.5">
              <div className="flex items-center gap-2.5">
                <div className="size-9 rounded-[10px] bg-sky-100 text-sky-800 inline-flex items-center justify-center">
                  <Phone size={17} />
                </div>
                <div className="min-w-0">
                  <div className="text-[15px] font-semibold text-[#12182a]">МТС Exolve</div>
                  <div className="text-[12px] text-[#5b6575]">Один SIP + DID-номера</div>
                </div>
              </div>

              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-semibold text-[#12182a]">SIP login</span>
                <input
                  value={sipLogin}
                  onChange={(e) => setSipLogin(e.target.value)}
                  placeholder="88314…"
                  autoComplete="off"
                  className={monoInputClass}
                />
              </label>

              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-semibold text-[#12182a]">
                  SIP пароль
                  {data?.exolve?.has_password ? (
                    <span className="font-normal text-[#5b6575]"> · пусто = не менять</span>
                  ) : null}
                </span>
                <div className="relative">
                  <input
                    type={sipPassShow ? 'text' : 'password'}
                    value={sipPass}
                    onChange={(e) => setSipPass(e.target.value)}
                    placeholder={data?.exolve?.has_password ? '••••••••' : 'пароль SIP'}
                    autoComplete="new-password"
                    className={`${inputClass} pr-11`}
                  />
                  <button
                    type="button"
                    className="absolute right-3 top-1/2 -translate-y-1/2 text-[#5b6575] hover:text-[#12182a]"
                    onClick={() => setSipPassShow((v) => !v)}
                  >
                    {sipPassShow ? <EyeOff size={16} /> : <Eye size={16} />}
                  </button>
                </div>
              </label>

              <label className="flex flex-col gap-1.5">
                <span className="text-[12px] font-semibold text-[#12182a]">
                  DID-номера <span className="font-normal text-[#5b6575]">по одному в строке</span>
                </span>
                <textarea
                  value={numbersText}
                  onChange={(e) => setNumbersText(e.target.value)}
                  placeholder={'79001234567\n79007654321'}
                  className={areaClass}
                />
              </label>

              <div className="flex flex-wrap gap-2 pt-1 mt-auto">
                <Button
                  type="button"
                  variant="green"
                  className="h-10 rounded-[12px] px-4"
                  disabled={Boolean(busy) || !sipLogin.trim()}
                  onClick={() => void saveExolve()}
                  leftIcon={<Save size={15} />}
                >
                  {busy === 'exolve' ? 'Сохраняем…' : 'Сохранить Exolve'}
                </Button>
                {exolveOn ? (
                  <button
                    type="button"
                    disabled={Boolean(busy)}
                    onClick={() => void clearExolve()}
                    className="h-10 px-3 rounded-[12px] text-[13px] font-medium text-rose-700 hover:bg-rose-50 inline-flex items-center gap-1.5"
                  >
                    <Trash2 size={14} />
                    Отвязать
                  </button>
                ) : null}
              </div>
            </section>

            {/* Vox */}
            <section className="rounded-[16px] border border-black/8 bg-[#fbfcfd] p-4 sm:p-5 flex flex-col gap-3.5">
              <div className="flex items-center gap-2.5">
                <div className="size-9 rounded-[10px] bg-violet-100 text-violet-800 inline-flex items-center justify-center text-[13px] font-bold">
                  Vx
                </div>
                <div className="min-w-0">
                  <div className="text-[15px] font-semibold text-[#12182a]">Voximplant</div>
                  <div className="text-[12px] text-[#5b6575]">Несколько аккаунтов, один основной</div>
                </div>
              </div>

              <div className="flex flex-col gap-2 max-h-[220px] overflow-y-auto b24-scrollbar pr-0.5">
                {voxCount === 0 ? (
                  <div className="rounded-[12px] border border-dashed border-black/12 bg-white px-3.5 py-5 text-center text-[13px] text-[#5b6575]">
                    Пока пусто — добавьте username и пароль ниже
                  </div>
                ) : (
                  (data?.vox ?? []).map((v) => {
                    const editing = editVox === v.id
                    const title = v.label || v.username
                    return (
                      <div
                        key={v.id}
                        className={[
                          'rounded-[12px] border bg-white px-3 py-2.5',
                          v.is_primary ? 'border-amber-300 shadow-[0_0_0_1px_rgba(251,191,36,0.25)]' : 'border-black/8',
                        ].join(' ')}
                      >
                        {editing ? (
                          <div className="flex flex-col gap-2">
                            <input
                              value={editDraft.label}
                              onChange={(e) => setEditDraft((d) => ({ ...d, label: e.target.value }))}
                              placeholder="Метка"
                              className={inputClass}
                            />
                            <input
                              value={editDraft.username}
                              onChange={(e) => setEditDraft((d) => ({ ...d, username: e.target.value }))}
                              placeholder="user@app.account.voximplant.com"
                              className={monoInputClass}
                            />
                            <input
                              type="password"
                              value={editDraft.password}
                              onChange={(e) => setEditDraft((d) => ({ ...d, password: e.target.value }))}
                              placeholder="новый пароль (необязательно)"
                              className={inputClass}
                            />
                            <div className="flex gap-2">
                              <Button
                                type="button"
                                variant="green"
                                className="h-9 rounded-[10px] px-3 text-[12px]"
                                disabled={Boolean(busy)}
                                onClick={() => void saveVoxEdit(v.id)}
                              >
                                Сохранить
                              </Button>
                              <button
                                type="button"
                                className="h-9 px-2 text-[12px] text-[#5b6575]"
                                onClick={() => setEditVox(null)}
                              >
                                Отмена
                              </button>
                            </div>
                          </div>
                        ) : (
                          <div className="flex items-start gap-2">
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-1.5 flex-wrap">
                                <span className="text-[14px] font-semibold text-[#12182a] truncate">
                                  {title}
                                </span>
                                {v.is_primary ? (
                                  <span className="inline-flex items-center gap-0.5 text-[10px] font-semibold text-amber-900 bg-amber-50 ring-1 ring-amber-200 px-1.5 py-0.5 rounded-full">
                                    <Star size={10} fill="currentColor" />
                                    основной
                                  </span>
                                ) : null}
                              </div>
                              <div className="mt-0.5 text-[12px] font-mono text-[#5b6575] truncate">
                                {v.username}
                              </div>
                            </div>
                            <div className="flex items-center gap-0.5 shrink-0">
                              {!v.is_primary ? (
                                <button
                                  type="button"
                                  title="Сделать основным"
                                  disabled={Boolean(busy)}
                                  onClick={() => void setPrimary(v.id)}
                                  className="size-8 inline-flex items-center justify-center rounded-full text-[#5b6575] hover:bg-amber-50 hover:text-amber-700"
                                >
                                  <Star size={14} />
                                </button>
                              ) : null}
                              <button
                                type="button"
                                disabled={Boolean(busy)}
                                onClick={() => {
                                  setEditVox(v.id)
                                  setEditDraft({
                                    label: v.label || '',
                                    username: v.username,
                                    password: '',
                                  })
                                }}
                                className="h-8 px-2 text-[12px] font-semibold text-b24-cyan-deep hover:bg-sky-50 rounded-full"
                              >
                                Изменить
                              </button>
                              <button
                                type="button"
                                disabled={Boolean(busy)}
                                onClick={() => void removeVox(v.id, title)}
                                className="size-8 inline-flex items-center justify-center rounded-full text-rose-600 hover:bg-rose-50"
                              >
                                <Trash2 size={14} />
                              </button>
                            </div>
                          </div>
                        )}
                      </div>
                    )
                  })
                )}
              </div>

              <div className="rounded-[12px] border border-black/8 bg-white p-3 flex flex-col gap-2.5 mt-auto">
                <div className="text-[12px] font-semibold text-[#12182a]">Добавить аккаунт</div>
                <input
                  value={voxForm.label}
                  onChange={(e) => setVoxForm((f) => ({ ...f, label: e.target.value }))}
                  placeholder="Метка (необязательно)"
                  className={inputClass}
                />
                <input
                  value={voxForm.username}
                  onChange={(e) => setVoxForm((f) => ({ ...f, username: e.target.value }))}
                  placeholder="user@app.account.voximplant.com"
                  autoComplete="off"
                  className={monoInputClass}
                />
                <div className="relative">
                  <input
                    type={voxPassShow ? 'text' : 'password'}
                    value={voxForm.password}
                    onChange={(e) => setVoxForm((f) => ({ ...f, password: e.target.value }))}
                    placeholder="пароль"
                    autoComplete="new-password"
                    className={`${inputClass} pr-11`}
                  />
                  <button
                    type="button"
                    className="absolute right-3 top-1/2 -translate-y-1/2 text-[#5b6575] hover:text-[#12182a]"
                    onClick={() => setVoxPassShow((v) => !v)}
                  >
                    {voxPassShow ? <EyeOff size={16} /> : <Eye size={16} />}
                  </button>
                </div>
                <Button
                  type="button"
                  variant="green"
                  className="h-10 rounded-[12px] self-start px-4"
                  disabled={Boolean(busy) || !voxForm.username.trim() || !voxForm.password}
                  onClick={() => void addVox()}
                  leftIcon={<Plus size={15} />}
                >
                  {busy === 'vox-add' ? 'Добавляем…' : 'Добавить Vox'}
                </Button>
              </div>
            </section>
          </div>
        </div>

        <footer className="px-5 sm:px-7 py-3.5 border-t border-black/6 bg-[#f8f9fb] flex justify-end">
          <button
            type="button"
            onClick={onClose}
            className="h-10 px-4 rounded-[12px] text-[13px] font-medium text-[#5b6575] hover:bg-white hover:text-[#12182a] border border-transparent hover:border-black/8"
          >
            Закрыть
          </button>
        </footer>
      </div>
    </div>
  )
}

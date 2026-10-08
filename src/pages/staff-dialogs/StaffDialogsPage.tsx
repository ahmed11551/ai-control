import { MessageSquarePlus, Pencil, Search, X } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  ChatMediaBubble,
  ChatMessageBubble,
  ChatPanel,
  type ChatBubble,
} from '../../components/chat/ChatPanel'
import { PageHeader } from '../../components/layout/PageHeader'
import { Button } from '../../components/ui/Button'
import { api, apiDownload, wsURL } from '../../lib/api'
import { isLogistRole } from '../../lib/roles'
import { useAuth } from '../../store/auth'

type DialogItem = {
  id: string
  deal_id: string
  lead_id?: string
  status: string
  last_body?: string
  last_at?: string
  created_at?: string
  deal_title?: string
  contact?: string
  custom_title?: string
  owner_name?: string
  opened_by_name?: string
  message_count?: number
}

type Candidate = {
  deal_id: string
  title?: string
  lead_id?: string
  owner?: string
  stage?: string
  stage_short?: string
  contact?: string
  has_open_dialog?: boolean
}

type DialogMessage = {
  id: string
  body: string
  created_at: string
  author_id?: string
  author?: string
  author_role?: string
  has_media?: boolean
  media_kind?: string
  media_filename?: string
  media_count?: number
  media_download?: boolean
}

type DialogPayload = {
  dialog: DialogItem & {
    closed_by_name?: string
    closed_at?: string
  }
  candidate: Candidate & {
    deal_id?: string
    owner_department?: string
    contact_phone?: string
  }
  messages: DialogMessage[]
}

function fmtTime(iso?: string) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function dialogDisplayTitle(d: {
  custom_title?: string
  contact?: string
  deal_title?: string
  candidateTitle?: string
}) {
  const custom = String(d.custom_title || '').trim()
  if (custom) return custom
  return (
    String(d.contact || '').trim() ||
    String(d.candidateTitle || '').trim() ||
    String(d.deal_title || '').trim() ||
    'Кандидат'
  )
}

export function StaffDialogsPage() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const myId = String(user?.id || '')
  const canRename =
    isLogistRole(user?.role) ||
    String(user?.role || '').toLowerCase() === 'admin'

  const [tab, setTab] = useState<'open' | 'closed'>('open')
  const [list, setList] = useState<DialogItem[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(params.get('open'))
  const [payload, setPayload] = useState<DialogPayload | null>(null)
  const [draft, setDraft] = useState('')
  const [attachFiles, setAttachFiles] = useState<File[]>([])
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [pickerOpen, setPickerOpen] = useState(false)
  const [candQuery, setCandQuery] = useState('')
  const [candidates, setCandidates] = useState<Candidate[]>([])
  const [candLoading, setCandLoading] = useState(false)
  const [renameOpen, setRenameOpen] = useState(false)
  const [renameDraft, setRenameDraft] = useState('')
  const [renamePending, setRenamePending] = useState(false)

  const loadList = useCallback(async () => {
    const data = await api<{ items: DialogItem[] }>(`/staff-dialogs?status=${tab}`)
    const items = [...(data.items ?? [])].sort((a, b) => {
      const ta = Date.parse(String(a.last_at || '')) || 0
      const tb = Date.parse(String(b.last_at || '')) || 0
      return tb - ta
    })
    setList(items)
  }, [tab])

  const loadDialog = useCallback(async (id: string) => {
    const data = await api<DialogPayload>(`/staff-dialogs/${id}`)
    setPayload(data)
  }, [])

  useEffect(() => {
    void loadList().catch((e: Error) => setError(e.message))
  }, [loadList])

  useEffect(() => {
    const id = params.get('open')
    if (id) setSelectedId(id)
  }, [params])

  useEffect(() => {
    if (!selectedId) {
      setPayload(null)
      return
    }
    void loadDialog(selectedId).catch((e: Error) => setError(e.message))
  }, [selectedId, loadDialog])

  useEffect(() => {
    const ws = new WebSocket(wsURL())
    let timer: number | undefined
    const bump = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        void loadList().catch(() => undefined)
        if (selectedId) void loadDialog(selectedId).catch(() => undefined)
      }, 350)
    }
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(String(ev.data)) as { type?: string; id?: string }
        if (msg.type === 'staff_dialog.updated') bump()
      } catch {
        /* ignore */
      }
    }
    return () => {
      window.clearTimeout(timer)
      ws.close()
    }
  }, [loadList, loadDialog, selectedId])

  const bubbles: ChatBubble[] = useMemo(() => {
    const msgs = [...(payload?.messages ?? [])].sort((a, b) => {
      const ta = Date.parse(String(a.created_at || '')) || 0
      const tb = Date.parse(String(b.created_at || '')) || 0
      if (ta !== tb) return ta - tb
      return String(a.id).localeCompare(String(b.id))
    })
    return msgs.map((m) => ({
      id: m.id,
      dir: String(m.author_id || '') === myId ? 'out' : 'in',
      body: m.body,
      at: fmtTime(m.created_at),
      author: m.author || undefined,
      has_media: Boolean(m.has_media),
      media_kind: m.media_kind,
      media_filename: m.media_filename,
      media_count: Number(m.media_count || 0),
      media_download: Boolean(m.media_download ?? m.has_media),
      media_url: `/api/staff-dialogs/messages/${m.id}/media`,
    }))
  }, [payload?.messages, myId])

  const selectDialog = (id: string) => {
    setSelectedId(id)
    setError('')
    setDraft('')
    setAttachFiles([])
    const next = new URLSearchParams(params)
    next.set('open', id)
    setParams(next, { replace: true })
  }

  const openPicker = async () => {
    setPickerOpen(true)
    setCandQuery('')
    setCandLoading(true)
    setError('')
    try {
      const data = await api<{ items: Candidate[] }>('/staff-dialogs/candidates')
      setCandidates(data.items ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось загрузить кандидатов')
    } finally {
      setCandLoading(false)
    }
  }

  const searchCandidates = async (q: string) => {
    setCandQuery(q)
    setCandLoading(true)
    try {
      const data = await api<{ items: Candidate[] }>(
        `/staff-dialogs/candidates?q=${encodeURIComponent(q.trim())}`,
      )
      setCandidates(data.items ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'поиск не удался')
    } finally {
      setCandLoading(false)
    }
  }

  const startDialog = async (dealId: string) => {
    setPending(true)
    setError('')
    try {
      const res = await api<DialogPayload & { created?: boolean }>(`/staff-dialogs`, {
        method: 'POST',
        body: JSON.stringify({ deal_id: dealId }),
      })
      setPickerOpen(false)
      setTab('open')
      await loadList()
      const id = String(res.dialog?.id || '')
      if (id) {
        setPayload(res)
        selectDialog(id)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось открыть диалог')
    } finally {
      setPending(false)
    }
  }

  const send = async () => {
    if (!selectedId || payload?.dialog?.status !== 'open') return
    if (!draft.trim() && attachFiles.length === 0) return
    setPending(true)
    setError('')
    try {
      let res: DialogPayload
      if (attachFiles.length > 0) {
        const fd = new FormData()
        fd.append('body', draft.trim())
        for (const f of attachFiles) fd.append('file', f)
        res = await api<DialogPayload>(`/staff-dialogs/${selectedId}/messages`, {
          method: 'POST',
          body: fd,
        })
      } else {
        res = await api<DialogPayload>(`/staff-dialogs/${selectedId}/messages`, {
          method: 'POST',
          body: JSON.stringify({ body: draft.trim() }),
        })
      }
      setDraft('')
      setAttachFiles([])
      setPayload((prev) =>
        prev
          ? {
              ...prev,
              messages: res.messages ?? prev.messages,
              dialog: { ...prev.dialog, ...(res.dialog || {}) },
            }
          : prev,
      )
      void loadList()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не отправилось')
    } finally {
      setPending(false)
    }
  }

  const closeDialog = async () => {
    if (!selectedId || payload?.dialog?.status !== 'open') return
    if (!window.confirm('Завершить диалог? Писать больше будет нельзя.')) return
    setPending(true)
    setError('')
    try {
      const res = await api<{ dialog: DialogItem }>(`/staff-dialogs/${selectedId}/close`, {
        method: 'POST',
        body: '{}',
      })
      setPayload((prev) => (prev ? { ...prev, dialog: { ...prev.dialog, ...res.dialog } } : prev))
      void loadList()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось завершить')
    } finally {
      setPending(false)
    }
  }

  const openRename = () => {
    if (!payload?.dialog || !canRename) return
    setRenameDraft(
      String(payload.dialog.custom_title || '').trim() ||
        dialogDisplayTitle({
          contact: payload.candidate?.contact,
          deal_title: payload.dialog.deal_title,
          candidateTitle: payload.candidate?.title,
        }),
    )
    setRenameOpen(true)
    setError('')
  }

  const saveRename = async () => {
    if (!selectedId || !canRename) return
    setRenamePending(true)
    setError('')
    try {
      const res = await api<{ dialog: DialogItem }>(`/staff-dialogs/${selectedId}`, {
        method: 'PATCH',
        body: JSON.stringify({ title: renameDraft.trim() }),
      })
      setPayload((prev) =>
        prev ? { ...prev, dialog: { ...prev.dialog, ...(res.dialog || {}) } } : prev,
      )
      setList((prev) =>
        prev.map((d) =>
          d.id === selectedId ? { ...d, custom_title: res.dialog?.custom_title || '' } : d,
        ),
      )
      setRenameOpen(false)
      void loadList()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось переименовать')
    } finally {
      setRenamePending(false)
    }
  }

  const openLeadCard = () => {
    const leadId = String(payload?.candidate?.lead_id || payload?.dialog?.lead_id || '').trim()
    if (!leadId) {
      setError('Нет привязанного лида у кандидата')
      return
    }
    const role = String(user?.role || '').toLowerCase()
    if (isLogistRole(role)) {
      const dealId = String(payload?.dialog?.deal_id || payload?.candidate?.deal_id || '').trim()
      const q = new URLSearchParams()
      if (dealId) q.set('open', dealId)
      q.set('lead', leadId)
      navigate(`/logistics?${q.toString()}`)
      return
    }
    navigate(`/crm/leads?open=${encodeURIComponent(leadId)}`)
  }

  const filteredCandidates = candidates

  return (
    <div className="h-full min-h-0 flex flex-col gap-3 py-3">
      <PageHeader
        title="Диалоги"
        subtitle="Один общий чат логист ↔ менеджер по кандидату. Список — по времени последнего сообщения."
        actions={
          <Button
            type="button"
            variant="green"
            className="h-9 rounded-[10px]"
            leftIcon={<MessageSquarePlus size={16} />}
            onClick={() => void openPicker()}
          >
            Новый диалог
          </Button>
        }
      />

      {error ? <p className="text-[13px] text-red-600 px-1">{error}</p> : null}

      <div className="flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-[320px_minmax(0,1fr)] gap-3">
        <aside className="rounded-[14px] border border-black/8 bg-white overflow-hidden flex flex-col min-h-0">
          <div className="flex border-b border-black/6">
            {(['open', 'closed'] as const).map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => setTab(t)}
                className={[
                  'flex-1 h-10 text-[13px] font-medium',
                  tab === t ? 'text-b24-cyan border-b-2 border-b24-cyan' : 'text-b24-text-muted',
                ].join(' ')}
              >
                {t === 'open' ? 'Открытые' : 'Завершённые'}
              </button>
            ))}
          </div>
          <div className="flex-1 overflow-y-auto b24-scrollbar">
            {list.length === 0 ? (
              <p className="p-4 text-[13px] text-b24-text-muted">
                Пока нет переписок. Откройте диалог из карточки кандидата и напишите первое сообщение.
              </p>
            ) : (
              list.map((d) => {
                const active = d.id === selectedId
                const title = dialogDisplayTitle(d)
                return (
                  <button
                    key={d.id}
                    type="button"
                    onClick={() => selectDialog(d.id)}
                    className={[
                      'w-full text-left px-3.5 py-3 border-b border-black/5 hover:bg-[#f5f8fa]',
                      active ? 'bg-[#e8f8fe]' : '',
                    ].join(' ')}
                  >
                    <div className="flex items-baseline justify-between gap-2">
                      <div className="text-[14px] font-semibold text-b24-text-dark truncate">
                        {title}
                      </div>
                      <div className="shrink-0 text-[11px] text-b24-text-muted tabular-nums">
                        {fmtTime(d.last_at)}
                      </div>
                    </div>
                    <div className="mt-0.5 text-[12px] text-b24-text-muted truncate">
                      {[d.owner_name, d.message_count ? `${d.message_count} сообщ.` : '']
                        .filter(Boolean)
                        .join(' · ') || '—'}
                    </div>
                    {d.last_body ? (
                      <div className="mt-1 text-[12px] text-b24-text-dark/80 truncate">{d.last_body}</div>
                    ) : null}
                  </button>
                )
              })
            )}
          </div>
        </aside>

        <section className="rounded-[14px] border border-black/8 bg-[#eef2f4] overflow-hidden min-h-[420px] flex flex-col">
          {!selectedId || !payload ? (
            <div className="flex-1 grid place-items-center text-[14px] text-b24-text-muted px-6 text-center">
              Выберите диалог или создайте новый — сначала обязательно укажите кандидата
            </div>
          ) : (
            <>
              <div className="shrink-0 bg-white border-b border-black/8 px-4 py-3 flex flex-wrap items-center gap-3 justify-between">
                <div className="min-w-0">
                  <div className="text-[11px] uppercase tracking-wide text-b24-text-muted">Чат</div>
                  <div className="flex items-center gap-1.5 min-w-0">
                    <div className="text-[16px] font-semibold text-b24-text-dark truncate">
                      {dialogDisplayTitle({
                        custom_title: payload.dialog.custom_title,
                        contact: payload.candidate?.contact,
                        deal_title: payload.dialog.deal_title,
                        candidateTitle: payload.candidate?.title,
                      })}
                    </div>
                    {canRename ? (
                      <button
                        type="button"
                        title="Переименовать"
                        className="shrink-0 size-7 rounded-lg text-b24-text-muted hover:bg-black/5 hover:text-b24-text-dark inline-flex items-center justify-center"
                        onClick={openRename}
                      >
                        <Pencil size={14} />
                      </button>
                    ) : null}
                  </div>
                  <div className="mt-0.5 text-[12px] text-b24-text-muted">
                    {[
                      payload.candidate?.title &&
                      payload.dialog.custom_title &&
                      payload.candidate.title !== payload.dialog.custom_title
                        ? payload.candidate.title
                        : null,
                      payload.candidate?.stage_short || payload.candidate?.stage,
                      payload.candidate?.owner || payload.dialog.owner_name,
                      payload.candidate?.owner_department,
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                  </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <Button
                    type="button"
                    variant="filter"
                    className="h-8 rounded-[10px] text-[12px]"
                    onClick={openLeadCard}
                  >
                    Открыть карточку
                  </Button>
                  {payload.dialog.status === 'open' ? (
                    <Button
                      type="button"
                      variant="filter"
                      className="h-8 rounded-[10px] text-[12px] text-red-600"
                      disabled={pending}
                      onClick={() => void closeDialog()}
                    >
                      Завершить диалог
                    </Button>
                  ) : (
                    <span className="text-[12px] text-b24-text-muted">
                      Завершён{payload.dialog.closed_by_name ? ` · ${payload.dialog.closed_by_name}` : ''}
                    </span>
                  )}
                </div>
              </div>

              <div className="flex-1 min-h-0">
                <ChatPanel
                  title="Переписка"
                  subtitle={
                    payload.dialog.status === 'open'
                      ? 'Логист и менеджер'
                      : 'Диалог завершён — только чтение'
                  }
                  messages={bubbles}
                  draft={draft}
                  onDraftChange={setDraft}
                  onSend={() => void send()}
                  pending={pending}
                  error={error}
                  composerDisabled={payload.dialog.status !== 'open'}
                  allowAttach={payload.dialog.status === 'open'}
                  attachFiles={attachFiles}
                  onAttachFiles={setAttachFiles}
                  placeholder={
                    payload.dialog.status === 'open'
                      ? 'Написать сообщение или прикрепить файл…'
                      : 'Диалог завершён'
                  }
                  emptyText="Напишите первый вопрос по кандидату"
                  className="h-full border-0 rounded-none shadow-none"
                  renderMessage={(msg) =>
                    msg.has_media ? (
                      <ChatMediaBubble
                        key={msg.id}
                        msg={msg}
                        managerMode={false}
                        onDownload={(id) => {
                          const count = Number(msg.media_count || 0)
                          const name =
                            count > 1 ? 'attachments.zip' : msg.media_filename || 'file.bin'
                          void apiDownload(`/staff-dialogs/messages/${id}/media`, name).catch(
                            (e: Error) => setError(e.message),
                          )
                        }}
                      />
                    ) : (
                      <ChatMessageBubble
                        key={msg.id}
                        dir={msg.dir}
                        body={msg.body}
                        at={msg.at}
                        author={msg.author}
                      />
                    )
                  }
                />
              </div>
            </>
          )}
        </section>
      </div>

      {pickerOpen ? (
        <div className="fixed inset-0 z-[140] flex items-center justify-center px-4">
          <button
            type="button"
            className="absolute inset-0 bg-[#1a1545]/45"
            aria-label="Закрыть"
            onClick={() => setPickerOpen(false)}
          />
          <div className="relative z-10 w-full max-w-[520px] rounded-[16px] bg-white shadow-2xl border border-black/8 overflow-hidden">
            <div className="flex items-center justify-between px-4 py-3 border-b border-black/6">
              <div>
                <div className="text-[15px] font-semibold text-b24-text-dark">Новый диалог</div>
                <div className="text-[12px] text-b24-text-muted">
                  Обязательно выберите кандидата — вопрос будет привязан к его карточке
                </div>
              </div>
              <button
                type="button"
                className="size-8 rounded-full hover:bg-black/5 inline-flex items-center justify-center"
                onClick={() => setPickerOpen(false)}
              >
                <X size={18} />
              </button>
            </div>
            <div className="px-4 py-3 border-b border-black/6">
              <div className="relative">
                <Search
                  size={15}
                  className="absolute left-3 top-1/2 -translate-y-1/2 text-b24-text-muted"
                />
                <input
                  value={candQuery}
                  onChange={(e) => void searchCandidates(e.target.value)}
                  placeholder="Поиск кандидата…"
                  className="h-10 w-full rounded-[10px] border border-black/10 bg-white pl-9 pr-3 text-[13px] text-b24-text-dark placeholder:text-b24-text-muted outline-none focus:border-b24-cyan"
                />
              </div>
            </div>
            <div className="max-h-[360px] overflow-y-auto b24-scrollbar">
              {candLoading ? (
                <p className="p-4 text-[13px] text-b24-text-muted">Загрузка…</p>
              ) : filteredCandidates.length === 0 ? (
                <p className="p-4 text-[13px] text-b24-text-muted">Никого не найдено</p>
              ) : (
                filteredCandidates.map((c) => (
                  <button
                    key={c.deal_id}
                    type="button"
                    disabled={pending}
                    onClick={() => void startDialog(c.deal_id)}
                    className="w-full text-left px-4 py-3 border-b border-black/5 hover:bg-[#f5f8fa] disabled:opacity-50"
                  >
                    <div className="text-[14px] font-semibold text-b24-text-dark truncate">
                      {c.title || c.contact || 'Кандидат'}
                    </div>
                    <div className="mt-0.5 text-[12px] text-b24-text-muted truncate">
                      {[c.stage_short || c.stage, c.owner, c.has_open_dialog ? 'есть открытый диалог' : '']
                        .filter(Boolean)
                        .join(' · ')}
                    </div>
                  </button>
                ))
              )}
            </div>
          </div>
        </div>
      ) : null}

      {renameOpen ? (
        <div className="fixed inset-0 z-[140] flex items-center justify-center px-4">
          <button
            type="button"
            className="absolute inset-0 bg-[#1a1545]/45"
            aria-label="Закрыть"
            onClick={() => setRenameOpen(false)}
          />
          <div className="relative z-10 w-full max-w-[420px] rounded-[16px] bg-white shadow-2xl border border-black/8 overflow-hidden">
            <div className="flex items-center justify-between px-4 py-3 border-b border-black/6">
              <div>
                <div className="text-[15px] font-semibold text-b24-text-dark">Название чата</div>
                <div className="text-[12px] text-b24-text-muted">
                  Только у вас в списке диалогов. Пустое — вернуть имя кандидата.
                </div>
              </div>
              <button
                type="button"
                className="size-8 rounded-full hover:bg-black/5 inline-flex items-center justify-center"
                onClick={() => setRenameOpen(false)}
              >
                <X size={18} />
              </button>
            </div>
            <form
              className="px-4 py-4 flex flex-col gap-3"
              onSubmit={(e) => {
                e.preventDefault()
                void saveRename()
              }}
            >
              <input
                autoFocus
                value={renameDraft}
                onChange={(e) => setRenameDraft(e.target.value)}
                maxLength={120}
                placeholder="Например: Билет · Иванов"
                className="h-10 w-full rounded-[10px] border border-black/10 bg-white px-3 text-[13px] text-b24-text-dark placeholder:text-b24-text-muted outline-none focus:border-b24-cyan"
              />
              <div className="flex justify-end gap-2">
                <Button
                  type="button"
                  variant="filter"
                  className="h-9 rounded-[10px]"
                  disabled={renamePending}
                  onClick={() => setRenameOpen(false)}
                >
                  Отмена
                </Button>
                <Button
                  type="submit"
                  variant="green"
                  className="h-9 rounded-[10px]"
                  disabled={renamePending}
                >
                  {renamePending ? 'Сохраняем…' : 'Сохранить'}
                </Button>
              </div>
            </form>
          </div>
        </div>
      ) : null}
    </div>
  )
}

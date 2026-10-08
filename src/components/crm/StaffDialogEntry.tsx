import { MessageCircle, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  ChatMediaBubble,
  ChatMessageBubble,
  ChatPanel,
  type ChatBubble,
} from '../chat/ChatPanel'
import { api, apiDownload, wsURL } from '../../lib/api'
import { useAuth } from '../../store/auth'
import { Button } from '../ui/Button'

type DialogMessage = {
  id: string
  body: string
  created_at: string
  author_id?: string
  author?: string
  has_media?: boolean
  media_kind?: string
  media_filename?: string
  media_count?: number
  media_download?: boolean
}

type DialogPayload = {
  dialog: { id?: string; status?: string; deal_title?: string; custom_title?: string }
  candidate?: { contact?: string; title?: string }
  messages?: DialogMessage[]
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

/** Блок «Диалог по кандидату» в карточке лида — переписка открывается окном. */
export function StaffDialogEntry({ dealId }: { dealId?: string }) {
  const { user } = useAuth()
  const role = String(user?.role || '').toLowerCase()
  const allowed = ['admin', 'logist', 'night_logist', 'manager', 'sales', 'rop'].includes(role)
  const myId = String(user?.id || '')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [dialogId, setDialogId] = useState('')
  const [payload, setPayload] = useState<DialogPayload | null>(null)
  const [draft, setDraft] = useState('')
  const [attachFiles, setAttachFiles] = useState<File[]>([])
  const [pos, setPos] = useState({ x: 24, y: 72 })
  const drag = useRef<{ dx: number; dy: number } | null>(null)

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

  useEffect(() => {
    if (!open || !dialogId) return
    let closed = false
    let ws: WebSocket | null = null
    let retry: number | undefined
    let timer: number | undefined

    const reload = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        void api<DialogPayload>(`/staff-dialogs/${dialogId}`)
          .then(data => {
            if (!closed) setPayload(data)
          })
          .catch(() => undefined)
      }, 200)
    }

    const connect = () => {
      if (closed) return
      ws = new WebSocket(wsURL())
      ws.onmessage = ev => {
        try {
          const msg = JSON.parse(String(ev.data)) as {
            type?: string
            payload?: { id?: string }
          }
          if (msg.type !== 'staff_dialog.updated') return
          const id = String(msg.payload?.id || '')
          if (id && id !== dialogId) return
          reload()
        } catch {
          /* ignore */
        }
      }
      ws.onclose = () => {
        if (closed) return
        retry = window.setTimeout(connect, 3000)
      }
    }
    connect()

    return () => {
      closed = true
      window.clearTimeout(timer)
      if (retry) window.clearTimeout(retry)
      ws?.close()
    }
  }, [open, dialogId])

  if (!allowed || !dealId) return null

  const openOrCreate = async () => {
    setPending(true)
    setError('')
    try {
      const res = await api<DialogPayload & { id?: string }>('/staff-dialogs', {
        method: 'POST',
        body: JSON.stringify({ deal_id: dealId }),
      })
      const id = String(res.dialog?.id || res.id || '')
      if (!id) throw new Error('Не удалось открыть диалог')
      const full = res.messages ? res : await api<DialogPayload>(`/staff-dialogs/${id}`)
      setDialogId(id)
      setPayload(full)
      setDraft('')
      setAttachFiles([])
      setPos({
        x: Math.max(16, window.innerWidth - 460),
        y: Math.max(16, window.innerHeight - 640),
      })
      setOpen(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Ошибка')
    } finally {
      setPending(false)
    }
  }

  const send = async () => {
    if (!dialogId || payload?.dialog?.status !== 'open') return
    if (!draft.trim() && attachFiles.length === 0) return
    setPending(true)
    setError('')
    try {
      let res: DialogPayload
      if (attachFiles.length > 0) {
        const fd = new FormData()
        fd.append('body', draft.trim())
        for (const f of attachFiles) fd.append('file', f)
        res = await api<DialogPayload>(`/staff-dialogs/${dialogId}/messages`, {
          method: 'POST',
          body: fd,
        })
      } else {
        res = await api<DialogPayload>(`/staff-dialogs/${dialogId}/messages`, {
          method: 'POST',
          body: JSON.stringify({ body: draft.trim() }),
        })
      }
      setDraft('')
      setAttachFiles([])
      setPayload(res)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось отправить')
    } finally {
      setPending(false)
    }
  }

  const title =
    payload?.dialog?.custom_title ||
    payload?.candidate?.contact ||
    payload?.candidate?.title ||
    payload?.dialog?.deal_title ||
    'Переписка'

  return (
    <>
      <div className="mt-3 rounded-[10px] border border-black/[0.07] bg-[#f7f8fa] px-3.5 py-3">
        <div className="flex items-start gap-2.5">
          <div className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg bg-white border border-black/[0.06]">
            <MessageCircle size={16} className="text-b24-text-muted" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-[13px] font-semibold text-b24-text-dark">Диалог по кандидату</div>
            <p className="mt-0.5 text-[12px] text-b24-text-muted leading-snug">
              Внутренний чат логист ↔ менеджер. Кандидат уже выбран.
            </p>
            {error && !open ? <p className="mt-1.5 text-[12px] text-red-500">{error}</p> : null}
            <Button
              type="button"
              variant="filter"
              className="mt-2 rounded-b24-sm"
              disabled={pending}
              onClick={() => void openOrCreate()}
            >
              {pending && !open ? 'Открываем…' : 'Открыть диалог'}
            </Button>
          </div>
        </div>
      </div>
      {open
        ? createPortal(
        <div
          className="fixed z-[200] flex h-[min(560px,calc(100dvh-24px))] w-[min(420px,calc(100vw-16px))] flex-col overflow-hidden rounded-[14px] border border-black/10 bg-white shadow-[0_16px_48px_rgba(15,23,42,0.28)]"
          style={{ left: pos.x, top: pos.y }}
        >
            <div
              className="flex cursor-grab items-center justify-between gap-3 border-b border-black/8 px-4 py-3 active:cursor-grabbing"
              onPointerDown={(e) => {
                if ((e.target as HTMLElement).closest('button')) return
                drag.current = { dx: e.clientX - pos.x, dy: e.clientY - pos.y }
                e.currentTarget.setPointerCapture(e.pointerId)
              }}
              onPointerMove={(e) => {
                if (!drag.current) return
                setPos({
                  x: e.clientX - drag.current.dx,
                  y: e.clientY - drag.current.dy,
                })
              }}
              onPointerUp={() => {
                drag.current = null
              }}
            >
              <div className="min-w-0">
                <div className="text-[11px] uppercase tracking-wide text-b24-text-muted">Диалог</div>
                <div className="truncate text-[16px] font-semibold text-b24-text-dark">{title}</div>
              </div>
              <button
                type="button"
                className="inline-flex size-8 items-center justify-center rounded-lg text-b24-text-muted hover:bg-black/5"
                onClick={() => setOpen(false)}
                aria-label="Закрыть"
              >
                <X size={18} />
              </button>
            </div>
            <div className="min-h-0 flex-1 bg-[#eef2f4]">
              <ChatPanel
                title="Переписка"
                subtitle={payload?.dialog?.status === 'open' ? 'Логист и менеджер' : 'Только чтение'}
                messages={bubbles}
                draft={draft}
                onDraftChange={setDraft}
                onSend={() => void send()}
                pending={pending}
                error={error}
                composerDisabled={payload?.dialog?.status !== 'open'}
                allowAttach={payload?.dialog?.status === 'open'}
                attachFiles={attachFiles}
                onAttachFiles={setAttachFiles}
                placeholder="Написать сообщение или прикрепить файл…"
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
                        const name = count > 1 ? 'attachments.zip' : msg.media_filename || 'file.bin'
                        void apiDownload(`/staff-dialogs/messages/${id}/media`, name)
                      }}
                    />
                  ) : (
                    <ChatMessageBubble key={msg.id} dir={msg.dir} body={msg.body} at={msg.at} author={msg.author} />
                  )
                }
              />
            </div>
        </div>,
        document.body,
      )
        : null}
    </>
  )
}

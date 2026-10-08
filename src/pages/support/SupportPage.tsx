import { Paperclip, Send } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { PageHeader, PageShell } from '../../components/layout/PageHeader'
import { api, apiDownload, token, wsURL } from '../../lib/api'
import { homePathForRole } from '../../data/nav'
import { useAuth } from '../../store/auth'

type SupportFile = { id: string; name: string; mime: string; kind: string }

type SupportMessage = {
  id: string
  body: string
  created_at: string
  author_id: string
  author: string
  mine: boolean
  files: SupportFile[]
}

type SupportThread = {
  id: string
  user_id?: string
  name?: string
  last_body?: string
  last_at?: string
  unread?: number
}

function canUseSupport(role?: string) {
  const r = String(role || '').toLowerCase()
  return r === 'admin' || r === 'rop' || r === 'manager' || r === 'sales'
}

function fmtTime(iso?: string) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function AuthImage({ id, alt }: { id: string; alt: string }) {
  const [url, setUrl] = useState('')
  useEffect(() => {
    let revoke = ''
    let dead = false
    const headers = new Headers()
    const t = token()
    if (t) headers.set('Authorization', `Bearer ${t}`)
    fetch(`/api/support/files/${id}`, { headers })
      .then((res) => (res.ok ? res.blob() : Promise.reject(new Error('file'))))
      .then((blob) => {
        if (dead) return
        revoke = URL.createObjectURL(blob)
        setUrl(revoke)
      })
      .catch(() => {})
    return () => {
      dead = true
      if (revoke) URL.revokeObjectURL(revoke)
    }
  }, [id])
  if (!url) return <div className="h-24 w-32 rounded-[10px] bg-black/5" />
  return <img src={url} alt={alt} className="max-h-48 max-w-full rounded-[10px]" />
}

export function SupportPage() {
  const { user } = useAuth()
  const admin = String(user?.role || '').toLowerCase() === 'admin'
  const [threads, setThreads] = useState<SupportThread[]>([])
  const [selected, setSelected] = useState('')
  const [peerName, setPeerName] = useState('')
  const [messages, setMessages] = useState<SupportMessage[]>([])
  const [draft, setDraft] = useState('')
  const [files, setFiles] = useState<File[]>([])
  const [error, setError] = useState('')
  const [sending, setSending] = useState(false)
  const bottom = useRef<HTMLDivElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const selectedRef = useRef('')

  async function loadMine() {
    const res = await api<{ thread?: { id: string } | null; messages?: SupportMessage[] }>('/support')
    setMessages(res.messages || [])
    setSelected(res.thread?.id || '')
  }

  async function loadAdminList(keep?: string) {
    const res = await api<{ threads?: SupportThread[] }>('/support')
    const items = res.threads || []
    setThreads(items)
    const next = keep && items.some((item) => item.id === keep) ? keep : items[0]?.id || ''
    if (next) await openThread(next)
    else {
      setSelected('')
      setMessages([])
      setPeerName('')
    }
  }

  async function openThread(id: string) {
    const res = await api<{ thread?: SupportThread; messages?: SupportMessage[] }>(`/support/threads/${id}`)
    setSelected(id)
    setPeerName(res.thread?.name || 'Сотрудник')
    setMessages(res.messages || [])
    setThreads((prev) => prev.map((item) => (item.id === id ? { ...item, unread: 0 } : item)))
  }

  useEffect(() => {
    if (!canUseSupport(user?.role)) return
    void (admin ? loadAdminList() : loadMine()).catch((e) => {
      setError(e instanceof Error ? e.message : 'Не удалось открыть')
    })
  }, [user?.role, admin])

  useEffect(() => {
    selectedRef.current = selected
  }, [selected])

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: 'end' })
  }, [messages])

  useEffect(() => {
    if (!canUseSupport(user?.role)) return
    let closed = false
    let ws: WebSocket | null = null
    const connect = () => {
      if (closed) return
      ws = new WebSocket(wsURL())
      ws.onmessage = (ev) => {
        try {
          const msg = JSON.parse(String(ev.data)) as { type?: string; payload?: { thread_id?: string } }
          if (msg.type !== 'support.updated') return
          const id = String(msg.payload?.thread_id || '')
          if (admin) {
            void api<{ threads?: SupportThread[] }>('/support').then((res) => {
              setThreads(res.threads || [])
              if (id && id === selectedRef.current) void openThread(id)
            })
          } else void loadMine()
        } catch {
          /* ignore malformed frames */
        }
      }
      ws.onclose = () => {
        if (!closed) window.setTimeout(connect, 2000)
      }
    }
    connect()
    return () => {
      closed = true
      ws?.close()
    }
  }, [user?.role, admin])

  async function send() {
    if (!draft.trim() && files.length === 0) return
    if (admin && !selected) return
    setSending(true)
    setError('')
    try {
      let res: { thread_id?: string; messages?: SupportMessage[] }
      if (files.length > 0) {
        const fd = new FormData()
        fd.append('body', draft.trim())
        if (admin) fd.append('thread_id', selected)
        for (const file of files) fd.append('file', file)
        res = await api('/support/messages', { method: 'POST', body: fd })
      } else {
        res = await api('/support/messages', {
          method: 'POST',
          body: JSON.stringify({ body: draft.trim(), thread_id: admin ? selected : '' }),
        })
      }
      setDraft('')
      setFiles([])
      setMessages(res.messages || [])
      if (res.thread_id) setSelected(res.thread_id)
      if (admin) {
        const list = await api<{ threads?: SupportThread[] }>('/support')
        setThreads(list.threads || [])
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось отправить')
    } finally {
      setSending(false)
    }
  }

  if (!canUseSupport(user?.role)) return <Navigate to={homePathForRole(user?.role)} replace />

  const composer = (
    <div className="border-t border-black/6 bg-white/80 p-3">
      {files.length > 0 ? (
        <div className="mb-2 flex flex-wrap gap-1.5">
          {files.map((file) => (
            <button
              key={`${file.name}-${file.size}`}
              type="button"
              className="rounded-full bg-[#f5f7f9] px-2 py-1 text-[12px] text-b24-text-dark"
              onClick={() => setFiles((prev) => prev.filter((item) => item !== file))}
            >
              {file.name} ×
            </button>
          ))}
        </div>
      ) : null}
      <div className="flex items-end gap-2">
        <button
          type="button"
          className="grid size-10 shrink-0 place-items-center rounded-[10px] border border-black/10 text-b24-text-dark"
          onClick={() => fileRef.current?.click()}
          aria-label="Прикрепить фото или файл"
        >
          <Paperclip size={16} />
        </button>
        <input
          ref={fileRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            const picked = Array.from(e.target.files || [])
            setFiles((prev) => [...prev, ...picked].slice(0, 8))
            e.target.value = ''
          }}
        />
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          rows={2}
          placeholder="Опишите, что случилось"
          className="min-h-10 flex-1 resize-none rounded-[10px] border border-black/10 bg-white px-3 py-2 text-[14px] text-b24-text-dark outline-none"
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              void send()
            }
          }}
        />
        <button
          type="button"
          disabled={sending || (!draft.trim() && files.length === 0) || (admin && !selected)}
          onClick={() => void send()}
          className="grid size-10 shrink-0 place-items-center rounded-[10px] bg-[#1f6feb] text-white disabled:opacity-50"
          aria-label="Отправить"
        >
          <Send size={16} />
        </button>
      </div>
    </div>
  )

  const transcript = (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="min-h-[280px] flex-1 space-y-2 overflow-auto px-4 py-3">
        {messages.length === 0 ? (
          <p className="py-8 text-center text-[13px] text-b24-text-muted">
            {admin ? 'Выберите сотрудника слева' : 'Напишите, если нашли ошибку. Сообщение увидит администратор.'}
          </p>
        ) : (
          messages.map((msg) => (
            <div key={msg.id} className={msg.mine ? 'flex justify-end' : 'flex justify-start'}>
              <div
                className={[
                  'max-w-[80%] rounded-[14px] px-3 py-2 text-[14px]',
                  msg.mine ? 'bg-[#1f6feb] text-white' : 'bg-[#f5f7f9] text-b24-text-dark',
                ].join(' ')}
              >
                {!msg.mine && admin ? <div className="mb-0.5 text-[11px] opacity-70">{msg.author || peerName}</div> : null}
                {msg.body ? <div className="whitespace-pre-wrap">{msg.body}</div> : null}
                {msg.files?.map((file) =>
                  file.kind === 'image' ? (
                    <div key={file.id} className="mt-2">
                      <AuthImage id={file.id} alt={file.name || 'Фото'} />
                    </div>
                  ) : (
                    <button
                      key={file.id}
                      type="button"
                      className="mt-2 block text-left text-[13px] underline"
                      onClick={() => void apiDownload(`/support/files/${file.id}`, file.name || 'file')}
                    >
                      {file.name || 'Файл'}
                    </button>
                  ),
                )}
                <div className={['mt-1 text-[11px]', msg.mine ? 'text-white/70' : 'text-b24-text-muted'].join(' ')}>
                  {fmtTime(msg.created_at)}
                </div>
              </div>
            </div>
          ))
        )}
        <div ref={bottom} />
      </div>
      {composer}
    </div>
  )

  return (
    <PageShell className="min-h-0">
      <PageHeader
        title="Техподдержка"
        subtitle={admin ? 'Сообщения от менеджеров и РОПов' : 'Нашли ошибку — напишите сюда. Ответит администратор.'}
      />
      {error ? <p className="text-[13px] text-orange-200">{error}</p> : null}
      {admin ? (
        <div className="grid min-h-[520px] gap-3 lg:grid-cols-[280px_minmax(0,1fr)]">
          <div className="b24-glass-panel overflow-auto">
            {threads.length === 0 ? (
              <p className="px-4 py-8 text-center text-[13px] text-b24-text-muted">Пока никто не писал</p>
            ) : (
              threads.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => void openThread(item.id)}
                  className={[
                    'flex w-full items-start gap-2 border-b border-black/5 px-4 py-3 text-left',
                    item.id === selected ? 'bg-white' : 'hover:bg-white/60',
                  ].join(' ')}
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[14px] font-medium text-b24-text-dark">{item.name || 'Сотрудник'}</span>
                    <span className="block truncate text-[12px] text-b24-text-muted">{item.last_body}</span>
                  </span>
                  {item.unread ? (
                    <span className="min-w-[18px] rounded-full bg-[#f04438] px-1 text-center text-[10px] font-semibold leading-[18px] text-white">
                      {item.unread}
                    </span>
                  ) : (
                    <span className="text-[11px] text-b24-text-muted">{fmtTime(item.last_at)}</span>
                  )}
                </button>
              ))
            )}
          </div>
          <div className="b24-glass-panel flex min-h-[520px] flex-col overflow-hidden">{transcript}</div>
        </div>
      ) : (
        <div className="b24-glass-panel flex min-h-[520px] flex-col overflow-hidden">{transcript}</div>
      )}
    </PageShell>
  )
}

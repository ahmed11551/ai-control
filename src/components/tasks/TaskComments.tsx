import { useEffect, useState } from 'react'
import { api } from '../../lib/api'
import { formatActivity } from '../../data/tasks'
import { Button } from '../ui/Button'

type Comment = {
  id: string
  body: string
  author: string
  createdAt: string
}

export function TaskComments({ taskId }: { taskId: string }) {
  const [items, setItems] = useState<Comment[]>([])
  const [body, setBody] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)

  useEffect(() => {
    let cancelled = false
    setError('')
    api<Comment[]>(`/tasks/${taskId}/comments`)
      .then((rows) => {
        if (!cancelled) setItems(Array.isArray(rows) ? rows : [])
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Не удалось загрузить комментарии')
      })
    return () => {
      cancelled = true
    }
  }, [taskId])

  const send = async () => {
    const text = body.trim()
    if (!text || pending) return
    setPending(true)
    setError('')
    try {
      const row = await api<Comment>(`/tasks/${taskId}/comments`, {
        method: 'POST',
        body: JSON.stringify({ body: text }),
      })
      setItems((prev) => [...prev, row])
      setBody('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось отправить')
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="text-[12px] font-medium text-b24-text-dark">
        Комментарии {items.length ? `(${items.length})` : ''}
      </div>
      {error ? <p className="text-[12px] text-red-600">{error}</p> : null}
      <div className="flex flex-col gap-2.5 max-h-[240px] overflow-y-auto pr-1">
        {items.length === 0 ? (
          <p className="text-[13px] text-b24-text-muted">Пока нет комментариев.</p>
        ) : (
          items.map((c) => (
            <div key={c.id} className="rounded-b24-sm bg-[#f4f6f8] px-3 py-2">
              <div className="flex items-baseline justify-between gap-2 mb-1">
                <span className="text-[12px] font-medium text-b24-text-dark">{c.author || '—'}</span>
                <span className="text-[11px] text-b24-text-muted">{formatActivity(c.createdAt)}</span>
              </div>
              <p className="text-[13px] text-b24-text-dark whitespace-pre-wrap break-words">{c.body}</p>
            </div>
          ))
        )}
      </div>
      <textarea
        value={body}
        onChange={(e) => setBody(e.target.value)}
        rows={3}
        placeholder="Написать комментарий"
        className="w-full resize-none rounded-b24-sm border border-black/10 px-3 py-2 text-[13px] text-b24-text-dark outline-none focus:border-b24-cyan"
      />
      <Button
        type="button"
        variant="green"
        className="self-start rounded-b24-sm"
        disabled={pending || !body.trim()}
        onClick={() => void send()}
      >
        {pending ? 'Отправка…' : 'Отправить'}
      </Button>
    </div>
  )
}

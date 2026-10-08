import { useEffect, useState } from 'react'
import { formatPhone, NightActions, NightBrief, NightCallMark, NightPosition, NightSelfNote, noteWhen, type NightRequest } from './NightRequestsPage'

export function NightPreviewPage() {
  const [items, setItems] = useState<NightRequest[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    const q = new URLSearchParams(window.location.search).get('q') || ''
    const path = q ? `/api/preview/night-requests?q=${encodeURIComponent(q)}` : '/api/preview/night-requests'
    void fetch(path)
      .then(async (res) => {
        const data = (await res.json().catch(() => ({}))) as { items?: NightRequest[]; error?: string }
        if (!res.ok) throw new Error(data.error || 'Не удалось открыть пример')
        setItems(data.items || [])
      })
      .catch((e: Error) => setError(e.message))
  }, [])

  return (
    <div className="min-h-screen bg-[#1c1915] px-4 py-8 text-white">
      <div className="mx-auto flex w-full max-w-[760px] flex-col gap-4">
        <div>
          <p className="text-[12px] font-semibold uppercase tracking-[0.16em] text-white/45">Локально, без входа</p>
          <h1 className="mt-1 text-[32px] font-semibold leading-none">Заявки</h1>
          <p className="mt-2 text-[14px] text-white/65">Так карточка выглядит у ночного логиста.</p>
        </div>
        {error ? <p className="text-[14px] text-red-300">{error}</p> : null}
        {items && items.length === 0 ? <p className="text-white/70">Пока пусто.</p> : null}
        {(items || []).map((item) => (
          <article key={item.id} className="rounded-[16px] bg-white px-5 py-4 text-[#1c1915]">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h2 className="text-[18px] font-bold leading-tight">
                  {item.full_name || 'Клиент'}
                  <span className="ml-2 font-semibold tracking-tight text-[#3d3832]">
                    {item.phone?.trim() ? formatPhone(item.phone) : 'нет номера'}
                  </span>
                </h2>
              </div>
              <div className="text-right text-[12px] leading-tight text-[#6d675f]">
                <div className="font-semibold text-[#3d3832]">{noteWhen(item.sent_at) || item.sent_at}</div>
                {item.sent_by_name ? <div className="mt-0.5">отправил {item.sent_by_name}</div> : null}
              </div>
            </div>
            <NightPosition position={item.position} />
            <NightCallMark item={item} />
            <div className="mt-3 flex flex-wrap items-center gap-2 text-[13px] font-semibold text-[#3d3832]">
              {item.department ? <span className="rounded-full bg-[#f1efeb] px-3 py-1">{item.department}</span> : null}
              {item.city ? <span className="rounded-full bg-[#f1efeb] px-3 py-1">{item.city}</span> : null}
              {item.stage ? <span className="rounded-full bg-[#f1efeb] px-3 py-1">{item.stage}</span> : null}
              <span className="rounded-full bg-[#1c1915] px-3 py-1 text-white">Открыть карточку</span>
            </div>
            <NightSelfNote
              item={item}
              preview
              onSaved={(note, at) => {
                setItems((prev) =>
                  prev
                    ? prev.map((row) => (row.id === item.id ? { ...row, self_note: note, self_note_at: at } : row))
                    : prev,
                )
              }}
            />
            <NightBrief item={item} />
            <div className="mt-2">
              <NightActions item={item} preview />
            </div>
          </article>
        ))}
      </div>
    </div>
  )
}


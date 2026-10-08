import { ChevronDown, Save, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../../lib/api'
import { Button } from '../ui/Button'

export type ScheduleItem = {
  id: string
  sort_order: number
  day_from: number
  day_to: number | null
  calls_min: number
  calls_max: number
  interval_minutes: number
  note: string
  period_label: string
}

type CallsPreset = {
  id: string
  label: string
  min: number
  max: number
}

const CALL_PRESETS: CallsPreset[] = [
  { id: '5-15', label: '5–15 звонков', min: 5, max: 15 },
  { id: 'upto-7', label: 'до 7 звонков', min: 1, max: 7 },
  { id: 'upto-5', label: 'до 5 звонков', min: 1, max: 5 },
  { id: '3-4', label: '3–4 звонка', min: 3, max: 4 },
  { id: '1', label: '1 звонок в день', min: 1, max: 1 },
  { id: 'admin', label: 'По решению администратора', min: 0, max: 0 },
]

function presetFor(min: number, max: number): string {
  const hit = CALL_PRESETS.find((p) => p.min === min && p.max === max)
  return hit?.id || 'custom'
}

function labelForPreset(id: string, min: number, max: number) {
  const hit = CALL_PRESETS.find((p) => p.id === id)
  if (hit) return hit.label
  if (min === max) return `${min} звон. / день`
  return `${min}–${max} звонков`
}

function splitInterval(total: number) {
  const safe = Math.max(0, Math.floor(total || 0))
  return { hours: Math.floor(safe / 60), minutes: safe % 60 }
}

function joinInterval(hours: number, minutes: number) {
  return Math.max(0, Math.floor(hours) * 60 + Math.floor(minutes))
}

function Stepper({
  value,
  onChange,
  min = 0,
  max = 99,
  suffix,
}: {
  value: number
  onChange: (n: number) => void
  min?: number
  max?: number
  suffix: string
}) {
  return (
    <div className="inline-flex items-center gap-1.5">
      <div className="relative">
        <input
          type="number"
          min={min}
          max={max}
          value={value}
          onChange={(e) => {
            const n = Number(e.target.value)
            if (!Number.isFinite(n)) return
            onChange(Math.min(max, Math.max(min, Math.floor(n))))
          }}
          className="h-9 w-[52px] rounded-[10px] border border-[#c5d4e8] bg-white pr-6 pl-2 text-center text-[13px] font-medium text-[#1e2a44] outline-none focus:border-[#2b7de9]/50"
        />
        <div className="absolute top-0.5 right-0.5 bottom-0.5 flex w-4 flex-col overflow-hidden rounded-[6px]">
          <button
            type="button"
            className="flex flex-1 items-center justify-center bg-[#e8f1fb] text-[#2b7de9] hover:bg-[#d7e8f8]"
            onClick={() => onChange(Math.min(max, value + 1))}
            aria-label="Больше"
          >
            <ChevronDown size={10} className="rotate-180" strokeWidth={3} />
          </button>
          <button
            type="button"
            className="flex flex-1 items-center justify-center bg-[#e8f1fb] text-[#2b7de9] hover:bg-[#d7e8f8]"
            onClick={() => onChange(Math.max(min, value - 1))}
            aria-label="Меньше"
          >
            <ChevronDown size={10} strokeWidth={3} />
          </button>
        </div>
      </div>
      <span className="text-[12px] text-[#5a6a85]">{suffix}</span>
    </div>
  )
}

export function AutodialScheduleModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [items, setItems] = useState<ScheduleItem[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (!open) return
    setError('')
    setSaved(false)
    setLoading(true)
    void api<{ items: ScheduleItem[] }>('/telephony/autodial/schedule')
      .then((r) => setItems(r.items || []))
      .catch((e: Error) => setError(e.message || 'Не удалось загрузить'))
      .finally(() => setLoading(false))
  }, [open])

  const dirtyReady = useMemo(() => items.length > 0, [items])

  const patch = (id: string, patch: Partial<ScheduleItem>) => {
    setItems((prev) => prev.map((it) => (it.id === id ? { ...it, ...patch } : it)))
    setSaved(false)
  }

  const save = async () => {
    setSaving(true)
    setError('')
    try {
      const res = await api<{ items: ScheduleItem[] }>('/telephony/autodial/schedule', {
        method: 'PUT',
        body: JSON.stringify({
          items: items.map((it) => ({
            id: it.id,
            calls_min: it.calls_min,
            calls_max: it.calls_max,
            interval_minutes: it.interval_minutes,
            note: it.note,
          })),
        }),
      })
      setItems(res.items || items)
      setSaved(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось сохранить')
    } finally {
      setSaving(false)
    }
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-[200] flex items-start justify-center px-4 pt-[6vh] pb-8">
      <button type="button" className="absolute inset-0 bg-[#1a2140]/45 backdrop-blur-[2px]" onClick={onClose} aria-label="Закрыть" />
      <div className="relative z-10 flex max-h-[min(860px,88vh)] w-full max-w-[1080px] flex-col overflow-hidden rounded-[18px] border border-black/5 bg-[#f4f7fb] shadow-[0_28px_80px_rgba(18,24,42,0.35)]">
        <div className="flex items-start justify-between gap-3 border-b border-black/6 bg-white px-5 py-4">
          <div>
            <div className="text-[18px] font-semibold tracking-tight text-[#1a2140]">Настройки скорозвона</div>
            <p className="mt-0.5 text-[13px] text-[#6b7a94]">
              Частота обзвона по возрасту лида — сколько раз в день и с каким интервалом
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="flex size-9 shrink-0 items-center justify-center rounded-full text-[#6b7a94] hover:bg-black/[0.04]"
            aria-label="Закрыть"
          >
            <X size={18} />
          </button>
        </div>

        <div className="b24-scrollbar min-h-0 flex-1 overflow-auto p-4">
          {error ? <div className="mb-3 rounded-[12px] bg-rose-50 px-3 py-2 text-[13px] text-rose-700">{error}</div> : null}
          {loading ? (
            <div className="py-16 text-center text-[13px] text-[#6b7a94]">Загрузка…</div>
          ) : (
            <div className="overflow-hidden rounded-[14px] border border-[#d5e0ef] bg-white shadow-sm">
              <table className="w-full min-w-[820px] border-collapse text-left">
                <thead>
                  <tr className="bg-[#e8f1fb] text-[12px] font-semibold text-[#2a3a5c]">
                    <th className="px-4 py-3 font-semibold">Период</th>
                    <th className="px-4 py-3 font-semibold">Сколько раз звонить в день</th>
                    <th className="px-4 py-3 font-semibold">Интервал между звонками</th>
                    <th className="px-4 py-3 font-semibold">Рекомендации / комментарий</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((it, idx) => {
                    const preset = presetFor(it.calls_min, it.calls_max)
                    const { hours, minutes } = splitInterval(it.interval_minutes)
                    const zebra = idx % 2 === 1 ? 'bg-[#f7fafd]' : 'bg-white'
                    return (
                      <tr key={it.id} className={`border-t border-[#e3ebf5] ${zebra}`}>
                        <td className="px-4 py-3 align-middle text-[13px] font-semibold text-[#1e2a44]">
                          {it.period_label}
                        </td>
                        <td className="px-4 py-3 align-middle">
                          <div className="relative min-w-[200px]">
                            <select
                              value={preset === 'custom' ? '' : preset}
                              onChange={(e) => {
                                const p = CALL_PRESETS.find((x) => x.id === e.target.value)
                                if (!p) return
                                patch(it.id, { calls_min: p.min, calls_max: p.max })
                              }}
                              className="h-10 w-full appearance-none rounded-[10px] border border-[#c5d4e8] bg-white py-2 pr-9 pl-3 text-[13px] text-[#1e2a44] outline-none focus:border-[#2b7de9]/50"
                            >
                              {preset === 'custom' ? (
                                <option value="">
                                  {labelForPreset('custom', it.calls_min, it.calls_max)}
                                </option>
                              ) : null}
                              {CALL_PRESETS.map((p) => (
                                <option key={p.id} value={p.id}>
                                  {p.label}
                                </option>
                              ))}
                            </select>
                            <ChevronDown
                              size={14}
                              className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-[#6b7a94]"
                            />
                          </div>
                        </td>
                        <td className="px-4 py-3 align-middle">
                          <div className="flex flex-wrap items-center gap-2">
                            <Stepper
                              value={hours}
                              min={0}
                              max={168}
                              suffix="ч"
                              onChange={(h) =>
                                patch(it.id, { interval_minutes: joinInterval(h, minutes) })
                              }
                            />
                            <Stepper
                              value={minutes}
                              min={0}
                              max={59}
                              suffix="мин"
                              onChange={(m) =>
                                patch(it.id, { interval_minutes: joinInterval(hours, m) })
                              }
                            />
                          </div>
                        </td>
                        <td className="px-4 py-3 align-middle">
                          <input
                            value={it.note}
                            onChange={(e) => patch(it.id, { note: e.target.value })}
                            className="h-10 w-full min-w-[200px] rounded-[10px] border border-[#c5d4e8] bg-white px-3 text-[13px] text-[#4a5a75] outline-none focus:border-[#2b7de9]/50"
                          />
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div className="flex items-center justify-between gap-3 border-t border-black/6 bg-white px-5 py-3">
          <div className="text-[12px] text-[#6b7a94]">
            {saved ? 'Сохранено' : 'Изменения применяются после сохранения'}
          </div>
          <div className="flex items-center gap-2">
            <Button variant="filter" className="rounded-[10px]" onClick={onClose}>
              Закрыть
            </Button>
            <Button
              variant="green"
              className="rounded-[10px]"
              disabled={!dirtyReady || saving || loading}
              onClick={() => void save()}
            >
              <Save size={14} strokeWidth={2.2} />
              {saving ? 'Сохранение…' : 'Сохранить'}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

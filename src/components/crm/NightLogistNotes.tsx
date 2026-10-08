import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';
import { api } from '../../lib/api';
import { formatActivity } from '../../data/tasks';

type Note = {
  id: string;
  body: string;
  author?: string;
  created_at?: string;
};

type Brief = {
  recruiter?: string;
  candidate?: string;
  rop?: string;
  from_city?: string;
  to_city?: string;
  depart_at?: string;
  arrive_at?: string;
  transport?: string;
};

function transportCode(raw?: string) {
  if (raw === 'air' || raw === 'самолет' || raw === 'самолёт') return 'самолет';
  if (raw === 'rail' || raw === 'поезд' || raw === 'жд') return 'поезд';
  if (raw === 'bus' || raw === 'автобус') return 'автобус';
  return '';
}

function localDate(iso?: string) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

function dayPart(iso?: string) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const mins = d.getHours() * 60 + d.getMinutes();
  if (d.getHours() === 0 && d.getMinutes() === 0) return '';
  if (mins >= 21 * 60 || mins < 6 * 60) return 'ночь';
  if (mins < 12 * 60) return 'утро';
  return 'вечер';
}

function toIso(date: string, part: string) {
  if (!date) return '';
  const hm = part === 'утро' ? '09:00' : part === 'вечер' ? '18:00' : part === 'ночь' ? '23:00' : '00:00';
  const d = new Date(`${date}T${hm}`);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString();
}

type Draft = {
  recruiter: string;
  candidate: string;
  rop: string;
  from_city: string;
  to_city: string;
  departDate: string;
  departPart: string;
  arriveDate: string;
  arrivePart: string;
  transport: string;
};

function draftFrom(brief: Brief): Draft {
  return {
    recruiter: brief.recruiter || '',
    candidate: brief.candidate || '',
    rop: brief.rop || '',
    from_city: brief.from_city || '',
    to_city: brief.to_city || '',
    departDate: localDate(brief.depart_at),
    departPart: dayPart(brief.depart_at),
    arriveDate: localDate(brief.arrive_at),
    arrivePart: dayPart(brief.arrive_at),
    transport: transportCode(brief.transport),
  };
}

const fieldClass =
  'mt-1 h-9 w-full rounded-lg border border-black/10 bg-[#faf7f1] px-2.5 text-[13px] text-[#10243e] outline-none focus:border-[#10243e]/40';

function TextField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="block">
      <span className="text-[11px] font-medium text-[#10243e]/55">{label}</span>
      <input value={value} onChange={e => onChange(e.target.value)} className={fieldClass} />
    </label>
  );
}

function Pills({
  value,
  options,
  onChange,
}: {
  value: string;
  options: { id: string; label: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {options.map(option => {
        const on = value === option.id;
        return (
          <button
            key={option.id}
            type="button"
            onClick={() => onChange(on ? '' : option.id)}
            className={`h-8 rounded-full px-3 text-[12px] font-medium ${
              on ? 'bg-[#10243e] text-white' : 'bg-[#faf7f1] text-[#10243e] ring-1 ring-black/10'
            }`}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}

const MONTHS = [
  'января',
  'февраля',
  'марта',
  'апреля',
  'мая',
  'июня',
  'июля',
  'августа',
  'сентября',
  'октября',
  'ноября',
  'декабря',
];

function whenLabel(iso?: string) {
  const date = localDate(iso);
  if (!date) return '—';
  const [year, month, day] = date.split('-');
  const part = dayPart(iso);
  const text = `${Number(day)} ${MONTHS[Number(month) - 1] || ''} ${year}`;
  return part ? `${text}, ${part}` : text;
}

function transportLabel(raw?: string) {
  const code = transportCode(raw);
  if (code === 'самолет') return 'Самолёт';
  if (code === 'поезд') return 'Поезд';
  if (code === 'автобус') return 'Автобус';
  return (raw || '').trim() || '—';
}

function textOrDash(value?: string) {
  const text = (value || '').trim();
  return text || '—';
}

function preview(items: Note[]) {
  const last = items[items.length - 1]?.body?.replace(/\s+/g, ' ').trim();
  if (!last) return 'Комментарии';
  return last.length > 72 ? `${last.slice(0, 72)}…` : last;
}

export function NightLogistNotes({ dealId }: { dealId: string }) {
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [items, setItems] = useState<Note[]>([]);
  const [brief, setBrief] = useState<Brief>({});
  const [draft, setDraft] = useState<Draft>(() => draftFrom({}));
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [editing, setEditing] = useState(false);
  const [open, setOpen] = useState(false);
  const [body, setBody] = useState('');
  const [error, setError] = useState('');
  const [pending, setPending] = useState(false);
  const [pos, setPos] = useState(() => ({
    x: Math.max(16, window.innerWidth - 520),
    y: Math.max(16, window.innerHeight - 560),
  }));
  const drag = useRef<{ dx: number; dy: number } | null>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const load = async () => {
    const res = await api<{ allowed?: boolean; items?: Note[]; brief?: Brief }>(
      `/deals/${dealId}/night-notes`,
    );
    setAllowed(Boolean(res.allowed));
    setItems(Array.isArray(res.items) ? res.items : []);
    setBrief(res.brief || {});
  };

  useEffect(() => {
    if (!dirty) setDraft(draftFrom(brief));
  }, [brief, dirty]);

  const edit = (patch: Partial<Draft>) => {
    setSaved(false);
    setDirty(true);
    setDraft(prev => ({ ...prev, ...patch }));
  };

  const saveBrief = async () => {
    if (saving) return;
    setSaving(true);
    setError('');
    setSaved(false);
    try {
      const res = await api<{ brief?: Brief }>(`/deals/${dealId}/night-brief`, {
        method: 'PUT',
        body: JSON.stringify({
          recruiter: draft.recruiter,
          candidate: draft.candidate,
          rop: draft.rop,
          from_city: draft.from_city,
          to_city: draft.to_city,
          depart_at: toIso(draft.departDate, draft.departPart),
          arrive_at: toIso(draft.arriveDate, draft.arrivePart),
          transport: draft.transport,
        }),
      });
      setBrief(res.brief || {});
      setDirty(false);
      setSaved(true);
      setEditing(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось сохранить');
    } finally {
      setSaving(false);
    }
  };

  useEffect(() => {
    let cancelled = false;
    setAllowed(null);
    setItems([]);
    setBrief({});
    setDirty(false);
    setSaved(false);
    setEditing(false);
    api<{ allowed?: boolean; items?: Note[]; brief?: Brief }>(`/deals/${dealId}/night-notes`)
      .then(res => {
        if (cancelled) return;
        setAllowed(Boolean(res.allowed));
        setItems(Array.isArray(res.items) ? res.items : []);
        setBrief(res.brief || {});
      })
      .catch(() => {
        if (!cancelled) setAllowed(false);
      });
    return () => {
      cancelled = true;
    };
  }, [dealId]);

  useEffect(() => {
    if (!open) return;
    const node = listRef.current;
    if (node) node.scrollTop = node.scrollHeight;
  }, [open, items.length]);

  const send = async () => {
    const text = body.trim();
    if (!text || pending) return;
    setPending(true);
    setError('');
    try {
      const row = await api<Note>(`/deals/${dealId}/night-notes`, {
        method: 'POST',
        body: JSON.stringify({ body: text }),
      });
      setItems(prev => [...prev, row]);
      setBody('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось отправить');
      try {
        await load();
      } catch {
        /* список остаётся как был */
      }
    } finally {
      setPending(false);
    }
  };

  if (!allowed) return null;

  return (
    <>
      <button
        type="button"
        className="mt-2 flex w-full items-center justify-between rounded-[12px] border border-black/8 bg-white px-3 py-2.5 text-left hover:bg-[#f7f8fa]"
        onClick={() => {
          setError('');
          setOpen(true);
          void load().catch(() => {
            /* оставляем уже загруженное */
          });
        }}
      >
        <span className="min-w-0">
          <span className="block text-[13px] font-semibold text-b24-text-dark">
            Информация для ночного логиста
          </span>
          <span className="block truncate text-[12px] text-b24-text-muted">{preview(items)}</span>
        </span>
        <span className="shrink-0 text-[12px] text-b24-cyan">Открыть</span>
      </button>
      {open
        ? createPortal(
            <div
              className="fixed z-[200] flex max-h-[min(760px,calc(100vh-32px))] w-[min(460px,calc(100vw-16px))] flex-col overflow-hidden rounded-[16px] bg-[#f4efe4] text-[#10243e] shadow-[0_16px_48px_rgba(15,23,42,0.28)] ring-1 ring-black/10"
              style={{ left: pos.x, top: pos.y }}
            >
              <div
                className="flex cursor-grab items-center justify-between bg-[#10243e] px-4 py-3 text-white active:cursor-grabbing"
                onPointerDown={e => {
                  if ((e.target as HTMLElement).closest('button')) return;
                  drag.current = { dx: e.clientX - pos.x, dy: e.clientY - pos.y };
                  e.currentTarget.setPointerCapture(e.pointerId);
                }}
                onPointerMove={e => {
                  if (!drag.current) return;
                  setPos({ x: e.clientX - drag.current.dx, y: e.clientY - drag.current.dy });
                }}
                onPointerUp={() => {
                  drag.current = null;
                }}
              >
                <div>
                  <p className="text-[10px] uppercase tracking-[0.2em] text-white/50">Смена</p>
                  <p className="text-[15px] font-semibold">Информация для ночного логиста</p>
                </div>
                <button
                  type="button"
                  className="ml-2 inline-flex size-8 items-center justify-center rounded-md text-white/80 hover:bg-white/10"
                  onClick={() => {
                    setOpen(false);
                    setEditing(false);
                    setDirty(false);
                  }}
                  aria-label="Закрыть"
                >
                  <X className="size-4" />
                </button>
              </div>
              <div ref={listRef} className="flex min-h-[140px] flex-1 flex-col gap-2 overflow-y-auto px-4 py-3">
                <form
                  className="rounded-xl bg-white px-3 py-3 ring-1 ring-black/5"
                  onSubmit={e => {
                    e.preventDefault();
                    if (!editing) return;
                    void saveBrief();
                  }}
                >
                  <p className="mb-2 text-[11px] text-[#10243e]/45">Данные подлежат редактированию</p>
                  {editing ? (
                    <div className="grid gap-2.5">
                      <TextField label="Имя рекрутера" value={draft.recruiter} onChange={v => edit({ recruiter: v })} />
                      <TextField label="Имя кандидата" value={draft.candidate} onChange={v => edit({ candidate: v })} />
                      <TextField label="Имя РОПа" value={draft.rop} onChange={v => edit({ rop: v })} />
                      <TextField
                        label="Регион, с которого едет"
                        value={draft.from_city}
                        onChange={v => edit({ from_city: v })}
                      />
                      <TextField
                        label="Регион, на который едет"
                        value={draft.to_city}
                        onChange={v => edit({ to_city: v })}
                      />
                      <label className="block">
                        <span className="text-[11px] font-medium text-[#10243e]/55">Время отправления</span>
                        <input
                          type="date"
                          value={draft.departDate}
                          onChange={e => edit({ departDate: e.target.value })}
                          className={fieldClass}
                        />
                        <Pills
                          value={draft.departPart}
                          options={[
                            { id: 'утро', label: 'Утро' },
                            { id: 'вечер', label: 'Вечер' },
                            { id: 'ночь', label: 'Ночь' },
                          ]}
                          onChange={departPart => edit({ departPart })}
                        />
                      </label>
                      <label className="block">
                        <span className="text-[11px] font-medium text-[#10243e]/55">Время прибытия</span>
                        <input
                          type="date"
                          value={draft.arriveDate}
                          onChange={e => edit({ arriveDate: e.target.value })}
                          className={fieldClass}
                        />
                        <Pills
                          value={draft.arrivePart}
                          options={[
                            { id: 'утро', label: 'Утро' },
                            { id: 'вечер', label: 'Вечер' },
                            { id: 'ночь', label: 'Ночь' },
                          ]}
                          onChange={arrivePart => edit({ arrivePart })}
                        />
                      </label>
                      <div>
                        <span className="text-[11px] font-medium text-[#10243e]/55">Транспорт</span>
                        <Pills
                          value={draft.transport}
                          options={[
                            { id: 'самолет', label: 'Самолёт' },
                            { id: 'поезд', label: 'Поезд' },
                            { id: 'автобус', label: 'Автобус' },
                          ]}
                          onChange={transport => edit({ transport })}
                        />
                      </div>
                    </div>
                  ) : (
                    <dl className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)] gap-x-3 gap-y-1.5 text-[13px]">
                      {(
                        [
                          ['Имя рекрутера', textOrDash(brief.recruiter)],
                          ['Имя кандидата', textOrDash(brief.candidate)],
                          ['Имя РОПа', textOrDash(brief.rop)],
                          ['Регион, с которого едет', textOrDash(brief.from_city)],
                          ['Регион, на который едет', textOrDash(brief.to_city)],
                          ['Время отправления', whenLabel(brief.depart_at)],
                          ['Время прибытия', whenLabel(brief.arrive_at)],
                          ['Транспорт', transportLabel(brief.transport)],
                        ] as const
                      ).map(([label, value]) => (
                        <div key={label} className="contents">
                          <dt className="text-[12px] text-[#10243e]/55">{label}</dt>
                          <dd className="break-words font-medium text-[#10243e]">{value}</dd>
                        </div>
                      ))}
                    </dl>
                  )}
                  <div className="mt-3 flex items-center justify-end gap-3">
                    {saved && !editing ? (
                      <span className="text-[12px] text-[#10243e]/55">Сохранено</span>
                    ) : null}
                    {editing ? (
                      <button
                        type="submit"
                        disabled={saving}
                        className="inline-flex h-9 items-center rounded-xl bg-[#10243e] px-4 text-[13px] font-semibold text-white disabled:opacity-40"
                      >
                        {saving ? 'Сохранение…' : 'Сохранить'}
                      </button>
                    ) : (
                      <button
                        type="button"
                        onClick={e => {
                          e.preventDefault();
                          e.stopPropagation();
                          setDraft(draftFrom(brief));
                          setDirty(false);
                          setSaved(false);
                          setEditing(true);
                        }}
                        className="inline-flex h-9 items-center rounded-xl bg-[#10243e] px-4 text-[13px] font-semibold text-white"
                      >
                        Изменить
                      </button>
                    )}
                  </div>
                </form>
                {items.length === 0 ? (
                  <p className="text-[13px] text-[#10243e]/55">Пока нет записей.</p>
                ) : (
                  items.map(note => (
                    <div key={note.id} className="rounded-xl bg-white px-3 py-2 ring-1 ring-black/5">
                      <div className="mb-1 flex items-baseline justify-between gap-2">
                        <span className="text-[12px] font-semibold text-[#10243e]">
                          {note.author || '—'}
                        </span>
                        <span className="text-[11px] text-[#10243e]/45">
                          {note.created_at ? formatActivity(note.created_at) : ''}
                        </span>
                      </div>
                      <p className="whitespace-pre-wrap break-words text-[13px] text-[#10243e]">{note.body}</p>
                    </div>
                  ))
                )}
              </div>
              <div className="border-t border-black/8 px-4 py-3">
                {error ? <p className="mb-2 text-[12px] text-red-600">{error}</p> : null}
                <textarea
                  value={body}
                  onChange={e => setBody(e.target.value)}
                  rows={3}
                  placeholder="Написать комментарий"
                  className="w-full resize-none rounded-xl border border-black/10 bg-white px-3 py-2 text-[13px] text-[#10243e] outline-none focus:border-[#10243e]/40"
                />
                <button
                  type="button"
                  disabled={pending || !body.trim()}
                  onClick={() => void send()}
                  className="mt-2 inline-flex h-10 items-center rounded-xl bg-[#10243e] px-4 text-[13px] font-semibold text-white disabled:opacity-50"
                >
                  {pending ? 'Отправка…' : 'Добавить'}
                </button>
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}

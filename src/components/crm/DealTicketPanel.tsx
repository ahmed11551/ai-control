import { Check, ChevronDown, Copy, X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { api, apiDownload } from '../../lib/api';
import { BiletDocsButton } from './BiletFileModal';
import { NightLogistNotes } from './NightLogistNotes';
import { SendToCustomer } from './SendToCustomer';
import { isLogistRole } from '../../lib/roles';
import { useAuth } from '../../store/auth';

type Ticket = {
  transport?: string;
  from_city?: string;
  to_city?: string;
  flight_or_train?: string;
  depart_at?: string;
  arrive_at?: string;
  route?: string;
  notes?: string;
  wagon?: string;
  seat?: string;
  amount?: number;
};

type TicketFile = { id: string; kind: string; filename: string; size_bytes?: number };

function toLocalInput(iso?: string) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromLocalInput(v: string) {
  if (!v) return '';
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString();
}

function splitWhen(iso?: string) {
  const v = toLocalInput(iso);
  if (!v) return { date: '', time: '' };
  const [date, time] = v.split('T');
  return { date: date || '', time: time || '' };
}

function joinWhen(date: string, time: string) {
  if (!date) return '';
  return fromLocalInput(`${date}T${time || '00:00'}`);
}

const MONTHS = [
  'январь',
  'февраль',
  'март',
  'апрель',
  'май',
  'июнь',
  'июль',
  'август',
  'сентябрь',
  'октябрь',
  'ноябрь',
  'декабрь',
];

const MONTHS_GENITIVE = [
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

function dayText(raw: string) {
  const digits = raw.replace(/\D/g, '').slice(0, 2);
  if (!digits) return '';
  const n = Number(digits);
  if (!n) return '';
  if (n > 31) {
    const first = Number(digits[0]);
    return first >= 1 && first <= 31 ? String(first) : '';
  }
  return String(n);
}

function partsOf(date: string) {
  const [y, m, d] = date ? date.split('-') : ['', '', ''];
  return {
    y: y || String(new Date().getFullYear()),
    m: m || '',
    d: dayText(d || ''),
  };
}

function TicketSelect({
  value,
  placeholder,
  options,
  disabled,
  invalid,
  onChange,
}: {
  value: string;
  placeholder: string;
  options: { value: string; label: string }[];
  disabled?: boolean;
  invalid?: boolean;
  onChange: (value: string) => void;
}) {
  const btn = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<{ top: number; left: number; width: number } | null>(null);
  const current = options.find(item => item.value === value)?.label || placeholder;

  useEffect(() => {
    if (!box) return;
    const close = (e: PointerEvent) => {
      const target = e.target as Node;
      if (btn.current?.contains(target) || menu.current?.contains(target)) return;
      setBox(null);
    };
    window.addEventListener('pointerdown', close);
    return () => window.removeEventListener('pointerdown', close);
  }, [box]);

  return (
    <div>
      <button
        ref={btn}
        type="button"
        disabled={disabled}
        onClick={() => {
          if (box) {
            setBox(null);
            return;
          }
          const rect = btn.current?.getBoundingClientRect();
          if (!rect) return;
          const spaceBelow = window.innerHeight - rect.bottom;
          const top = spaceBelow < 220 ? Math.max(8, rect.top - 246) : rect.bottom + 6;
          setBox({ top, left: rect.left, width: rect.width });
        }}
        className={`flex h-12 w-full items-center justify-between gap-2 rounded-xl bg-[#145c45] px-3.5 text-left shadow-[0_8px_18px_rgba(20,92,69,0.28)] ring-1 disabled:opacity-50 ${
          invalid ? 'ring-red-400' : 'ring-white/15'
        }`}
      >
        <span className={`truncate text-[15px] font-semibold ${value ? 'text-[#f6f0e4]' : 'text-[#f6f0e4]/55'}`}>
          {current}
        </span>
        <ChevronDown className={`size-4 shrink-0 text-[#e7c56a] transition ${box ? 'rotate-180' : ''}`} />
      </button>
      {box
        ? createPortal(
            <div
              ref={menu}
              className="max-h-60 overflow-auto rounded-xl bg-[#0e3d30] py-1 shadow-[0_18px_40px_rgba(8,30,24,0.45)] ring-1 ring-[#e7c56a]/30"
              style={{ position: 'fixed', top: box.top, left: box.left, width: box.width, zIndex: 260 }}
            >
              {options.map(item => (
                <button
                  key={item.value || 'empty'}
                  type="button"
                  className={`flex w-full px-3.5 py-2.5 text-left text-[14px] ${
                    item.value === value
                      ? 'bg-[#1d7a5c] font-semibold text-[#f6f0e4]'
                      : 'text-[#f6f0e4]/90 hover:bg-white/10'
                  }`}
                  onClick={() => {
                    onChange(item.value);
                    setBox(null);
                  }}
                >
                  {item.label}
                </button>
              ))}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}

function daypartFromClock(time: string) {
  if (!time || time === '00:00') return '';
  const [hs = '', ms = '0'] = time.split(':');
  const h = Number(hs);
  const m = Number(ms);
  if (!Number.isFinite(h)) return '';
  const mins = h * 60 + (Number.isFinite(m) ? m : 0);
  if (mins >= 21 * 60 || mins < 6 * 60) return 'ночь';
  if (mins < 12 * 60) return 'утро';
  if (mins < 21 * 60) return 'вечер';
  return '';
}

function clockFromDaypart(part: string) {
  if (part === 'утро') return '09:00';
  if (part === 'вечер') return '18:00';
  if (part === 'ночь') return '23:00';
  return '';
}

function WhenFields({
  label,
  date,
  time,
  disabled,
  invalid,
  onChange,
}: {
  label: string;
  date: string;
  time: string;
  disabled?: boolean;
  invalid?: boolean;
  onChange: (date: string, time: string) => void;
}) {
  const [parts, setParts] = useState(() => partsOf(date));
  const [part, setPart] = useState(() => daypartFromClock(time));
  const emitted = useRef(`${date}|${clockFromDaypart(daypartFromClock(time))}`);
  useEffect(() => {
    const key = `${date}|${clockFromDaypart(daypartFromClock(time))}`;
    if (key === emitted.current) return;
    emitted.current = key;
    setParts(partsOf(date));
    setPart(daypartFromClock(time));
  }, [date, time]);

  const emit = (nextParts: ReturnType<typeof partsOf>, nextPart: string, preserveClock: boolean) => {
    const day = Number(nextParts.d);
    const iso =
      nextParts.m && nextParts.d && day >= 1 && day <= 31
        ? `${nextParts.y}-${nextParts.m}-${String(day).padStart(2, '0')}`
        : '';
    const hm =
      preserveClock && daypartFromClock(time) === nextPart
        ? nextPart
          ? time
          : ''
        : clockFromDaypart(nextPart);
    emitted.current = `${iso}|${clockFromDaypart(nextPart)}`;
    onChange(iso, hm);
  };

  const change = (patch: Partial<ReturnType<typeof partsOf>>) => {
    const next = { ...parts, ...patch };
    setParts(next);
    emit(next, part, true);
  };

  const years = Array.from(
    new Set([parts.y, String(new Date().getFullYear()), String(new Date().getFullYear() + 1)]),
  );
  const months = MONTHS.map((name, i) => ({
    value: String(i + 1).padStart(2, '0'),
    label: name,
  }));

  return (
    <div>
      <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
        {label}
      </span>
      <div className="mt-1.5 grid grid-cols-[96px_1fr_100px] gap-2">
        <input
          inputMode="numeric"
          autoComplete="off"
          disabled={disabled}
          placeholder="День"
          value={parts.d}
          onChange={e => change({ d: dayText(e.target.value) })}
          className={`h-12 w-full rounded-xl bg-[#145c45] px-3 text-center text-[15px] font-semibold text-[#f6f0e4] shadow-[0_8px_18px_rgba(20,92,69,0.28)] ring-1 outline-none placeholder:text-[#f6f0e4]/55 disabled:opacity-50 ${
            invalid ? 'ring-red-400' : 'ring-white/15'
          }`}
        />
        <TicketSelect
          value={parts.m}
          placeholder="Месяц"
          disabled={disabled}
          invalid={invalid}
          options={[{ value: '', label: 'Месяц' }, ...months]}
          onChange={m => change({ m })}
        />
        <TicketSelect
          value={parts.y}
          placeholder="Год"
          disabled={disabled}
          invalid={invalid}
          options={years.map(item => ({ value: item, label: item }))}
          onChange={y => change({ y })}
        />
      </div>
      <div className="mt-2">
        <TicketSelect
          value={part}
          placeholder="Время суток"
          disabled={disabled}
          invalid={invalid}
          options={[
            { value: '', label: 'Время суток' },
            { value: 'утро', label: 'Утро' },
            { value: 'вечер', label: 'Вечер' },
            { value: 'ночь', label: 'Ночь' },
          ]}
          onChange={value => {
            setPart(value);
            emit(parts, value, false);
          }}
        />
      </div>
    </div>
  );
}

function transportValue(raw?: string) {
  if (raw === 'air' || raw === 'самолет') return 'самолет';
  if (raw === 'rail' || raw === 'поезд') return 'поезд';
  if (raw === 'bus' || raw === 'автобус') return 'автобус';
  return '';
}

function transportLabel(raw?: string) {
  const value = transportValue(raw);
  if (value === 'самолет') return 'Самолёт';
  if (value === 'поезд') return 'Поезд';
  if (value === 'автобус') return 'Автобус';
  return (raw || '').trim();
}

function whenLine(iso?: string) {
  const { date, time } = splitWhen(iso);
  if (!date) return '';
  const [year, month, day] = date.split('-');
  const name = MONTHS_GENITIVE[Number(month) - 1] || '';
  return [`${Number(day)} ${name} ${year}`, daypartFromClock(time)].filter(Boolean).join(', ');
}

function CopyBeside({ text }: { text: string }) {
  const value = text.trim();
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  if (!value) return null;

  const markCopied = () => {
    setCopied(true);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), 1400);
  };

  const copy = () => {
    const legacy = () => {
      const area = document.createElement('textarea');
      area.value = value;
      area.setAttribute('readonly', '');
      area.style.position = 'fixed';
      area.style.opacity = '0';
      document.body.appendChild(area);
      area.focus();
      area.select();
      let ok = false;
      try {
        ok = document.execCommand('copy');
      } catch {
        ok = false;
      }
      area.remove();
      return ok;
    };
    if (navigator.clipboard?.writeText) {
      void navigator.clipboard.writeText(value).then(markCopied).catch(() => {
        if (legacy()) markCopied();
      });
      return;
    }
    if (legacy()) markCopied();
  };

  return (
    <button
      type="button"
      aria-label={copied ? 'Скопировано' : 'Скопировать'}
      onClick={copy}
      className={`relative inline-flex size-7 shrink-0 items-center justify-center rounded-full transition-colors duration-300 ${
        copied ? 'bg-[#145c45] text-[#f6f0e4]' : 'text-[#145c45] hover:bg-[#145c45]/10'
      }`}
    >
      <Copy
        className={`absolute size-3.5 transition-all duration-300 ease-out ${
          copied ? 'scale-50 opacity-0' : 'scale-100 opacity-100'
        }`}
      />
      <Check
        className={`absolute size-3.5 transition-all duration-300 ease-out ${
          copied ? 'scale-100 opacity-100' : 'scale-50 opacity-0'
        }`}
      />
      <span
        aria-hidden="true"
        className={`pointer-events-none absolute -top-7 left-1/2 -translate-x-1/2 whitespace-nowrap rounded-full bg-[#10243e] px-2 py-0.5 text-[10px] font-medium text-white transition-all duration-300 ${
          copied ? 'translate-y-0 opacity-100' : 'translate-y-1 opacity-0'
        }`}
      >
        Скопировано
      </span>
    </button>
  );
}

/** Карточка билета (ТЗ §15.2–15.3) + действия передачи / отправки кандидату. */
export function DealTicketPanel({
  dealId,
  onChanged,
  popupOnly,
  forceOpen,
  onForceClose,
  onConfirmed,
  hint,
  viewLocked,
}: {
  dealId: string;
  logisticsNeeded?: boolean;
  onChanged?: () => void;
  popupOnly?: boolean;
  forceOpen?: boolean;
  onForceClose?: () => void;
  onConfirmed?: () => void | Promise<void>;
  hint?: string;
  viewLocked?: boolean;
}) {
  const { user } = useAuth();
  const role = String(user?.role || '').toLowerCase();
  const canEdit = role === 'admin' || role === 'rop' || isLogistRole(role);
  const isManager = role === 'manager' || role === 'sales';
  const viewOnly = isLogistRole(role) || role === 'admin' || (isManager && Boolean(viewLocked) && !popupOnly);
  const canFill = !viewOnly && (canEdit || isManager);
  const [ticket, setTicket] = useState<Ticket>({});
  const [files, setFiles] = useState<TicketFile[]>([]);
  const [canDownloadDocs, setCanDownloadDocs] = useState(false);
  const [canSeeTicketFiles, setCanSeeTicketFiles] = useState(false);
  const canSeeFiles = role === 'admin' || isLogistRole(role) || canSeeTicketFiles;
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (forceOpen == null) return;
    setOpen(forceOpen);
  }, [forceOpen]);
  const [pos, setPos] = useState(() => ({
    x: Math.max(16, window.innerWidth - 620),
    y: Math.max(16, window.innerHeight - 760),
  }));
  const drag = useRef<{ dx: number; dy: number } | null>(null);
  const [error, setError] = useState('');
  const [pending, setPending] = useState(false);
  const [msg, setMsg] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const load = async () => {
    const data = await api<{
      ticket: Ticket;
      files: TicketFile[];
      sensitive: boolean;
      protected_docs?: number;
      can_see_files?: boolean;
      can_download_docs?: boolean;
    }>(`/deals/${dealId}/ticket`);
    setTicket(data.ticket || {});
    setFiles(data.files || []);
    setCanSeeTicketFiles(Boolean(data.can_see_files));
    setCanDownloadDocs(Boolean(data.can_download_docs));
  };

  useEffect(() => {
    void load().catch((e: Error) => setError(e.message));
  }, [dealId]);

  const save = async (next: Ticket = ticket) => {
    setPending(true);
    setError('');
    const from = (next.from_city || '').trim();
    const to = (next.to_city || '').trim();
    try {
      await api(`/deals/${dealId}/ticket`, {
        method: 'PUT',
        body: JSON.stringify({
          transport: next.transport || '',
          from_city: from,
          to_city: to,
          flight_or_train: next.flight_or_train || '',
          depart_at: next.depart_at || '',
          arrive_at: next.arrive_at || '',
          route: from && to ? `${from} → ${to}` : next.route || '',
          notes: next.notes || '',
          wagon: next.wagon || '',
          seat: next.seat || '',
          amount: next.amount,
        }),
      });
      onChanged?.();
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : 'ошибка');
      return false;
    } finally {
      setPending(false);
    }
  };

  const set = (k: keyof Ticket, v: string | number | undefined) => {
    setConfirmed(false);
    const errorKey =
      k === 'from_city'
        ? 'from'
        : k === 'to_city'
          ? 'to'
          : k === 'transport'
            ? 'transport'
            : k === 'depart_at'
              ? 'depart'
              : k === 'arrive_at'
                ? 'arrive'
                : '';
    if (errorKey && String(v ?? '').trim()) {
      setFieldErrors(prev => {
        if (!prev[errorKey]) return prev;
        const next = { ...prev };
        delete next[errorKey];
        return next;
      });
    }
    setTicket(prev => {
      const next = { ...prev, [k]: v };
      if (canFill) {
        if (saveTimer.current) clearTimeout(saveTimer.current);
        saveTimer.current = setTimeout(() => void save(next), 500);
      }
      return next;
    });
  };

  const confirmTicket = async () => {
    const missing: Record<string, string> = {};
    if (!(ticket.from_city || '').trim()) missing.from = 'Откуда не заполнено';
    if (!(ticket.to_city || '').trim()) missing.to = 'Куда не заполнено';
    if (!transportValue(ticket.transport)) missing.transport = 'Транспорт не заполнен';
    if (!splitWhen(ticket.depart_at).date) missing.depart = 'Выезд не заполнен';
    if (!splitWhen(ticket.arrive_at).date) missing.arrive = 'Прибытие не заполнено';
    setFieldErrors(missing);
    if (Object.keys(missing).length) {
      setConfirmed(false);
      return;
    }
    if (saveTimer.current) clearTimeout(saveTimer.current);
    setConfirming(true);
    const ok = await save(ticket);
    setConfirming(false);
    if (ok) {
      setConfirmed(true);
      await onConfirmed?.();
    }
  };

  const sendCandidate = async () => {
    setPending(true);
    setError('');
    try {
      const res = await api<{ as_owner?: string; sent_file?: boolean; note?: string }>(
        `/deals/${dealId}/ticket/send-to-candidate`,
        { method: 'POST', body: '{}' }
      );
      const who = res.as_owner ? ` от имени ${res.as_owner}` : '';
      const fileNote = res.sent_file ? '' : res.note ? ` · ${res.note}` : '';
      setMsg(`Билет отправлен кандидату${who}${fileNote}`);
      onChanged?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'ошибка');
    } finally {
      setPending(false);
    }
  };

  const depart = splitWhen(ticket.depart_at);
  const arrive = splitWhen(ticket.arrive_at);
  const tripLocked = viewOnly || !canFill;

  return (
    <section className={popupOnly ? '' : 'mt-4'}>
      {popupOnly ? null : (
      <button
        type="button"
        className="flex w-full items-center justify-between rounded-[12px] border border-black/8 bg-white px-3 py-2.5 text-left hover:bg-[#f7f8fa]"
        onClick={() => setOpen(true)}
      >
        <span>
          <span className="block text-[13px] font-semibold text-b24-text-dark">Билет</span>
          <span className="block text-[12px] text-b24-text-muted">
            {[ticket.from_city, ticket.to_city].filter(Boolean).join(' → ') || 'Открыть и заполнить'}
          </span>
        </span>
        <span className="text-[12px] text-b24-cyan">Открыть</span>
      </button>
      )}
      {popupOnly ? null : <BiletDocsButton dealId={dealId} onSent={onChanged} />}
      {popupOnly || (!isLogistRole(role) && role !== 'admin') ? null : (
        <TicketReportButton
          dealId={dealId}
          from={ticket.from_city}
          to={ticket.to_city}
          onChanged={() => {
            void load();
            onChanged?.();
          }}
        />
      )}
      {popupOnly ? null : <NightLogistNotes dealId={dealId} />}
      {popupOnly || (role !== 'admin' && !isLogistRole(role)) ? null : (
        <SendToCustomer dealId={dealId} files={files} canSend={role === 'admin' || isLogistRole(role)} />
      )}
      {error && !open && !popupOnly ? <p className="mt-2 text-[12px] text-red-600">{error}</p> : null}

      {open
        ? createPortal(
      <div
        className="fixed z-[200] w-[min(560px,calc(100vw-16px))] overflow-hidden rounded-[16px] bg-[#f4efe4] text-[#10243e] shadow-[0_16px_48px_rgba(15,23,42,0.28)] ring-1 ring-black/10"
        style={{ left: pos.x, top: pos.y }}
      >
        <div
          className="flex cursor-grab items-center justify-between bg-[#10243e] px-4 py-3 text-white active:cursor-grabbing"
          onPointerDown={e => {
            if ((e.target as HTMLElement).closest('button')) return
            drag.current = { dx: e.clientX - pos.x, dy: e.clientY - pos.y }
            e.currentTarget.setPointerCapture(e.pointerId)
          }}
          onPointerMove={e => {
            if (!drag.current) return
            setPos({ x: e.clientX - drag.current.dx, y: e.clientY - drag.current.dy })
          }}
          onPointerUp={() => {
            drag.current = null
          }}
        >
          <div>
            <p className="text-[10px] uppercase tracking-[0.2em] text-white/50">Поездка</p>
            <p className="text-[15px] font-semibold">Билет</p>
            {hint ? <p className="mt-1 max-w-[280px] text-[12px] font-normal text-white/70">{hint}</p> : null}
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              className="ml-2 inline-flex size-8 items-center justify-center rounded-md text-white/80 hover:bg-white/10"
              onClick={() => {
                setOpen(false);
                onForceClose?.();
              }}
              aria-label="Закрыть"
            >
              <X className="size-4" />
            </button>
          </div>
        </div>

        <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-2 px-4 pt-4">
          <label>
            <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
              Откуда
            </span>
            <span className="flex items-center gap-1">
              <input
                className={`min-w-0 flex-1 bg-transparent text-[18px] font-semibold outline-none placeholder:text-[#10243e]/25 disabled:opacity-70 ${
                  viewOnly ? 'pointer-events-none' : ''
                }`}
                placeholder="Город"
                readOnly={viewOnly}
                disabled={tripLocked && !viewOnly}
                value={ticket.from_city || ''}
                onChange={e => set('from_city', e.target.value)}
              />
              {viewOnly ? <CopyBeside text={ticket.from_city || ''} /> : null}
            </span>
            {fieldErrors.from ? (
              <span className="mt-1 block text-[12px] font-medium text-red-600">{fieldErrors.from}</span>
            ) : null}
          </label>
          <span className="pb-1 text-[18px] text-[#10243e]/35">→</span>
          <label className="text-right">
            <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
              Куда
            </span>
            <span className="flex items-center justify-end gap-1">
              {viewOnly ? <CopyBeside text={ticket.to_city || ''} /> : null}
              <input
                className={`min-w-0 flex-1 bg-transparent text-right text-[18px] font-semibold outline-none placeholder:text-[#10243e]/25 disabled:opacity-70 ${
                  viewOnly ? 'pointer-events-none' : ''
                }`}
                placeholder="Город"
                readOnly={viewOnly}
                disabled={tripLocked && !viewOnly}
                value={ticket.to_city || ''}
                onChange={e => set('to_city', e.target.value)}
              />
            </span>
            {fieldErrors.to ? (
              <span className="mt-1 block text-[12px] font-medium text-red-600">{fieldErrors.to}</span>
            ) : null}
          </label>
        </div>

        <div className="mt-4 px-4">
          <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
            Транспорт
          </span>
          <div className="mt-1.5">
            {viewOnly ? (
              <span className="flex items-center gap-1">
                <span className="text-[16px] font-semibold">{transportLabel(ticket.transport) || '—'}</span>
                <CopyBeside text={transportLabel(ticket.transport)} />
              </span>
            ) : (
              <TicketSelect
                value={transportValue(ticket.transport)}
                placeholder="Выберите транспорт"
                disabled={tripLocked}
                invalid={Boolean(fieldErrors.transport)}
                options={[
                  { value: '', label: 'Выберите транспорт' },
                  { value: 'самолет', label: 'Самолёт' },
                  { value: 'поезд', label: 'Поезд' },
                  { value: 'автобус', label: 'Автобус' },
                ]}
                onChange={value => set('transport', value)}
              />
            )}
          </div>
          {fieldErrors.transport ? (
            <p className="mt-1 text-[12px] font-medium text-red-600">{fieldErrors.transport}</p>
          ) : null}
        </div>

        <div className="mt-4 flex flex-col gap-3 px-4 pb-1">
          {viewOnly ? (
            <>
              <div>
                <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
                  Выезд
                </span>
                <span className="mt-1 flex items-center gap-1">
                  <span className="text-[15px] font-semibold">{whenLine(ticket.depart_at) || '—'}</span>
                  <CopyBeside text={whenLine(ticket.depart_at)} />
                </span>
              </div>
              <div>
                <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
                  Прибытие
                </span>
                <span className="mt-1 flex items-center gap-1">
                  <span className="text-[15px] font-semibold">{whenLine(ticket.arrive_at) || '—'}</span>
                  <CopyBeside text={whenLine(ticket.arrive_at)} />
                </span>
              </div>
            </>
          ) : (
            <>
              <div>
                <WhenFields
                  label="Выезд"
                  date={depart.date}
                  time={depart.time}
                  disabled={tripLocked}
                  invalid={Boolean(fieldErrors.depart)}
                  onChange={(date, time) => set('depart_at', joinWhen(date, time))}
                />
                {fieldErrors.depart ? (
                  <p className="mt-1 text-[12px] font-medium text-red-600">{fieldErrors.depart}</p>
                ) : null}
              </div>
              <div>
                <WhenFields
                  label="Прибытие"
                  date={arrive.date}
                  time={arrive.time}
                  disabled={tripLocked}
                  invalid={Boolean(fieldErrors.arrive)}
                  onChange={(date, time) => set('arrive_at', joinWhen(date, time))}
                />
                {fieldErrors.arrive ? (
                  <p className="mt-1 text-[12px] font-medium text-red-600">{fieldErrors.arrive}</p>
                ) : null}
              </div>
            </>
          )}
        </div>

        <div className="relative mt-4">
          <div className="absolute -left-2 top-0 size-4 -translate-y-1/2 rounded-full bg-black/40" />
          <div className="absolute -right-2 top-0 size-4 -translate-y-1/2 rounded-full bg-black/40" />
          <div className="border-t border-dashed border-[#10243e]/20" />
        </div>

        <div className="px-4 py-3">
          <div className="rounded-xl bg-[#fff6dc] px-3 py-2.5 ring-1 ring-[#d7a84a]">
            <div className="flex items-center justify-between gap-2">
              <p className="text-[11px] font-semibold uppercase tracking-[0.14em] text-[#8a6420]">Комментарий</p>
              {viewOnly ? <CopyBeside text={ticket.notes || ''} /> : null}
            </div>
            {viewOnly ? null : (
              <p className="mt-1 text-[12px] leading-snug text-[#10243e]/75">
                Если человек прислал данные текстом — паспорт, серию, номер — напишите их сюда.
              </p>
            )}
            <textarea
              className={`mt-2 min-h-16 w-full resize-none bg-transparent text-[14px] outline-none placeholder:text-[#10243e]/35 disabled:opacity-70 ${
                viewOnly ? 'pointer-events-none' : ''
              }`}
              placeholder={viewOnly ? '' : 'Данные текстом'}
              readOnly={viewOnly}
              disabled={tripLocked && !viewOnly}
              value={ticket.notes || ''}
              onChange={e => set('notes', e.target.value)}
            />
          </div>
          {canSeeFiles && files.length > 0 ? (
            <div className="mt-2 flex flex-col gap-1">
              {files.map(f => {
                const isDoc = f.kind === 'doc' || f.kind === 'passport' || f.kind === 'document';
                const canDl = (isDoc && canDownloadDocs) || !isDoc;
                return (
                  <div key={f.id} className="flex items-center gap-2 text-[12px]">
                    <button
                      type="button"
                      className="truncate text-left text-[#10243e] underline decoration-[#10243e]/20"
                      disabled={!canDl}
                      onClick={() =>
                        canDl &&
                        void apiDownload(`/deals/${dealId}/ticket/files/${f.id}`, f.filename || 'file')
                      }
                    >
                      {f.filename || 'файл'}
                    </button>
                    {f.kind === 'ticket' && (role === 'admin' || isLogistRole(role)) ? (
                      <button
                        type="button"
                        className="shrink-0 text-[#10243e]/60 hover:text-[#10243e]"
                        disabled={pending}
                        onClick={() => void sendCandidate()}
                      >
                        кандидату
                      </button>
                    ) : null}
                  </div>
                );
              })}
            </div>
          ) : null}
        </div>
        {canFill ? (
          <div className="px-4 pb-4">
            <button
              type="button"
              disabled={confirming}
              onClick={() => void confirmTicket()}
              className={`flex h-[52px] w-full items-center justify-center gap-2 rounded-xl text-[16px] font-semibold tracking-[0.03em] shadow-[0_10px_24px_rgba(180,130,40,0.32)] ring-1 transition active:translate-y-px disabled:opacity-60 ${
                confirmed
                  ? 'bg-[#145c45] text-[#f6f0e4] ring-white/20'
                  : 'bg-gradient-to-b from-[#f3d48a] to-[#d7a84a] text-[#10243e] ring-[#a8832e]/50 hover:from-[#f8df9e] hover:to-[#e0b45a]'
              }`}
            >
              <Check className="size-4" />
              {confirmed ? 'Подтверждено' : confirming ? 'Сохраняем…' : 'Подтвердить'}
            </button>
          </div>
        ) : null}
        {error ? <p className="px-4 pb-3 text-[12px] text-red-600">{error}</p> : null}
        {msg ? <p className="px-4 pb-3 text-[12px] text-[#145c45]">{msg}</p> : null}
      </div>,
      document.body,
        )
        : null}
    </section>
  );
}

const reportFieldClass =
  'h-12 w-full rounded-xl bg-[#145c45] px-3 text-[15px] font-semibold text-[#f6f0e4] shadow-[0_8px_18px_rgba(20,92,69,0.28)] ring-1 ring-white/15 outline-none placeholder:text-[#f6f0e4]/55';

function TicketReportButton({
  dealId,
  from,
  to,
  onChanged,
}: {
  dealId: string;
  from?: string;
  to?: string;
  onChanged?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<Ticket>({});
  const [pos, setPos] = useState(() => ({
    x: Math.max(16, window.innerWidth - 644),
    y: Math.max(16, window.innerHeight - 820),
  }));
  const drag = useRef<{ dx: number; dy: number } | null>(null);
  const [error, setError] = useState('');
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState(false);

  const patch = (key: keyof Ticket, value: string) => {
    setDraft(prev => ({ ...prev, [key]: value }));
    setSent(false);
    setFieldErrors(prev => {
      if (!prev[key]) return prev;
      const next = { ...prev };
      delete next[key];
      return next;
    });
  };

  const openReport = async () => {
    setError('');
    setSent(false);
    setFieldErrors({});
    setOpen(true);
    try {
      const data = await api<{ ticket: Ticket }>(`/deals/${dealId}/ticket`);
      setDraft(data.ticket || {});
    } catch (e) {
      setError(e instanceof Error ? e.message : 'ошибка');
    }
  };

  const send = async () => {
    const fromCity = (draft.from_city || '').trim();
    const toCity = (draft.to_city || '').trim();
    const transport = (draft.transport || '').trim();
    const depart = splitWhen(draft.depart_at);
    const missing: Record<string, string> = {};
    if (!fromCity) missing.from_city = 'Откуда не заполнено';
    if (!toCity) missing.to_city = 'Куда не заполнено';
    if (!transport) missing.transport = 'Выберите транспорт';
    if (!depart.date) missing.depart_at = 'Выезд не заполнен';
    setFieldErrors(missing);
    if (Object.keys(missing).length) return;

    setSending(true);
    setError('');
    try {
      await api(`/deals/${dealId}/ticket/report-to-manager`, {
        method: 'POST',
        body: JSON.stringify({
          transport,
          from_city: fromCity,
          to_city: toCity,
          flight_or_train: (draft.flight_or_train || '').trim(),
          depart_at: draft.depart_at || '',
          arrive_at: draft.arrive_at || '',
          wagon: (draft.wagon || '').trim(),
          seat: (draft.seat || '').trim(),
        }),
      });
      setSent(true);
      onChanged?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'ошибка');
    } finally {
      setSending(false);
    }
  };

  const depart = splitWhen(draft.depart_at);
  const arrive = splitWhen(draft.arrive_at);
  const route = [from, to].filter(Boolean).join(' → ');

  return (
    <>
      <button
        type="button"
        className="mt-2 flex w-full items-center justify-between rounded-[12px] border border-black/8 bg-white px-3 py-2.5 text-left hover:bg-[#f7f8fa]"
        onClick={() => void openReport()}
      >
        <span>
          <span className="block text-[13px] font-semibold text-b24-text-dark">Отчёт менеджеру</span>
          <span className="block text-[12px] text-b24-text-muted">{route || 'Открыть'}</span>
        </span>
        <span className="text-[12px] text-b24-cyan">Открыть</span>
      </button>
      {open
        ? createPortal(
            <div
              className="fixed z-[200] w-[min(560px,calc(100vw-16px))] overflow-hidden rounded-[16px] bg-[#f4efe4] text-[#10243e] shadow-[0_16px_48px_rgba(15,23,42,0.28)] ring-1 ring-black/10"
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
                  <p className="text-[10px] uppercase tracking-[0.2em] text-white/50">Поездка</p>
                  <p className="text-[15px] font-semibold">Отчёт менеджеру</p>
                </div>
                <button
                  type="button"
                  className="ml-2 inline-flex size-8 items-center justify-center rounded-md text-white/80 hover:bg-white/10"
                  onClick={() => setOpen(false)}
                  aria-label="Закрыть"
                >
                  <X className="size-4" />
                </button>
              </div>

              <div className="grid grid-cols-[1fr_auto_1fr] items-end gap-2 px-4 pt-4">
                <label>
                  <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
                    Откуда
                  </span>
                  <input
                    className="w-full bg-transparent text-[18px] font-semibold outline-none placeholder:text-[#10243e]/25"
                    placeholder="Город"
                    value={draft.from_city || ''}
                    onChange={e => patch('from_city', e.target.value)}
                  />
                  {fieldErrors.from_city ? (
                    <span className="mt-1 block text-[12px] font-medium text-red-600">{fieldErrors.from_city}</span>
                  ) : null}
                </label>
                <span className="pb-1 text-[18px] text-[#10243e]/35">→</span>
                <label className="text-right">
                  <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
                    Куда
                  </span>
                  <input
                    className="w-full bg-transparent text-right text-[18px] font-semibold outline-none placeholder:text-[#10243e]/25"
                    placeholder="Город"
                    value={draft.to_city || ''}
                    onChange={e => patch('to_city', e.target.value)}
                  />
                  {fieldErrors.to_city ? (
                    <span className="mt-1 block text-[12px] font-medium text-red-600">{fieldErrors.to_city}</span>
                  ) : null}
                </label>
              </div>

              <div className="mt-4 px-4">
                <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
                  Транспорт
                </span>
                <div className="mt-1.5">
                  <TicketSelect
                    value={transportValue(draft.transport)}
                    placeholder="Выберите транспорт"
                    invalid={Boolean(fieldErrors.transport)}
                    options={[
                      { value: '', label: 'Выберите транспорт' },
                      { value: 'самолет', label: 'Самолёт' },
                      { value: 'поезд', label: 'Поезд' },
                      { value: 'автобус', label: 'Автобус' },
                    ]}
                    onChange={value => patch('transport', value)}
                  />
                </div>
                {fieldErrors.transport ? (
                  <p className="mt-1 text-[12px] font-medium text-red-600">{fieldErrors.transport}</p>
                ) : null}
              </div>

              <div className="mt-4 grid grid-cols-3 gap-2 px-4">
                {(
                  [
                    ['flight_or_train', 'Рейс'],
                    ['wagon', 'Вагон'],
                    ['seat', 'Место'],
                  ] as const
                ).map(([key, label]) => (
                  <label key={key}>
                    <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#10243e]/50">
                      {label}
                    </span>
                    <input
                      className={`mt-1.5 ${reportFieldClass}`}
                      value={draft[key] || ''}
                      onChange={e => patch(key, e.target.value)}
                    />
                  </label>
                ))}
              </div>

              <div className="mt-4 flex flex-col gap-3 px-4 pb-4">
                <div>
                  <WhenFields
                    label="Выезд"
                    date={depart.date}
                    time={depart.time}
                    invalid={Boolean(fieldErrors.depart_at)}
                    onChange={(date, time) => patch('depart_at', joinWhen(date, time))}
                  />
                  {fieldErrors.depart_at ? (
                    <p className="mt-1 text-[12px] font-medium text-red-600">{fieldErrors.depart_at}</p>
                  ) : null}
                </div>
                <WhenFields
                  label="Прибытие"
                  date={arrive.date}
                  time={arrive.time}
                  onChange={(date, time) => patch('arrive_at', joinWhen(date, time))}
                />
              </div>

              <div className="px-4 pb-4">
                <button
                  type="button"
                  disabled={sending}
                  onClick={() => void send()}
                  className={`flex h-[52px] w-full items-center justify-center gap-2 rounded-xl text-[16px] font-semibold tracking-[0.03em] shadow-[0_10px_24px_rgba(180,130,40,0.32)] ring-1 transition active:translate-y-px disabled:opacity-60 ${
                    sent
                      ? 'bg-[#145c45] text-[#f6f0e4] ring-white/20'
                      : 'bg-gradient-to-b from-[#f3d48a] to-[#d7a84a] text-[#10243e] ring-[#a8832e]/50 hover:from-[#f8df9e] hover:to-[#e0b45a]'
                  }`}
                >
                  <Check className="size-4" />
                  {sent ? 'Отправлено менеджеру' : sending ? 'Отправляем…' : 'Отправить менеджеру'}
                </button>
                {error ? <p className="mt-2 text-[12px] text-red-600">{error}</p> : null}
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}

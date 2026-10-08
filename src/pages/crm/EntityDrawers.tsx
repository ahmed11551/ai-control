import { Check, ChevronDown, UserRound } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { BiletFileModal, biletGate } from '../../components/crm/BiletFileModal';
import { BindMaxModal } from '../../components/crm/BindMaxModal';
import { CrmDrawer, CrmFieldRow, CrmRelated } from '../../components/crm/CrmDrawer';
import { DealTicketPanel } from '../../components/crm/DealTicketPanel';
import { StaffDialogEntry } from '../../components/crm/StaffDialogEntry';
import {
  LeadCardPanel,
  LeadCommentComposer,
  LeadFieldLine,
  LeadPlanPanel,
  LeadTimelineCard,
  LeadTimelineEvent,
  CallRecordingPlayer,
  MaxIphonePreview,
  PhoneCallTimeBanner,
  type TimelineEventKind,
} from '../../components/crm/LeadCardPanel';
import {
  StageChevronBar,
  type StageChevronItem,
} from '../../components/crm/StageChevronBar';
import { EntitySearch } from '../../components/crm/EntitySearch';
import { Button } from '../../components/ui/Button';
import { api } from '../../lib/api';
import { canSeePhones, isLogistRole } from '../../lib/roles';
import { type CrmRow, leadStatusLabel, money, sourceLabel } from '../../lib/crm';
import { useAuth } from '../../store/auth';
import { useTelephony } from '../../store/telephony';
import { callReachLabel } from '../../telephony/callDisposition';

function asList(value: unknown): CrmRow[] {
  return Array.isArray(value) ? (value as CrmRow[]) : [];
}

function text(row: CrmRow | null, key: string) {
  const value = row?.[key];
  return value == null ? '' : String(value);
}

function ticketStageReached(
  row: CrmRow | null,
  stages?: { id: string; name: string; short_name?: string }[],
) {
  const currentId = text(row, 'stage_id');
  const currentName = text(row, 'stage_short') || text(row, 'stage');
  const list = (stages?.length ? stages : asList(row?.stages)).map(stage => ({
    id: String(stage.id || ''),
    name: String(stage.name || ''),
    short: String(stage.short_name || ''),
  }));
  const biletIdx = list.findIndex(stage => stage.short === 'Билет' || stage.name === 'Билет');
  const currentIdx = list.findIndex(
    stage => stage.id === currentId || stage.short === currentName || stage.name === currentName,
  );
  if (biletIdx >= 0 && currentIdx >= 0) return currentIdx >= biletIdx;
  return currentName === 'Билет';
}

function formatTimelineTime(raw: string) {
  if (!raw) return '';
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function formatCallDuration(sec: number) {
  if (!sec || sec <= 0) return '';
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  return `${m}:${String(s).padStart(2, '0')}`;
}

function digitsPhone(raw: string) {
  return raw.replace(/\D/g, '').replace(/^8(\d{10})$/, '7$1');
}

function parsePayload(raw: unknown): Record<string, unknown> {
  if (raw && typeof raw === 'object' && !Array.isArray(raw))
    return raw as Record<string, unknown>;
  if (typeof raw !== 'string' || !raw.trim()) return {};
  try {
    const v = JSON.parse(raw) as unknown;
    return v && typeof v === 'object' && !Array.isArray(v)
      ? (v as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

type StageMeta = {
  name: string;
  short: string;
  desc: string;
};

type TimelineItem = {
  key: string;
  at: number;
  time: string;
  title: string;
  body?: string;
  kind?: TimelineEventKind;
  callKey?: string;
  score?: number;
  /** CRM call id — для плеера записи Exolve */
  recordingCallId?: string;
};

function callDedupeKey(peer: string, at: number, direction: string) {
  const bucket = Math.floor(at / 60000); // минута
  return `${direction}|${digitsPhone(peer)}|${bucket}`;
}

function stageKindFromName(name: string): TimelineEventKind {
  return /недозвон/i.test(name) ? 'nedozvon' : 'stage';
}

function buildLeadTimeline(
  row: CrmRow | null,
  createdLabel: string,
  stageById?: Map<string, StageMeta>
): TimelineItem[] {
  const items: TimelineItem[] = [];
  const seenCallIds = new Set<string>();
  const callByKey = new Map<string, TimelineItem>();
  const seenActKeys = new Set<string>();

  const pushCall = (item: TimelineItem) => {
    if (item.callKey) {
      const prev = callByKey.get(item.callKey);
      if (prev) {
        // оставляем более «содержательную» запись (с длительностью / дозвоном)
        if ((item.score || 0) <= (prev.score || 0)) {
          if (item.recordingCallId && !prev.recordingCallId)
            prev.recordingCallId = item.recordingCallId;
          return;
        }
        const idx = items.indexOf(prev);
        if (idx >= 0) items.splice(idx, 1);
        if (prev.recordingCallId && !item.recordingCallId)
          item.recordingCallId = prev.recordingCallId;
      }
      callByKey.set(item.callKey, item);
    }
    items.push(item);
  };

  for (const act of asList(row?.activity)) {
    const action = text(act, 'action');
    const payload = parsePayload(act.payload);
    const atRaw = text(act, 'created_at');
    const at = Date.parse(atRaw) || 0;
    const time = formatTimelineTime(atRaw) || createdLabel;
    const actor = text(act, 'actor');
    const actKey = [
      action,
      Math.floor(at / 2000),
      String(payload.call_id || ''),
      String(payload.attempt_id || ''),
      String(payload.stage_id || payload.stage_key || ''),
      String(payload.action || ''),
      action === 'comment' ? String(payload.body || '').slice(0, 80) : '',
      String(act.id || ''),
    ].join('|');
    if (seenActKeys.has(actKey)) continue;
    seenActKeys.add(actKey);
    const key = `act-${String(act.id || actKey)}`;

    if (action === 'call') {
      const callId = String(payload.call_id || '');
      if (callId) seenCallIds.add(callId);
      const dir = String(payload.direction || '');
      const inbound = dir === 'inbound' || dir === 'incoming';
      const peer = String(payload.peer || '');
      const durationSec = Number(payload.duration || 0);
      const dur = formatCallDuration(durationSec);
      const dispRaw = String(payload.disposition || '');
      const missed =
        inbound &&
        (dispRaw.toLowerCase() === 'missed' ||
          dispRaw.toLowerCase() === 'no_answer' ||
          (durationSec <= 0 && callReachLabel(dispRaw, durationSec) === 'Недозвон'));
      const title =
        String(payload.title || '') ||
        (missed
          ? 'Пропущенный звонок'
          : inbound
            ? 'Входящий звонок'
            : 'Исходящий звонок');
      const disp = missed ? 'Нет ответа' : callReachLabel(dispRaw, durationSec);
      const parts = [disp, dur, peer, actor].filter(Boolean);
      const score = (durationSec > 0 ? 10 : 0) + (disp === 'Дозвон' ? 5 : 1);
      const hasRec = Boolean(payload.recording_key || payload.exolve_call_id);
      pushCall({
        key,
        at,
        time,
        title,
        body: parts.join(' · '),
        kind: missed ? 'missed' : 'call',
        callKey: callId || callDedupeKey(peer, at, inbound ? 'in' : 'out'),
        score,
        recordingCallId: hasRec && callId ? callId : undefined,
      });
      continue;
    }

    if (action === 'created') {
      // deal.created попадает в UNION после конвертации — не дублируем «Источник»
      if (text(act, 'entity_type') === 'deal') continue;
      if (items.some(i => i.kind === 'created')) continue;
      items.push({
        key,
        at,
        time,
        title: actor || text(row, 'title') || 'Лид создан',
        body: `Источник: ${sourceLabel(text(row, 'source') || String(payload.source || ''))}`,
        kind: 'created',
      });
      continue;
    }

    if (action === 'converted' || action === 'convert') {
      items.push({
        key,
        at,
        time,
        title: 'Сделка создана',
        body: 'Лид конвертирован в сделку',
        kind: 'converted',
      });
      continue;
    }

    if (action === 'comment') {
      items.push({
        key,
        at,
        time,
        title: actor ? `Комментарий · ${actor}` : 'Комментарий',
        body: String(payload.body || ''),
        kind: 'comment',
      });
      continue;
    }

    if (action === 'task_planned') {
      const due = String(payload.due_at || '');
      const dueLabel = due
        ? new Date(due).toLocaleString('ru-RU', {
            day: 'numeric',
            month: 'short',
            hour: '2-digit',
            minute: '2-digit',
          })
        : '';
      const parts = [
        String(payload.description || ''),
        dueLabel ? `Срок: ${dueLabel}` : '',
        payload.is_important ? 'Важная' : '',
        actor,
      ].filter(Boolean);
      items.push({
        key,
        at,
        time,
        title: String(payload.title || 'Запланировано дело'),
        body: parts.join(' · ') || undefined,
        kind: 'task_planned',
      });
      continue;
    }

    if (action === 'task_resolved') {
      const resultMap: Record<string, string> = {
        done: 'Выполнена',
        not_done: 'Не выполнена',
        no_answer: 'Не ответил',
        reschedule: 'Перенесена',
        other: 'Иное',
      };
      const res = resultMap[String(payload.result || '')] || String(payload.result || '');
      items.push({
        key,
        at,
        time,
        title: String(payload.title || 'Дело закрыто'),
        body:
          [res, String(payload.note || ''), actor].filter(Boolean).join(' · ') ||
          undefined,
        kind: 'task_done',
      });
      continue;
    }

    if (action === 'distributed' || action === 'distributed_manager') {
      const parts = [
        payload.department ? String(payload.department) : '',
        payload.owner ? String(payload.owner) : '',
      ].filter(Boolean);
      const stage = String(payload.stage || '');
      const title =
        action === 'distributed_manager' || stage === 'manager'
          ? 'Назначен менеджер'
          : 'Назначен РОП';
      items.push({
        key,
        at,
        time,
        title,
        body: parts.join(' · ') || undefined,
        kind: 'distributed',
      });
      continue;
    }

    if (action === 'duplicate_application') {
      const parts = [
        payload.source ? `Источник: ${String(payload.source)}` : '',
        payload.title ? String(payload.title) : '',
        payload.note ? String(payload.note) : '',
      ].filter(Boolean);
      items.push({
        key,
        at,
        time,
        title: 'Повторная заявка',
        body: parts.join(' · ') || 'Кандидат оставил заявку ещё раз — второй лид не создан',
        kind: 'distributed',
      });
      continue;
    }

    if (action === 'owner_changed_forced') {
      items.push({
        key,
        at,
        time,
        title: 'Ответственный сменён принудительно',
        body: [actor, 'администратор снял закрепление'].filter(Boolean).join(' · '),
        kind: 'distributed',
      });
      continue;
    }

    if (action === 'stage_changed') {
      const stageId = String(payload.stage_id || '');
      const meta = stageId && stageById ? stageById.get(stageId) : undefined;
      const stageName = String(
        payload.stage_name ||
          payload.stage_short ||
          meta?.name ||
          payload.stage_key ||
          'Стадия изменена'
      );
      const desc = String(payload.stage_desc || meta?.desc || '');
      const body = [desc || undefined, actor].filter(Boolean).join(' · ') || undefined;
      items.push({
        key,
        at,
        time,
        title: stageName,
        body,
        kind: stageKindFromName(stageName),
      });
      continue;
    }

    if (action === 'autodial_started') {
      items.push({
        key,
        at,
        time,
        title: 'Скорозвон',
        body: 'Автодозвон запущен',
        kind: 'skorozvon',
      });
      continue;
    }

    if (action === 'autodial_offered') {
      items.push({
        key,
        at,
        time,
        title: 'Скорозвон: на линии',
        body: 'Кандидат ответил, ожидание менеджера',
        kind: 'skorozvon',
      });
      continue;
    }

    if (action === 'autodial_claimed') {
      const manager = String(payload.manager || actor || '');
      items.push({
        key,
        at,
        time,
        title: 'Скорозвон принят',
        body: manager ? `Менеджер: ${manager}` : undefined,
        kind: 'sk_accept',
      });
      continue;
    }

    if (action === 'autodial_finished') {
      const finishKind = String(payload.kind || '');
      const detail = String(payload.detail || '');
      const status = String(payload.status || payload.deal_status || '');
      let title = 'Скорозвон: разговор';
      let body = detail || undefined;
      let kind: TimelineEventKind = 'sk_talk';
      if (finishKind === 'no_answer' || status === 'no_answer' || status === 'busy') {
        title = 'Недозвон скорозвона';
        body = 'Кандидат не ответил';
        kind = 'sk_fail';
      } else if (
        finishKind === 'reject' ||
        status === 'rejected' ||
        status === 'canceled'
      ) {
        title = 'Скорозвон: сброс';
        body = detail || status || undefined;
        kind = 'sk_fail';
      } else if (finishKind === 'error' || status === 'error' || status === 'failed') {
        title = 'Скорозвон: сбой';
        body = detail || 'Ошибка линии';
        kind = 'sk_fail';
      } else if (finishKind === 'talk' || status === 'connected') {
        title = 'Скорозвон: разговор';
        body = detail || undefined;
        kind = 'sk_talk';
      }
      items.push({ key, at, time, title, body, kind });
      continue;
    }

    if (action === 'autodial_disposition') {
      const disp = String(payload.action || '');
      const reason = String(payload.reason || '');
      const newOwner = String(payload.new_owner_name || '');
      const stageName = String(payload.stage_name || '');
      if (disp === 'take') {
        items.push({
          key,
          at,
          time,
          title: 'Забран после скорозвона',
          body: [
            newOwner ? `Новый ответственный: ${newOwner}` : '',
            stageName || 'В работе',
            reason,
          ]
            .filter(Boolean)
            .join(' · '),
          kind: 'sk_take',
        });
      } else if (disp === 'return') {
        items.push({
          key,
          at,
          time,
          title: 'Вернулся после скорозвона',
          body: [
            newOwner ? `Владельцу: ${newOwner}` : '',
            stageName || 'Принято после скорозвона',
            reason,
          ]
            .filter(Boolean)
            .join(' · '),
          kind: 'sk_return',
        });
      } else if (disp === 'bad') {
        items.push({
          key,
          at,
          time,
          title: 'Некачественный лид',
          body: ['После скорозвона', reason].filter(Boolean).join(' · '),
          kind: 'sk_bad',
        });
      } else {
        items.push({
          key,
          at,
          time,
          title: 'Решение после скорозвона',
          body: [disp, reason].filter(Boolean).join(' · ') || undefined,
          kind: 'skorozvon',
        });
      }
      continue;
    }

    if (action === 'originate' || action === 'call_started') {
      const to = String(payload.to || '');
      items.push({
        key,
        at,
        time,
        title: 'Звонок инициирован',
        body: to ? `на ${to}` : actor || undefined,
        kind: 'originate',
      });
      continue;
    }
  }

  for (const call of asList(row?.calls)) {
    const callId = text(call, 'id');
    if (callId && seenCallIds.has(callId)) continue;
    const atRaw = text(call, 'started_at');
    const at = Date.parse(atRaw) || 0;
    const dir = text(call, 'direction');
    const inbound = dir === 'inbound' || dir === 'incoming';
    const peer = inbound ? text(call, 'from_number') : text(call, 'to_number');
    const durationSec = Number(call.duration_sec || 0);
    const dur = formatCallDuration(durationSec);
    const dispRaw = text(call, 'disposition');
    const missed =
      inbound &&
      (dispRaw.toLowerCase() === 'missed' ||
        dispRaw.toLowerCase() === 'no_answer' ||
        (durationSec <= 0 && callReachLabel(dispRaw, durationSec) === 'Недозвон'));
    const disp = missed ? 'Нет ответа' : callReachLabel(dispRaw, durationSec);
    const score = (durationSec > 0 ? 10 : 0) + (disp === 'Дозвон' ? 5 : 1);
    const hasRec = Boolean(text(call, 'recording_key') || text(call, 'exolve_call_id'));
    pushCall({
      key: `call-${callId || atRaw}`,
      at,
      time: formatTimelineTime(atRaw),
      title: missed
        ? 'Пропущенный звонок'
        : inbound
          ? 'Входящий звонок'
          : 'Исходящий звонок',
      body: [disp, dur, peer].filter(Boolean).join(' · '),
      kind: missed ? 'missed' : 'call',
      callKey: callId || callDedupeKey(peer, at, inbound ? 'in' : 'out'),
      score,
      recordingCallId: hasRec && callId ? callId : undefined,
    });
  }

  const hasCreated = items.some(i => i.kind === 'created');
  if (!hasCreated) {
    const createdAt = text(row, 'created_at');
    items.push({
      key: 'created-fallback',
      at: Date.parse(createdAt) || 0,
      time: createdLabel,
      title: text(row, 'title') || 'Лид создан',
      body: `Источник: ${sourceLabel(text(row, 'source'))}`,
      kind: 'created',
    });
  }

  const converted = text(row, 'converted_deal_id');
  if (converted && !items.some(i => i.title === 'Сделка создана')) {
    items.push({
      key: 'converted-fallback',
      at: Date.parse(text(row, 'created_at')) || 0,
      time: createdLabel,
      title: 'Сделка создана',
      body: 'Лид конвертирован в сделку',
      kind: 'converted',
    });
  }

  items.sort((a, b) => b.at - a.at);
  return items;
}

function ManagerPicker({
  managers,
  ownerId,
  ownerName = '',
  assigning,
  onSelect,
}: {
  managers: { id: string; full_name: string }[];
  ownerId: string;
  ownerName?: string;
  assigning: boolean;
  onSelect: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const selectedManager = managers.find(manager => manager.id === ownerId);
  const label =
    selectedManager?.full_name ||
    (ownerId && ownerName ? ownerName : '') ||
    'Выбрать менеджера';
  const assigned = Boolean(selectedManager || (ownerId && ownerName));

  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', close);
    return () => document.removeEventListener('mousedown', close);
  }, [open]);

  return (
    <div ref={root} className="relative w-full max-w-[270px]">
      <button
        type="button"
        disabled={assigning || (managers.length === 0 && !assigned)}
        onClick={() => setOpen(value => !value)}
        className="group flex min-h-11 w-full items-center gap-2.5 rounded-[12px] border border-black/10 bg-white px-2.5 py-2 text-left transition hover:border-b24-cyan/60 disabled:cursor-not-allowed disabled:opacity-60"
      >
        <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-b24-cyan/12 text-b24-cyan">
          <UserRound size={16} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13px] font-medium text-[#1a2332]">
            {assigning ? 'Назначаем…' : label}
          </span>
          <span className="block text-[11px] text-b24-text-muted">
            {assigned
              ? 'Менеджер назначен'
              : managers.length
                ? 'Менеджер не выбран'
                : 'Нет менеджеров в отделе'}
          </span>
        </span>
        <ChevronDown
          size={16}
          className={`shrink-0 text-b24-text-muted transition-transform ${open ? 'rotate-180' : ''}`}
        />
      </button>

      {open ? (
        <div className="absolute left-0 top-[calc(100%+6px)] z-30 w-full overflow-hidden rounded-[12px] border border-black/10 bg-white p-1.5">
          <div className="px-2.5 pb-1.5 pt-1 text-[10px] font-semibold uppercase tracking-wide text-b24-text-muted">
            Назначить менеджера
          </div>
          <div className="max-h-52 overflow-y-auto b24-scrollbar">
            {managers.map(manager => {
              const selected = manager.id === ownerId;
              return (
                <button
                  key={manager.id}
                  type="button"
                  onClick={() => {
                    setOpen(false);
                    if (!selected) onSelect(manager.id);
                  }}
                  className={`flex w-full items-center gap-2 rounded-[9px] px-2.5 py-2 text-left text-[13px] transition ${
                    selected
                      ? 'bg-b24-cyan/10 font-medium text-[#1a2332]'
                      : 'text-b24-text hover:bg-[#f2f5f7]'
                  }`}
                >
                  <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-[#e8f0f5] text-[11px] font-semibold text-[#52606d]">
                    {(manager.full_name || '?').slice(0, 1).toUpperCase()}
                  </span>
                  <span className="min-w-0 flex-1 truncate">{manager.full_name}</span>
                  {selected ? (
                    <Check size={15} className="shrink-0 text-b24-cyan" />
                  ) : null}
                </button>
              );
            })}
          </div>
        </div>
      ) : null}
    </div>
  );
}

type FieldVal = {
  code?: string;
  label?: string;
  field_group?: string;
  field_type?: string;
  value?: string;
};

function customFields(row: CrmRow | null): FieldVal[] {
  const raw = row?.fields;
  return Array.isArray(raw) ? (raw as FieldVal[]) : [];
}

function FieldGroups({ row }: { row: CrmRow | null }) {
  const items = customFields(row).filter(f => String(f.value ?? '').trim());
  if (items.length === 0) return null;
  const groups: { name: string; items: FieldVal[] }[] = [];
  for (const field of items) {
    const name = String(field.field_group || 'Поля');
    const last = groups[groups.length - 1];
    if (last && last.name === name) last.items.push(field);
    else groups.push({ name, items: [field] });
  }
  return (
    <div className="flex flex-col gap-4">
      {groups.map(group => (
        <section key={group.name}>
          <h3 className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted mb-2">
            {group.name}
          </h3>
          <div className="flex flex-col gap-2.5">
            {group.items.map(field => (
              <CrmFieldRow
                key={String(field.code)}
                label={String(field.label || field.code)}
              >
                {field.field_type === 'file' ? 'есть файл' : String(field.value)}
              </CrmFieldRow>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}

function CallPhoneButton({
  phone,
  leadId,
  contactId,
  dealId,
}: {
  phone: string;
  leadId?: string;
  contactId?: string;
  dealId?: string;
}) {
  const { user } = useAuth();
  const { dial, snap, configReady } = useTelephony();
  const [err, setErr] = useState('');
  const busy =
    snap.status === 'connecting' ||
    snap.status === 'dialing' ||
    snap.status === 'active' ||
    snap.status === 'ringing';
  const num = phone.trim();
  const showNumber = canSeePhones(user);
  if (!num && !leadId && !contactId && !dealId) return null;
  return (
    <div className="flex flex-col gap-1 self-start">
      <Button
        type="button"
        variant="green"
        className="rounded-b24-sm self-start"
        disabled={busy || !configReady}
        onClick={() => {
          setErr('');
          void (async () => {
            let dest = num;
            if (!canSeePhones(user) || !dest || dest.includes('•')) {
              const res = await api<{ to?: string }>('/calls/resolve-dest', {
                method: 'POST',
                body: JSON.stringify({
                  to: showNumber ? num : '',
                  lead_id: leadId || '',
                  contact_id: contactId || '',
                  deal_id: dealId || '',
                }),
              });
              dest = String(res.to || '');
            }
            if (!dest) throw new Error('Нет номера для звонка');
            await dial(dest, {
              leadId: leadId || undefined,
              dealId: dealId || undefined,
            });
          })().catch((e: Error) => setErr(e.message));
        }}
      >
        {showNumber && num && !num.includes('•') ? `Позвонить ${num}` : 'Позвонить'}
      </Button>
      {err ? <p className="text-[12px] text-red-500">{err}</p> : null}
    </div>
  );
}

function WriteMaxButton({
  phone,
  leadId,
  contactId,
  dealId,
}: {
  phone?: string;
  leadId?: string;
  contactId?: string;
  dealId?: string;
}) {
  const navigate = useNavigate();
  const [err, setErr] = useState('');
  const [note, setNote] = useState('');
  const [invite, setInvite] = useState('');
  const [busy, setBusy] = useState(false);
  const hasRef = Boolean(phone?.trim() || leadId || contactId || dealId);
  if (!hasRef) return null;

  const onClick = async () => {
    setBusy(true);
    setErr('');
    setNote('');
    setInvite('');
    try {
      const res = await api<{
        opened?: boolean;
        thread_id?: string;
        invite_link?: string;
        note?: string;
        via?: string;
        phone?: string;
      }>('/inbox/max/open', {
        method: 'POST',
        body: JSON.stringify({
          phone: phone || '',
          lead_id: leadId || '',
          contact_id: contactId || '',
          deal_id: dealId || '',
        }),
      });
      if (res.via === 'wappi' && res.thread_id) {
        navigate(`/inbox?channel=max&open=${res.thread_id}`);
        return;
      }
      if (res.via === 'wappi' && res.phone) {
        navigate(`/inbox?channel=max&phone=${encodeURIComponent(res.phone)}`);
        return;
      }
      if (res.opened && res.thread_id) {
        navigate(`/inbox?channel=max&open=${res.thread_id}`);
        return;
      }
      setInvite(res.invite_link || '');
      setNote(res.note || 'Клиент ещё не писал в MAX');
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Не удалось открыть MAX');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-1.5 self-start max-w-full">
      <Button
        type="button"
        variant="filter"
        className="rounded-b24-sm self-start"
        disabled={busy}
        onClick={() => void onClick()}
      >
        {busy ? '…' : 'Написать в MAX'}
      </Button>
      {err ? <p className="text-[12px] text-red-500">{err}</p> : null}
      {note ? <p className="text-[12px] text-b24-text-muted">{note}</p> : null}
      {invite ? (
        <div className="flex flex-col gap-1">
          <a
            href={invite}
            target="_blank"
            rel="noreferrer"
            className="text-[12px] text-b24-cyan break-all"
          >
            {invite}
          </a>
          <button
            type="button"
            className="text-[12px] text-b24-cyan text-left"
            onClick={() => void navigator.clipboard.writeText(invite)}
          >
            Скопировать ссылку
          </button>
        </div>
      ) : null}
    </div>
  );
}

export function ContactDrawer({
  id,
  onClose,
  onChanged,
}: {
  id: string;
  onClose: () => void;
  onChanged: () => void;
}) {
  const navigate = useNavigate();
  const [row, setRow] = useState<CrmRow | null>(null);
  const [error, setError] = useState('');

  const load = () =>
    api<CrmRow>(`/contacts/${id}`)
      .then(setRow)
      .catch((e: Error) => setError(e.message));

  useEffect(() => {
    void load();
  }, [id]);

  if (!row && !error) return null;

  return (
    <CrmDrawer
      title={text(row, 'full_name') || 'Контакт'}
      subtitle="Контакт"
      onClose={onClose}
    >
      {error ? <p className="text-[13px] text-red-500">{error}</p> : null}
      <CrmFieldRow label="Телефон">{text(row, 'phone') || '—'}</CrmFieldRow>
      <CallPhoneButton phone={text(row, 'phone')} contactId={id} />
      <WriteMaxButton phone={text(row, 'phone')} contactId={id} />
      <CrmFieldRow label="Email">{text(row, 'email') || '—'}</CrmFieldRow>
      <CrmFieldRow label="Должность">{text(row, 'position') || '—'}</CrmFieldRow>
      <CrmFieldRow label="Компания">
        {text(row, 'company_id') ? (
          <button
            type="button"
            className="text-b24-cyan hover:underline"
            onClick={() => navigate(`/crm/companies?open=${text(row, 'company_id')}`)}
          >
            {text(row, 'company') || 'Компания'}
          </button>
        ) : (
          '—'
        )}
      </CrmFieldRow>
      <CrmFieldRow label="Источник">{sourceLabel(text(row, 'source'))}</CrmFieldRow>
      <FieldGroups row={row} />
      <CrmRelated
        title="Сделки"
        empty="Сделок пока нет"
        items={asList(row?.deals).map(d => ({
          id: String(d.id),
          label: String(d.title ?? 'Сделка'),
          extra: money(d.amount, String(d.currency ?? 'RUB')),
        }))}
        onOpen={dealId => navigate(`/crm?open=${dealId}`)}
      />
      <CrmRelated
        title="Лиды"
        empty="Лидов нет"
        items={asList(row?.leads).map(l => ({
          id: String(l.id),
          label: String(l.title ?? 'Лид'),
          extra: leadStatusLabel(String(l.status ?? '')),
        }))}
        onOpen={leadId => navigate(`/crm/leads?open=${leadId}`)}
      />
      <div className="pt-2">
        <Button
          type="button"
          variant="filter"
          className="rounded-b24-sm"
          onClick={onChanged}
        >
          Изменить
        </Button>
      </div>
    </CrmDrawer>
  );
}

export function CompanyDrawer({
  id,
  onClose,
  onChanged,
}: {
  id: string;
  onClose: () => void;
  onChanged: () => void;
}) {
  const navigate = useNavigate();
  const [row, setRow] = useState<CrmRow | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    api<CrmRow>(`/companies/${id}`)
      .then(setRow)
      .catch((e: Error) => setError(e.message));
  }, [id]);

  if (!row && !error) return null;

  return (
    <CrmDrawer
      title={text(row, 'name') || 'Компания'}
      subtitle="Компания"
      onClose={onClose}
    >
      {error ? <p className="text-[13px] text-red-500">{error}</p> : null}
      <CrmFieldRow label="ИНН">{text(row, 'inn') || '—'}</CrmFieldRow>
      <CrmFieldRow label="Телефон">{text(row, 'phone') || '—'}</CrmFieldRow>
      <CallPhoneButton phone={text(row, 'phone')} />
      <CrmFieldRow label="Email">{text(row, 'email') || '—'}</CrmFieldRow>
      <CrmFieldRow label="Сайт">{text(row, 'website') || '—'}</CrmFieldRow>
      <CrmFieldRow label="Адрес">{text(row, 'address') || '—'}</CrmFieldRow>
      <CrmRelated
        title="Контакты"
        empty="Контактов нет"
        items={asList(row?.contacts).map(c => ({
          id: String(c.id),
          label: String(c.full_name ?? 'Контакт'),
          extra: [c.position, c.phone].filter(Boolean).join(' · '),
        }))}
        onOpen={contactId => navigate(`/crm/contacts?open=${contactId}`)}
      />
      <CrmRelated
        title="Сделки"
        empty="Сделок нет"
        items={asList(row?.deals).map(d => ({
          id: String(d.id),
          label: String(d.title ?? 'Сделка'),
          extra: money(d.amount, String(d.currency ?? 'RUB')),
        }))}
        onOpen={dealId => navigate(`/crm?open=${dealId}`)}
      />
      <div className="pt-2">
        <Button
          type="button"
          variant="filter"
          className="rounded-b24-sm"
          onClick={onChanged}
        >
          Изменить
        </Button>
      </div>
    </CrmDrawer>
  );
}

export function LeadDrawer({
  id,
  seed,
  onClose,
  onChanged,
  onDeleted,
}: {
  id: string;
  /** снимок карточки при открытии */
  seed?: CrmRow | null;
  onClose: () => void;
  onChanged: () => void;
  onDeleted?: () => void;
}) {
  const navigate = useNavigate();
  const { user } = useAuth();
  const { dial, snap, configReady, liveLeadId } = useTelephony();
  const [row, setRow] = useState<CrmRow | null>(() =>
    seed && String(seed.id ?? '') === id ? seed : null
  );
  const [error, setError] = useState('');
  const [pending, setPending] = useState(false);
  const [bindOpen, setBindOpen] = useState(false);
  const [callErr, setCallErr] = useState('');
  const [managers, setManagers] = useState<{ id: string; full_name: string }[]>([]);
  const [assigning, setAssigning] = useState(false);
  const [stageById, setStageById] = useState<Map<string, StageMeta>>(() => new Map());
  const [pipelines, setPipelines] = useState<
    {
      id: string;
      key?: string;
      name?: string;
      stages?: (StageChevronItem & { description?: string })[];
    }[]
  >([]);
  const [movingStage, setMovingStage] = useState(false);
  const [pendingStageId, setPendingStageId] = useState('');
  const [biletFileStage, setBiletFileStage] = useState('');
  const [biletRequestStage, setBiletRequestStage] = useState('');
  const [commentDraft, setCommentDraft] = useState('');
  const [commentPending, setCommentPending] = useState(false);
  const [commentError, setCommentError] = useState('');
  const callBusyRef = useRef(false);
  const location = useLocation();
  const canAssign =
    String(user?.role || '').toLowerCase() === 'rop' ||
    String(user?.role || '').toLowerCase() === 'admin';
  const canDeleteLead = String(user?.role || '').toLowerCase() === 'admin';
  const isLogist = isLogistRole(user?.role);
  const logisticsCabinet =
    isLogist || location.pathname.startsWith('/logistics');

  useEffect(() => {
    setBindOpen(false);
    setError('');
    setCommentDraft('');
    setCommentError('');
    let cancelled = false;
    api<CrmRow>(`/leads/${id}`)
      .then(fresh => {
        if (!cancelled) setRow(fresh);
      })
      .catch((e: Error) => {
        if (!cancelled) setError(e.message);
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  // подтянуть seed при мягком переходе (без сброса карточки)
  useEffect(() => {
    if (seed && String(seed.id ?? '') === id) {
      setRow(prev => prev ?? seed);
    }
  }, [id, seed]);

  // после звонка из карточки — обновить историю
  useEffect(() => {
    const busy =
      snap.status === 'connecting' ||
      snap.status === 'dialing' ||
      snap.status === 'active' ||
      snap.status === 'ringing';
    if (callBusyRef.current && !busy && (liveLeadId === id || !liveLeadId)) {
      void api<CrmRow>(`/leads/${id}`)
        .then(fresh => setRow(fresh))
        .catch(() => undefined);
    }
    callBusyRef.current = busy;
  }, [snap.status, id, liveLeadId]);

  useEffect(() => {
    if (!canAssign) return;
    api<{ managers: { id: string; full_name: string }[] }>(`/leads/${id}/assignees`)
      .then(d => setManagers(d.managers ?? []))
      .catch(() => setManagers([]));
  }, [id, canAssign]);

  useEffect(() => {
    let cancelled = false;
    api<
      {
        id: string;
        key?: string;
        name?: string;
        stages?: {
          id: string;
          name: string;
          short_name?: string;
          description?: string;
          is_won?: boolean;
          is_lost?: boolean;
        }[];
      }[]
    >('/pipelines')
      .then(pipes => {
        if (cancelled) return;
        const map = new Map<string, StageMeta>();
        for (const p of pipes || []) {
          for (const s of p.stages || []) {
            const sid = String(s.id || '');
            if (!sid) continue;
            map.set(sid, {
              name: String(s.name || s.short_name || ''),
              short: String(s.short_name || s.name || ''),
              desc: String(s.description || ''),
            });
          }
        }
        setStageById(map);
        setPipelines(
          (pipes || []).map(p => ({
            id: String(p.id),
            key: p.key ? String(p.key) : undefined,
            name: p.name ? String(p.name) : undefined,
            stages: (p.stages || []).map(s => ({
              id: String(s.id),
              name: String(s.name || ''),
              short_name: s.short_name ? String(s.short_name) : undefined,
              description: s.description ? String(s.description) : undefined,
              is_won: Boolean(s.is_won),
              is_lost: Boolean(s.is_lost),
            })),
          }))
        );
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  const dealStages = useMemo(() => {
    const escortPipe = pipelines.find(
      p =>
        p.key === 'local:escort' ||
        p.name === 'Оформление и сопровождение',
    );
    // Кабинет логиста: шевроны только escort (как на стартовой странице),
    // даже если /leads ещё отдал стадии «Адаптации».
    if (logisticsCabinet) {
      if (escortPipe?.stages?.length) return escortPipe.stages;
      // Пока /pipelines грузится — берём stages с карточки, только если это escort.
      const fromLead = asList(row?.stages)
        .map(s => ({
          id: String(s.id || ''),
          name: String(s.name || ''),
          short_name: s.short_name ? String(s.short_name) : undefined,
          is_won: Boolean(s.is_won),
          is_lost: Boolean(s.is_lost),
        }))
        .filter(s => s.id);
      const looksEscort = fromLead.some(
        s =>
          /заявк/i.test(String(s.short_name || s.name || '')) ||
          /посадк/i.test(String(s.short_name || s.name || '')),
      );
      if (looksEscort) return fromLead;
      return [];
    }

    // Стадии с самой карточки (тот же pipeline, что у сделки) — точный match по id
    const fromLead = asList(row?.stages)
      .map(s => ({
        id: String(s.id || ''),
        name: String(s.name || ''),
        short_name: s.short_name ? String(s.short_name) : undefined,
        is_won: Boolean(s.is_won),
        is_lost: Boolean(s.is_lost),
      }))
      .filter(s => s.id);
    if (fromLead.length) return fromLead;

    const stageId = text(row, 'stage_id');
    if (stageId) {
      const byStage = pipelines.find(x => x.stages?.some(s => s.id === stageId));
      if (byStage?.stages?.length) return byStage.stages;
    }
    const pipeId = text(row, 'pipeline_id');
    if (pipeId) {
      const p = pipelines.find(x => x.id === pipeId);
      if (p?.stages?.length) return p.stages;
    }
    return pipelines[0]?.stages ?? [];
  }, [row, pipelines, logisticsCabinet]);

  const chevronCurrentId = useMemo(() => {
    const sid = text(row, 'stage_id');
    if (sid && dealStages.some(s => s.id === sid)) return sid;
    if (
      logisticsCabinet &&
      (Boolean(row?.logistics_needed) || isLogist) &&
      dealStages.length
    ) {
      const req = dealStages.find(
        s =>
          /заявк/i.test(String(s.short_name || '')) ||
          /заявк/i.test(String(s.name || '')),
      );
      return req?.id || dealStages[0]?.id || sid;
    }
    return sid;
  }, [row, dealStages, logisticsCabinet, isLogist]);

  const chevronCurrentLabel = useMemo(() => {
    const fromRow = text(row, 'stage_short') || text(row, 'stage');
    if (chevronCurrentId) {
      const meta = dealStages.find(s => s.id === chevronCurrentId);
      if (meta) return meta.short_name || meta.name || fromRow;
    }
    return fromRow;
  }, [row, dealStages, chevronCurrentId]);

  const moveStage = async (stageId: string, skipBilet = false) => {
    const dealId = text(row, 'deal_id') || text(row, 'converted_deal_id');
    if (!dealId || !stageId || stageId === text(row, 'stage_id')) return;
    const gate = skipBilet
      ? { required: false, bilet: false, need_cities: false, need_ticket: false }
      : await biletGate(dealId, stageId);
    if (gate.bilet && gate.need_ticket) {
      setBiletRequestStage(stageId);
      return;
    }
    const prevStageId = text(row, 'stage_id');
    const meta = dealStages.find(s => s.id === stageId);
    setMovingStage(true);
    setPendingStageId(stageId);
    setError('');
    // Optimistic: сразу подсвечиваем новую стадию, канбан обновится после ответа
    setRow(r =>
      r
        ? {
            ...r,
            stage_id: stageId,
            stage: meta?.name || r.stage,
            stage_short: meta?.short_name || meta?.name || r.stage_short,
          }
        : r
    );
    try {
      await api(`/deals/${dealId}/stage`, {
        method: 'POST',
        body: JSON.stringify({ stage_id: stageId }),
      });
      onChanged();
      const fresh = await api<CrmRow>(`/leads/${id}`);
      setRow(fresh);
    } catch (e) {
      setRow(r => (r ? { ...r, stage_id: prevStageId } : r));
      setError(e instanceof Error ? e.message : 'Не удалось сменить стадию');
    } finally {
      setMovingStage(false);
      setPendingStageId('');
    }
  };

  const postComment = async () => {
    const body = commentDraft.trim();
    if (!body || commentPending) return;
    setCommentPending(true);
    setCommentError('');
    try {
      await api(`/activity/lead/${id}`, {
        method: 'POST',
        body: JSON.stringify({ body }),
      });
      setCommentDraft('');
      const fresh = await api<CrmRow>(`/leads/${id}`);
      setRow(fresh);
      onChanged();
    } catch (e) {
      setCommentError(
        e instanceof Error ? e.message : 'Не удалось сохранить комментарий'
      );
    } finally {
      setCommentPending(false);
    }
  };

  const reloadLead = async () => {
    const fresh = await api<CrmRow>(`/leads/${id}`);
    setRow(fresh);
    onChanged();
  };

  const assignTo = async (ownerId: string) => {
    setAssigning(true);
    setError('');
    try {
      await api(`/leads/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ owner_id: ownerId }),
      });
      const fresh = await api<CrmRow>(`/leads/${id}`);
      setRow(fresh);
      onChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'не удалось назначить');
    } finally {
      setAssigning(false);
    }
  };

  const remove = async () => {
    const title = text(row, 'title') || 'эту заявку';
    if (
      !window.confirm(
        `Удалить заявку «${title}»? Карточка пропадёт и из Лидов, и из Найма.`
      )
    )
      return;
    setPending(true);
    setError('');
    try {
      await api(`/leads/${id}`, { method: 'DELETE' });
      onDeleted?.();
      onClose();
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Не удалось удалить';
      setError(msg);
      window.alert(msg);
    } finally {
      setPending(false);
    }
  };

  if (!row && !error) return null;

  const clientName = text(row, 'contact') || text(row, 'title') || 'Клиент';
  const clientPhone = text(row, 'contact_phone');
  const createdAt = text(row, 'created_at');
  const createdLabel = createdAt
    ? new Date(createdAt).toLocaleString('ru-RU', {
        day: '2-digit',
        month: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
    : '';
  const callBusy =
    snap.status === 'connecting' ||
    snap.status === 'dialing' ||
    snap.status === 'active' ||
    snap.status === 'ringing';
  const fields = customFields(row).filter(f => String(f.value ?? '').trim());
  const linkedDealId =
    text(row, 'deal_id') || text(row, 'converted_deal_id') || '';

  return (
    <>
      <BiletFileModal
        open={Boolean(biletFileStage)}
        dealId={linkedDealId}
        onClose={() => setBiletFileStage('')}
        onSent={async () => {
          const stageId = biletFileStage;
          setBiletFileStage('');
          if (!stageId || !linkedDealId) return;
          const gate = await biletGate(linkedDealId, stageId);
          if (gate.need_ticket) {
            setBiletRequestStage(stageId);
            return;
          }
          await moveStage(stageId, true);
        }}
      />
      {biletRequestStage && linkedDealId ? (
        <DealTicketPanel
          popupOnly
          forceOpen
          dealId={linkedDealId}
          hint="Заполните билет, чтобы перейти в эту стадию."
          onForceClose={() => setBiletRequestStage('')}
          onConfirmed={async () => {
            const stageId = biletRequestStage;
            setBiletRequestStage('');
            if (stageId) await moveStage(stageId, true);
          }}
        />
      ) : null}
      <BindMaxModal
        open={bindOpen}
        leadId={id}
        clientName={clientName}
        clientPhone={clientPhone}
        ownerId={text(row, 'owner_id')}
        onClose={() => setBindOpen(false)}
        onBound={() => {
          setBindOpen(false);
          onClose();
        }}
      />
      <LeadCardPanel
        title={text(row, 'title') || 'Лид'}
        subtitle="Лид"
        badge={
          text(row, 'stage_short') ||
          text(row, 'stage') ||
          leadStatusLabel(text(row, 'status'))
        }
        clientName={clientName}
        clientPhone={clientPhone}
        onClose={onClose}
        onTitleSave={async nextTitle => {
          await api(`/leads/${id}`, {
            method: 'PATCH',
            body: JSON.stringify({ title: nextTitle }),
          })
          const fresh = await api<CrmRow>(`/leads/${id}`)
          setRow(fresh)
          onChanged()
        }}
        callDisabled={
          (!clientPhone && !text(row, 'contact_id')) || callBusy || !configReady
        }
        stageBar={
          dealStages.length &&
          (text(row, 'deal_id') || text(row, 'converted_deal_id')) ? (
            <StageChevronBar
              stages={dealStages}
              currentId={chevronCurrentId}
              currentLabel={chevronCurrentLabel}
              disabled={movingStage || pending}
              pendingId={pendingStageId}
              onSelect={stageId => void moveStage(stageId)}
            />
          ) : null
        }
        onCall={() => {
          setCallErr('');
          void (async () => {
            let dest = clientPhone;
            if (!dest || dest.includes('•')) {
              const res = await api<{ to?: string }>('/calls/resolve-dest', {
                method: 'POST',
                body: JSON.stringify({
                  lead_id: id,
                  contact_id: text(row, 'contact_id'),
                }),
              });
              dest = String(res.to || '');
            }
            if (!dest) throw new Error('Нет номера для звонка');
            await dial(dest, { leadId: id });
          })().catch((e: Error) => setCallErr(e.message));
        }}
        onWriteMax={() => setBindOpen(true)}
        left={
          <>
            {error ? <p className="text-[13px] text-red-500">{error}</p> : null}
            {callErr ? <p className="text-[13px] text-red-500">{callErr}</p> : null}
            <LeadFieldLine label="Источник">
              {sourceLabel(text(row, 'source'))}
            </LeadFieldLine>
            {text(row, 'city') ? (
              <LeadFieldLine label="Город">{text(row, 'city')}</LeadFieldLine>
            ) : null}
            <LeadFieldLine label="Ответственный">
              {canAssign ? (
                <ManagerPicker
                  managers={managers}
                  ownerId={text(row, 'owner_id')}
                  ownerName={text(row, 'owner')}
                  assigning={assigning}
                  onSelect={managerId => void assignTo(managerId)}
                />
              ) : (
                text(row, 'owner') || '—'
              )}
            </LeadFieldLine>
            <LeadFieldLine label="РОП">
              {text(row, 'rop_name') || '—'}
            </LeadFieldLine>
            {text(row, 'owner_department') ? (
              <LeadFieldLine label="Отдел">
                {text(row, 'owner_department')}
              </LeadFieldLine>
            ) : null}
            <LeadFieldLine label="Контакт">
              {text(row, 'contact_id') ? (
                <button
                  type="button"
                  className="text-b24-cyan hover:underline"
                  onClick={() =>
                    navigate(`/crm/contacts?open=${text(row, 'contact_id')}`)
                  }
                >
                  {text(row, 'contact') || 'Контакт'}
                </button>
              ) : (
                '—'
              )}
            </LeadFieldLine>
            <LeadFieldLine label="Телефон">{clientPhone || '—'}</LeadFieldLine>
            {canSeePhones(user) && clientPhone && !/[•*]/.test(clientPhone) ? (
              <PhoneCallTimeBanner phone={clientPhone} />
            ) : null}
            <LeadFieldLine label="Комментарий">
              {text(row, 'card_note') || '—'}
            </LeadFieldLine>
            {fields.map(field => (
              <LeadFieldLine
                key={String(field.code)}
                label={String(field.label || field.code)}
              >
                {field.field_type === 'file' ? 'есть файл' : String(field.value)}
              </LeadFieldLine>
            ))}
            {linkedDealId ? <StaffDialogEntry dealId={linkedDealId} /> : null}
            {linkedDealId ? (
              <div className="mt-3">
                <DealTicketPanel
                  dealId={linkedDealId}
                  logisticsNeeded={Boolean(row?.logistics_needed)}
                  viewLocked={Boolean(row?.logistics_needed) || ticketStageReached(row)}
                  onChanged={() => void reloadLead()}
                />
              </div>
            ) : null}
          </>
        }
        timeline={
          <>
            <LeadCommentComposer
              value={commentDraft}
              onChange={setCommentDraft}
              pending={commentPending}
              error={commentError}
              onSubmit={() => void postComment()}
            />
            <LeadPlanPanel
              leadId={id}
              dealId={text(row, 'deal_id') || text(row, 'converted_deal_id') || undefined}
              leadTitle={text(row, 'title') || clientName}
              tasks={asList(row?.planned_tasks).map(t => ({
                id: String(t.id),
                title: String(t.title || 'Дело'),
                description: String(t.description || ''),
                due_at: t.due_at ? String(t.due_at) : undefined,
                status: String(t.status || ''),
                is_important: Boolean(t.is_important),
                block_work: Boolean(t.block_work),
                kind: String(t.kind || 'task'),
              }))}
              onChanged={() => void reloadLead()}
            />
            <LeadTimelineCard title="История">
              {buildLeadTimeline(row, createdLabel, stageById).map(ev => (
                <LeadTimelineEvent
                  key={ev.key}
                  time={ev.time}
                  title={ev.title}
                  body={ev.body}
                  kind={ev.kind}
                  recordingCallId={ev.recordingCallId}
                />
              ))}
            </LeadTimelineCard>
          </>
        }
        chat={
          <MaxIphonePreview
            leadId={id}
            clientName={clientName}
            clientPhone={clientPhone}
            onNeedBind={() => setBindOpen(true)}
          />
        }
        footer={
          canDeleteLead ? (
            <Button
              type="button"
              variant="filter"
              className="rounded-b24-sm text-red-600 hover:text-red-700"
              disabled={pending}
              onClick={e => {
                e.preventDefault();
                e.stopPropagation();
                void remove();
              }}
            >
              {pending ? 'Удаляем…' : 'Удалить заявку'}
            </Button>
          ) : null
        }
      />
    </>
  );
}

export function DealDrawer({
  id,
  stages,
  onClose,
  onChanged,
  hideAmount,
  subtitle = 'Сделка',
}: {
  id: string;
  stages: { id: string; name: string }[];
  onClose: () => void;
  onChanged: () => void;
  hideAmount?: boolean;
  subtitle?: string;
}) {
  const navigate = useNavigate();
  const [row, setRow] = useState<CrmRow | null>(null);
  const [error, setError] = useState('');
  const [taskTitle, setTaskTitle] = useState('');
  const [pending, setPending] = useState(false);
  const [pendingStageId, setPendingStageId] = useState('');
  const [biletFileStage, setBiletFileStage] = useState('');
  const [biletRequestStage, setBiletRequestStage] = useState('');

  const load = () =>
    api<CrmRow>(`/deals/${id}`)
      .then(setRow)
      .catch((e: Error) => setError(e.message));

  useEffect(() => {
    void load();
  }, [id]);

  const saveLink = async (patch: Record<string, string>) => {
    setPending(true);
    setError('');
    try {
      await api(`/deals/${id}`, { method: 'PATCH', body: JSON.stringify(patch) });
      await load();
      onChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось сохранить');
    } finally {
      setPending(false);
    }
  };

  const move = async (stageId: string, skipBilet = false) => {
    if (!stageId || stageId === text(row, 'stage_id')) return;
    const gate = skipBilet
      ? { required: false, bilet: false, need_cities: false, need_ticket: false }
      : await biletGate(id, stageId);
    if (gate.bilet && gate.need_ticket) {
      setBiletRequestStage(stageId);
      return;
    }
    const prev = text(row, 'stage_id');
    const meta = stages.find(s => s.id === stageId);
    setPending(true);
    setPendingStageId(stageId);
    setError('');
    setRow(r =>
      r
        ? {
            ...r,
            stage_id: stageId,
            stage: meta?.name || r.stage,
          }
        : r
    );
    try {
      await api(`/deals/${id}/stage`, {
        method: 'POST',
        body: JSON.stringify({ stage_id: stageId }),
      });
      onChanged();
      await load();
    } catch (e) {
      setRow(r => (r ? { ...r, stage_id: prev } : r));
      setError(e instanceof Error ? e.message : 'Не удалось сменить стадию');
    } finally {
      setPending(false);
      setPendingStageId('');
    }
  };

  const addTask = async (e: React.FormEvent) => {
    e.preventDefault();
    const title = taskTitle.trim();
    if (!title) return;
    setPending(true);
    try {
      await api('/tasks', {
        method: 'POST',
        body: JSON.stringify({ title, entity_type: 'deal', entity_id: id }),
      });
      setTaskTitle('');
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось создать задачу');
    } finally {
      setPending(false);
    }
  };

  if (!row && !error) return null;

  return (
    <CrmDrawer
      title={text(row, 'title') || (subtitle === 'Кандидат' ? 'Кандидат' : 'Сделка')}
      subtitle={subtitle}
      badge={text(row, 'stage')}
      onClose={onClose}
    >
      <BiletFileModal
        open={Boolean(biletFileStage)}
        dealId={id}
        onClose={() => setBiletFileStage('')}
        onSent={async () => {
          const stageId = biletFileStage;
          setBiletFileStage('');
          if (!stageId) return;
          const gate = await biletGate(id, stageId);
          if (gate.need_ticket) {
            setBiletRequestStage(stageId);
            return;
          }
          await move(stageId, true);
        }}
      />
      {biletRequestStage ? (
        <DealTicketPanel
          popupOnly
          forceOpen
          dealId={id}
          hint="Заполните билет, чтобы перейти в эту стадию."
          onForceClose={() => setBiletRequestStage('')}
          onConfirmed={async () => {
            const stageId = biletRequestStage;
            setBiletRequestStage('');
            if (stageId) await move(stageId, true);
          }}
        />
      ) : null}
      {error ? <p className="text-[13px] text-red-500">{error}</p> : null}
      {stages.length ? (
        <div className="-mx-5 -mt-1 mb-3 border-b border-black/8 bg-white">
          <StageChevronBar
            stages={stages.map(s => ({
              id: s.id,
              name: s.name,
              short_name: (s as { short_name?: string }).short_name,
              is_won: Boolean((s as { is_won?: boolean }).is_won),
              is_lost: Boolean((s as { is_lost?: boolean }).is_lost),
            }))}
            currentId={text(row, 'stage_id')}
            currentLabel={text(row, 'stage')}
            disabled={pending}
            pendingId={pendingStageId}
            onSelect={stageId => void move(stageId)}
          />
        </div>
      ) : null}
      {!hideAmount ? (
        <CrmFieldRow label="Сумма">
          {money(row?.amount, text(row, 'currency') || 'RUB')}
        </CrmFieldRow>
      ) : null}
      <CrmFieldRow label="Ответственный">{text(row, 'owner') || '—'}</CrmFieldRow>
      <CrmFieldRow label="Контакт">
        <EntitySearch
          path="/contacts"
          titleKey="full_name"
          value={text(row, 'contact_id')}
          label={text(row, 'contact')}
          disabled={pending}
          placeholder="Поиск контакта"
          onChange={contactId => void saveLink({ contact_id: contactId })}
        />
        {text(row, 'contact_id') ? (
          <button
            type="button"
            className="mt-1 text-[12px] text-b24-cyan hover:underline"
            onClick={() => navigate(`/crm/contacts?open=${text(row, 'contact_id')}`)}
          >
            Открыть карточку
          </button>
        ) : null}
      </CrmFieldRow>
      <CrmFieldRow label="Компания">
        <EntitySearch
          path="/companies"
          titleKey="name"
          value={text(row, 'company_id')}
          label={text(row, 'company')}
          disabled={pending}
          placeholder="Поиск компании"
          onChange={companyId => void saveLink({ company_id: companyId })}
        />
        {text(row, 'company_id') ? (
          <button
            type="button"
            className="mt-1 text-[12px] text-b24-cyan hover:underline"
            onClick={() => navigate(`/crm/companies?open=${text(row, 'company_id')}`)}
          >
            Открыть карточку
          </button>
        ) : null}
      </CrmFieldRow>
      <FieldGroups row={row} />
      <StaffDialogEntry dealId={id} />
      <DealTicketPanel
        dealId={id}
        logisticsNeeded={Boolean(row?.logistics_needed)}
        viewLocked={Boolean(row?.logistics_needed) || ticketStageReached(row, stages)}
        onChanged={() => void load()}
      />
      {text(row, 'contact_phone') || text(row, 'contact_id') ? (
        <CallPhoneButton
          phone={text(row, 'contact_phone')}
          dealId={id}
          contactId={text(row, 'contact_id')}
        />
      ) : null}
      <WriteMaxButton
        phone={text(row, 'contact_phone')}
        dealId={id}
        contactId={text(row, 'contact_id')}
      />
      <section>
        <h3 className="text-[12px] font-semibold uppercase tracking-wide text-b24-text-muted mb-2">
          Звонки
        </h3>
        {asList(row?.calls).length === 0 ? (
          <p className="text-[13px] text-b24-text-muted">Звонков по сделке нет</p>
        ) : (
          <ul className="flex flex-col gap-1.5">
            {asList(row?.calls).map(c => {
              const cid = String(c.id || '');
              const hasRec = Boolean(c.recording_key || c.exolve_call_id);
              return (
                <li key={cid} className="rounded-[10px] border border-black/6 px-3 py-2">
                  <div className="text-[13px] font-medium text-b24-text-dark">
                    {`${String(c.from_number || '—')} → ${String(c.to_number || '—')}`}
                  </div>
                  <div className="text-[12px] text-b24-text-muted mt-0.5">
                    {[String(c.direction || ''), String(c.disposition || '')]
                      .filter(Boolean)
                      .join(' · ')}
                  </div>
                  {hasRec && cid ? <CallRecordingPlayer callId={cid} /> : null}
                </li>
              );
            })}
          </ul>
        )}
      </section>
      <CrmRelated
        title="Задачи"
        empty="Задач по сделке нет"
        items={asList(row?.tasks).map(t => ({
          id: String(t.id),
          label: String(t.title ?? 'Задача'),
          extra: String(t.status) === 'completed' ? 'Выполнена' : 'В работе',
        }))}
        onOpen={() => navigate('/tasks')}
      />
      <form onSubmit={e => void addTask(e)} className="flex gap-2">
        <input
          value={taskTitle}
          onChange={e => setTaskTitle(e.target.value)}
          placeholder="Новая задача по сделке"
          className="h-9 flex-1 rounded-b24-sm border border-black/10 px-3 text-[13px] outline-none"
        />
        <Button
          type="submit"
          variant="green"
          disabled={pending}
          className="rounded-b24-sm h-9"
        >
          Добавить
        </Button>
      </form>
    </CrmDrawer>
  );
}

import {
  Building2,
  Info,
  Kanban,
  List,
  Plus,
  Search,
  UserRound,
  Users,
  X,
} from 'lucide-react';
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
} from 'react';
import { createPortal } from 'react-dom';
import { useLocation } from 'react-router-dom';
import { BiletFileModal, biletGate } from '../../components/crm/BiletFileModal';
import { DealTicketPanel } from '../../components/crm/DealTicketPanel';
import { LeadFormModal } from '../../components/crm/LeadFormModal';
import { Button } from '../../components/ui/Button';
import { ListPager } from '../../components/ui/ListPager';
import { PeriodFilter, type PeriodDays } from '../../components/ui/PeriodFilter';
import { api, wsURL } from '../../lib/api';
import { type Page, qs } from '../../lib/page';
import { isLogistRole, isManagerRole } from '../../lib/roles';
import { useAuth } from '../../store/auth';
import { useCrmLeadDrawer } from '../../store/crmLeadDrawer';
import {
  dealBelongsOnBoard as dealBelongsOnBoardCheck,
  shouldSkipForeignWsDeal,
} from './dealBoardAcl';
import { useOpenId } from './useOpenId';

type Stage = {
  id: string;
  name: string;
  short_name?: string;
  description?: string;
  sort_order: number;
  is_won: boolean;
  is_lost: boolean;
};

type Pipeline = { id: string; name: string; key?: string; stages: Stage[] };

function stageTitle(stage: Stage) {
  return (stage.short_name || stage.name || '').trim() || stage.name;
}

function isRecruitPipeline(p?: Pipeline | null) {
  if (!p) return false;
  return (
    p.key === 'local:hire' ||
    p.key === 'local:escort' ||
    p.key === 'local:old-leads' ||
    p.name === 'Адаптация' ||
    p.name === 'Оформление и сопровождение' ||
    p.name === 'Найм'
  );
}

/** Горизонтальный ползунок канбана — всегда на экране (macOS прячет системный). */
function KanbanHScrollRail({
  scrollRef,
}: {
  scrollRef: RefObject<HTMLDivElement | null>;
}) {
  const trackRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ startX: number; startLeft: number } | null>(null);
  const [m, setM] = useState({ left: 0, sw: 0, cw: 0, tw: 0 });

  const sync = useCallback(() => {
    const el = scrollRef.current;
    const track = trackRef.current;
    if (!el || !track) return;
    setM({
      left: el.scrollLeft,
      sw: el.scrollWidth,
      cw: el.clientWidth,
      tw: track.clientWidth,
    });
  }, [scrollRef]);

  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    sync();
    el.addEventListener('scroll', sync, { passive: true });
    const ro = new ResizeObserver(sync);
    ro.observe(el);
    if (el.firstElementChild) ro.observe(el.firstElementChild);
    window.addEventListener('resize', sync);
    return () => {
      el.removeEventListener('scroll', sync);
      ro.disconnect();
      window.removeEventListener('resize', sync);
    };
  }, [scrollRef, sync]);

  const maxScroll = Math.max(0, m.sw - m.cw);
  const needed = maxScroll > 1;
  const thumbW = needed ? Math.max(48, (m.cw / Math.max(m.sw, 1)) * m.tw) : m.tw;
  const travel = Math.max(0, m.tw - thumbW);
  const thumbX = needed && travel > 0 ? (m.left / maxScroll) * travel : 0;

  const onThumbDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();
    const el = scrollRef.current;
    if (!el) return;
    dragRef.current = { startX: e.clientX, startLeft: el.scrollLeft };
    e.currentTarget.setPointerCapture(e.pointerId);
  };

  const onThumbMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    const el = scrollRef.current;
    if (!drag || !el || maxScroll <= 0 || travel <= 0) return;
    el.scrollLeft = drag.startLeft + ((e.clientX - drag.startX) / travel) * maxScroll;
  };

  const onThumbUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    dragRef.current = null;
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      /* ignore */
    }
  };

  const onTrackDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if ((e.target as HTMLElement).dataset.thumb === '1') return;
    const el = scrollRef.current;
    const track = trackRef.current;
    if (!el || !track || maxScroll <= 0) return;
    const rect = track.getBoundingClientRect();
    const ratio = (e.clientX - rect.left - thumbW / 2) / Math.max(1, travel);
    el.scrollLeft = Math.max(0, Math.min(maxScroll, ratio * maxScroll));
  };

  return (
    <div
      ref={trackRef}
      className="h-3.5 shrink-0 mx-4 mb-3 rounded-full bg-[#d0d7de] relative select-none"
      onPointerDown={onTrackDown}
      role="scrollbar"
      aria-orientation="horizontal"
      aria-valuemin={0}
      aria-valuemax={Math.round(maxScroll)}
      aria-valuenow={Math.round(m.left)}
      aria-controls="kanban-board-scroll"
    >
      <div
        data-thumb="1"
        className={[
          'absolute top-0 h-full rounded-full cursor-grab active:cursor-grabbing touch-none',
          needed ? 'bg-[#525c69] hover:bg-[#3f4754]' : 'bg-[#b0b8c1]',
        ].join(' ')}
        style={{ width: Math.max(thumbW, needed ? 48 : m.tw || 48), left: thumbX }}
        onPointerDown={onThumbDown}
        onPointerMove={onThumbMove}
        onPointerUp={onThumbUp}
        onPointerCancel={onThumbUp}
      />
    </div>
  );
}

function StageInfoButton({ stage }: { stage: Stage }) {
  const [open, setOpen] = useState(false);
  const [coords, setCoords] = useState({ top: 0, left: 0 });
  const btnRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const text = (stage.description || '').trim();

  const place = useCallback(() => {
    const btn = btnRef.current;
    if (!btn) return;
    const rect = btn.getBoundingClientRect();
    const width = Math.min(300, window.innerWidth - 16);
    let left = rect.right - width;
    left = Math.max(8, Math.min(left, window.innerWidth - width - 8));
    let top = rect.bottom + 8;
    const approxHeight = 140;
    if (top + approxHeight > window.innerHeight - 8) {
      top = Math.max(8, rect.top - approxHeight - 8);
    }
    setCoords({ top, left });
  }, []);

  useLayoutEffect(() => {
    if (!open) return;
    place();
  }, [open, place]);

  useEffect(() => {
    if (!open) return;
    const onScroll = () => place();
    const close = (event: MouseEvent) => {
      const t = event.target as Node;
      if (btnRef.current?.contains(t) || panelRef.current?.contains(t)) return;
      setOpen(false);
    };
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onScroll);
    document.addEventListener('mousedown', close);
    return () => {
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', onScroll);
      document.removeEventListener('mousedown', close);
    };
  }, [open, place]);

  if (!text) return null;

  return (
    <>
      <button
        ref={btnRef}
        type="button"
        className="size-7 shrink-0 inline-flex items-center justify-center rounded-full bg-white/20 hover:bg-white/30 transition"
        aria-label={`Описание стадии: ${stage.name}`}
        onClick={e => {
          e.stopPropagation();
          setOpen(v => !v);
        }}
      >
        <Info size={14} strokeWidth={2.4} />
      </button>
      {open
        ? createPortal(
            <div
              ref={panelRef}
              role="dialog"
              className="fixed z-[200] w-[min(300px,calc(100vw-16px))] rounded-[12px] border border-black/10 bg-white p-3 text-left shadow-[0_16px_40px_rgba(15,23,42,0.22)]"
              style={{ top: coords.top, left: coords.left }}
              onClick={e => e.stopPropagation()}
            >
              <div className="flex items-start justify-between gap-2">
                <div className="text-[13px] font-semibold text-b24-text-dark leading-snug">
                  {stage.name}
                </div>
                <button
                  type="button"
                  className="size-6 shrink-0 inline-flex items-center justify-center rounded-full text-b24-text-muted hover:bg-black/5"
                  aria-label="Закрыть"
                  onClick={() => setOpen(false)}
                >
                  <X size={14} />
                </button>
              </div>
              <p className="mt-1.5 text-[12px] leading-relaxed text-b24-text-muted">
                {text}
              </p>
            </div>,
            document.body
          )
        : null}
    </>
  );
}

type Deal = {
  id: string;
  title: string;
  amount: string;
  currency: string;
  stage_id: string;
  pipeline_id?: string;
  lead_id?: string;
  contact: string;
  company: string;
  card_note?: string;
  contact_id?: string;
  company_id?: string;
  contact_phone?: string;
  owner?: string;
  owner_id?: string;
  owner_department?: string;
  owner_department_id?: string;
  created_at?: string;
  is_won?: boolean;
  is_lost?: boolean;
  autodial_status?: string;
  visual_only?: boolean;
};

type StageStat = {
  pipeline_id: string;
  stage_id: string;
  count: number;
  amount?: string;
};

const STAGE_PAGE = 20;
const STAGE_COLORS = [
  '#39a8ef',
  '#2fc6f6',
  '#55d0e0',
  '#47e4c2',
  '#9dcf00',
  '#f7a700',
  '#ff9f1a',
];

function stageColor(stage: Stage, index: number) {
  if (stage.is_won) return '#9dcf00';
  if (stage.is_lost) return '#ff5752';
  return STAGE_COLORS[index % STAGE_COLORS.length];
}

function formatPhone(raw: string) {
  const d = raw.replace(/\D/g, '');
  if (d.length === 11 && (d[0] === '7' || d[0] === '8')) {
    const n = '7' + d.slice(1);
    return `+${n[0]} ${n.slice(1, 4)} ${n.slice(4, 7)}-${n.slice(7, 9)}-${n.slice(9, 11)}`;
  }
  if (d.length === 10) {
    return `+7 ${d.slice(0, 3)} ${d.slice(3, 6)}-${d.slice(6, 8)}-${d.slice(8, 10)}`;
  }
  return raw || '';
}

function relativeCreated(value: unknown) {
  const raw = String(value ?? '');
  if (!raw) return '—';
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return '—';
  const diff = Date.now() - date.getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'только что';
  if (mins < 60) return `${mins} мин назад`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours} ч назад`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days} дн назад`;
  return date.toLocaleDateString('ru-RU', { day: '2-digit', month: 'short' });
}

function initialOf(deal: Deal) {
  return String(deal.contact || deal.title || '?')
    .slice(0, 1)
    .toUpperCase();
}

export function DealsPage({
  lockedPipelineKey,
  heading,
  subtitle,
}: {
  /** Например local:escort — кабинет логистики на той же воронке «Оформление и сопровождение». */
  lockedPipelineKey?: string;
  heading?: string;
  subtitle?: string;
} = {}) {
  const { user } = useAuth();
  const { openLead, leadId: drawerLeadId, revision } = useCrmLeadDrawer();
  const isAdmin = user?.role === 'admin';
  const isManager = isManagerRole(user?.role);
  const isLogist = isLogistRole(user?.role);
  const logisticsMode = lockedPipelineKey === 'local:escort' || isLogist;
  const oldLeadsMode = lockedPipelineKey === 'local:old-leads';
  // Банк видят РОП и админ. Менеджеру — только карточки, которые уже выдали.
  const canCreateCandidate = !isManager && !logisticsMode && !oldLeadsMode;
  const canUploadOldLeads = oldLeadsMode && isAdmin;
  const [openId, setOpenId] = useOpenId();
  const location = useLocation();
  const [biletFile, setBiletFile] = useState<{ id: string; stageId: string } | null>(null);
  const [biletRequest, setBiletRequest] = useState<{ id: string; stageId: string } | null>(
    null
  );
  const [archive, setArchive] = useState(false);
  const [pipes, setPipes] = useState<Pipeline[]>([]);
  const [byStage, setByStage] = useState<Record<string, Deal[]>>({});
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [listRows, setListRows] = useState<Deal[]>([]);
  const [listTotal, setListTotal] = useState(0);
  const [listOffset, setListOffset] = useState(0);
  const [listLimit, setListLimit] = useState(20);
  const [sort] = useState('created_at');
  const [dir] = useState<'ASC' | 'DESC'>('DESC');
  const [formOpen, setFormOpen] = useState(false);
  const [createStage, setCreateStage] = useState('');
  const [pending, setPending] = useState(false);
  const [formError, setFormError] = useState('');
  const [dragOver, setDragOver] = useState<string | null>(null);
  const [error, setError] = useState('');
  const [importNote, setImportNote] = useState('');
  const oldFileRef = useRef<HTMLInputElement>(null);
  const [pipeId, setPipeId] = useState('');
  const [query, setQuery] = useState('');
  const [debounced, setDebounced] = useState('');
  const [mine, setMine] = useState(false);
  const [deptId, setDeptId] = useState(() => {
    try {
      return sessionStorage.getItem('kanban-dept-filter') || '';
    } catch {
      return '';
    }
  });
  const [departments, setDepartments] = useState<{ id: string; name: string }[]>([]);
  const [mode, setMode] = useState<'kanban' | 'list'>('kanban');
  const [loadingMore, setLoadingMore] = useState<string | null>(null);
  const [stageFilter, setStageFilter] = useState('');
  const [period, setPeriod] = useState<PeriodDays>(0);
  const [sourceFilter, setSourceFilter] = useState<'' | 'manual' | 'vk_ads'>(() => {
    try {
      const v = sessionStorage.getItem('kanban-source-filter') || '';
      if (v === 'manual' || v === 'vk_ads') return v;
    } catch {
      /* ignore */
    }
    return '';
  });
  const stageScrollRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const listScrollRef = useRef<HTMLDivElement | null>(null);
  const boardScrollRef = useRef<HTMLDivElement | null>(null);
  const boardPanRef = useRef<{ pointerId: number; x: number; left: number } | null>(null);
  const boardGen = useRef(0);

  const captureScrolls = () => {
    const stagesScroll: Record<string, number> = {};
    for (const [id, el] of Object.entries(stageScrollRefs.current)) {
      if (el) stagesScroll[id] = el.scrollTop;
    }
    return {
      stages: stagesScroll,
      list: listScrollRef.current?.scrollTop ?? 0,
      boardX: boardScrollRef.current?.scrollLeft ?? 0,
      windowY: window.scrollY,
    };
  };

  const restoreScrolls = (saved: ReturnType<typeof captureScrolls>) => {
    requestAnimationFrame(() => {
      for (const [id, top] of Object.entries(saved.stages)) {
        const el = stageScrollRefs.current[id];
        if (el) el.scrollTop = top;
      }
      if (listScrollRef.current) listScrollRef.current.scrollTop = saved.list;
      if (boardScrollRef.current) boardScrollRef.current.scrollLeft = saved.boardX;
      if (saved.windowY > 0) window.scrollTo({ top: saved.windowY });
    });
  };

  // Мышь: вертикальное колесо → горизонтальный канбан; средняя кнопка — «рука».
  useEffect(() => {
    if (mode !== 'kanban') return;
    const el = boardScrollRef.current;
    if (!el) return;

    const onWheel = (e: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth + 1) return;
      const target = e.target as HTMLElement | null;
      const col = target?.closest('[data-kanban-col-scroll]') as HTMLElement | null;
      if (col && col.scrollHeight > col.clientHeight + 1 && !e.shiftKey) {
        return;
      }
      const dx = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
      if (dx === 0) return;
      e.preventDefault();
      el.scrollLeft += dx;
    };

    const endPan = (e: PointerEvent) => {
      const pan = boardPanRef.current;
      if (!pan || pan.pointerId !== e.pointerId) return;
      boardPanRef.current = null;
      try {
        el.releasePointerCapture(e.pointerId);
      } catch {
        /* ignore */
      }
      el.classList.remove('cursor-grabbing');
    };

    const onPointerDown = (e: PointerEvent) => {
      if (e.button !== 1) return; // только средняя кнопка — не мешает DnD карточек
      e.preventDefault();
      boardPanRef.current = { pointerId: e.pointerId, x: e.clientX, left: el.scrollLeft };
      el.setPointerCapture(e.pointerId);
      el.classList.add('cursor-grabbing');
    };

    const onMouseDown = (e: MouseEvent) => {
      // блокируем автоскролл браузера средней кнопкой
      if (e.button === 1) e.preventDefault();
    };

    const onPointerMove = (e: PointerEvent) => {
      const pan = boardPanRef.current;
      if (!pan || pan.pointerId !== e.pointerId) return;
      el.scrollLeft = pan.left - (e.clientX - pan.x);
    };

    el.addEventListener('wheel', onWheel, { passive: false });
    el.addEventListener('mousedown', onMouseDown);
    el.addEventListener('pointerdown', onPointerDown);
    el.addEventListener('pointermove', onPointerMove);
    el.addEventListener('pointerup', endPan);
    el.addEventListener('pointercancel', endPan);
    return () => {
      el.removeEventListener('wheel', onWheel);
      el.removeEventListener('mousedown', onMouseDown);
      el.removeEventListener('pointerdown', onPointerDown);
      el.removeEventListener('pointermove', onPointerMove);
      el.removeEventListener('pointerup', endPan);
      el.removeEventListener('pointercancel', endPan);
    };
  }, [mode, pipeId]);

  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(query.trim()), 280);
    return () => window.clearTimeout(t);
  }, [query]);

  useEffect(() => {
    setListOffset(0);
  }, [
    debounced,
    pipeId,
    mine,
    deptId,
    archive,
    sort,
    dir,
    listLimit,
    stageFilter,
    period,
    sourceFilter,
  ]);

  useEffect(() => {
    if (!isAdmin) {
      setDepartments([]);
      return;
    }
    let cancelled = false;
    api<{ departments?: { id?: string; name?: string }[] }>('/employees')
      .then(data => {
        if (cancelled) return;
        const rows = (data.departments ?? [])
          .map(d => ({ id: String(d.id || ''), name: String(d.name || '').trim() }))
          .filter(d => d.id && d.name);
        setDepartments(rows);
        if (deptId && !rows.some(d => d.id === deptId)) {
          setDeptId('');
          try {
            sessionStorage.removeItem('kanban-dept-filter');
          } catch {
            /* ignore */
          }
        }
      })
      .catch(() => {
        if (!cancelled) setDepartments([]);
      });
    return () => {
      cancelled = true;
    };
  }, [isAdmin]);

  useEffect(() => {
    const statsReq =
      lockedPipelineKey || isLogist
        ? Promise.resolve({ items: [] as StageStat[] })
        : api<{ items: StageStat[] }>('/deals/stats');
    Promise.all([
      api<Pipeline[]>('/pipelines'),
      statsReq,
    ])
      .then(([p, stats]) => {
        const locked = lockedPipelineKey || (isLogist ? 'local:escort' : '');
        const hireOnly = (x: Pipeline) =>
          x.key === 'local:hire' || x.name === 'Адаптация' || x.name === 'Найм';
        const escortOnly = (x: Pipeline) =>
          x.key === 'local:escort' || x.name === 'Оформление и сопровождение';
        const visible = locked
          ? p.filter(
              x => x.key === locked || (locked === 'local:escort' && escortOnly(x))
            )
          : isLogist
            ? p.filter(escortOnly)
            : p.filter(hireOnly);
        const list = visible.length ? visible : p;
        setPipes(list);
        const totals: Record<string, number> = {};
        for (const row of stats.items ?? []) {
          totals[row.pipeline_id] =
            (totals[row.pipeline_id] ?? 0) + (Number(row.count) || 0);
        }
        setPipeId(prev => {
          if (prev && list.some(x => x.id === prev)) return prev;
          if (locked) {
            const esc = list.find(x => x.key === locked || escortOnly(x));
            if (esc) return esc.id;
          }
          const hire = list.find(hireOnly);
          if (hire && !logisticsMode) return hire.id;
          const best = [...list].sort(
            (a, b) => (totals[b.id] ?? 0) - (totals[a.id] ?? 0)
          )[0];
          return best?.id || list[0]?.id || '';
        });
      })
      .catch((e: Error) => setError(e.message));
  }, [user?.role, lockedPipelineKey, isLogist, logisticsMode]);

  const current = pipes.find(p => p.id === pipeId) ?? pipes[0];
  const isRecruit = isRecruitPipeline(current);
  const isAdaptation =
    current?.key === 'local:hire' ||
    current?.name === 'Адаптация' ||
    current?.name === 'Найм';
  const isEscort =
    current?.key === 'local:escort' ||
    current?.name === 'Оформление и сопровождение' ||
    logisticsMode;
  // Найм: закрывающие стадии — обычные колонки канбана (не отдельный «Архив»).
  // Остальные воронки: отказы только в режиме Архив.
  const stages = useMemo(() => {
    const all = current?.stages ?? [];
    if (isRecruit) return all;
    return all.filter(s => (archive ? s.is_lost : !s.is_lost));
  }, [archive, current?.stages, isRecruit]);
  const funnelTabs = useMemo(() => {
    if (logisticsMode) return [];
    // РОП/менеджер работают в адаптации; escort — отдельный кабинет логиста
    if (isLogist) {
      return pipes.filter(
        p => p.key === 'local:escort' || p.name === 'Оформление и сопровождение'
      );
    }
    return pipes.filter(
      p => p.key === 'local:hire' || p.name === 'Адаптация' || p.name === 'Найм'
    );
  }, [pipes, logisticsMode, isLogist]);
  const listMode = mode === 'list';
  const shared = {
    pipeline_id: pipeId,
    q: debounced,
    mine: mine ? 1 : undefined,
    department_id: isAdmin && deptId && !oldLeadsMode ? deptId : undefined,
    scope: isRecruit ? 'all' : archive ? 'archive' : 'active',
    days: period || undefined,
    source: sourceFilter || undefined,
  };

  const applyStats = (items: StageStat[]) => {
    const nextCounts: Record<string, number> = {};
    for (const row of items) {
      nextCounts[row.stage_id] = Number(row.count) || 0;
    }
    setCounts(nextCounts);
  };

  const refreshStats = useCallback(async () => {
    if (!pipeId) return;
    try {
      const stats = await api<{ items: StageStat[] }>(`/deals/stats${qs(shared)}`);
      applyStats(stats.items ?? []);
    } catch {
      /* ignore */
    }
  }, [pipeId, debounced, mine, deptId, archive, period, sourceFilter, isAdmin]);

  const selectDepartment = (id: string) => {
    setDeptId(id);
    try {
      if (id) sessionStorage.setItem('kanban-dept-filter', id);
      else sessionStorage.removeItem('kanban-dept-filter');
    } catch {
      /* ignore */
    }
  };

  const selectSource = (v: '' | 'manual' | 'vk_ads') => {
    setSourceFilter(v);
    try {
      if (v) sessionStorage.setItem('kanban-source-filter', v);
      else sessionStorage.removeItem('kanban-source-filter');
    } catch {
      /* ignore */
    }
  };

  const normalizeDeal = (raw: Record<string, unknown>): Deal => ({
    id: String(raw.id ?? ''),
    title: String(raw.title ?? ''),
    amount: String(raw.amount ?? '0'),
    currency: String(raw.currency ?? 'RUB'),
    stage_id: String(raw.stage_id ?? ''),
    pipeline_id: raw.pipeline_id != null ? String(raw.pipeline_id) : undefined,
    lead_id:
      raw.lead_id != null && String(raw.lead_id) !== '' ? String(raw.lead_id) : undefined,
    contact: String(raw.contact ?? ''),
    company: String(raw.company ?? ''),
    card_note: raw.card_note != null ? String(raw.card_note) : undefined,
    contact_id: raw.contact_id != null ? String(raw.contact_id) : undefined,
    company_id: raw.company_id != null ? String(raw.company_id) : undefined,
    contact_phone: raw.contact_phone != null ? String(raw.contact_phone) : undefined,
    owner: raw.owner != null && String(raw.owner) !== '' ? String(raw.owner) : undefined,
    owner_id:
      raw.owner_id != null && String(raw.owner_id) !== ''
        ? String(raw.owner_id)
        : undefined,
    owner_department:
      raw.owner_department != null && String(raw.owner_department) !== ''
        ? String(raw.owner_department)
        : undefined,
    owner_department_id:
      raw.owner_department_id != null && String(raw.owner_department_id) !== ''
        ? String(raw.owner_department_id)
        : undefined,
    created_at: raw.created_at != null ? String(raw.created_at) : undefined,
    is_won: Boolean(raw.is_won),
    is_lost: Boolean(raw.is_lost),
    autodial_status:
      raw.autodial_status != null ? String(raw.autodial_status) : undefined,
    visual_only: Boolean(raw.visual_only),
  });

  const removeDealLocal = useCallback((dealId: string) => {
    setByStage(prev => {
      const next: Record<string, Deal[]> = {};
      for (const [sid, rows] of Object.entries(prev)) {
        next[sid] = rows.filter(d => d.id !== dealId);
      }
      return next;
    });
    setListRows(prev => prev.filter(d => d.id !== dealId));
  }, []);

  const upsertDealLocal = useCallback(
    (deal: Deal) => {
      if (!deal.id || !deal.stage_id) return;
      setByStage(prev => {
        let oldStage = '';
        let oldIdx = -1;
        for (const [sid, rows] of Object.entries(prev)) {
          const i = rows.findIndex(d => d.id === deal.id);
          if (i >= 0) {
            oldStage = sid;
            oldIdx = i;
            break;
          }
        }
        const next: Record<string, Deal[]> = {};
        for (const [sid, rows] of Object.entries(prev)) {
          next[sid] = rows.filter(d => d.id !== deal.id);
        }
        const col = next[deal.stage_id] ?? [];
        if (oldStage === deal.stage_id && oldIdx >= 0) {
          const copy = [...col];
          copy.splice(Math.min(oldIdx, copy.length), 0, deal);
          next[deal.stage_id] = copy;
        } else {
          next[deal.stage_id] = [deal, ...col];
        }
        return next;
      });
      setListRows(prev => {
        const i = prev.findIndex(d => d.id === deal.id);
        if (i < 0) return prev;
        if (stageFilter && deal.stage_id !== stageFilter) {
          return prev.filter(d => d.id !== deal.id);
        }
        const copy = [...prev];
        copy[i] = { ...copy[i], ...deal };
        return copy;
      });
    },
    [stageFilter]
  );

  /** Канбан = как listDeals ACL. Недозвон-пул открывает карточку, но не должен совать чужие на доску по WS. */
  const dealBelongsOnBoard = useCallback(
    (deal: Deal) =>
      dealBelongsOnBoardCheck(deal, {
        userId: user?.id,
        mine,
        isManager,
        isAdmin,
        deptId,
        sharedBoard: oldLeadsMode && !isManager,
      }),
    [user?.id, mine, isManager, isAdmin, deptId, oldLeadsMode]
  );

  const patchDealFromServer = useCallback(
    async (dealId: string) => {
      try {
        const raw = await api<Record<string, unknown>>(`/deals/${dealId}?for_board=1`);
        const deal = normalizeDeal(raw);
        if (pipeId && deal.pipeline_id && deal.pipeline_id !== pipeId) {
          removeDealLocal(dealId);
          return;
        }
        if (!dealBelongsOnBoard(deal)) {
          removeDealLocal(dealId);
          return;
        }
        const scrolls = captureScrolls();
        upsertDealLocal(deal);
        restoreScrolls(scrolls);
      } catch {
        removeDealLocal(dealId);
      }
    },
    [pipeId, removeDealLocal, upsertDealLocal, dealBelongsOnBoard]
  );

  /** Колонки рисуются по мере ответа. «Заявка» не держит остальные. */
  const loadBoard = useCallback(async () => {
    const pipe = pipes.find(p => p.id === pipeId);
    const cols = pipe?.stages ?? [];
    if (!pipeId || cols.length === 0) return;

    const gen = ++boardGen.current;
    const scrolls = captureScrolls();

    void api<{ items: StageStat[] }>(`/deals/stats${qs(shared)}`)
      .then(stats => {
        if (gen !== boardGen.current) return;
        applyStats(stats.items ?? []);
      })
      .catch(() => undefined);

    await Promise.all(
      cols.map(async stage => {
        try {
          const page = await api<Page<Deal>>(
            `/deals${qs({ ...shared, stage_id: stage.id, limit: STAGE_PAGE })}`
          );
          if (gen !== boardGen.current) return;
          const fresh = (page.items ?? []).filter(d => dealBelongsOnBoard(d));
          setByStage(prev => {
            const old = prev[stage.id] ?? [];
            if (old.length > STAGE_PAGE) {
              const freshIds = new Set(fresh.map(d => d.id));
              const tail = old
                .slice(STAGE_PAGE)
                .filter(d => !freshIds.has(d.id) && dealBelongsOnBoard(d));
              return { ...prev, [stage.id]: [...fresh, ...tail] };
            }
            return { ...prev, [stage.id]: fresh };
          });
          restoreScrolls(scrolls);
        } catch (e) {
          if (gen === boardGen.current) {
            setError(e instanceof Error ? e.message : 'Не удалось загрузить колонку');
          }
        }
      })
    );
  }, [
    pipeId,
    pipes,
    debounced,
    mine,
    deptId,
    archive,
    period,
    sourceFilter,
    isAdmin,
    dealBelongsOnBoard,
  ]);

  const loadList = useCallback(async () => {
    if (!pipeId) return;
    const gen = ++boardGen.current;
    const scrolls = captureScrolls();
    const page = await api<Page<Deal>>(
      `/deals${qs({
        ...shared,
        stage_id: stageFilter || undefined,
        limit: listLimit > 0 ? listLimit : 20,
        offset: listOffset,
        sort,
        dir,
      })}`
    );
    if (gen !== boardGen.current) return;
    setListRows(page.items ?? []);
    setListTotal(page.total ?? 0);
    restoreScrolls(scrolls);
  }, [
    pipeId,
    debounced,
    mine,
    deptId,
    archive,
    listLimit,
    listOffset,
    sort,
    dir,
    stageFilter,
    period,
    sourceFilter,
    isAdmin,
  ]);

  useEffect(() => {
    if (!pipeId) return;
    const run = listMode ? loadList : loadBoard;
    run().catch((e: Error) => setError(e.message));
  }, [pipeId, listMode, loadList, loadBoard]);

  useEffect(() => {
    const ws = new WebSocket(wsURL());
    let statsTimer: number | undefined;
    let fullTimer: number | undefined;
    const bumpStats = () => {
      window.clearTimeout(statsTimer);
      statsTimer = window.setTimeout(() => {
        void refreshStats();
      }, 500);
    };
    const bumpFull = () => {
      window.clearTimeout(fullTimer);
      fullTimer = window.setTimeout(() => {
        void (listMode ? loadList() : loadBoard()).catch(() => undefined);
      }, 1200);
    };
    ws.onmessage = ev => {
      try {
        const msg = JSON.parse(String(ev.data)) as {
          type?: string;
          id?: string;
          payload?: { id?: string; lead_id?: string; owner_id?: string };
        };
        const type = msg.type || '';
        const payload = msg.payload && typeof msg.payload === 'object' ? msg.payload : {};
        const id = String(msg.id || payload.id || '').trim();
        if (type === 'deal.deleted' && id) {
          const scrolls = captureScrolls();
          removeDealLocal(id);
          restoreScrolls(scrolls);
          bumpStats();
          return;
        }
        if (type === 'deal.updated' || type === 'deal.created') {
          if (id) {
            const payloadOwnerId = String(payload.owner_id || '');
            const skipForeign = shouldSkipForeignWsDeal({
              mine,
              isManager,
              userId: user?.id,
              payloadOwnerId,
              sharedBoard: oldLeadsMode && !isManager,
            });
            if (skipForeign) {
              const scrolls = captureScrolls();
              removeDealLocal(id);
              restoreScrolls(scrolls);
              bumpStats();
              return;
            }
            void patchDealFromServer(id).then(() => bumpStats());
            if (listMode || debounced || mine || deptId || period || sourceFilter)
              bumpFull();
          } else {
            bumpFull();
          }
          return;
        }
      } catch {
        /* ignore */
      }
    };
    return () => {
      window.clearTimeout(statsTimer);
      window.clearTimeout(fullTimer);
      try {
        ws.close();
      } catch {
        /* ignore */
      }
    };
  }, [
    listMode,
    loadList,
    loadBoard,
    refreshStats,
    removeDealLocal,
    patchDealFromServer,
    debounced,
    mine,
    deptId,
    period,
    sourceFilter,
    isManager,
    user?.id,
    oldLeadsMode,
  ]);

  const reload = () => (listMode ? loadList() : loadBoard());

  const loadMore = async (stageId: string) => {
    const have = byStage[stageId]?.length ?? 0;
    setLoadingMore(stageId);
    try {
      const page = await api<Page<Deal>>(
        `/deals${qs({ ...shared, stage_id: stageId, limit: STAGE_PAGE, offset: have })}`
      );
      const extra = (page.items ?? []).filter(d => dealBelongsOnBoard(d));
      setByStage(prev => ({ ...prev, [stageId]: [...(prev[stageId] ?? []), ...extra] }));
    } finally {
      setLoadingMore(null);
    }
  };

  const openCreate = (stageId?: string) => {
    if (!canCreateCandidate) return;
    setCreateStage(stageId || stages[0]?.id || '');
    setFormError('');
    setFormOpen(true);
  };

  const patchDealLead = (dealId: string, leadId: string) => {
    setByStage(prev => {
      const next: Record<string, Deal[]> = {};
      for (const [sid, rows] of Object.entries(prev)) {
        next[sid] = rows.map(d => (d.id === dealId ? { ...d, lead_id: leadId } : d));
      }
      return next;
    });
    setListRows(prev => prev.map(d => (d.id === dealId ? { ...d, lead_id: leadId } : d)));
  };

  const visualLocked = (deal: { visual_only?: boolean }) =>
    Boolean(deal.visual_only) && !isAdmin;

  const openLeadForDeal = async (deal: Deal | { id: string; lead_id?: string; visual_only?: boolean }) => {
    if (visualLocked(deal)) return;
    setError('');
    try {
      let leadId = deal.lead_id || '';
      if (!leadId) {
        // подтянуть lead_id со сделки (иногда в карточке канбана пусто)
        try {
          const full = await api<{ lead_id?: string }>(`/deals/${deal.id}`);
          leadId = full.lead_id || '';
          if (leadId) patchDealLead(deal.id, leadId);
        } catch {
          /* continue to ensure-lead */
        }
      }
      if (!leadId) {
        const res = await api<{ lead_id?: string }>(`/deals/${deal.id}/ensure-lead`, {
          method: 'POST',
        });
        leadId = res.lead_id || '';
        if (leadId) patchDealLead(deal.id, leadId);
      }
      if (!leadId) throw new Error('Не удалось открыть карточку кандидата');
      openLead(leadId);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось открыть');
    }
  };

  useEffect(() => {
    if (revision === 0) return;
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [revision]);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const leadHint = params.get('lead');
    if (!openId && !leadHint) return;
    let cancelled = false;
    void (async () => {
      try {
        // карточка уже открыта глобально (конвертация с Лидов) — не трогаем, только чистим ?lead=
        if (leadHint) {
          params.delete('lead');
          const nextQs = params.toString();
          window.history.replaceState(
            window.history.state,
            '',
            nextQs ? `${window.location.pathname}?${nextQs}` : window.location.pathname
          );
          if (!drawerLeadId) openLead(leadHint);
          return;
        }
        if (drawerLeadId) return;
        const deal = await api<Deal>(`/deals/${openId}`);
        if (cancelled) return;
        if (deal.visual_only && user?.role !== 'admin') return;
        await openLeadForDeal(deal);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Сделка не найдена');
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openId, location.search]);

  const create = async (values: Record<string, string>) => {
    setPending(true);
    setFormError('');
    try {
      const res = await api<{ id: string; lead_id?: string }>('/deals', {
        method: 'POST',
        body: JSON.stringify({
          title: values.title.trim(),
          candidate: values.candidate || '',
          phone: values.phone || '',
          city: values.city || '',
          card_note: values.card_note || '',
          amount: '0',
          source: 'manual',
          status: 'new',
          pipeline_id: current?.id,
          stage_id: createStage || stages[0]?.id,
          create_lead: true,
        }),
      });
      setFormOpen(false);
      await reload();
      if (res.lead_id) {
        openLead(res.lead_id);
        if (res.id) setOpenId(res.id);
      } else if (res.id) {
        await openLeadForDeal({ id: res.id });
        setOpenId(res.id);
      }
    } catch (e) {
      setFormError(e instanceof Error ? e.message : 'Не удалось создать');
    } finally {
      setPending(false);
    }
  };

  const move = async (id: string, stage_id: string, skipBilet = false) => {
    const moving = Object.values(byStage)
      .flat()
      .find(d => d.id === id);
    if (moving && visualLocked(moving)) return;
    if (!skipBilet) {
      const gate = await biletGate(id, stage_id);
      if (gate.bilet && gate.need_ticket) {
        setBiletRequest({ id, stageId: stage_id });
        return;
      }
    }
    let snapshot: Record<string, Deal[]> | null = null;
    const scrolls = captureScrolls();
    setByStage(prev => {
      snapshot = prev;
      let found: Deal | null = null;
      const next: Record<string, Deal[]> = {};
      for (const [sid, rows] of Object.entries(prev)) {
        const kept: Deal[] = [];
        for (const d of rows) {
          if (d.id === id) found = d;
          else kept.push(d);
        }
        next[sid] = kept;
      }
      if (!found) return prev;
      next[stage_id] = [{ ...found, stage_id }, ...(next[stage_id] ?? [])];
      return next;
    });
    restoreScrolls(scrolls);
    try {
      await api(`/deals/${id}/stage`, {
        method: 'POST',
        body: JSON.stringify({ stage_id }),
      });
      void refreshStats();
    } catch (e) {
      if (snapshot) {
        setByStage(snapshot);
        restoreScrolls(scrolls);
      }
      setError(e instanceof Error ? e.message : 'Не удалось переместить');
    }
  };

  const uploadOldLeads = async (file: File) => {
    const body = new FormData();
    body.append('file', file);
    setImportNote('');
    setError('');
    try {
      const res = await api<{
        created: number;
        skipped_empty: number;
        skipped_duplicate: number;
      }>('/crm/old-leads/import', { method: 'POST', body });
      setImportNote(
        `Добавлено ${res.created}. Пустых строк ${res.skipped_empty}, уже были в спящих лидах ${res.skipped_duplicate}.`
      );
      await loadBoard();
      void refreshStats();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Не удалось залить файл');
    }
  };

  const pipelineCount = useMemo(
    () => Object.values(counts).reduce((sum, n) => sum + n, 0),
    [counts]
  );

  const title = heading || (logisticsMode ? 'Логистика' : current?.name || 'Кандидаты');
  const createLabel = isRecruit ? 'Кандидат' : 'Сделка';
  const visibleStages =
    listMode && stageFilter ? stages.filter(s => s.id === stageFilter) : stages;

  return (
    <div className="flex flex-col gap-4 pt-1 h-[calc(100dvh-7.25rem)] min-h-[560px]">
      <BiletFileModal
        open={Boolean(biletFile)}
        dealId={biletFile?.id || ''}
        onClose={() => setBiletFile(null)}
        onSent={async () => {
          const job = biletFile;
          setBiletFile(null);
          if (!job) return;
          const gate = await biletGate(job.id, job.stageId);
          if (gate.need_ticket) {
            setBiletRequest(job);
            return;
          }
          await move(job.id, job.stageId, true);
        }}
      />
      {biletRequest ? (
        <DealTicketPanel
          popupOnly
          forceOpen
          dealId={biletRequest.id}
          hint="Заполните билет, чтобы перейти в эту стадию."
          onForceClose={() => setBiletRequest(null)}
          onConfirmed={async () => {
            const job = biletRequest;
            setBiletRequest(null);
            if (job) await move(job.id, job.stageId, true);
          }}
        />
      ) : null}
      <div className="flex flex-wrap items-end justify-between gap-3 shrink-0">
        <div className="min-w-0">
          <h1 className="text-[28px] font-semibold text-white tracking-tight leading-none">
            {title}
          </h1>
          <p className="mt-1.5 text-[13px] text-white/65">
            {subtitle
              ? subtitle
              : logisticsMode || isEscort
                ? 'Оформление и сопровождение · билеты, посадка, путь, размещение, финал'
                : isAdaptation
                  ? 'Воронка адаптации · первичный контакт и квалификация'
                  : isRecruit
                    ? 'Воронка кандидатов'
                    : 'Воронка сделок'}{' '}
            · {(listMode ? listTotal : pipelineCount).toLocaleString('ru-RU')} в работе
          </p>
          {funnelTabs.length > 1 ? (
            <div className="mt-3 inline-flex rounded-[12px] border border-white/15 bg-white/10 p-1 backdrop-blur-sm">
              {funnelTabs.map(pipe => {
                const active = pipe.id === (current?.id || pipeId);
                return (
                  <button
                    key={pipe.id}
                    type="button"
                    onClick={() => {
                      setPipeId(pipe.id);
                      setStageFilter('');
                      setArchive(false);
                    }}
                    className={[
                      'h-9 px-3.5 rounded-[9px] text-[13px] font-medium transition',
                      active
                        ? 'bg-white text-[#1a2332] shadow-sm'
                        : 'text-white/80 hover:text-white hover:bg-white/10',
                    ].join(' ')}
                  >
                    {pipe.name}
                  </button>
                );
              })}
            </div>
          ) : null}
        </div>
        {canCreateCandidate ? (
          <Button
            type="button"
            variant="green"
            className="h-10 rounded-[10px] px-4 shadow-[0_8px_24px_rgba(47,192,110,0.35)]"
            onClick={() => openCreate()}
            leftIcon={<Plus size={16} strokeWidth={2.5} />}
          >
            {isRecruit ? 'Новый кандидат' : 'Новая сделка'}
          </Button>
        ) : null}
        {canUploadOldLeads ? (
          <>
            <input
              ref={oldFileRef}
              type="file"
              accept=".xlsx,.csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv"
              className="hidden"
              onChange={e => {
                const file = e.target.files?.[0];
                e.target.value = '';
                if (file) void uploadOldLeads(file);
              }}
            />
            <Button
              type="button"
              variant="green"
              className="h-10 rounded-[10px] px-4"
              onClick={() => oldFileRef.current?.click()}
            >
              Загрузить Excel
            </Button>
          </>
        ) : null}
      </div>

      {importNote ? (
        <div className="mx-4 rounded-[10px] bg-white/90 px-3 py-2 text-[13px] text-[#1a2332]">
          {importNote}
        </div>
      ) : null}

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-2.5 shrink-0">
        <div className="rounded-[14px] bg-white/12 border border-white/10 px-4 py-3 backdrop-blur-sm">
          <div className="text-[11px] uppercase tracking-wide text-white/55">Всего</div>
          <div className="mt-1 text-[26px] font-semibold text-white tabular-nums leading-none">
            {pipelineCount.toLocaleString('ru-RU')}
          </div>
          <div className="mt-1 text-[11px] text-white/45">в воронке</div>
        </div>
        {stages.slice(0, 3).map(stage => (
          <div
            key={stage.id}
            className="rounded-[14px] bg-white/12 border border-white/10 px-4 py-3 backdrop-blur-sm"
          >
            <div className="text-[11px] uppercase tracking-wide text-white/55 truncate">
              {stageTitle(stage)}
            </div>
            <div className="mt-1 text-[26px] font-semibold text-white tabular-nums leading-none">
              {(counts[stage.id] ?? 0).toLocaleString('ru-RU')}
            </div>
            <div className="mt-1 text-[11px] text-white/45">на стадии</div>
          </div>
        ))}
      </div>

      <div className="b24-glass-panel overflow-hidden flex flex-col flex-1 min-h-0">
        <div className="px-4 py-3 border-b border-black/6 bg-white/80 flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <div className="inline-flex rounded-[10px] overflow-hidden border border-black/8 bg-white">
              <button
                type="button"
                className={`h-10 px-3 inline-flex items-center gap-1.5 text-[13px] font-medium ${
                  !listMode
                    ? 'bg-b24-text-dark text-white'
                    : 'text-b24-text hover:bg-[#f5f7f9]'
                }`}
                onClick={() => setMode('kanban')}
              >
                <Kanban size={15} />
                Канбан
              </button>
              <button
                type="button"
                className={`h-10 px-3 inline-flex items-center gap-1.5 text-[13px] font-medium ${
                  listMode
                    ? 'bg-b24-text-dark text-white'
                    : 'text-b24-text hover:bg-[#f5f7f9]'
                }`}
                onClick={() => setMode('list')}
              >
                <List size={15} />
                Список
              </button>
            </div>

            <div className="flex-1 min-w-[200px] relative">
              <Search
                size={16}
                className="absolute left-3 top-1/2 -translate-y-1/2 text-b24-text-muted pointer-events-none"
              />
              <input
                value={query}
                onChange={e => setQuery(e.target.value)}
                placeholder={isRecruit ? 'Поиск кандидата, контакта…' : 'Поиск сделки…'}
                className="h-10 w-full rounded-[10px] border border-black/8 bg-[#f5f7f9] pl-9 pr-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan focus:bg-white"
              />
            </div>

            <button
              type="button"
              onClick={() => setMine(v => !v)}
              className={[
                'h-10 px-3.5 rounded-[10px] text-[13px] font-medium border inline-flex items-center gap-1.5 transition',
                mine
                  ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep'
                  : 'bg-white border-black/8 text-b24-text hover:bg-[#f8fafb]',
              ].join(' ')}
            >
              <UserRound size={15} />
              Мои
            </button>

            {isAdmin && departments.length > 0 ? (
              <label className="relative inline-flex h-10 items-center">
                <Building2
                  size={15}
                  className="pointer-events-none absolute left-3 text-b24-text-muted"
                />
                <select
                  value={deptId}
                  onChange={e => selectDepartment(e.target.value)}
                  className={[
                    'h-10 appearance-none rounded-[10px] border pl-9 pr-8 text-[13px] font-medium outline-none transition',
                    deptId
                      ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep'
                      : 'bg-white border-black/8 text-b24-text hover:bg-[#f8fafb]',
                  ].join(' ')}
                  title="Фильтр по отделу"
                >
                  <option value="">Все отделы</option>
                  {departments.map(d => (
                    <option key={d.id} value={d.id}>
                      {d.name}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}

            <PeriodFilter value={period} onChange={setPeriod} />

            <select
              value={sourceFilter}
              onChange={e => {
                const v = e.target.value;
                selectSource(v === 'manual' || v === 'vk_ads' ? v : '');
              }}
              className={[
                'h-10 appearance-none rounded-[10px] border px-3 pr-8 text-[13px] font-medium outline-none transition',
                sourceFilter
                  ? 'bg-[#e8f8fe] border-b24-cyan text-b24-cyan-deep'
                  : 'bg-white border-black/8 text-b24-text hover:bg-[#f8fafb]',
              ].join(' ')}
              title="Источник заявки"
            >
              <option value="">Все источники</option>
              <option value="manual">Вручную</option>
              <option value="vk_ads">VK Ads</option>
            </select>

            {isAdmin && !isRecruit ? (
              <button
                type="button"
                onClick={() => setArchive(v => !v)}
                className={[
                  'h-10 px-3.5 rounded-[10px] text-[13px] font-medium border transition',
                  archive
                    ? 'bg-rose-50 border-rose-200 text-rose-700'
                    : 'bg-white border-black/8 text-b24-text hover:bg-[#f8fafb]',
                ].join(' ')}
              >
                Архив
              </button>
            ) : null}
          </div>

          {listMode ? (
            <div className="flex flex-wrap gap-1.5">
              <button
                type="button"
                onClick={() => setStageFilter('')}
                className={[
                  'h-8 px-3 rounded-full text-[12px] font-medium inline-flex items-center gap-1.5 transition',
                  !stageFilter
                    ? 'bg-b24-text-dark text-white'
                    : 'bg-[#eef1f4] text-b24-text hover:bg-[#e4e8ec]',
                ].join(' ')}
              >
                Все
                <span
                  className={`tabular-nums text-[11px] px-1.5 py-0.5 rounded-full ${!stageFilter ? 'bg-white/20' : 'bg-white text-b24-text-muted'}`}
                >
                  {pipelineCount}
                </span>
              </button>
              {stages.map(stage => {
                const active = stageFilter === stage.id;
                const n = counts[stage.id] ?? 0;
                return (
                  <button
                    key={stage.id}
                    type="button"
                    onClick={() => setStageFilter(stage.id)}
                    className={[
                      'h-8 px-3 rounded-full text-[12px] font-medium inline-flex items-center gap-1.5 transition',
                      active
                        ? 'bg-b24-text-dark text-white'
                        : 'bg-[#eef1f4] text-b24-text hover:bg-[#e4e8ec]',
                    ].join(' ')}
                  >
                    {stageTitle(stage)}
                    <span
                      className={[
                        'tabular-nums text-[11px] px-1.5 py-0.5 rounded-full',
                        active ? 'bg-white/20' : 'bg-white text-b24-text-muted',
                      ].join(' ')}
                    >
                      {n}
                    </span>
                  </button>
                );
              })}
            </div>
          ) : null}
        </div>

        {error ? <p className="px-4 py-2 text-[13px] text-red-500">{error}</p> : null}

        {listMode ? (
          <>
            <div ref={listScrollRef} className="flex-1 overflow-x-auto b24-scrollbar">
              {listRows.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-20 px-6 text-center">
                  <div className="size-14 rounded-2xl bg-[#e8f8fe] text-b24-cyan-deep inline-flex items-center justify-center mb-3">
                    <Users size={26} />
                  </div>
                  <div className="text-[16px] font-semibold text-b24-text-dark">
                    {debounced || mine || deptId || stageFilter
                      ? 'Ничего не найдено'
                      : isRecruit
                        ? 'Пока нет кандидатов'
                        : 'Пока пусто'}
                  </div>
                  <p className="mt-1 text-[13px] text-b24-text-muted max-w-sm">
                    {debounced || mine || deptId || stageFilter
                      ? 'Сбросьте фильтры или измените запрос.'
                      : isManager
                        ? 'РОП назначит вам кандидатов — они появятся здесь.'
                        : 'Добавьте первого кандидата в воронку адаптации.'}
                  </p>
                  {canCreateCandidate &&
                  !debounced &&
                  !mine &&
                  !deptId &&
                  !stageFilter ? (
                    <Button
                      type="button"
                      variant="green"
                      className="mt-4 rounded-[10px]"
                      onClick={() => openCreate()}
                      leftIcon={<Plus size={16} />}
                    >
                      {createLabel}
                    </Button>
                  ) : null}
                </div>
              ) : (
                <table className="w-full min-w-[860px] text-left">
                  <thead className="sticky top-0 z-10 bg-[#f8f9fb] text-[11px] uppercase tracking-wide text-b24-text-muted">
                    <tr>
                      <th className="px-4 py-3 font-semibold">
                        {isRecruit ? 'Кандидат' : 'Сделка'}
                      </th>
                      <th className="px-3 py-3 font-semibold">Стадия</th>
                      <th className="px-3 py-3 font-semibold">Контакт</th>
                      <th className="px-3 py-3 font-semibold">
                        {isRecruit ? 'Комментарий' : 'Компания'}
                      </th>
                      <th className="px-3 py-3 font-semibold text-right">Создан</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-black/[0.05]">
                    {listRows.map(deal => {
                      const stageName =
                        stages.find(s => s.id === deal.stage_id)?.name ?? '—';
                      return (
                        <tr
                          key={deal.id}
                          className={
                            visualLocked(deal)
                              ? 'group cursor-default opacity-60'
                              : 'group cursor-pointer hover:bg-[#f3fafd] transition-colors'
                          }
                          onClick={() => {
                            if (visualLocked(deal)) return;
                            setOpenId(deal.id);
                            void openLeadForDeal(deal);
                          }}
                        >
                          <td className="px-4 py-3.5">
                            <div className="flex items-center gap-3 min-w-0">
                              <div className="size-10 rounded-full bg-[linear-gradient(135deg,#2fc6f6,#1ea8d4)] text-white text-[14px] font-semibold inline-flex items-center justify-center shrink-0 shadow-[0_4px_12px_rgba(47,198,246,0.35)]">
                                {initialOf(deal)}
                              </div>
                              <div className="min-w-0">
                                <div className="text-[14px] font-semibold text-b24-text-dark truncate group-hover:text-b24-cyan-deep">
                                  {deal.title || 'Без названия'}
                                </div>
                                {deal.autodial_status &&
                                ['dialing', 'ringing'].includes(deal.autodial_status) ? (
                                  <div className="mt-0.5 text-[11px] font-medium text-amber-700">
                                    Автодозвон…
                                  </div>
                                ) : null}
                                {deal.contact_phone ? (
                                  <div className="mt-0.5 text-[12px] text-b24-text-muted tabular-nums">
                                    {formatPhone(deal.contact_phone)}
                                  </div>
                                ) : null}
                              </div>
                            </div>
                          </td>
                          <td className="px-3 py-3.5">
                            <span className="inline-flex h-7 items-center rounded-full px-2.5 text-[12px] font-medium bg-sky-50 text-sky-700 ring-1 ring-inset ring-sky-200">
                              {stageName}
                            </span>
                          </td>
                          <td className="px-3 py-3.5 text-[13px] text-b24-text truncate max-w-[180px]">
                            {deal.contact || '—'}
                          </td>
                          <td className="px-3 py-3.5 text-[13px] text-b24-text truncate max-w-[180px]">
                            {(isRecruit ? deal.card_note : deal.company) || '—'}
                          </td>
                          <td className="px-3 py-3.5 text-right text-[12px] text-b24-text-muted whitespace-nowrap">
                            {relativeCreated(deal.created_at)}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              )}
            </div>
            <ListPager
              total={listTotal}
              limit={listLimit}
              offset={listOffset}
              onOffset={setListOffset}
              onLimit={next => {
                setListLimit(next);
                setListOffset(0);
              }}
            />
          </>
        ) : (
          <div className="flex-1 min-h-0 flex flex-col">
            <div
              id="kanban-board-scroll"
              ref={boardScrollRef}
              className="flex-1 min-h-0 overflow-x-auto overflow-y-hidden b24-scrollbar-board-native-hidden px-4 pt-4"
            >
              <div className="flex gap-3 min-w-max items-stretch h-full">
                {visibleStages.map((stage, index) => {
                  const items = byStage[stage.id] ?? [];
                  const total = counts[stage.id] ?? items.length;
                  const color = stageColor(stage, index);
                  const isOver = dragOver === stage.id;
                  return (
                    <section
                      key={stage.id}
                      className={[
                        'w-[280px] shrink-0 rounded-[14px] bg-[#f3f6f8] border border-black/5 flex flex-col h-full max-h-full overflow-hidden',
                        isOver ? 'ring-2 ring-b24-cyan/50' : '',
                      ].join(' ')}
                      onDragOver={e => {
                        e.preventDefault();
                        setDragOver(stage.id);
                      }}
                      onDragLeave={() =>
                        setDragOver(prev => (prev === stage.id ? null : prev))
                      }
                      onDrop={e => {
                        e.preventDefault();
                        const id = e.dataTransfer.getData('text/deal-id');
                        setDragOver(null);
                        if (id) void move(id, stage.id);
                      }}
                    >
                      <header
                        className="px-3.5 pt-3 pb-2.5 text-white relative"
                        style={{ background: color }}
                      >
                        <div className="flex items-start justify-between gap-2">
                          <div className="min-w-0 flex-1">
                            <div className="flex items-start gap-1.5">
                              <h3 className="text-[14px] font-semibold leading-tight min-w-0">
                                {stageTitle(stage)}
                              </h3>
                              <StageInfoButton stage={stage} />
                            </div>
                            <div className="mt-1 text-[12px] text-white/85 tabular-nums">
                              {total.toLocaleString('ru-RU')}{' '}
                              {isRecruit ? 'кандидат.' : 'сделок'}
                            </div>
                          </div>
                          {canCreateCandidate ? (
                            <button
                              type="button"
                              className="size-8 shrink-0 inline-flex items-center justify-center rounded-full bg-white/20 hover:bg-white/30"
                              aria-label="Добавить"
                              onClick={() => openCreate(stage.id)}
                            >
                              <Plus size={16} />
                            </button>
                          ) : (
                            <span className="size-8 shrink-0" aria-hidden />
                          )}
                        </div>
                      </header>

                      <div
                        data-kanban-col-scroll
                        ref={node => {
                          stageScrollRefs.current[stage.id] = node;
                        }}
                        className="flex-1 overflow-y-auto b24-scrollbar p-2.5 flex flex-col gap-2"
                      >
                        {items.length === 0 ? (
                          <div className="flex-1 min-h-[88px] rounded-[12px] border border-dashed border-black/10 text-[12px] text-b24-text-muted flex items-center justify-center px-3 text-center">
                            {canCreateCandidate
                              ? 'Перетащите сюда или добавьте'
                              : 'Пока пусто'}
                          </div>
                        ) : (
                          items.map(deal => (
                            <div
                              key={deal.id}
                              draggable={!visualLocked(deal)}
                              onDragStart={e => {
                                if (visualLocked(deal)) {
                                  e.preventDefault();
                                  return;
                                }
                                e.dataTransfer.setData('text/deal-id', deal.id);
                                e.dataTransfer.effectAllowed = 'move';
                              }}
                              onClick={() => {
                                if (visualLocked(deal)) return;
                                setOpenId(deal.id);
                                void openLeadForDeal(deal);
                              }}
                              className={
                                visualLocked(deal)
                                  ? 'rounded-[12px] bg-[#eef1f4] shadow-none border border-black/10 px-3 py-2.5 cursor-default opacity-70'
                                  : 'rounded-[12px] bg-white shadow-[0_2px_8px_rgba(15,23,42,0.06)] border border-black/4 px-3 py-2.5 cursor-pointer hover:shadow-[0_6px_16px_rgba(15,23,42,0.1)] hover:border-b24-cyan/25 transition'
                              }
                            >
                              <div className="flex items-start gap-2.5">
                                <div className="size-9 rounded-full bg-[linear-gradient(135deg,#2fc6f6,#1ea8d4)] text-white text-[12px] font-semibold inline-flex items-center justify-center shrink-0">
                                  {initialOf(deal)}
                                </div>
                                <div className="min-w-0 flex-1">
                                  <div className="text-[13px] font-semibold text-b24-text-dark leading-snug">
                                    {deal.title || 'Без названия'}
                                  </div>
                                  {deal.autodial_status &&
                                  ['dialing', 'ringing'].includes(
                                    deal.autodial_status
                                  ) ? (
                                    <div className="mt-1 inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide bg-[#fff3cd] text-[#8a6d1d]">
                                      Автодозвон…
                                    </div>
                                  ) : null}
                                  {deal.owner ? (
                                    <div className="mt-1 flex items-center gap-1 text-[12px] text-[#2067b0] truncate">
                                      <UserRound size={12} className="shrink-0" />
                                      {deal.owner}
                                    </div>
                                  ) : null}
                                  {deal.owner_department ? (
                                    <div className="mt-0.5 flex items-center gap-1 text-[11px] text-b24-text-muted truncate">
                                      <Building2 size={11} className="shrink-0" />
                                      {deal.owner_department}
                                    </div>
                                  ) : null}
                                  {deal.created_at ? (
                                    <div className="mt-1.5 text-[10px] text-b24-text-muted">
                                      {relativeCreated(deal.created_at)}
                                    </div>
                                  ) : null}
                                </div>
                              </div>
                            </div>
                          ))
                        )}
                        {total > items.length ? (
                          <button
                            type="button"
                            className="text-[12px] text-[#2067b0] hover:underline py-1.5"
                            disabled={loadingMore === stage.id}
                            onClick={() => void loadMore(stage.id)}
                          >
                            {loadingMore === stage.id
                              ? 'Загрузка…'
                              : `Ещё ${(total - items.length).toLocaleString('ru-RU')}`}
                          </button>
                        ) : null}
                      </div>
                    </section>
                  );
                })}
              </div>
            </div>
            <KanbanHScrollRail scrollRef={boardScrollRef} />
          </div>
        )}
      </div>

      {canCreateCandidate ? (
        <LeadFormModal
          open={formOpen}
          pending={pending}
          error={formError}
          headline="Новый кандидат"
          initial={{ status: 'new', source: 'manual' }}
          onClose={() => setFormOpen(false)}
          onSubmit={values => void create(values)}
        />
      ) : null}
    </div>
  );
}

export type PeriodDays = 0 | 1 | 7 | 30;

const PERIODS: { days: PeriodDays; label: string }[] = [
  { days: 1, label: '1 день' },
  { days: 7, label: '7 дней' },
  { days: 30, label: '30 дней' },
  { days: 0, label: 'Все' },
];

/** Фильтр периода: 1 / 7 / 30 / все. */
export function PeriodFilter({
  value,
  onChange,
  tone = 'light',
}: {
  value: PeriodDays;
  onChange: (days: PeriodDays) => void;
  tone?: 'light' | 'dark';
}) {
  return (
    <div
      className={
        tone === 'dark'
          ? 'inline-flex flex-wrap gap-1 rounded-[10px] bg-white/10 p-1 border border-white/10'
          : 'inline-flex flex-wrap gap-1 rounded-[10px] bg-[#eef1f4] p-1'
      }
    >
      {PERIODS.map(p => {
        const active = value === p.days;
        return (
          <button
            key={p.days}
            type="button"
            onClick={() => onChange(p.days)}
            className={[
              'h-7 px-2.5 rounded-[8px] text-[12px] font-medium transition',
              tone === 'dark'
                ? active
                  ? 'bg-white text-b24-text-dark'
                  : 'text-white/75 hover:text-white hover:bg-white/10'
                : active
                  ? 'bg-b24-text-dark text-white shadow-sm'
                  : 'text-b24-text hover:bg-white/70',
            ].join(' ')}
          >
            {p.label}
          </button>
        );
      })}
    </div>
  );
}

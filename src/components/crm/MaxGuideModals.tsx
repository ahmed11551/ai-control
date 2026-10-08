import { Info, X } from 'lucide-react'
import { useEffect, useId, useState, type ReactNode } from 'react'

export type MaxGuideId = 'work' | 'reasons'

const STORAGE_PREFIX = 'forgecrm.maxGuide.dismissed.'

export function isMaxGuideDismissed(id: MaxGuideId, userId?: string) {
  try {
    const key = `${STORAGE_PREFIX}${id}.${userId || 'anon'}`
    return localStorage.getItem(key) === '1'
  } catch {
    return false
  }
}

export function setMaxGuideDismissed(id: MaxGuideId, dismissed: boolean, userId?: string) {
  try {
    const key = `${STORAGE_PREFIX}${id}.${userId || 'anon'}`
    if (dismissed) localStorage.setItem(key, '1')
    else localStorage.removeItem(key)
  } catch {
    /* ignore */
  }
}

type GuideShellProps = {
  open: boolean
  title: string
  subtitle: string
  items: string[]
  footerNote?: ReactNode
  onClose: () => void
  onConfirm: (dontShowAgain: boolean) => void
}

function GuideShell({
  open,
  title,
  subtitle,
  items,
  footerNote,
  onClose,
  onConfirm,
}: GuideShellProps) {
  const titleId = useId()
  const [dontShow, setDontShow] = useState(false)

  useEffect(() => {
    if (open) setDontShow(false)
  }, [open])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-[160] flex items-start justify-center px-4 pt-[10vh]">
      <button
        type="button"
        className="absolute inset-0 z-0 bg-black/35"
        aria-label="Закрыть"
        onClick={onClose}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="relative z-10 w-full max-w-[520px] overflow-hidden rounded-[16px] border border-black/5 bg-white shadow-[0_20px_50px_rgba(15,23,42,0.22)]"
      >
        <div className="flex items-start gap-3 px-5 pb-2 pt-5">
          <div className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-full bg-[#2b7de9] text-white">
            <Info size={18} strokeWidth={2.4} />
          </div>
          <div className="min-w-0 flex-1 pt-0.5">
            <h2 id={titleId} className="text-[18px] font-semibold leading-snug tracking-tight text-[#1a2332]">
              {title}
            </h2>
            <p className="mt-1.5 text-[13px] leading-relaxed text-b24-text-muted">{subtitle}</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-b24-text-muted hover:bg-black/5"
            aria-label="Закрыть"
          >
            <X size={18} />
          </button>
        </div>

        <ol className="flex flex-col gap-3.5 px-5 py-4">
          {items.map((text, i) => (
            <li key={i} className="flex gap-3">
              <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-[#e8f1ff] text-[13px] font-semibold text-[#2b7de9]">
                {i + 1}
              </span>
              <span className="pt-0.5 text-[14px] leading-snug text-[#1a2332]">{text}</span>
            </li>
          ))}
        </ol>

        {footerNote ? <div className="px-5 pb-3">{footerNote}</div> : null}

        <div className="flex items-center justify-between gap-3 border-t border-black/6 bg-[#fafbfc] px-5 py-4">
          <label className="inline-flex cursor-pointer items-center gap-2 text-[13px] text-b24-text-dark">
            <input
              type="checkbox"
              checked={dontShow}
              onChange={(e) => setDontShow(e.target.checked)}
              className="size-4 rounded border-black/20 text-[#2b7de9] focus:ring-[#2b7de9]/30"
            />
            Больше не показывать
          </label>
          <button
            type="button"
            onClick={() => onConfirm(dontShow)}
            className="h-10 rounded-[10px] bg-[#2b7de9] px-5 text-[14px] font-semibold text-white shadow-[0_2px_8px_rgba(43,125,233,0.35)] hover:bg-[#246fce]"
          >
            Понятно
          </button>
        </div>
      </div>
    </div>
  )
}

export function MaxWorkGuideModal({
  open,
  accountsCount,
  onClose,
  onConfirm,
}: {
  open: boolean
  accountsCount: number
  onClose: () => void
  onConfirm: (dontShowAgain: boolean) => void
}) {
  const n = Math.max(1, accountsCount || 20)
  return (
    <GuideShell
      open={open}
      title="Памятка по работе с перепиской"
      subtitle="Следуйте этим рекомендациям, чтобы эффективно работать с перепиской и не упустить кандидата."
      items={[
        'Рекомендуется продолжить тот диалог, который уже был передан менеджеру.',
        'Если кандидат не отвечает в текущем диалоге, менеджер может при необходимости написать ему с любого другого доступного аккаунта MAX.',
        `В системе доступно ${n} аккаунтов MAX, которые можно использовать для связи с этим лидом.`,
        'При необходимости менеджер может написать с другого аккаунта и представиться другим менеджером.',
        'Все действия синхронизируются с карточкой сделки и фиксируются в истории.',
      ]}
      onClose={onClose}
      onConfirm={onConfirm}
    />
  )
}

export function MaxReasonsGuideModal({
  open,
  onClose,
  onConfirm,
}: {
  open: boolean
  onClose: () => void
  onConfirm: (dontShowAgain: boolean) => void
}) {
  return (
    <GuideShell
      open={open}
      title="Возможные причины перехода в новый диалог"
      subtitle="Ниже приведены ситуации, когда менеджер может начать новый диалог с кандидатом через другой аккаунт MAX."
      items={[
        'Кандидат не отвечает в текущем диалоге.',
        'Кандидат отказался общаться с текущим менеджером и хочет общаться с другим.',
        'Изменился регион или формат сопровождения, поэтому требуется новый контакт от другого менеджера.',
        'Возникла антипатия или недоверие к текущему менеджеру.',
        'Необходимо написать с другого аккаунта MAX для повторного контакта.',
      ]}
      footerNote={
        <div className="flex items-start gap-2.5 rounded-[12px] border border-[#cfe3ff] bg-[#eaf3ff] px-3.5 py-3 text-[13px] leading-snug text-[#1a5fb4]">
          <Info size={16} className="mt-0.5 shrink-0" />
          <span>
            Рекомендуется сначала продолжить переданный диалог. Новый диалог использовать только при
            необходимости.
          </span>
        </div>
      }
      onClose={onClose}
      onConfirm={onConfirm}
    />
  )
}

/** Компактная кнопка «i» + меню: открыть памятки вручную, не мешая основному потоку. */
export function MaxGuidesInfoButton({
  onOpenWork,
  onOpenReasons,
  className = '',
}: {
  onOpenWork: () => void
  onOpenReasons: () => void
  className?: string
}) {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const t = e.target as HTMLElement | null
      if (t?.closest('[data-max-guides-menu]')) return
      setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  return (
    <div className={`relative ${className}`} data-max-guides-menu>
      <button
        type="button"
        title="Памятки MAX"
        onClick={(e) => {
          e.stopPropagation()
          setOpen((v) => !v)
        }}
        className="inline-flex size-8 items-center justify-center rounded-full text-[#2b7de9] hover:bg-[#e8f1ff]"
      >
        <Info size={18} />
      </button>
      {open ? (
        <div className="absolute right-0 top-full z-20 mt-1 w-56 overflow-hidden rounded-[12px] border border-black/8 bg-white py-1 shadow-[0_12px_28px_rgba(15,23,42,0.16)]">
          <button
            type="button"
            className="flex w-full px-3 py-2.5 text-left text-[13px] text-b24-text-dark hover:bg-[#f5f7f9]"
            onClick={() => {
              setOpen(false)
              onOpenWork()
            }}
          >
            Памятка по переписке
          </button>
          <button
            type="button"
            className="flex w-full px-3 py-2.5 text-left text-[13px] text-b24-text-dark hover:bg-[#f5f7f9]"
            onClick={() => {
              setOpen(false)
              onOpenReasons()
            }}
          >
            Когда новый диалог
          </button>
        </div>
      ) : null}
    </div>
  )
}

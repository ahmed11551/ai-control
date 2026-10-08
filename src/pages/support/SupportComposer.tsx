import { FileText, Loader2, MessageSquareText, Paperclip, SendHorizontal, X } from 'lucide-react'
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { fileSize, SUPPORT_QUICK_REPLIES } from './supportTypes'

const LINE_PX = 21
const MAX_LINES = 5

export function SupportComposer({
  value,
  onChange,
  files,
  onAddFiles,
  onRemoveFile,
  onSend,
  sending,
  disabled,
  placeholder,
  quickReplies,
  focusKey,
}: {
  value: string
  onChange: (v: string) => void
  files: File[]
  onAddFiles: (files: File[]) => void
  onRemoveFile: (index: number) => void
  onSend: () => void
  sending: boolean
  disabled: boolean
  placeholder: string
  quickReplies: boolean
  focusKey: string
}) {
  const areaRef = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const [quickOpen, setQuickOpen] = useState(false)
  const canSend = !disabled && !sending && (value.trim() !== '' || files.length > 0)

  useLayoutEffect(() => {
    const el = areaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, LINE_PX * MAX_LINES + 18)}px`
  }, [value])

  useEffect(() => {
    if (!disabled && window.matchMedia('(pointer: fine)').matches) areaRef.current?.focus()
  }, [focusKey, disabled])

  useEffect(() => {
    if (!quickOpen) return
    const close = () => setQuickOpen(false)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [quickOpen])

  const previews = useMemo(() => files.map((f) => (f.type.startsWith('image/') ? URL.createObjectURL(f) : '')), [files])
  useEffect(() => () => previews.forEach((u) => u && URL.revokeObjectURL(u)), [previews])

  function insertQuick(text: string) {
    const el = areaRef.current
    const cur = value
    if (!el) {
      onChange(cur ? `${cur} ${text}` : text)
      return
    }
    const start = el.selectionStart ?? cur.length
    const end = el.selectionEnd ?? cur.length
    const before = cur.slice(0, start)
    const sep = before && !/\s$/.test(before) ? ' ' : ''
    const next = `${before}${sep}${text}${cur.slice(end)}`
    onChange(next)
    requestAnimationFrame(() => {
      el.focus()
      const pos = before.length + sep.length + text.length
      el.setSelectionRange(pos, pos)
    })
  }

  return (
    <div className="shrink-0 border-t border-black/6 bg-white/85 px-3 pb-2 pt-2 backdrop-blur sm:px-4">
      {files.length > 0 ? (
        <div className="mb-2 flex gap-2 overflow-x-auto pb-1">
          {files.map((file, i) => (
            <div key={`${file.name}-${file.size}-${i}`} className="group/att relative shrink-0">
              {previews[i] ? (
                <img src={previews[i]} alt={file.name} className="size-16 rounded-[12px] object-cover ring-1 ring-black/10" />
              ) : (
                <div className="flex h-16 w-44 items-center gap-2 rounded-[12px] bg-black/[0.04] px-2.5 ring-1 ring-black/10">
                  <span className="grid size-9 shrink-0 place-items-center rounded-[10px] bg-[#1f6feb]/10 text-[#1f6feb]">
                    <FileText size={17} />
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate text-[12.5px] font-medium text-b24-text-dark">{file.name}</span>
                    <span className="block text-[11px] text-b24-text-muted">{fileSize(file.size)}</span>
                  </span>
                </div>
              )}
              <button
                type="button"
                onClick={() => onRemoveFile(i)}
                className="absolute -right-1.5 -top-1.5 grid size-5 place-items-center rounded-full bg-[#1b1f23] text-white shadow ring-2 ring-white"
                aria-label={`Убрать ${file.name}`}
              >
                <X size={12} />
              </button>
            </div>
          ))}
        </div>
      ) : null}

      <div className="flex items-end gap-2">
        <button
          type="button"
          className="grid size-10 shrink-0 place-items-center rounded-full text-b24-text-muted transition hover:bg-black/5 hover:text-b24-text-dark disabled:opacity-40"
          onClick={() => fileRef.current?.click()}
          disabled={disabled}
          aria-label="Прикрепить фото или файл"
          title="Прикрепить фото или файл"
        >
          <Paperclip size={19} />
        </button>
        <input
          ref={fileRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            onAddFiles(Array.from(e.target.files || []))
            e.target.value = ''
          }}
        />
        {quickReplies ? (
          <div className="relative shrink-0">
            <button
              type="button"
              className={[
                'grid size-10 place-items-center rounded-full transition hover:bg-black/5 disabled:opacity-40',
                quickOpen ? 'bg-black/5 text-[#1f6feb]' : 'text-b24-text-muted hover:text-b24-text-dark',
              ].join(' ')}
              onClick={(e) => {
                e.stopPropagation()
                setQuickOpen((v) => !v)
              }}
              disabled={disabled}
              aria-label="Быстрые ответы"
              aria-expanded={quickOpen}
              title="Быстрые ответы"
            >
              <MessageSquareText size={19} />
            </button>
            {quickOpen ? (
              <div
                className="absolute bottom-12 left-0 z-20 w-[290px] overflow-hidden rounded-[14px] bg-white py-1 shadow-[0_12px_40px_rgba(15,23,42,0.2)] ring-1 ring-black/8"
                onClick={(e) => e.stopPropagation()}
                role="menu"
              >
                <div className="px-3 pb-1 pt-1.5 text-[11px] font-semibold uppercase tracking-wide text-b24-text-muted">Быстрые ответы</div>
                {SUPPORT_QUICK_REPLIES.map((text) => (
                  <button
                    key={text}
                    type="button"
                    role="menuitem"
                    className="block w-full px-3 py-2 text-left text-[13.5px] text-b24-text-dark hover:bg-[#1f6feb]/8"
                    onClick={() => {
                      insertQuick(text)
                      setQuickOpen(false)
                    }}
                  >
                    {text}
                  </button>
                ))}
              </div>
            ) : null}
          </div>
        ) : null}
        <div className="flex min-h-10 flex-1 items-center rounded-[20px] bg-black/[0.045] px-3.5 py-[9px] ring-[#1f6feb]/30 transition focus-within:bg-white focus-within:ring-2">
          <textarea
            ref={areaRef}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            rows={1}
            disabled={disabled}
            placeholder={placeholder}
            className="block max-h-[123px] w-full resize-none bg-transparent text-[14.5px] leading-[21px] text-b24-text-dark outline-none placeholder:text-b24-text-muted disabled:cursor-not-allowed"
            onPaste={(e) => {
              const pasted = Array.from(e.clipboardData?.files || [])
              if (pasted.length > 0) {
                e.preventDefault()
                onAddFiles(
                  pasted.map((f, i) =>
                    f.name && f.name !== 'image.png'
                      ? f
                      : new File([f], `Скриншот ${new Date().toLocaleTimeString('ru-RU').replace(/:/g, '-')}${i ? `-${i}` : ''}.png`, {
                          type: f.type || 'image/png',
                        }),
                  ),
                )
              }
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                if (!e.repeat) onSend()
              }
            }}
          />
        </div>
        <button
          type="button"
          disabled={!canSend}
          onClick={onSend}
          className="grid size-10 shrink-0 place-items-center rounded-full bg-[#1f6feb] text-white shadow-[0_4px_14px_rgba(31,111,235,0.35)] transition hover:bg-[#1a5fd0] active:scale-95 disabled:bg-black/15 disabled:shadow-none"
          aria-label="Отправить"
          title="Отправить (Enter)"
        >
          {sending ? <Loader2 size={18} className="animate-spin" /> : <SendHorizontal size={18} />}
        </button>
      </div>
      <div className="mt-1 flex items-center justify-between gap-2 px-1 text-[11px] text-b24-text-muted">
        {sending ? (
          <span className="truncate">Отправка…</span>
        ) : (
          <span className="hidden truncate sm:block">Enter — отправить · Shift+Enter — новая строка · Cmd+V — вставить скриншот</span>
        )}
      </div>
    </div>
  )
}


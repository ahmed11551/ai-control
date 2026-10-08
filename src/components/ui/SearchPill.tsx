import { Search } from 'lucide-react'
import type { InputHTMLAttributes } from 'react'

type SearchPillProps = InputHTMLAttributes<HTMLInputElement> & {
  tone?: 'dark' | 'light'
}

export function SearchPill({
  tone = 'dark',
  className = '',
  ...props
}: SearchPillProps) {
  const isDark = tone === 'dark'

  return (
    <label
      className={[
        'inline-flex items-center gap-2 rounded-b24-pill px-3 h-8 min-w-[160px]',
        isDark
          ? 'bg-b24-glass text-b24-white-soft border border-white/10'
          : 'bg-white/90 text-b24-text border border-black/5 shadow-sm',
        className,
      ].join(' ')}
    >
      <Search
        size={15}
        strokeWidth={1.75}
        className={isDark ? 'text-b24-white-mute shrink-0' : 'text-b24-text-muted shrink-0'}
      />
      <input
        type="search"
        className={[
          'bg-transparent border-0 outline-none w-full text-[13px] placeholder:opacity-70',
          isDark ? 'text-white placeholder:text-b24-white-mute' : 'text-b24-text-dark placeholder:text-b24-text-muted',
        ].join(' ')}
        {...props}
      />
    </label>
  )
}

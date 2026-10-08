import type { ButtonHTMLAttributes, ReactNode } from 'react'

type Variant = 'green' | 'ghost' | 'glass' | 'filter'

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: Variant
  children: ReactNode
  leftIcon?: ReactNode
  rightIcon?: ReactNode
}

const variants: Record<Variant, string> = {
  green:
    'bg-b24-green text-white hover:bg-b24-green-hover shadow-[0_2px_8px_rgba(47,192,110,0.35)]',
  ghost: 'bg-transparent text-b24-white-soft hover:bg-b24-glass',
  glass:
    'bg-b24-glass text-b24-white-soft hover:bg-b24-glass-strong border border-white/10',
  filter:
    'bg-white/90 text-b24-text border border-black/5 hover:bg-white shadow-sm',
}

export function Button({
  variant = 'ghost',
  children,
  leftIcon,
  rightIcon,
  className = '',
  ...props
}: ButtonProps) {
  return (
    <button
      type="button"
      className={[
        'inline-flex items-center justify-center gap-1.5 rounded-b24-pill px-3.5 py-1.5 text-[13px] font-medium transition-colors whitespace-nowrap select-none',
        'disabled:opacity-50 disabled:pointer-events-none',
        variants[variant],
        className,
      ].join(' ')}
      {...props}
    >
      {leftIcon}
      <span>{children}</span>
      {rightIcon}
    </button>
  )
}

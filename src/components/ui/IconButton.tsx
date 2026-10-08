import type { ButtonHTMLAttributes, ReactNode } from 'react'

type IconButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  children: ReactNode
  label: string
  size?: 'sm' | 'md'
}

export function IconButton({
  children,
  label,
  size = 'md',
  className = '',
  ...props
}: IconButtonProps) {
  const sizeClass = size === 'sm' ? 'h-7 w-7' : 'h-8 w-8'

  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      className={[
        'inline-flex items-center justify-center rounded-full text-b24-white-soft',
        'hover:bg-b24-glass transition-colors',
        sizeClass,
        className,
      ].join(' ')}
      {...props}
    >
      {children}
    </button>
  )
}

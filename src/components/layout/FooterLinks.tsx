import { ChevronDown } from 'lucide-react'

export function FooterLinks() {
  return (
    <footer className="flex flex-wrap items-center gap-x-4 gap-y-1 px-1 pt-3 pb-1 text-[12px] text-b24-white-mute">
      <span>© «Битрикс», 2026</span>
      <button type="button" className="hover:text-white transition-colors inline-flex items-center gap-0.5">
        Русский
        <ChevronDown size={12} strokeWidth={1.75} />
      </button>
      <button type="button" className="hover:text-white transition-colors">
        Темы
      </button>
    </footer>
  )
}

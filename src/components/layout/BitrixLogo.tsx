export function BitrixLogo({ collapsed = false }: { collapsed?: boolean }) {
  if (collapsed) {
    return (
      <div className="flex items-center justify-center w-full py-1" aria-label="Битрикс24">
        <span className="text-b24-cyan text-[22px] font-bold leading-none">24</span>
      </div>
    )
  }

  return (
    <div className="flex items-baseline gap-0 select-none" aria-label="Битрикс24">
      <span className="text-white text-[20px] font-semibold tracking-tight leading-none">
        Битрикс
      </span>
      <span className="text-b24-cyan text-[20px] font-bold leading-none">24</span>
    </div>
  )
}

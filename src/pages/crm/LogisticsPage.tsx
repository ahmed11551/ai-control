import { DealsPage } from './DealsPage'

/**
 * Кабинет логиста = воронка «Оформление и сопровождение» (тот же UI, что Адаптация).
 * Отдельный «самодельный» канбан не нужен — это и есть логистика.
 */
export function LogisticsPage() {
  return (
    <DealsPage
      lockedPipelineKey="local:escort"
      heading="Логистика"
      subtitle="Оформление и сопровождение · билеты, посадка, путь, размещение, финал"
    />
  )
}

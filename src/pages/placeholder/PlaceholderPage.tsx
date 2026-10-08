import { useParams } from 'react-router-dom'
import { PageHeader, PageShell } from '../../components/layout/PageHeader'
import { placeholderMeta } from '../../data/nav'

export function PlaceholderPage() {
  const { section } = useParams<{ section: string }>()
  const meta = (section && placeholderMeta[section]) || {
    title: 'Раздел',
    description: 'Этот раздел появится позже.',
    icon: placeholderMeta.apps.icon,
  }
  const Icon = meta.icon

  return (
    <PageShell>
      <PageHeader title={meta.title} subtitle={meta.description} />

      <div className="b24-glass-panel flex flex-col items-center justify-center flex-1 min-h-[420px] px-8 py-16 text-center">
        <div className="mb-4 flex size-14 items-center justify-center rounded-2xl bg-[#eef2f4] text-b24-text">
          <Icon size={28} strokeWidth={1.5} />
        </div>
        <h3 className="text-[22px] font-semibold text-[#a0a5ab] mb-2">{meta.title}</h3>
        <p className="max-w-[400px] text-[14px] leading-relaxed text-[#b0b5bb]">
          {meta.description}
        </p>
      </div>
    </PageShell>
  )
}

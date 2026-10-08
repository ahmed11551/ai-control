import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { LeadDrawer } from '../pages/crm/EntityDrawers'
import { type CrmRow } from '../lib/crm'

type CrmLeadDrawerValue = {
  leadId: string | null
  openLead: (id: string, seed?: CrmRow | null) => void
  closeLead: () => void
  /** страницы подписываются и перезагружают списки */
  revision: number
  setEditHandler: (fn: ((id: string) => void) | null) => void
}

const CrmLeadDrawerContext = createContext<CrmLeadDrawerValue | null>(null)

export function CrmLeadDrawerProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const location = useLocation()
  const [leadId, setLeadId] = useState<string | null>(null)
  const [seed, setSeed] = useState<CrmRow | null>(null)
  const [revision, setRevision] = useState(0)
  const editHandler = useRef<((id: string) => void) | null>(null)

  const bump = useCallback(() => setRevision((n) => n + 1), [])

  useEffect(() => {
    if (!leadId) return
    // Карточка лида: CRM, звонки менеджера, входящие и кабинет логиста.
    // /dialogs — нет: иначе LeadDrawer перекрывает staff-чат после «Открыть диалог».
    const path = location.pathname
    if (
      path.startsWith('/crm') ||
      path === '/calls' ||
      path.startsWith('/inbox') ||
      path.startsWith('/logistics')
    ) {
      return
    }
    setLeadId(null)
    setSeed(null)
  }, [location.pathname, leadId])

  const openLead = useCallback((id: string, nextSeed?: CrmRow | null) => {
    setLeadId(id)
    if (nextSeed && String(nextSeed.id ?? '') === id) setSeed(nextSeed)
    else if (!nextSeed) setSeed(null)
  }, [])

  const closeLead = useCallback(() => {
    setLeadId(null)
    setSeed(null)
  }, [])

  const setEditHandler = useCallback((fn: ((id: string) => void) | null) => {
    editHandler.current = fn
  }, [])

  const value = useMemo(
    () => ({ leadId, openLead, closeLead, revision, setEditHandler }),
    [leadId, openLead, closeLead, revision, setEditHandler],
  )

  return (
    <CrmLeadDrawerContext.Provider value={value}>
      {children}
      {leadId ? (
        <LeadDrawer
          id={leadId}
          seed={seed && String(seed.id ?? '') === leadId ? seed : null}
          onClose={() => {
            closeLead()
            const params = new URLSearchParams(window.location.search)
            if (params.has('open') || params.has('lead')) {
              params.delete('open')
              params.delete('lead')
              const qs = params.toString()
              navigate(qs ? `${window.location.pathname}?${qs}` : window.location.pathname, {
                replace: true,
              })
            }
          }}
          onChanged={() => {
            if (leadId) editHandler.current?.(leadId)
            bump()
          }}
          onDeleted={() => {
            closeLead()
            bump()
            const params = new URLSearchParams(window.location.search)
            params.delete('open')
            params.delete('lead')
            const qs = params.toString()
            navigate(qs ? `${window.location.pathname}?${qs}` : window.location.pathname, {
              replace: true,
            })
          }}
        />
      ) : null}
    </CrmLeadDrawerContext.Provider>
  )
}

export function useCrmLeadDrawer() {
  const ctx = useContext(CrmLeadDrawerContext)
  if (!ctx) throw new Error('useCrmLeadDrawer outside provider')
  return ctx
}

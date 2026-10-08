import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { api, setToken, token } from '../lib/api'

export type AuthUser = {
  id: string
  email: string
  login?: string
  full_name: string
  role: string
  extension: string
  can_call: boolean
  see_phones?: boolean
  can_see_old_leads?: boolean
  session_restricted?: boolean
  creatable_roles?: string[]
  customer_site?: string
  customer_site_code?: string
}

type AuthContextValue = {
  user: AuthUser | null
  ready: boolean
  login: (loginOrEmail: string, password: string) => Promise<AuthUser>
  logout: () => void
  initials: string
}

const AuthContext = createContext<AuthContextValue | null>(null)

function initialsOf(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '??'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null)
  const [ready, setReady] = useState(false)

  useEffect(() => {
    const t = token()
    if (!t) {
      setReady(true)
      return
    }
    api<AuthUser>('/me')
      .then(setUser)
      .catch(() => {
        setToken(null)
        setUser(null)
      })
      .finally(() => setReady(true))
  }, [])

  const login = useCallback(async (loginOrEmail: string, password: string) => {
    const data = await api<{ token: string; user: AuthUser }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ login: loginOrEmail, password }),
    })
    setToken(data.token)
    setUser(data.user)
    return data.user
  }, [])

  const logout = useCallback(() => {
    setToken(null)
    setUser(null)
  }, [])

  const value = useMemo(
    () => ({
      user,
      ready,
      login,
      logout,
      initials: initialsOf(user?.full_name || ''),
    }),
    [user, ready, login, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}

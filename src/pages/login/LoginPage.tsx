import { useState } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { Button } from '../../components/ui/Button'
import { homePathForRole } from '../../data/nav'
import { useAuth } from '../../store/auth'

export function LoginPage() {
  const { user, ready, login } = useAuth()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const [loginId, setLoginId] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState(
    params.get('office') === '1'
      ? 'Доступ только из офиса'
      : params.get('dept') === '1'
        ? 'Отдел временно отключён администратором'
        : '',
  )
  const [pending, setPending] = useState(false)

  if (ready && user) {
    return <Navigate to={homePathForRole(user.role)} replace />
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setPending(true)
    try {
      const u = await login(loginId.trim(), password)
      navigate(homePathForRole(u?.role ?? user?.role), { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось войти')
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="relative min-h-full">
      <div className="b24-theme-bg" aria-hidden />
      <main className="relative z-10 min-h-full grid place-items-center px-4 py-10">
        <form
          onSubmit={submit}
          className="w-full max-w-[400px] rounded-b24 bg-white shadow-2xl border border-black/5 overflow-hidden"
        >
          <div className="px-6 pt-6 pb-2">
            <div className="flex items-baseline gap-0 select-none mb-4">
              <span className="text-b24-text-dark text-[22px] font-semibold tracking-tight">
                Битрикс
              </span>
              <span className="text-b24-cyan text-[22px] font-bold">24</span>
            </div>
            <h1 className="text-[20px] font-semibold text-b24-text-dark">Вход в портал</h1>
            <p className="mt-1 text-[13px] text-b24-text">
              ForgeCRM на вашем сервере. Без облака Bitrix24.
            </p>
          </div>

          <div className="px-6 py-4 flex flex-col gap-3.5">
            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-medium text-b24-text">Логин или email</span>
              <input
                autoFocus
                type="text"
                autoComplete="username"
                value={loginId}
                onChange={(e) => setLoginId(e.target.value)}
                className="h-10 rounded-b24-sm border border-black/10 px-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan focus:ring-2 focus:ring-b24-cyan/20"
              />
            </label>
            <label className="flex flex-col gap-1.5">
              <span className="text-[12px] font-medium text-b24-text">Пароль</span>
              <input
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="h-10 rounded-b24-sm border border-black/10 px-3 text-[14px] text-b24-text-dark outline-none focus:border-b24-cyan focus:ring-2 focus:ring-b24-cyan/20"
              />
            </label>
            {error ? <p className="text-[12px] text-red-500">{error}</p> : null}
          </div>

          <div className="px-6 py-4 bg-[#f8f9fb] border-t border-black/6">
            <Button type="submit" variant="green" className="w-full rounded-b24-sm" disabled={pending}>
              {pending ? 'Входим…' : 'Войти'}
            </Button>
          </div>
        </form>
      </main>
    </div>
  )
}

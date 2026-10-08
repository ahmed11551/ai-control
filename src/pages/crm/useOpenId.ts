import { useSearchParams } from 'react-router-dom'

export function useOpenId() {
  const [params, setParams] = useSearchParams()
  const openId = params.get('open')

  const setOpenId = (id: string | null) => {
    const next = new URLSearchParams(params)
    if (id) next.set('open', id)
    else next.delete('open')
    setParams(next, { replace: true })
  }

  return [openId, setOpenId] as const
}

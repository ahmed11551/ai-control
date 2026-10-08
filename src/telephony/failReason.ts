export function explainCallFail(code?: number, reason?: string): string {
  const text = String(reason || '').toLowerCase()
  if (code === 401 || text.includes('unauthorized')) {
    return (
      '401 Unauthorized: проверьте логин/пароль softphone (VITE_VOX_*) или сценарий в кабинете Vox.'
    )
  }
  if (code === 403 || text.includes('forbidden')) {
    return (
      'Vox вернул 403: для исходящих нужен callPSTN и CLI из номеров, привязанных к приложению Vox ' +
      '(Numbers → Attach). В сценарии OUTBOUND_CALLER_ID = ваш номер Vox (+7901…). ' +
      'Также проверьте GEO IP restrictions в аккаунте.'
    )
  }
  if (code === 402 || text.includes('insufficient') || text.includes('funds')) {
    return 'Недостаточно средств на счёте телефонии (402).'
  }
  if (code === 404) {
    return 'Номер не найден (404). Проверьте формат: 79001234567.'
  }
  if (code === 486) {
    return 'Абонент занят (486).'
  }
  if (code === 480 || code === 408) {
    return 'Абонент не отвечает.'
  }
  const detail = [code != null ? `код ${code}` : null, reason].filter(Boolean).join(' — ')
  return detail ? `Звонок не удался (${detail})` : 'Звонок не удался'
}

export function phoneTail4(raw: string): string {
  const d = raw.replace(/\D/g, '')
  return d.length >= 4 ? d.slice(-4) : ''
}

export function maxDownBanner(currentLabel: string, otherLabel?: string): string {
  const cur = phoneTail4(currentLabel)
  const num = cur ? `····${cur}` : 'этот номер'
  if (otherLabel) {
    const otherTail = phoneTail4(otherLabel)
    const other = otherTail ? `····${otherTail}` : 'другого номера'
    return `Номер ${num} не работает. Сообщение из этого чата клиенту не дойдёт. Новым клиентам пишите с ${other}. Этот чат он не продолжает.`
  }
  return `Номер ${num} не работает. Сообщение из этого чата клиенту не дойдёт. Свободных рабочих номеров сейчас нет. Старая переписка сохраняется.`
}

export function maxColdBanner(currentLabel: string): string {
  const cur = phoneTail4(currentLabel)
  const num = cur ? `····${cur}` : 'этот номер'
  return `С номера ${num} нельзя написать новому человеку: лимит на новые контакты. Старые чаты с этого номера остаются.`
}

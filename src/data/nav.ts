import type { LucideIcon } from 'lucide-react'
import {
  BarChart3,
  BrainCircuit,
  BookOpen,
  Briefcase,
  CalendarDays,
  ChartPie,
  CheckSquare,
  ClipboardList,
  Database,
  FolderKanban,
  Grid3x3,
  Inbox,
  LayoutGrid,
  LifeBuoy,
  MessageCircle,
  Package,
  Phone,
  PhoneCall,
  Settings,
  Shield,
  ShieldAlert,
  Share2,
  Store,
  Truck,
  Users,
  Zap,
} from 'lucide-react'
import { isCustomerRole, isLogistRole } from '../lib/roles'
import { AUTODIAL_ENABLED } from '../telephony/config'

export type NavItem = {
  id: string
  label: string
  path: string
  icon: LucideIcon
  expandable?: boolean
}

export const sidebarNav: NavItem[] = [
  { id: 'crm', label: 'CRM', path: '/crm', icon: Briefcase },
  { id: 'logistics', label: 'Логистика', path: '/logistics', icon: Truck },
  { id: 'dialogs', label: 'Диалоги', path: '/dialogs', icon: MessageCircle },
  { id: 'tasks', label: 'Задачи', path: '/tasks', icon: CheckSquare },
  { id: 'telephony', label: 'Телефония', path: '/telephony', icon: Phone },
  { id: 'autodial', label: 'Скорозвон', path: '/autodial', icon: PhoneCall },
  { id: 'inbox', label: 'Мессенджер', path: '/inbox', icon: Inbox },
  { id: 'distribution', label: 'Распределение', path: '/distribution', icon: Share2 },
  { id: 'employees', label: 'Сотрудники', path: '/employees', icon: Users },
]

const analyticsNavItem: NavItem = {
  id: 'analytics',
  label: 'Аналитика',
  path: '/analytics',
  icon: BarChart3,
}

const securityNavItem: NavItem = {
  id: 'security',
  label: 'Безопасность',
  path: '/security',
  icon: ShieldAlert,
}

const oldLeadsNavItem: NavItem = {
  id: 'old-leads',
  label: 'Спящие лиды',
  path: '/crm/old-leads',
  icon: FolderKanban,
}

function withOldLeads(items: NavItem[], show?: boolean): NavItem[] {
  if (!show || items.some((i) => i.id === 'old-leads')) return items
  const crm = items.findIndex((i) => i.id === 'crm')
  if (crm < 0) return [oldLeadsNavItem, ...items]
  return [...items.slice(0, crm + 1), oldLeadsNavItem, ...items.slice(crm + 1)]
}

/** Какие пункты меню видит роль. «Старые лиды» — отделение Тёмы и администратор. */
export function navForRole(role?: string, opts?: { canSeeOldLeads?: boolean }): NavItem[] {
  const r = String(role || '').toLowerCase()
  // Скорозвон на паузе — пункт меню скрыт у всех (код/роут сохранены).
  const hideAutodial = (items: NavItem[]) =>
    AUTODIAL_ENABLED ? items : items.filter((i) => i.id !== 'autodial')
  if (isCustomerRole(r)) {
    return [{ id: 'customer', label: 'Прибывающие', path: '/customer', icon: ClipboardList }]
  }
  if (isLogistRole(r)) {
    // Кабинет логиста = воронка «Оформление и сопровождение».
    // Ночной логист ещё звонит со своего Vox.
    const ids =
      r === 'night_logist'
        ? ['logistics', 'dialogs', 'tasks', 'telephony']
        : ['logistics', 'dialogs', 'tasks']
    return withOldLeads(
      hideAutodial(sidebarNav.filter((i) => ids.includes(i.id))),
      opts?.canSeeOldLeads,
    )
  }
  const support: NavItem = { id: 'support', label: 'Техподдержка', path: '/support', icon: LifeBuoy }
  if (r === 'manager' || r === 'sales') {
    const items = hideAutodial(
      sidebarNav.filter((i) => !['employees', 'distribution', 'autodial', 'logistics'].includes(i.id)),
    )
    items.push(analyticsNavItem, support)
    const calls: NavItem = { id: 'calls', label: 'Звонки', path: '/calls', icon: PhoneCall }
    const afterTelephony = items.findIndex((i) => i.id === 'telephony')
    const withCalls =
      afterTelephony < 0
        ? [...items, calls]
        : [...items.slice(0, afterTelephony + 1), calls, ...items.slice(afterTelephony + 1)]
    return withOldLeads(withCalls, opts?.canSeeOldLeads)
  }
  // Admin/ROP смотрят ту же воронку во вкладке CRM «Оформление и сопровождение» —
  // отдельный пункт «Логистика» дублирует и скрыт.
  if (r === 'rop') {
    return withOldLeads(
      hideAutodial([
        ...sidebarNav.filter((i) => !['autodial', 'logistics'].includes(i.id)),
        analyticsNavItem,
        securityNavItem,
        support,
      ]),
      opts?.canSeeOldLeads,
    )
  }
  if (r === 'admin') {
    const base = hideAutodial(sidebarNav.filter((i) => i.id !== 'logistics'))
    const arrivals: NavItem = { id: 'arrivals', label: 'Прибывающие', path: '/arrivals', icon: ClipboardList }
    const crmAt = base.findIndex((i) => i.id === 'crm')
    const withArrivals =
      crmAt < 0 ? [arrivals, ...base] : [...base.slice(0, crmAt + 1), arrivals, ...base.slice(crmAt + 1)]
    return withOldLeads(
      [
        ...withArrivals,
        analyticsNavItem,
        securityNavItem,
        support,
        { id: 'max-accounts', label: 'MAX', path: '/max-accounts', icon: MessageCircle },
        { id: 'office', label: 'Доступ по IP', path: '/office-access', icon: Shield },
        { id: 'ai-control', label: 'AI Control', path: '/ai-control', icon: BrainCircuit },
      ],
      true,
    )
  }
  return withOldLeads(
    hideAutodial(sidebarNav.filter((i) => i.id !== 'logistics')),
    opts?.canSeeOldLeads,
  )
}

export const tasksModuleTabs = [{ id: 'tasks', label: 'Задачи', path: '/tasks' }]

export const crmModuleTabs = [
  { id: 'crm', label: 'Кандидаты', path: '/crm' },
  { id: 'leads', label: 'Лиды', path: '/crm/leads' },
]

const oldLeadsTab = { id: 'old-leads', label: 'Спящие лиды', path: '/crm/old-leads' }

/** CRM-вкладки по роли. «Старые лиды» — только отделение Тёмы и администратор. */
export function crmTabsForRole(role?: string, opts?: { canSeeOldLeads?: boolean }) {
  if (isLogistRole(role)) return []
  if (!opts?.canSeeOldLeads) return crmModuleTabs
  return [...crmModuleTabs, oldLeadsTab]
}

/** Стартовый путь после входа. */
export function homePathForRole(role?: string) {
  if (isCustomerRole(role)) return '/customer'
  if (isLogistRole(role)) return '/logistics'
  return '/crm'
}

export const tasksViewTabs = [
  { id: 'list', label: 'Список' },
  { id: 'deadlines', label: 'Сроки' },
  { id: 'plan', label: 'Мой план' },
  { id: 'calendar', label: 'Календарь' },
  { id: 'gantt', label: 'Гант' },
  { id: 'chats', label: 'Чаты задач' },
]

export const taskTableColumns = [
  'Название',
  'Активность',
  'Крайний срок',
  'Постановщик',
  'Исполнитель',
  'Проект',
  'Теги',
]

export const placeholderMeta: Record<
  string,
  { title: string; description: string; icon: LucideIcon }
> = {
  collaboration: {
    title: 'Совместная работа',
    description: 'Лента, чаты, диск и календарь появятся здесь.',
    icon: MessageCircle,
  },
  crm: {
    title: 'CRM',
    description: 'Кандидаты и лиды.',
    icon: Briefcase,
  },
  telephony: {
    title: 'Телефония',
    description: 'Softphone Voximplant / МТС Exolve.',
    icon: Phone,
  },
  inbox: {
    title: 'Мессенджер',
    description: 'Все диалоги с кандидатами в одном месте.',
    icon: Inbox,
  },
  distribution: {
    title: 'Распределение',
    description: 'Доли и лимиты: сначала по отделам, затем менеджерам внутри отдела.',
    icon: Share2,
  },
  autodial: {
    title: 'Скорозвон',
    description: 'История автодозвонов, статусы и настройки частоты.',
    icon: PhoneCall,
  },
  booking: {
    title: 'Онлайн-запись',
    description: 'Запись клиентов и расписание услуг.',
    icon: CalendarDays,
  },
  inventory: {
    title: 'Складской учёт',
    description: 'Остатки, документы и каталог товаров.',
    icon: Package,
  },
  marketing: {
    title: 'Маркетинг',
    description: 'Рассылки, реклама и аналитика кампаний.',
    icon: ChartPie,
  },
  sites: {
    title: 'Сайты и магазины',
    description: 'Конструктор сайтов и интернет-магазин.',
    icon: Store,
  },
  bi: {
    title: 'BI Конструктор',
    description: 'Дашборды и отчёты по данным компании.',
    icon: BarChart3,
  },
  employees: {
    title: 'Сотрудники',
    description: 'Структура компании и профили сотрудников.',
    icon: Users,
  },
  apps: {
    title: 'Приложения',
    description: 'Маркетплейс и установленные приложения.',
    icon: Grid3x3,
  },
  knowledge: {
    title: 'База знаний 2.0',
    description: 'Статьи, инструкции и база знаний команды.',
    icon: BookOpen,
  },
  projects: {
    title: 'Проекты',
    description: 'Список проектов и рабочих групп.',
    icon: FolderKanban,
  },
  flows: {
    title: 'Потоки',
    description: 'Потоки задач и распределение нагрузки.',
    icon: Zap,
  },
  scrum: {
    title: 'Скрам',
    description: 'Спринты, бэклог и доска команды.',
    icon: LayoutGrid,
  },
  efficiency: {
    title: 'Эффективность',
    description: 'Показатели вовлечённости и скорости.',
    icon: ClipboardList,
  },
  analytics: {
    title: 'Аналитика',
    description: 'Отчёты по задачам и проектам.',
    icon: Database,
  },
  settings: {
    title: 'Настройки',
    description: 'Параметры портала и персональные настройки.',
    icon: Settings,
  },
}

import { BrowserRouter, Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import { AppShell } from '../components/layout/AppShell'
import { LeadsPage } from '../pages/crm/LeadsPage'
import { ManagerCallsPage } from '../pages/crm/ManagerCallsPage'
import { DealsPage } from '../pages/crm/DealsPage'
import { LogisticsPage } from '../pages/crm/LogisticsPage'
import { ImportPage } from '../pages/import/ImportPage'
import { MaxAccountsPage } from '../pages/max/MaxAccountsPage'
import { OfficeAccessPage } from '../pages/settings/OfficeAccessPage'
import { LoginPage } from '../pages/login/LoginPage'
import { PlaceholderPage } from '../pages/placeholder/PlaceholderPage'
import { TasksPage } from '../pages/tasks/TasksPage'
import { InboxPage } from '../pages/inbox/InboxPage'
import { TelephonyPage } from '../pages/telephony/TelephonyPage'
import { AutodialHistoryPage } from '../pages/autodial/AutodialHistoryPage'
import { DistributionPage } from '../pages/distribution/DistributionPage'
import { EmployeePage } from '../pages/employees/EmployeePage'
import { EmployeesPage } from '../pages/employees/EmployeesPage'
import { StaffDialogsPage } from '../pages/staff-dialogs/StaffDialogsPage'
import { AnalyticsPage } from '../pages/analytics/AnalyticsPage'
import { SecurityPage } from '../pages/security/SecurityPage'
import { SupportPage } from '../pages/support/SupportPage'
import { homePathForRole } from '../data/nav'
import { isCustomerRole, isLogistRole } from '../lib/roles'
import { CustomerShell } from '../pages/customer/CustomerShell'
import { CustomerHome, CustomerPerson } from '../pages/customer/CustomerPages'
import { AuthProvider, useAuth } from '../store/auth'
import { CrmLeadDrawerProvider } from '../store/crmLeadDrawer'
import { TasksProvider } from '../store/tasks'
import { TelephonyProvider } from '../store/telephony'
import { AiControlPage } from '../pages/ai/AiControlPage'

function RequireAuth() {
  const { user, ready } = useAuth()
  if (!ready) {
    return (
      <div className="relative h-full">
        <div className="b24-theme-bg" aria-hidden />
        <div className="relative z-10 grid h-full place-items-center text-white/80 text-[14px]">
          Загрузка…
        </div>
      </div>
    )
  }
  if (!user) return <Navigate to="/login" replace />
  return <Outlet />
}

function RequireAdmin() {
  const { user } = useAuth()
  if (String(user?.role || '').toLowerCase() !== 'admin') {
    return <Navigate to={homePathForRole(user?.role)} replace />
  }
  return <Outlet />
}

function OldLeadsGate() {
  const { user } = useAuth()
  if (!user?.can_see_old_leads) return <Navigate to="/crm" replace />
  return <DealsPage lockedPipelineKey="local:old-leads" heading="Спящие лиды" />
}

function HomeRedirect() {
  const { user } = useAuth()
  return <Navigate to={homePathForRole(user?.role)} replace />
}

function CustomerOnly() {
  const { user } = useAuth()
  if (!isCustomerRole(user?.role)) return <Navigate to={homePathForRole(user?.role)} replace />
  return <Outlet />
}

function StaffOnly() {
  const { user } = useAuth()
  if (isCustomerRole(user?.role)) return <Navigate to="/customer" replace />
  return <Outlet />
}

function BlockLogistFromCrm() {
  const { user } = useAuth()
  const location = useLocation()
  if (isLogistRole(user?.role)) {
    const params = new URLSearchParams(location.search)
    const open = params.get('open') || ''
    const lead = params.get('lead') || ''
    // Ссылка на лид: /crm/leads?open= — в логистике это ?lead=
    if (location.pathname.startsWith('/crm/leads')) {
      const id = open || lead
      if (id) return <Navigate to={`/logistics?lead=${encodeURIComponent(id)}`} replace />
    }
    // Ссылка на сделку: /crm?open= — та же карточка в кабинете логиста.
    if (open) return <Navigate to={`/logistics?open=${encodeURIComponent(open)}`} replace />
    if (lead) return <Navigate to={`/logistics?lead=${encodeURIComponent(lead)}`} replace />
    return <Navigate to="/logistics" replace />
  }
  return <Outlet />
}

function AuthedLayout() {
  return (
    <TasksProvider>
      <TelephonyProvider>
        <CrmLeadDrawerProvider>
          <AppShell />
        </CrmLeadDrawerProvider>
      </TelephonyProvider>
    </TasksProvider>
  )
}

export function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route element={<RequireAuth />}>
            <Route element={<CustomerOnly />}>
              <Route element={<CustomerShell />}>
                <Route path="customer" element={<CustomerHome />} />
                <Route path="customer/:id" element={<CustomerPerson />} />
              </Route>
            </Route>
            <Route element={<StaffOnly />}>
            <Route element={<AuthedLayout />}>
              <Route index element={<HomeRedirect />} />
              <Route path="dashboard" element={<HomeRedirect />} />
              <Route path="tasks" element={<TasksPage />} />
              <Route path="dialogs" element={<StaffDialogsPage />} />
              <Route path="support" element={<SupportPage />} />
              <Route path="logistics" element={<LogisticsPage />} />
              <Route element={<BlockLogistFromCrm />}>
                <Route path="crm/old-leads" element={<OldLeadsGate />} />
                <Route path="crm" element={<DealsPage />} />
                <Route path="crm/leads" element={<LeadsPage />} />
                <Route path="crm/calls" element={<Navigate to="/calls" replace />} />
              </Route>
              <Route path="calls" element={<ManagerCallsPage />} />
              <Route path="analytics" element={<AnalyticsPage />} />
              <Route element={<RequireAdmin />}>
                <Route path="security" element={<SecurityPage />} />
              </Route>
              <Route path="crm/contacts" element={<HomeRedirect />} />
              <Route path="crm/companies" element={<HomeRedirect />} />
              <Route path="crm/import" element={<HomeRedirect />} />
              <Route path="telephony" element={<TelephonyPage />} />
              <Route path="inbox" element={<InboxPage />} />
              <Route element={<RequireAdmin />}>
                <Route path="distribution" element={<DistributionPage />} />
              </Route>
              <Route element={<RequireAdmin />}>
                <Route path="autodial" element={<AutodialHistoryPage />} />
                <Route path="office-access" element={<OfficeAccessPage />} />
                <Route path="arrivals" element={<CustomerHome admin />} />
                <Route path="max-accounts" element={<MaxAccountsPage />} />
                <Route path="ai-control" element={<AiControlPage />} />
              </Route>
              <Route path="employees" element={<EmployeesPage />} />
              <Route path="employees/:id" element={<EmployeePage />} />
              <Route path="settings" element={<ImportPage />} />
              <Route path="automation" element={<HomeRedirect />} />
              <Route path=":section" element={<PlaceholderPage />} />
            </Route>
            </Route>
          </Route>
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  )
}

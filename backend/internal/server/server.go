package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/forgecrm/server/internal/config"
	"github.com/forgecrm/server/internal/schema"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type App struct {
	cfg config.Config
	db  *pgxpool.Pool
	rdb *redis.Client
	hub *Hub
	ami *AMI
	pbx *pbxHub
	imp *importHub

	maxBotUsername string
	maxBotName     string

	taskAttentionOnce   sync.Once
	logisticsSchemaOnce sync.Once
	customerSchemaOnce  sync.Once
	managerDistOnce     sync.Once
	sleepingDistOnce    sync.Once
	stickyIndexOnce     sync.Once
	recruitFunnelsOnce  sync.Once
	staffDialogsOnce    sync.Once
	securitySchemaOnce  sync.Once

	// Одна закачка медиа на сообщение. Повтор с открытого чата — не чаще раза в 2 минуты.
	mediaFetch mediaFetchGate
}

func Run() error {
	cfg := config.Load()
	ctx := context.Background()

	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	if err := db.Ping(ctx); err != nil {
		return err
	}

	var rdb *redis.Client
	if opt, err := redis.ParseURL(cfg.RedisURL); err == nil {
		c := redis.NewClient(opt)
		pingCtx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
		if err := c.Ping(pingCtx).Err(); err != nil {
			log.Println("redis unavailable, continuing without cache:", err)
			_ = c.Close()
		} else {
			rdb = c
		}
		cancel()
	}

	app := &App{cfg: cfg, db: db, rdb: rdb, hub: NewHub(), imp: &importHub{}}
	go app.hub.Run()

	if err := app.bootstrap(ctx); err != nil {
		return err
	}

	if cfg.SkipAMI {
		log.Println("AMI disabled (SKIP_AMI)")
	} else {
		app.ami = NewAMI(cfg.AMIHost, cfg.AMIPort, cfg.AMIUser, cfg.AMISecret, app)
		go app.ami.Loop()
	}
	app.pbx = newPBX(cfg)
	go app.vkFlushLoop()
	app.ensureMaxBotLine(ctx)
	go app.maxBotPollLoop()
	// go app.wappiPollLoop() // временно выкл: опрос MAX каждые 5с
	// Не блокируем ListenAndServe — иначе локально Bad Gateway, пока идут десятки url/set.
	go app.wappiEnsureWebhook()
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		app.syncWappiLineLabels(cctx)
	}()
	go app.yandexMailLoop()
	go app.distributionOrphanLoop()
	go app.distributionSleepingLoop()
	go app.callReviewLoop()
	go app.aiOutboxLoop()
	app.startTelegramBot()
	app.startMskTowerTelegramBot()

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "name": "portal"})
	})
	r.Post("/auth/login", app.login)
	r.HandleFunc("/ws", app.serveWS)
	r.Post("/webhooks/vk-ads", app.vkWebhook)
	r.Post("/webhooks/vk-ads/leads", app.vkLeadWebhook)
	r.Get("/webhooks/vk-ads/leads", app.vkLeadVerify)
	r.Post("/webhooks/telephony/{provider}", app.telephonyHook)
	r.Post("/webhooks/telephony/{provider}/{secret}", app.telephonyHook)
	// Exolve ЛК → Уведомления → Callback / события по звонкам (нужен публичный HTTPS).
	r.Post("/webhooks/exolve/callback/{secret}", app.exolveCallbackEventsHook)
	r.Post("/webhooks/exolve/events/{secret}", app.exolveCallbackEventsHook)
	r.Post("/webhooks/inbox/{provider}", app.inboxHook)
	r.Post("/webhooks/inbox/{provider}/{secret}", app.inboxHook)
	r.Post("/webhooks/leads/{secret}", app.leadsHook)
	r.Post("/webhooks/leads/{provider}/{secret}", app.leadsHook)
	r.Get("/webhooks/avito/{secret}", app.avitoApplicationsHook)
	r.Head("/webhooks/avito/{secret}", app.avitoApplicationsHook)
	r.Post("/webhooks/avito/{secret}", app.avitoApplicationsHook)
	r.Get("/public/ticket-file", app.publicTicketFile)
	r.Get("/public/inbox-media", app.publicInboxMedia)

	r.Group(func(r chi.Router) {
		r.Use(app.authMW)
		r.Use(app.customerCRMGate)
		r.Get("/me", app.me)
		r.Get("/settings/office-access", app.getOfficeAccess)
		r.Put("/settings/office-access", app.putOfficeAccess)
		r.Get("/search", app.search)
		r.Get("/dashboard", app.dashboard)
		r.Get("/analytics", app.analytics)
		r.Get("/analytics/meta", app.analyticsMeta)
		r.Get("/security/incidents", app.listSecurityIncidents)
		r.Get("/security/rules", app.listDLPRules)
		r.Get("/security/rules/audit", app.listDLPRuleAudit)
		r.Post("/security/rules", app.createDLPRule)
		r.Patch("/security/rules/{id}", app.patchDLPRule)
		r.Delete("/security/rules/{id}", app.deleteDLPRule)
		r.Post("/security/incidents/{id}", app.reviewSecurityIncident)
		r.Post("/security/users/{id}/release", app.releaseSessionRestriction)
		r.Post("/security/users/{id}/passport-text-approval", app.grantPassportTextApproval)
		r.Delete("/security/passport-text-approvals/{id}", app.revokePassportTextApproval)
		r.Get("/security/summary", app.securitySummary)
		r.Get("/security/feedback", app.securityFeedback)
		r.Post("/security/calls/{id}/transcript", app.postCallTranscript)
		r.Get("/ai-control/healthz", app.aiControlProxy)
		r.Get("/ai-control/readyz", app.aiControlProxy)
		r.Get("/ai-control/v1/jobs", app.aiControlProxy)
		r.Get("/ai-control/v1/jobs/{id}", app.aiControlProxy)
		r.Get("/ai-control/v1/analyses/{id}", app.aiControlProxy)
		r.Get("/ai-control/v1/events", app.aiControlProxy)
		r.Get("/ai-control/v1/findings", app.aiControlProxy)
		r.Get("/ai-control/v1/findings/{id}/reviews", app.aiControlProxy)
		r.Post("/ai-control/v1/findings/{id}/review", app.aiControlProxy)
		r.Post("/admin/ai-control/outbox/test", app.enqueueAITestEvent)
		r.Get("/pipelines", app.listPipelines)

		r.Get("/contacts", app.listContacts)
		r.Post("/contacts", app.createContact)
		r.Get("/contacts/{id}", app.getContact)
		r.Patch("/contacts/{id}", app.patchContact)

		r.Get("/companies", app.listCompanies)
		r.Post("/companies", app.createCompany)
		r.Get("/companies/{id}", app.getCompany)
		r.Patch("/companies/{id}", app.patchCompany)

		r.Get("/leads", app.listLeads)
		r.Post("/leads", app.createLead)
		r.Get("/leads/{id}", app.getLead)
		r.Patch("/leads/{id}", app.patchLead)
		r.Delete("/leads/{id}", app.deleteLead)
		r.Get("/leads/{id}/assignees", app.listLeadAssignees)
		r.Post("/leads/{id}/convert", app.convertLead)

		r.Get("/deals", app.listDeals)
		r.Get("/deals/stats", app.dealStats)
		r.Post("/deals", app.createDeal)
		r.Get("/deals/{id}", app.getDeal)
		r.Patch("/deals/{id}", app.patchDeal)
		r.Post("/deals/{id}/stage", app.moveDeal)
		r.Post("/deals/{id}/claim", app.claimOldLead)
		r.Post("/crm/old-leads/import", app.importOldLeads)
		r.Get("/deals/{id}/bilet-gate", app.biletGate)
		r.Get("/deals/{id}/bilet-doc", app.getBiletDocs)
		r.Post("/deals/{id}/bilet-doc", app.postBiletDoc)
		r.Post("/deals/{id}/bilet-doc/from-chat", app.pullBiletDocsFromChat)
		r.Get("/deals/{id}/bilet-doc/archive", app.downloadBiletDocsArchive)
		r.Post("/deals/{id}/ensure-lead", app.ensureDealLead)
		r.Post("/deals/{id}/transfer-to-logist", app.transferDealToLogist)
		r.Get("/deals/{id}/ticket", app.getDealTicket)
		r.Put("/deals/{id}/ticket", app.putDealTicket)
		r.Post("/deals/{id}/ticket/report-to-manager", app.reportTicketToManager)
		r.Get("/deals/{id}/night-notes", app.listNightNotes)
		r.Post("/deals/{id}/night-notes", app.addNightNote)
		r.Put("/deals/{id}/night-brief", app.putNightBrief)
		r.Get("/deals/{id}/customer-sends", app.getDealCustomerSends)
		r.Post("/deals/{id}/customer-sends", app.postDealCustomerSend)
		r.Delete("/deals/{id}/customer-sends/{code}", app.deleteDealCustomerSend)
		r.Get("/admin/arrivals", app.listAdminArrivals)
		r.Put("/admin/arrivals/{id}/arrived", app.putAdminArrival)
		r.Get("/admin/arrivals/{id}/files/{fileId}", app.getAdminArrivalFile)
		r.Get("/customer/people", app.listCustomerPeople)
		r.Get("/customer/positions", app.listCustomerPositions)
		r.Get("/customer/people/{id}", app.getCustomerPerson)
		r.Put("/customer/people/{id}/arrived", app.putCustomerPersonArrived)
		r.Get("/customer/people/{id}/files/{fileId}", app.getCustomerPersonFile)
		r.Post("/deals/{id}/ticket/files", app.uploadDealTicketFile)
		r.Get("/deals/{id}/ticket/files/{fileId}", app.getDealTicketFile)
		r.Post("/deals/{id}/ticket/send-to-candidate", app.sendDealTicketToCandidate)
		r.Get("/logistics/table", app.listLogisticsTable)

		r.Get("/tasks", app.listTasks)
		r.Post("/tasks", app.createTask)
		r.Get("/tasks/attention", app.listTaskAttention)
		r.Get("/tasks/{id}", app.getTask)
		r.Get("/tasks/{id}/comments", app.listTaskComments)
		r.Post("/tasks/{id}/comments", app.createTaskComment)
		r.Post("/tasks/{id}/resolve", app.resolveTask)
		r.Patch("/tasks/{id}", app.patchTask)
		r.Delete("/tasks/{id}", app.deleteTask)
		r.Post("/tasks/{id}/timer/start", app.timerStart)
		r.Post("/tasks/{id}/timer/stop", app.timerStop)

		r.Get("/projects", app.listProjects)
		r.Post("/projects", app.createProject)

		r.Get("/calls", app.listCalls)
		r.Get("/calls/mine", app.listManagerCalls)
		r.Post("/calls/open-card", app.openCallCard)
		r.Post("/calls/originate", app.originate)
		r.Post("/calls/resolve-dest", app.resolveCallDest)
		r.Get("/telephony", app.telephonyHome)
		r.Get("/telephony/live", app.liveCalls)
		r.Post("/telephony/demo-inbound", app.demoInboundCall)
		r.Post("/telephony/softphone-event", app.softphoneEvent)
		r.Get("/telephony/calls/{id}/recording", app.getCallRecording)
		r.Get("/telephony/autodial/check", app.autodialCheck)
		r.Get("/telephony/autodial/pending", app.autodialPending)
		r.Get("/telephony/autodial/history", app.autodialHistory)
		r.Get("/telephony/autodial/schedule", app.autodialScheduleGet)
		r.Put("/telephony/autodial/schedule", app.autodialSchedulePut)
		r.Post("/telephony/autodial/dismiss", app.autodialDismiss)
		r.Post("/telephony/autodial/claim", app.autodialClaim)
		r.Post("/telephony/autodial/disposition", app.autodialDisposition)
		r.Get("/telephony/exolve/dids", app.exolveOurDIDs)
		r.Get("/telephony/exolve/inbound", app.exolveInboundStatus)
		r.Post("/telephony/exolve/inbound", app.exolveEnableInbound)
		r.Get("/telephony/exolve/session", app.exolveSession)
		r.Get("/telephony/exolve/pool", app.exolvePoolStatus)
		r.Post("/telephony/exolve/cli/next", app.exolveCLINext)
		r.Post("/telephony/exolve/cli/release", app.exolveCLIRelease)
		r.Post("/telephony/exolve/pool/provision", app.exolvePoolProvision)
		r.Post("/telephony/exolve/pool/sync", app.exolvePoolSync)
		r.Post("/telephony/exolve/pool/ensure", app.exolvePoolEnsure)
		r.Post("/telephony/exolve/admin/import", app.exolveAdminImport)
		r.Get("/telephony/vox/inbound", app.voxInboundStatus)
		r.Post("/telephony/vox/inbound", app.voxEnableInbound)
		r.Get("/telephony/vox/session", app.voxSession)
		r.Get("/inbox", app.listInbox)
		r.Get("/inbox/lines", app.listInboxLines)
		r.Post("/inbox/lines", app.createInboxLine)
		r.Patch("/inbox/lines/{id}", app.patchInboxLine)
		r.Get("/inbox/max/numbers", app.listMaxNumbers)
		r.Get("/inbox/max/lines-for-lead", app.listMaxLinesForLead)
		r.Post("/inbox/wappi/open", app.wappiOpenByPhone)
		r.Post("/inbox/max/open", app.openMaxByPhone)
		r.Get("/inbox/messages/{id}/media", app.getInboxMessageMedia)
		r.Post("/inbox/messages/{id}/send-to-logist", app.sendInboxMediaToLogist)
		r.Get("/inbox/{id}/messages", app.listInboxMessages)
		r.Get("/inbox/{id}", app.getInboxThread)
		r.Post("/inbox/{id}/messages", app.replyInbox)

		r.Get("/distribution", app.listDistribution)
		r.Put("/distribution", app.saveDistribution)
		r.Get("/distribution/team", app.listDistributionTeam)
		r.Put("/distribution/team", app.saveDistributionTeam)
		r.Post("/distribution/team/bank/issue", app.postRopTeamBankIssue)
		r.Post("/distribution/team/bank/recall", app.postRopTeamBankRecall)
		r.Post("/distribution/assign", app.assignDistribution)
		r.Post("/distribution/orphans/drain", app.postDrainUnownedLeads)
		r.Get("/distribution/bank", app.getLeadBank)
		r.Post("/distribution/bank/issue", app.postLeadBankIssue)
		r.Post("/distribution/bank/recall", app.postLeadBankRecall)
		r.Post("/distribution/bank/reprocess-mail", app.postReprocessYandexMail)
		r.Post("/distribution/bank/catchup-mail", app.postCatchupYandexMail)
		r.Post("/departments", app.createDepartment)
		r.Post("/departments/quick", app.postQuickDepartment)
		r.Post("/departments/{id}/access", app.postDepartmentAccess)
		r.Delete("/departments/{id}", app.deleteDepartment)

		r.Get("/documents", app.listDocuments)
		r.Post("/documents", app.createDocument)

		r.Get("/calendar", app.listEvents)
		r.Post("/calendar", app.createEvent)

		r.Get("/notifications", app.listNotifications)
		r.Post("/notifications/{id}/read", app.readNotification)

		r.Get("/chat", app.listChat)
		r.Post("/chat", app.postChat)

		r.Get("/staff-dialogs", app.listStaffDialogs)
		r.Get("/staff-dialogs/candidates", app.listStaffDialogCandidates)
		r.Post("/staff-dialogs", app.createStaffDialog)
		r.Get("/staff-dialogs/messages/{id}/media", app.getStaffDialogMessageMedia)
		r.Get("/staff-dialogs/{id}", app.getStaffDialog)
		r.Patch("/staff-dialogs/{id}", app.renameStaffDialog)
		r.Post("/staff-dialogs/{id}/messages", app.postStaffDialogMessage)
		r.Post("/staff-dialogs/{id}/close", app.closeStaffDialog)

		r.Get("/support/summary", app.getSupportSummary)
		r.Get("/support/files/{id}", app.getSupportFile)
		r.Get("/support/threads/{id}", app.getSupportThread)
		r.Get("/support", app.getSupport)
		r.Post("/support/messages", app.postSupportMessage)

		r.Get("/activity/{type}/{id}", app.listActivity)
		r.Post("/activity/{type}/{id}", app.addComment)

		r.Get("/users", app.listUsers)
		r.Get("/employees", app.listEmployees)
		r.Get("/employees/{id}", app.getEmployee)
		r.Patch("/employees/{id}", app.patchEmployee)
		r.Post("/employees/{id}/offboard", app.offboardEmployee)
		r.Post("/employees/{id}/max", app.bindEmployeeMax)
		r.Post("/employees/{id}/max/check", app.checkEmployeeMaxPhone)
		r.Delete("/employees/{id}/max/{lineId}", app.unbindEmployeeMax)
		r.Get("/max-accounts", app.getMaxAccountsBoard)
		r.Post("/max-accounts/grant", app.postMaxAccountGrant)
		r.Post("/max-accounts/check", app.postMaxAccountCheck)
		r.Get("/employees/{id}/telephony", app.getEmployeeTelephony)
		r.Put("/employees/{id}/exolve", app.putEmployeeExolve)
		r.Delete("/employees/{id}/exolve", app.deleteEmployeeExolve)
		r.Post("/employees/{id}/vox", app.createEmployeeVox)
		r.Patch("/employees/{id}/vox/{vid}", app.patchEmployeeVox)
		r.Delete("/employees/{id}/vox/{vid}", app.deleteEmployeeVox)
		r.Post("/users", app.createUser)
		r.Get("/roles", app.listRoles)
		r.Get("/automations", app.listAutomations)
		r.Patch("/automations/{id}", app.patchAutomation)

		r.Get("/import/bitrix/status", app.importStatus)
		r.Post("/import/bitrix/inventory", app.importInventory)
		r.Post("/import/bitrix/run", app.importRun)
		r.Get("/fields", app.listUserFields)

		r.Get("/reports/summary", app.reportSummary)
		r.Get("/reports/vk", app.reportVK)
		r.Post("/vk/offline-conversions/flush", app.flushVKNow)
		r.Get("/vk/campaigns", app.listVKCampaigns)
		r.Post("/vk/campaigns/sync", app.syncVKCampaigns)
	})

	srv := &http.Server{Addr: cfg.Addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Println("ForgeCRM API on", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func (a *App) bootstrap(ctx context.Context) error {
	if _, err := a.db.Exec(ctx, schema.CompatSQL); err != nil {
		return err
	}
	if _, err := a.db.Exec(ctx, schema.ImportSQL); err != nil {
		return err
	}
	var n int
	if err := a.db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte(a.cfg.BootstrapPass), 12)
		if err != nil {
			return err
		}
		var roleID string
		if err := a.db.QueryRow(ctx, `SELECT id FROM roles WHERE name='admin'`).Scan(&roleID); err != nil {
			return err
		}
		_, err = a.db.Exec(ctx, `
			INSERT INTO users (email, login, password_hash, full_name, role_id, phone_extension, can_call)
			VALUES ($1,$2,$3,'Administrator',$4,'1001', true)`,
			strings.ToLower(a.cfg.BootstrapEmail),
			a.resolveStaffLogin(ctx, "", "Administrator", a.cfg.BootstrapEmail),
			string(hash), roleID)
		if err != nil {
			return err
		}
		log.Println("bootstrap admin:", a.cfg.BootstrapEmail)
	}
	a.seedDemoTeamAndLeads(ctx)
	// Schema/funnel ensures once at boot — not on every HTTP request (avoids WAL/LWLock storms).
	a.ensureTaskAttentionSchema(ctx)
	a.ensureLogisticsSchema(ctx)
	a.ensureCustomerSchema(ctx)
	a.ensureManagerDistTables(ctx)
	a.ensureRecruitFunnels(ctx)
	a.ensureSecuritySchema(ctx)
	a.ensureAIOutboxSchema(ctx)
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func actorID(r *http.Request) string {
	return r.Context().Value(ctxUserID).(string)
}

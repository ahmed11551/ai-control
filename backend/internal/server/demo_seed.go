package server

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// demoSeedEnabled — на проде выключено: иначе каждый рестарт создаёт «Отдел Ивана».
func demoSeedEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DEMO_SEED"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// seedDemoTeamAndLeads — идемпотентно создаёт демо РОПа, менеджера, отдел и тестовые лиды.
func (a *App) seedDemoTeamAndLeads(ctx context.Context) {
	if !demoSeedEnabled() {
		return
	}
	ropID := a.ensureDemoUser(ctx, "ivan.ivanov@forgecrm.local", "Иван Иванов", "rop", "IvanRop!123")
	mgrID := a.ensureDemoUser(ctx, "alexey.manager@forgecrm.local", "Алексей Менеджеров", "manager", "Manager!123")
	if ropID == "" {
		return
	}
	deptID := a.ensureDemoDepartment(ctx, "Отдел Ивана", ropID)
	if deptID != "" {
		_, _ = a.db.Exec(ctx, `
			UPDATE users SET department_id=$1::uuid
			WHERE id=$2::uuid AND (department_id IS NULL OR department_id<>$1::uuid)`, deptID, ropID)
		if mgrID != "" {
			_, _ = a.db.Exec(ctx, `
				UPDATE users SET department_id=$1::uuid
				WHERE id=$2::uuid AND (department_id IS NULL OR department_id<>$1::uuid)`, deptID, mgrID)
		}
		_, _ = a.db.Exec(ctx, `
			INSERT INTO distribution_quotas (department_id, pool, share, daily_limit)
			VALUES ($1::uuid,'depts',40,200)
			ON CONFLICT (department_id, pool) DO UPDATE SET
			  share=CASE WHEN distribution_quotas.share=0 THEN EXCLUDED.share ELSE distribution_quotas.share END,
			  daily_limit=CASE WHEN distribution_quotas.daily_limit=0 THEN EXCLUDED.daily_limit ELSE distribution_quotas.daily_limit END`,
			deptID)
	}

	var n int
	_ = a.db.QueryRow(ctx, `SELECT count(*) FROM leads WHERE source='demo'`).Scan(&n)
	if n < 6 {
		demos := []struct {
			title, phone, status string
		}{
			{"Демо · Анна Смирнова", "79031110001", "new"},
			{"Демо · Борис Козлов", "79031110002", "new"},
			{"Демо · Виктория Орлова", "79031110003", "in_progress"},
			{"Демо · Дмитрий Волков", "79031110004", "new"},
			{"Демо · Елена Соколова", "79031110005", "in_progress"},
			{"Демо · Игорь Морозов", "79031110006", "qualified"},
		}
		for _, d := range demos {
			var exists int
			_ = a.db.QueryRow(ctx, `SELECT count(*) FROM leads WHERE title=$1 AND source='demo'`, d.title).Scan(&exists)
			if exists > 0 {
				continue
			}
			contactID := a.ensureContactByPhone(ctx, d.phone, d.title)
			_, err := a.db.Exec(ctx, `
			INSERT INTO leads (title, source, status, owner_id, contact_id)
			VALUES ($1,'demo',$2,$3::uuid,nullif($4,'')::uuid)`,
				d.title, d.status, ropID, contactID)
			if err != nil {
				log.Println("demo lead:", err)
			}
		}
		log.Println("demo team/leads ready (ROP ivan.ivanov@forgecrm.local, manager alexey.manager@forgecrm.local)")
	}
	a.clearDemoInboxChats(ctx)
	a.seedDemoInbox(ctx, mgrID)
}

// seedDemoInbox создаёт один полностью синтетический MAX-диалог для проверки
// менеджерского кабинета и DLP. Реальный MAX/WAPPI и рабочие контакты не используются.
func (a *App) seedDemoInbox(ctx context.Context, managerID string) {
	if strings.TrimSpace(managerID) == "" {
		return
	}
	scenarios := []struct {
		phone, title, externalID string
		messages                 []string
	}{
		{"79031110001", "Демо · Анна Смирнова", "demo-max-normal", []string{
			"Здравствуйте! Хочу уточнить детали по вакансии.",
			"Подскажите, пожалуйста, когда можно обсудить условия?",
		}},
		{"79031110002", "Демо · Борис Козлов", "demo-max-documents", []string{
			"Я получил инструкции по оформлению.",
			"Подскажите, какие документы нужно подготовить?",
		}},
		{"79031110003", "Демо · Виктория Орлова", "demo-max-call", []string{
			"Готова обсудить вакансию.",
			"Сегодня после 18:00 буду на связи для звонка.",
		}},
		{"79031110004", "Демо · Дмитрий Волков", "demo-max-safe-followup", []string{
			"Спасибо, я остаюсь в рабочем чате.",
			"Жду дальнейшие инструкции по заявке.",
		}},
	}
	for _, scenario := range scenarios {
		contactID := a.ensureContactByPhone(ctx, scenario.phone, scenario.title)
		if contactID == "" {
			continue
		}
		_, _ = a.db.Exec(ctx, `
			UPDATE leads SET owner_id=$1::uuid, contact_id=$2::uuid
			WHERE source='demo' AND title=$3`, managerID, contactID, scenario.title)

		var threadID string
		err := a.db.QueryRow(ctx, `
			INSERT INTO inbox_threads (channel, external_id, title, phone, contact_id, last_body, last_at, unread)
			VALUES ('max',$1,$2,$3,$4::uuid,$5,now(),1)
			ON CONFLICT (channel, external_id) DO UPDATE SET
				title=EXCLUDED.title, phone=EXCLUDED.phone, contact_id=EXCLUDED.contact_id,
				last_body=EXCLUDED.last_body, last_at=EXCLUDED.last_at, unread=1
			RETURNING id::text`, scenario.externalID, scenario.title, scenario.phone, contactID, scenario.messages[len(scenario.messages)-1]).Scan(&threadID)
		if err != nil || threadID == "" {
			log.Printf("demo inbox thread %s: %v", scenario.externalID, err)
			continue
		}
		_, _ = a.db.Exec(ctx, `DELETE FROM inbox_messages WHERE thread_id=$1::uuid`, threadID)
		for i, body := range scenario.messages {
			_, _ = a.db.Exec(ctx, `
				INSERT INTO inbox_messages (thread_id, direction, body, author_label, external_id)
				VALUES ($1::uuid,'inbound',$2,'Демо-кандидат',$3)`, threadID, body, scenario.externalID+"-in-"+strconv.Itoa(i+1))
		}
	}
}

// clearDemoInboxChats убирает демо-диалоги и пустые треды без переписки.
func (a *App) clearDemoInboxChats(ctx context.Context) {
	_, _ = a.db.Exec(ctx, `
		DELETE FROM inbox_messages
		WHERE body = '__DEMO_PASSPORT__'
		   OR thread_id IN (
			SELECT id FROM inbox_threads
			WHERE external_id LIKE 'demo-%'
			   OR external_id = 'demo-passport'
			   OR title ILIKE 'Демо%'
		)`)
	tag, err := a.db.Exec(ctx, `
		DELETE FROM inbox_threads
		WHERE external_id LIKE 'demo-%'
		   OR external_id = 'demo-passport'
		   OR title ILIKE 'Демо%'
		   OR NOT EXISTS (
			SELECT 1 FROM inbox_messages m
			WHERE m.thread_id = inbox_threads.id
			  AND length(trim(coalesce(m.body,''))) > 0
			  AND m.body <> '__DEMO_PASSPORT__'
		)`)
	if err != nil {
		log.Println("clear empty/demo inbox:", err)
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Println("empty/demo inbox threads removed:", n)
	}
}

func (a *App) ensureDemoUser(ctx context.Context, email, fullName, role, pass string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	var id string
	_ = a.db.QueryRow(ctx, `SELECT id::text FROM users WHERE lower(email)=$1`, email).Scan(&id)
	if id != "" {
		return id
	}
	var roleID string
	if err := a.db.QueryRow(ctx, `SELECT id FROM roles WHERE name=$1`, role).Scan(&roleID); err != nil || roleID == "" {
		return ""
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), 12)
	if err != nil {
		return ""
	}
	err = a.db.QueryRow(ctx, `
		INSERT INTO users (email, login, password_hash, full_name, role_id, can_call, is_active)
		VALUES ($1,$2,$3,$4,$5::uuid,true,true)
		RETURNING id::text`, email, a.resolveStaffLogin(ctx, "", fullName, email), string(hash), fullName, roleID).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

func (a *App) ensureDemoDepartment(ctx context.Context, name, headID string) string {
	var id string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM departments WHERE head_user_id=$1::uuid LIMIT 1`, headID).Scan(&id)
	if id != "" {
		return id
	}
	_ = a.db.QueryRow(ctx, `SELECT id::text FROM departments WHERE name=$1 LIMIT 1`, name).Scan(&id)
	if id != "" {
		_, _ = a.db.Exec(ctx, `UPDATE departments SET head_user_id=$2::uuid WHERE id=$1::uuid`, id, headID)
		return id
	}
	err := a.db.QueryRow(ctx, `
		INSERT INTO departments (name, head_user_id)
		VALUES ($1,$2::uuid) RETURNING id::text`, name, headID).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

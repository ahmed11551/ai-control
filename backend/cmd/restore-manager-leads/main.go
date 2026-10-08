// restore-manager-leads — осторожный возврат лидов менеджеру после отзыва в банк РОПа.
//
// Меняет ТОЛЬКО owner_id у leads (+ deals с тем же lead_id).
// НЕ трогает status, stage, воронку, комментарии, звонки.
//
// Использование на сервере:
//
//	cd backend
//	export DATABASE_URL=...   # как у API
//	go run ./cmd/restore-manager-leads -name 'Курбатов'            # dry-run
//	go run ./cmd/restore-manager-leads -name 'Курбатов' -apply     # применить
//
// Критерий отбора (строгий):
//
//	activity_log.action = 'recalled_to_rop_bank'
//	AND payload.from_manager = <id менеджера>
//	AND текущий owner_id IS DISTINCT FROM менеджера
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	name := flag.String("name", "Курбатов", "фрагмент ФИО менеджера")
	since := flag.String("since", "2026-09-22", "отзывы не раньше этой даты (YYYY-MM-DD)")
	apply := flag.Bool("apply", false, "реально обновить owner_id (без флага — только просмотр)")
	limit := flag.Int("limit", 200, "максимум лидов за один прогон")
	minN := flag.Int("min", 1, "минимум кандидатов для -apply (защита от пустого/частичного)")
	maxN := flag.Int("max", 60, "максимум кандидатов для -apply (защита от слишком широкого отбора)")
	flag.Parse()

	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		log.Fatal("нужен DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatal(err)
	}
	db, err := pgxpool.NewWithConfig(ctx, pool)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var mgrID, mgrName string
	err = db.QueryRow(ctx, `
		SELECT id::text, full_name FROM users
		WHERE full_name ILIKE '%' || $1 || '%'
		  AND is_active IS DISTINCT FROM false
		ORDER BY
		  CASE WHEN full_name ILIKE $1 || '%' THEN 0 ELSE 1 END,
		  full_name
		LIMIT 1`, strings.TrimSpace(*name)).Scan(&mgrID, &mgrName)
	if err != nil {
		log.Fatalf("менеджер не найден по %q: %v", *name, err)
	}
	fmt.Printf("Менеджер: %s (%s)\n", mgrName, mgrID)
	fmt.Printf("Режим: %s\n", map[bool]string{false: "DRY-RUN (ничего не пишем)", true: "APPLY"}[*apply])
	fmt.Printf("Отзывы с: %s\n", *since)
	fmt.Printf("Окно -apply: %d…%d лидов\n\n", *minN, *maxN)

	sinceT, err := time.ParseInLocation("2006-01-02", *since, time.Local)
	if err != nil {
		log.Fatal("bad -since: ", err)
	}

	rows, err := db.Query(ctx, `
		WITH recalls AS (
			SELECT DISTINCT ON (a.entity_id)
				a.entity_id::text AS lead_id,
				a.created_at AS recalled_at,
				coalesce(a.payload->>'from_manager','') AS from_manager
			FROM activity_log a
			WHERE a.entity_type = 'lead'
			  AND a.action = 'recalled_to_rop_bank'
			  AND a.created_at >= $2::timestamptz
			  AND coalesce(a.payload->>'from_manager','') = $1
			ORDER BY a.entity_id, a.created_at DESC
		)
		SELECT r.lead_id,
		       coalesce(l.title,'') AS title,
		       coalesce(l.status,'') AS status,
		       coalesce(l.owner_id::text,'') AS owner_id,
		       coalesce(u.full_name,'(нет)') AS owner_name,
		       r.recalled_at
		FROM recalls r
		JOIN leads l ON l.id = r.lead_id::uuid
		LEFT JOIN users u ON u.id = l.owner_id
		WHERE l.owner_id IS DISTINCT FROM $1::uuid
		ORDER BY r.recalled_at DESC
		LIMIT $3`, mgrID, sinceT, *limit)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	type item struct {
		LeadID, Title, Status, OwnerID, OwnerName string
		RecalledAt                                time.Time
	}
	var list []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.LeadID, &it.Title, &it.Status, &it.OwnerID, &it.OwnerName, &it.RecalledAt); err != nil {
			log.Fatal(err)
		}
		list = append(list, it)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	if len(list) == 0 {
		fmt.Println("Кандидатов нет: либо уже у менеджера, либо нет recalled_to_rop_bank с from_manager.")
		return
	}

	fmt.Printf("Найдено кандидатов: %d\n", len(list))
	fmt.Println(strings.Repeat("-", 72))
	for i, it := range list {
		fmt.Printf("%2d. %s\n    title=%q status=%q\n    сейчас: %s (%s)\n    отзыв: %s\n",
			i+1, it.LeadID, truncate(it.Title, 60), it.Status, it.OwnerName, it.OwnerID,
			it.RecalledAt.Format("02.01.2006 15:04"))
	}
	fmt.Println(strings.Repeat("-", 72))

	if !*apply {
		fmt.Println("\nDRY-RUN завершён. Чтобы вернуть владельца:")
		fmt.Printf("  go run ./cmd/restore-manager-leads -name %q -min %d -max %d -apply\n", *name, *minN, *maxN)
		return
	}

	if len(list) < *minN || len(list) > *maxN {
		log.Fatalf("ABORT: найдено %d, окно %d…%d — без изменений. Подправьте -min/-max после dry-run.",
			len(list), *minN, *maxN)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	restored := 0
	for _, it := range list {
		// owner + status в WHERE: не трогаем, если статус уже сменился с момента dry-run выборки
		tag, err := tx.Exec(ctx, `
			UPDATE leads
			SET owner_id = $2::uuid, updated_at = now()
			WHERE id = $1::uuid
			  AND owner_id IS DISTINCT FROM $2::uuid
			  AND coalesce(status,'') = $3`, it.LeadID, mgrID, it.Status)
		if err != nil {
			log.Fatalf("lead %s: %v", it.LeadID, err)
		}
		if tag.RowsAffected() == 0 {
			fmt.Printf("SKIP %s (owner уже свой или status изменился: %q)\n", it.LeadID, it.Status)
			continue
		}
		_, err = tx.Exec(ctx, `
			UPDATE deals
			SET owner_id = $2::uuid, updated_at = now()
			WHERE lead_id = $1::uuid
			   OR id = (SELECT converted_deal_id FROM leads WHERE id = $1::uuid)`, it.LeadID, mgrID)
		if err != nil {
			log.Fatalf("deals for %s: %v", it.LeadID, err)
		}
		// страховка: status не должен был измениться этим UPDATE
		var statusNow string
		err = tx.QueryRow(ctx, `SELECT coalesce(status,'') FROM leads WHERE id=$1::uuid`, it.LeadID).Scan(&statusNow)
		if err != nil {
			log.Fatalf("verify %s: %v", it.LeadID, err)
		}
		if statusNow != it.Status {
			log.Fatalf("ABORT: у %s status изменился %q → %q, откат всей транзакции", it.LeadID, it.Status, statusNow)
		}
		payload, _ := json.Marshal(map[string]any{
			"from_owner_id":   it.OwnerID,
			"from_owner_name": it.OwnerName,
			"to_owner_id":     mgrID,
			"to_owner_name":   mgrName,
			"reason":          "restore_after_rop_bank_recall",
			"note":            "status/stage not changed",
			"status_kept":     it.Status,
		})
		_, err = tx.Exec(ctx, `
			INSERT INTO activity_log (entity_type, entity_id, actor_id, action, payload)
			VALUES ('lead', $1::uuid, NULL, 'restored_owner', $2::jsonb)`, it.LeadID, string(payload))
		if err != nil {
			log.Fatalf("log %s: %v", it.LeadID, err)
		}
		restored++
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatal("commit: ", err)
	}
	fmt.Printf("\nГотово. Вернули владельца: %d лидов → %s. Статусы/этапы не менялись.\n", restored, mgrName)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

package server

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"
)

type analyticsScope struct {
	Preset string
	From   time.Time
	To     time.Time
	UserID string
	DeptID string
}

func (a *App) resolveAnalyticsScope(r *http.Request) (analyticsScope, string) {
	role := roleName(r)
	if role == "customer" || role == "" {
		return analyticsScope{}, "нет доступа"
	}
	loc := analyticsLocation()
	preset := strings.TrimSpace(r.URL.Query().Get("preset"))
	from, to, err := parseAnalyticsRange(preset, r.URL.Query().Get("from"), r.URL.Query().Get("to"), time.Now(), loc)
	if err != nil {
		return analyticsScope{}, err.Error()
	}
	if preset == "" {
		preset = analyticsPresetMonth
	}
	scope := analyticsScope{Preset: strings.ToLower(preset), From: from, To: to}
	switch {
	case isAdmin(r):
		scope.UserID = strings.TrimSpace(r.URL.Query().Get("manager_id"))
		scope.DeptID = strings.TrimSpace(r.URL.Query().Get("department_id"))
	case isROP(r):
		scope.DeptID = a.actorDepartmentID(r.Context(), actorID(r))
		if scope.DeptID == "" {
			return analyticsScope{}, "у РОПа не указан отдел"
		}
		if asked := strings.TrimSpace(r.URL.Query().Get("department_id")); asked != "" && asked != scope.DeptID {
			return analyticsScope{}, "нет доступа к этому отделу"
		}
		scope.UserID = strings.TrimSpace(r.URL.Query().Get("manager_id"))
	default:
		scope.UserID = actorID(r)
	}
	if scope.UserID != "" && scope.DeptID != "" {
		var dept string
		if err := a.db.QueryRow(r.Context(), `SELECT coalesce(department_id::text,'') FROM users WHERE id=$1::uuid`, scope.UserID).Scan(&dept); err != nil || dept != scope.DeptID {
			return analyticsScope{}, "менеджер не в этом отделе"
		}
	}
	return scope, ""
}

func nullableUUID(id string) any {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return id
}

func (a *App) analytics(w http.ResponseWriter, r *http.Request) {
	scope, msg := a.resolveAnalyticsScope(r)
	if msg != "" {
		code := 400
		if msg == "нет доступа" || strings.HasPrefix(msg, "нет доступа") || msg == "у РОПа не указан отдел" || msg == "менеджер не в этом отделе" {
			code = 403
		}
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	a.ensureSecuritySchema(r.Context())
	ctx := r.Context()
	userArg := nullableUUID(scope.UserID)
	deptArg := nullableUUID(scope.DeptID)
	bucket := sqlLeadBucketExpr()
	base := `
		FROM deals d
		JOIN pipeline_stages s ON s.id=d.stage_id
		JOIN pipelines p ON p.id=d.pipeline_id AND p.bitrix_id='` + hirePipelineKey + `'
		LEFT JOIN leads l ON l.id=d.lead_id
		LEFT JOIN users u ON u.id=d.owner_id
		LEFT JOIN departments dep ON dep.id=u.department_id
		WHERE d.created_at >= $1 AND d.created_at < $2
		  AND ($3::uuid IS NULL OR d.owner_id = $3::uuid)
		  AND ($4::uuid IS NULL OR d.owner_id IN (SELECT id FROM users WHERE department_id=$4::uuid))`
	args := []any{scope.From, scope.To, userArg, deptArg}

	var arrived, inWork, success, rejected, toLogist int
	err := a.db.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE bucket='in_work')::int,
		       count(*) FILTER (WHERE bucket='success')::int,
		       count(*) FILTER (WHERE bucket='rejected')::int,
		       count(*) FILTER (WHERE bucket='to_logist')::int
		FROM (SELECT `+bucket+` AS bucket `+base+`) t`, args...).Scan(&arrived, &inWork, &success, &rejected, &toLogist)
	if err != nil {
		log.Printf("analytics leads: %v", err)
		writeJSON(w, 500, map[string]string{"error": "не удалось собрать аналитику"})
		return
	}

	byManager := a.analyticsGroups(ctx, `
		SELECT coalesce(d.owner_id::text,''), coalesce(nullif(u.full_name,''),'без менеджера'),
		       count(*)::int,
		       count(*) FILTER (WHERE `+bucket+`='in_work')::int,
		       count(*) FILTER (WHERE `+bucket+`='success')::int,
		       count(*) FILTER (WHERE `+bucket+`='rejected')::int,
		       count(*) FILTER (WHERE `+bucket+`='to_logist')::int
		`+base+`
		GROUP BY 1, 2
		ORDER BY 3 DESC, 2
		LIMIT 50`, args)
	byDept := a.analyticsGroups(ctx, `
		SELECT coalesce(dep.id::text,''), coalesce(nullif(dep.name,''),'без отдела'),
		       count(*)::int,
		       count(*) FILTER (WHERE `+bucket+`='in_work')::int,
		       count(*) FILTER (WHERE `+bucket+`='success')::int,
		       count(*) FILTER (WHERE `+bucket+`='rejected')::int,
		       count(*) FILTER (WHERE `+bucket+`='to_logist')::int
		`+base+`
		GROUP BY 1, 2
		ORDER BY 3 DESC, 2`, args)
	bySource := a.analyticsGroups(ctx, `
		SELECT coalesce(nullif(l.source,''),'manual'), coalesce(nullif(l.source,''),'manual'),
		       count(*)::int,
		       count(*) FILTER (WHERE `+bucket+`='in_work')::int,
		       count(*) FILTER (WHERE `+bucket+`='success')::int,
		       count(*) FILTER (WHERE `+bucket+`='rejected')::int,
		       count(*) FILTER (WHERE `+bucket+`='to_logist')::int
		`+base+`
		GROUP BY 1, 2
		ORDER BY 3 DESC
		LIMIT 30`, args)

	var callsN, callsDur, callsAvg, callsConn, callsFlagged int
	err = a.db.QueryRow(ctx, `
		SELECT count(*)::int,
		       coalesce(sum(c.duration_sec),0)::int,
		       coalesce(avg(c.duration_sec),0)::int,
		       count(*) FILTER (
		         WHERE c.duration_sec > 0
		           AND lower(replace(replace(coalesce(c.disposition,''), ' ', '_'), '-', '_'))
		               NOT IN ('missed','no_answer','noanswer','busy','canceled','cancelled','rejected','failed','no_reach')
		       )::int,
		       count(*) FILTER (WHERE cr.status='flagged')::int
		FROM calls c
		LEFT JOIN call_reviews cr ON cr.call_id=c.id
		WHERE c.started_at >= $1 AND c.started_at < $2
		  AND ($3::uuid IS NULL OR c.user_id = $3::uuid)
		  AND ($4::uuid IS NULL OR c.user_id IN (SELECT id FROM users WHERE department_id=$4::uuid))`, args...).Scan(
		&callsN, &callsDur, &callsAvg, &callsConn, &callsFlagged)
	if err != nil {
		log.Printf("analytics calls: %v", err)
		writeJSON(w, 500, map[string]string{"error": "не удалось собрать аналитику"})
		return
	}
	dispositions := a.queryMaps(ctx, `
		SELECT coalesce(nullif(c.disposition,''),'не указан') AS name, count(*)::int AS count
		FROM calls c
		WHERE c.started_at >= $1 AND c.started_at < $2
		  AND ($3::uuid IS NULL OR c.user_id = $3::uuid)
		  AND ($4::uuid IS NULL OR c.user_id IN (SELECT id FROM users WHERE department_id=$4::uuid))
		GROUP BY 1
		ORDER BY 2 DESC
		LIMIT 20`, args...)
	if dispositions == nil {
		dispositions = []map[string]any{}
	}

	funnelRows, err := a.db.Query(ctx, `
		SELECT coalesce(s.bitrix_id,''), s.name, count(d.id)::int
		FROM pipeline_stages s
		JOIN pipelines p ON p.id=s.pipeline_id AND p.bitrix_id='`+hirePipelineKey+`'
		LEFT JOIN deals d ON d.stage_id=s.id
		  AND d.created_at >= $1 AND d.created_at < $2
		  AND ($3::uuid IS NULL OR d.owner_id = $3::uuid)
		  AND ($4::uuid IS NULL OR d.owner_id IN (SELECT id FROM users WHERE department_id=$4::uuid))
		GROUP BY s.id
		ORDER BY s.sort_order`, args...)
	funnel := []map[string]any{}
	if err == nil {
		defer funnelRows.Close()
		for funnelRows.Next() {
			var key, name string
			var n int
			if err := funnelRows.Scan(&key, &name, &n); err != nil {
				continue
			}
			funnel = append(funnel, map[string]any{
				"key": key, "name": name, "count": n, "share": shareOf(n, arrived),
			})
		}
	}

	var incTotal, incOpen int
	_ = a.db.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE e.status='open')::int
		FROM security_events e
		WHERE e.created_at >= $1 AND e.created_at < $2
		  AND e.status IN ('open','confirmed')
		  AND ($3::uuid IS NULL OR e.actor_id = $3::uuid)
		  AND ($4::uuid IS NULL OR e.actor_id IN (SELECT id FROM users WHERE department_id=$4::uuid))`, args...).Scan(&incTotal, &incOpen)
	incByType := a.queryMaps(ctx, `
		SELECT e.event_type AS name, count(*)::int AS count
		FROM security_events e
		WHERE e.created_at >= $1 AND e.created_at < $2
		  AND e.status IN ('open','confirmed')
		  AND ($3::uuid IS NULL OR e.actor_id = $3::uuid)
		  AND ($4::uuid IS NULL OR e.actor_id IN (SELECT id FROM users WHERE department_id=$4::uuid))
		GROUP BY 1
		ORDER BY 2 DESC`, args...)
	if incByType == nil {
		incByType = []map[string]any{}
	}

	writeJSON(w, 200, map[string]any{
		"period": map[string]any{
			"preset": scope.Preset,
			"from":   scope.From.Format(time.RFC3339),
			"to":     scope.To.Format(time.RFC3339),
		},
		"leads": map[string]any{
			"arrived": arrived, "in_work": inWork, "success": success,
			"rejected": rejected, "to_logist": toLogist,
		},
		"by_manager":    byManager,
		"by_department": byDept,
		"by_source":     bySource,
		"calls": map[string]any{
			"count": callsN, "total_duration_sec": callsDur, "avg_duration_sec": callsAvg,
			"connected": callsConn, "connect_rate": shareOf(callsConn, callsN),
			"flagged": callsFlagged, "by_disposition": dispositions,
		},
		"funnel": funnel,
		"incidents": map[string]any{
			"total": incTotal, "open": incOpen, "by_type": incByType,
		},
	})
}

func (a *App) analyticsMeta(w http.ResponseWriter, r *http.Request) {
	if roleName(r) == "customer" || roleName(r) == "" {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	ctx := r.Context()
	deptSQL := `SELECT id::text, name FROM departments ORDER BY name`
	mgrSQL := `
		SELECT u.id::text, u.full_name, coalesce(u.department_id::text,'') AS department_id
		FROM users u
		JOIN roles r ON r.id=u.role_id
		WHERE u.is_active AND r.name IN ('manager','sales')
		ORDER BY u.full_name`
	args := []any{}
	if isROP(r) && !isAdmin(r) {
		dept := a.actorDepartmentID(ctx, actorID(r))
		if dept == "" {
			writeJSON(w, 200, map[string]any{"departments": []any{}, "managers": []any{}, "locked_department_id": ""})
			return
		}
		deptSQL = `SELECT id::text, name FROM departments WHERE id=$1::uuid ORDER BY name`
		mgrSQL = `
			SELECT u.id::text, u.full_name, coalesce(u.department_id::text,'') AS department_id
			FROM users u
			JOIN roles r ON r.id=u.role_id
			WHERE u.is_active AND r.name IN ('manager','sales') AND u.department_id=$1::uuid
			ORDER BY u.full_name`
		args = append(args, dept)
		writeJSON(w, 200, map[string]any{
			"departments":          a.queryMaps(ctx, deptSQL, args...),
			"managers":             a.queryMaps(ctx, mgrSQL, args...),
			"locked_department_id": dept,
		})
		return
	}
	if !isAdmin(r) && !isROP(r) {
		writeJSON(w, 200, map[string]any{
			"departments": []any{}, "managers": []any{}, "locked_user_id": actorID(r),
		})
		return
	}
	depts := a.queryMaps(ctx, deptSQL)
	mgrs := a.queryMaps(ctx, mgrSQL)
	if depts == nil {
		depts = []map[string]any{}
	}
	if mgrs == nil {
		mgrs = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"departments": depts, "managers": mgrs})
}

func (a *App) analyticsGroups(ctx context.Context, sql string, args []any) []map[string]any {
	rows, err := a.db.Query(ctx, sql, args...)
	if err != nil {
		log.Printf("analytics group: %v", err)
		return []map[string]any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name string
		var arrived, inWork, success, rejected, toLogist int
		if err := rows.Scan(&id, &name, &arrived, &inWork, &success, &rejected, &toLogist); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "name": name,
			"arrived": arrived, "in_work": inWork, "success": success,
			"rejected": rejected, "to_logist": toLogist,
		})
	}
	return out
}

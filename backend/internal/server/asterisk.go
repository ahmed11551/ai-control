package server

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type AMI struct {
	host, port, user, secret string
	app                      *App
	mu                       sync.Mutex
	live                     map[string]map[string]string
}

func NewAMI(host, port, user, secret string, app *App) *AMI {
	return &AMI{host: host, port: port, user: user, secret: secret, app: app, live: map[string]map[string]string{}}
}

func (a *AMI) Snapshot() []map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []map[string]string{}
	for _, v := range a.live {
		cp := map[string]string{}
		for k, val := range v {
			cp[k] = val
		}
		out = append(out, cp)
	}
	return out
}

func (a *AMI) Loop() {
	if a == nil {
		return
	}
	for {
		if err := a.connect(); err != nil {
			log.Println("AMI reconnect in 5s:", err)
			time.Sleep(5 * time.Second)
		}
	}
}

func (a *AMI) connect() error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(a.host, a.port), 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	fmt.Fprintf(conn, "Action: Login\r\nUsername: %s\r\nSecret: %s\r\nEvents: on\r\n\r\n", a.user, a.secret)
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	ev := map[string]string{}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			if len(ev) > 0 {
				a.handle(ev)
			}
			ev = map[string]string{}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if ok {
			ev[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return sc.Err()
}

func (a *AMI) handle(ev map[string]string) {
	et := ev["Event"]
	switch et {
	case "UserEvent":
		if ev["UserEvent"] == "CrmCallStart" {
			a.app.onCallStart(ev)
			a.mu.Lock()
			a.live[ev["Uniqueid"]] = ev
			a.mu.Unlock()
			if a.app.rdb != nil {
				_ = a.app.rdb.Incr(context.Background(), "live_calls")
			}
			a.app.hub.Broadcast("call.started", redactAMIEventForWS(ev))
		}
		if ev["UserEvent"] == "CrmCallEnd" {
			a.app.onCallEnd(ev)
			a.mu.Lock()
			delete(a.live, ev["Uniqueid"])
			a.mu.Unlock()
			if a.app.rdb != nil {
				_ = a.app.rdb.Decr(context.Background(), "live_calls")
			}
			a.app.hub.Broadcast("call.ended", redactAMIEventForWS(ev))
		}
	case "Newchannel":
		a.mu.Lock()
		a.live[ev["Uniqueid"]] = ev
		a.mu.Unlock()
	case "Hangup":
		a.mu.Lock()
		delete(a.live, ev["Uniqueid"])
		a.mu.Unlock()
	}
}

func (a *AMI) Originate(channel, exten, callerID string, vars map[string]string) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(a.host, a.port), 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	var b strings.Builder
	fmt.Fprintf(&b, "Action: Login\r\nUsername: %s\r\nSecret: %s\r\nEvents: off\r\n\r\n", a.user, a.secret)
	fmt.Fprintf(&b, "Action: Originate\r\nChannel: %s\r\nContext: from-crm\r\nExten: %s\r\nPriority: 1\r\nCallerID: %s\r\nAsync: true\r\nTimeout: 30000\r\n",
		channel, exten, callerID)
	for k, v := range vars {
		fmt.Fprintf(&b, "Variable: %s=%s\r\n", k, v)
	}
	b.WriteString("\r\nAction: Logoff\r\n\r\n")
	_, err = conn.Write([]byte(b.String()))
	return err
}

func (a *App) onCallStart(ev map[string]string) {
	ctx := context.Background()
	from, to := ev["From"], ev["To"]
	dir := ev["Direction"]
	uid := ev["Uniqueid"]
	entityType, entityID := a.matchPhone(ctx, from, to)
	userID := a.userByExt(ctx, from)
	if ev["UserId"] != "" {
		userID = ev["UserId"]
	}
	_, _ = a.db.Exec(ctx, `
		INSERT INTO calls (asterisk_uniqueid, direction, from_number, to_number, user_id, entity_type, entity_id, started_at)
		VALUES ($1,$2,$3,$4,nullif($5,'')::uuid,nullif($6,''),nullif($7,'')::uuid, now())`,
		uid, nz(dir, "inbound"), from, to, userID, entityType, entityID)
	if entityType != "" && entityID != "" {
		a.logAct(ctx, entityType, entityID, userID, "call_started", ev)
	}
}

func (a *App) onCallEnd(ev map[string]string) {
	ctx := context.Background()
	dur := 0
	fmt.Sscanf(ev["Duration"], "%d", &dur)
	_, _ = a.db.Exec(ctx, `UPDATE calls SET duration_sec=$1, disposition=$2, ended_at=now(), recording_key=$3 WHERE asterisk_uniqueid=$4`,
		dur, ev["Disposition"], ev["Recording"], ev["Uniqueid"])
}

func (a *App) matchPhone(ctx context.Context, from, to string) (string, string) {
	return a.matchPhoneForUser(ctx, from, to, "")
}

// matchPhoneForUser — карточка по номеру. Если у номера несколько лидов/сделок (дубли у разных
// менеджеров) — приоритет карточке, чей владелец = userID (кто говорит по телефону), затем самая свежая.
func (a *App) matchPhoneForUser(ctx context.Context, from, to, userID string) (string, string) {
	userID = strings.TrimSpace(userID)
	for _, n := range []string{normalizePhone(from), normalizePhone(to)} {
		if n == "" {
			continue
		}
		var deal string
		if err := a.db.QueryRow(ctx, `
			SELECT d.id::text
			FROM deals d
			JOIN contacts c ON c.id=d.contact_id
			WHERE regexp_replace(coalesce(c.phone,''),'[^0-9]','','g') LIKE '%'||$1
			  AND coalesce(d.visual_only,false)=false
			  AND `+sqlDealNotOldLeads("d")+`
			ORDER BY (d.owner_id::text = nullif($2,'')) DESC, d.created_at DESC
			LIMIT 1`, n, userID).Scan(&deal); err == nil && deal != "" {
			return "deal", deal
		}
		var lead string
		if err := a.db.QueryRow(ctx, `
			SELECT l.id::text
			FROM leads l
			JOIN contacts c ON c.id=l.contact_id
			WHERE regexp_replace(coalesce(c.phone,''),'[^0-9]','','g') LIKE '%'||$1
			  AND NOT EXISTS (
			    SELECT 1 FROM deals d
			    WHERE coalesce(d.visual_only,false)
			      AND (d.lead_id=l.id OR d.id=l.converted_deal_id)
			  )
			  AND `+sqlLeadNotOldLeads("l")+`
			ORDER BY (l.owner_id::text = nullif($2,'')) DESC, l.created_at DESC
			LIMIT 1`, n, userID).Scan(&lead); err == nil && lead != "" {
			return "lead", lead
		}
		var id string
		if err := a.db.QueryRow(ctx, `
			SELECT c.id::text FROM contacts c
			WHERE regexp_replace(coalesce(c.phone,''),'[^0-9]','','g') LIKE '%'||$1
			  AND NOT EXISTS (
			    SELECT 1 FROM deals od
			    LEFT JOIN pipelines op ON op.id=od.pipeline_id
			    WHERE od.contact_id=c.id
			      AND (coalesce(op.bitrix_id,'')='`+oldLeadsPipelineKey+`' OR coalesce(od.sleeping,false))
			  )
			ORDER BY c.created_at DESC LIMIT 1`, n).Scan(&id); err == nil && id != "" {
			return "contact", id
		}
	}
	return "", ""
}

func (a *App) userByExt(ctx context.Context, num string) string {
	var id string
	_ = a.db.QueryRow(ctx, `SELECT id::text FROM users WHERE phone_extension=$1`, num).Scan(&id)
	return id
}

func normalizePhone(s string) string {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	if len(d) == 11 && (d[0] == '8' || d[0] == '7') {
		return d[1:]
	}
	return d
}

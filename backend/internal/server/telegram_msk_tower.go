package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const mskTowerLeadSource = "telegram_tower"

var (
	errMskTowerDeptMissing   = errors.New("отдел МСК Башня не найден")
	errMskTowerDeptAmbiguous = errors.New("несколько отделов МСК Башня")
	errMskTowerNoROP         = errors.New("у отдела МСК Башня не назначен РОП")
	errMskTowerDuplicate     = errors.New("номер уже есть на живой карточке")
)

type mskTowerIntake struct {
	Phone string
	Name  string
	Age   string
	City  string
	Extra string
}

type towerDept struct {
	ID   string
	Name string
	Head string
}

func (a *App) mskTowerBotReady() bool {
	tok := strings.TrimSpace(a.cfg.TelegramMskTowerBotToken)
	if tok == "" {
		return false
	}
	return tok != strings.TrimSpace(a.cfg.TelegramBotToken)
}

func (a *App) startMskTowerTelegramBot() {
	tok := strings.TrimSpace(a.cfg.TelegramMskTowerBotToken)
	if tok == "" {
		log.Println("Telegram МСК Башня: TELEGRAM_MSK_TOWER_BOT_TOKEN не задан — бот заявок выкл")
		return
	}
	if tok == strings.TrimSpace(a.cfg.TelegramBotToken) {
		log.Println("Telegram МСК Башня: токен совпал с ботом «Билет» — второй опрос не запускаю")
		return
	}
	log.Println("Telegram МСК Башня: long poll запущен")
	go a.mskTowerPollLoop()
}

func (a *App) mskTowerPollLoop() {
	var offset int64
	client := &http.Client{Timeout: 60 * time.Second}
	token := strings.TrimSpace(a.cfg.TelegramMskTowerBotToken)
	for {
		u := fmt.Sprintf("%s/bot%s/getUpdates?timeout=50&offset=%d", telegramAPI, token, offset)
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		res, err := client.Do(req)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		_ = res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			time.Sleep(5 * time.Second)
			continue
		}
		var parsed struct {
			OK     bool `json:"ok"`
			Result []struct {
				UpdateID int64 `json:"update_id"`
				Message  *struct {
					Text string `json:"text"`
					Chat struct {
						ID int64 `json:"id"`
					} `json:"chat"`
					From *struct {
						ID int64 `json:"id"`
					} `json:"from"`
				} `json:"message"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil || !parsed.OK {
			time.Sleep(3 * time.Second)
			continue
		}
		for _, upd := range parsed.Result {
			if upd.UpdateID+1 > offset {
				offset = upd.UpdateID + 1
			}
			if upd.Message == nil {
				continue
			}
			var from int64
			if upd.Message.From != nil {
				from = upd.Message.From.ID
			}
			a.handleMskTowerTelegram(upd.Message.Chat.ID, from, upd.Message.Text)
		}
	}
}

func (a *App) handleMskTowerTelegram(chatID, userID int64, text string) {
	token := a.cfg.TelegramMskTowerBotToken
	if !telegramUserAllowed(a.cfg.TelegramMskTowerOperatorIDs, userID) {
		_ = a.telegramSendToken(token, chatID,
			"Нет доступа.\nВаш id: "+strconv.FormatInt(userID, 10)+"\nЕго нужно добавить в список операторов.")
		return
	}
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	if text == "" || strings.HasPrefix(lower, "/start") || strings.HasPrefix(lower, "/help") {
		_ = a.telegramSendToken(token, chatID, mskTowerIntakeHelp())
		return
	}
	in, err := parseMskTowerIntake(text)
	if err != nil {
		_ = a.telegramSendToken(token, chatID, escapeTelegramHTML(err.Error())+"\n\n"+mskTowerIntakeHelp())
		return
	}
	if _, err := a.createMskTowerTelegramLead(context.Background(), in); err != nil {
		_ = a.telegramSendToken(token, chatID, escapeTelegramHTML(mskTowerIntakeFailText(err)))
		return
	}
	_ = a.telegramSendToken(token, chatID,
		"Карточка создана и ушла РОПу МСК Башня.\nИмя: "+escapeTelegramHTML(in.Name))
}

func mskTowerIntakeHelp() string {
	return "Пришлите одним сообщением:\n\n" +
		"Номер: +7...\n" +
		"Имя: ...\n" +
		"Возраст: ...\n" +
		"Город: ...\n" +
		"Доп инф: ...\n\n" +
		"Номер и имя обязательны. Возраст, город и доп. информация — по желанию."
}

func mskTowerIntakeFailText(err error) string {
	switch {
	case errors.Is(err, errMskTowerDuplicate):
		return "Этот номер уже есть в базе. Новую карточку не создал."
	case errors.Is(err, errMskTowerDeptMissing), errors.Is(err, errMskTowerNoROP):
		return "Не нашёл отдел МСК Башня с РОПом. Карточку не создал."
	case errors.Is(err, errMskTowerDeptAmbiguous):
		return "Нашлось несколько отделов МСК Башня. Карточку не создал."
	default:
		return "Не удалось создать карточку."
	}
}

func telegramUserAllowed(list string, userID int64) bool {
	if userID == 0 {
		return false
	}
	want := strconv.FormatInt(userID, 10)
	for _, part := range strings.Split(list, ",") {
		if strings.TrimSpace(part) == want {
			return true
		}
	}
	return false
}

func foldIntakeKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "ё", "е")
	s = strings.ReplaceAll(s, ".", " ")
	return strings.Join(strings.Fields(s), " ")
}

func parseMskTowerIntake(text string) (mskTowerIntake, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var in mskTowerIntake
	var loose []string
	saw := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := splitIntakeField(line)
		if !ok {
			if saw {
				loose = append(loose, line)
			}
			continue
		}
		saw = true
		val = strings.TrimSpace(val)
		switch foldIntakeKey(key) {
		case "номер", "телефон", "phone":
			in.Phone = val
		case "имя", "name":
			in.Name = val
		case "возраст", "age":
			in.Age = val
		case "город", "city":
			in.City = val
		case "доп инф", "комментарий", "коммент", "comment":
			if in.Extra != "" {
				in.Extra += "\n"
			}
			if foldIntakeKey(key) == "доп инф" && val != "" {
				in.Extra += "Доп инф: " + val
			} else {
				in.Extra += val
			}
		default:
			loose = append(loose, line)
		}
	}
	if len(loose) > 0 {
		if in.Extra != "" {
			in.Extra += "\n"
		}
		in.Extra += strings.Join(loose, "\n")
	}
	phone, ok := canonRuPhone(in.Phone)
	if !ok {
		return mskTowerIntake{}, errors.New("Не разобрал номер. Нужен мобильный, например +7…")
	}
	in.Phone = phone
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return mskTowerIntake{}, errors.New("Не разобрал имя.")
	}
	in.City = strings.TrimSpace(in.City)
	in.Age = strings.TrimSpace(in.Age)
	in.Extra = strings.TrimSpace(in.Extra)
	return in, nil
}

func splitIntakeField(line string) (key, val string, ok bool) {
	i := strings.IndexAny(line, ":：")
	if i <= 0 {
		return "", "", false
	}
	_, size := utf8.DecodeRuneInString(line[i:])
	if size <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+size:]), true
}

func canonRuPhone(raw string) (string, bool) {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)
	switch {
	case len(d) == 11 && d[0] == '8':
		d = "7" + d[1:]
	case len(d) == 10:
		d = "7" + d
	}
	if len(d) != 11 || d[0] != '7' {
		return "", false
	}
	return d, true
}

func mskTowerCardNote(age, extra string) string {
	var lines []string
	if strings.TrimSpace(age) != "" {
		lines = append(lines, "Возраст: "+strings.TrimSpace(age))
	}
	if strings.TrimSpace(extra) != "" {
		lines = append(lines, strings.TrimSpace(extra))
	}
	return strings.Join(lines, "\n")
}

func pickMskTowerDepartment(deps []towerDept) (towerDept, error) {
	var hits []towerDept
	for _, d := range deps {
		if isMskTowerDepartmentName(d.Name) {
			hits = append(hits, d)
		}
	}
	if len(hits) == 0 {
		return towerDept{}, errMskTowerDeptMissing
	}
	var withHead []towerDept
	for _, d := range hits {
		head := strings.TrimSpace(d.Head)
		if head != "" && head != "<nil>" {
			withHead = append(withHead, d)
		}
	}
	if len(withHead) == 0 {
		return towerDept{}, errMskTowerNoROP
	}
	if len(withHead) > 1 {
		return towerDept{}, errMskTowerDeptAmbiguous
	}
	return withHead[0], nil
}

func (a *App) loadTowerDepartments(ctx context.Context) []towerDept {
	rows := a.queryMaps(ctx, `
		SELECT d.id::text AS id, coalesce(d.name,'') AS name,
		       coalesce(d.head_user_id::text,'') AS head
		FROM departments d`)
	out := make([]towerDept, 0, len(rows))
	for _, row := range rows {
		out = append(out, towerDept{
			ID:   strings.TrimSpace(fmt.Sprint(row["id"])),
			Name: strings.TrimSpace(fmt.Sprint(row["name"])),
			Head: strings.TrimSpace(fmt.Sprint(row["head"])),
		})
	}
	return out
}

func (a *App) ownerInDepartment(ctx context.Context, ownerID, deptID string) bool {
	ownerID = strings.TrimSpace(ownerID)
	deptID = strings.TrimSpace(deptID)
	if ownerID == "" || deptID == "" || ownerID == "<nil>" || a.db == nil {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1
		FROM users u
		WHERE u.id=$1::uuid
		  AND (
		    u.department_id=$2::uuid
		    OR EXISTS (
		      SELECT 1 FROM departments d
		      WHERE d.id=$2::uuid AND d.head_user_id=u.id
		    )
		  )`, ownerID, deptID).Scan(&n)
	return err == nil && n == 1
}

func (a *App) createMskTowerTelegramLead(ctx context.Context, in mskTowerIntake) (string, error) {
	if a == nil || a.db == nil {
		return "", errMskTowerDeptMissing
	}
	dept, err := pickMskTowerDepartment(a.loadTowerDepartments(ctx))
	if err != nil {
		return "", err
	}
	if a.activeLeadByPhone(ctx, in.Phone) != "" {
		return "", errMskTowerDuplicate
	}
	id, err := a.insertLead(ctx, map[string]any{
		"title":     in.Name,
		"full_name": in.Name,
		"phone":     in.Phone,
		"city":      in.City,
		"card_note": mskTowerCardNote(in.Age, in.Extra),
		"source":    mskTowerLeadSource,
		"status":    "new",
	}, dept.Head)
	if err != nil || strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("не удалось создать карточку")
	}
	var owner string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM leads WHERE id=$1::uuid`, id).Scan(&owner)
	if !a.ownerInDepartment(ctx, owner, dept.ID) {
		return "", errMskTowerDuplicate
	}
	log.Printf("telegram msk tower lead=%s dept=%s", id, dept.Name)
	return id, nil
}

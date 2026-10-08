package bitrix

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (im *Importer) importFields(ctx context.Context) error {
	if err := im.importFieldDefs(ctx, "deal", WorkingDealFields()); err != nil {
		return err
	}
	if err := im.importFieldDefs(ctx, "lead", WorkingLeadFields()); err != nil {
		return err
	}
	if err := im.importFieldDefs(ctx, "contact", WorkingContactFields()); err != nil {
		return err
	}
	return im.importDealFieldValues(ctx)
}

func (im *Importer) importFieldDefs(ctx context.Context, entity string, catalog []FieldSpec) error {
	remote, _ := im.loadRemoteFields(ctx, entity)
	n := 0
	for _, spec := range catalog {
		label, typ, multiple, opts := spec.Label, "string", false, []map[string]string(nil)
		if row, ok := remote[spec.Code]; ok {
			if s := userfieldLabel(row); s != "" {
				label = s
			}
			if t := rowString(row, "USER_TYPE_ID", "userTypeId", "type"); t != "" {
				typ = t
			}
			multiple = isMultiple(row["MULTIPLE"]) || isMultiple(row["isMultiple"])
			opts = userfieldOptions(row)
			if items, ok := row["items"].([]any); ok && len(opts) == 0 {
				opts = userfieldOptions(map[string]any{"LIST": items})
			}
		}
		rawOpts, _ := json.Marshal(opts)
		if rawOpts == nil {
			rawOpts = []byte("[]")
		}
		_, err := im.db.Exec(ctx, `
			INSERT INTO user_fields (entity_type, code, label, field_type, field_group, multiple, active, sort, options, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,true,$7,$8::jsonb,now())
			ON CONFLICT (entity_type, code) DO UPDATE SET
				label=EXCLUDED.label, field_type=EXCLUDED.field_type, field_group=EXCLUDED.field_group,
				multiple=EXCLUDED.multiple, active=true, sort=EXCLUDED.sort, options=EXCLUDED.options, updated_at=now()`,
			entity, spec.Code, label, typ, spec.Group, multiple, spec.Sort, string(rawOpts),
		)
		if err != nil {
			log.Println("user field", entity, spec.Code, err)
			continue
		}
		n++
	}
	im.setCount("fields", n, len(catalog), nil)
	return nil
}

func (im *Importer) loadRemoteFields(ctx context.Context, entity string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	method := map[string]string{
		"deal":    "crm.deal.fields",
		"lead":    "crm.lead.fields",
		"contact": "crm.contact.fields",
	}[entity]
	if method == "" {
		return out, nil
	}
	raw, err := im.client.Call(ctx, method, nil)
	if err != nil {
		rows, listErr := im.client.List(ctx, "crm.userfield.list", map[string]any{
			"filter": map[string]any{"ENTITY_ID": map[string]string{"deal": "CRM_DEAL", "lead": "CRM_LEAD", "contact": "CRM_CONTACT"}[entity]},
		})
		if listErr != nil {
			return out, err
		}
		for _, row := range rows {
			code := rowString(row, "FIELD_NAME", "fieldName")
			if code != "" {
				out[code] = row
			}
		}
		return out, nil
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return out, nil
	}
	for code, v := range obj {
		if !strings.HasPrefix(code, "UF_") {
			continue
		}
		if row, ok := v.(map[string]any); ok {
			out[code] = row
		}
	}
	return out, nil
}

type fieldRef struct {
	id   string
	code string
}

func (im *Importer) importDealFieldValues(ctx context.Context) error {
	var fields []fieldRef
	rows, err := im.db.Query(ctx, `SELECT id::text, code FROM user_fields WHERE entity_type='deal' AND active`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var f fieldRef
		if rows.Scan(&f.id, &f.code) == nil {
			fields = append(fields, f)
		}
	}
	rows.Close()
	if len(fields) == 0 {
		return fmt.Errorf("нет рабочих полей сделки: сначала справочник")
	}

	remote, _ := im.loadRemoteFields(ctx, "deal")
	selects := []string{"ID"}
	byCode := map[string]fieldRef{}
	for _, f := range fields {
		if len(remote) > 0 {
			if _, ok := remote[f.code]; !ok {
				continue
			}
		}
		selects = append(selects, f.code)
		byCode[f.code] = f
	}
	if len(byCode) == 0 {
		for _, f := range fields {
			selects = append(selects, f.code)
			byCode[f.code] = f
		}
	}

	var localTotal int
	_ = im.db.QueryRow(ctx, `SELECT count(*) FROM deals WHERE bitrix_id IS NOT NULL`).Scan(&localTotal)
	var already int
	_ = im.db.QueryRow(ctx, `SELECT count(DISTINCT entity_id) FROM entity_field_values WHERE entity_type='deal'`).Scan(&already)
	var afterID int64
	_ = im.db.QueryRow(ctx, `
		SELECT coalesce(max(nullif(d.bitrix_id,'')::bigint), 0)
		FROM entity_field_values v
		JOIN deals d ON d.id = v.entity_id
		WHERE v.entity_type='deal'`).Scan(&afterID)

	im.setCount("fields", already, localTotal, nil)
	im.emit("fields")

	params := map[string]any{
		"select": selects,
		"order":  map[string]any{"ID": "ASC"},
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, total, err := im.client.ListAfter(ctx, "crm.deal.list", params, afterID)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		if err := im.upsertDealFieldPage(ctx, page, byCode); err != nil {
			return err
		}
		for _, row := range page {
			if id, err := strconv.ParseInt(rowString(row, "ID", "id"), 10, 64); err == nil && id > afterID {
				afterID = id
			}
		}
		already += len(page)
		if total > 0 {
			im.setCount("fields", already, total, nil)
		} else {
			im.setCount("fields", already, localTotal, nil)
		}
		im.emit("fields")
		_, _ = im.db.Exec(ctx, `
			INSERT INTO bitrix_sync_state (entity, last_id, last_sync_at)
			VALUES ('deal_fields', $1, now())
			ON CONFLICT (entity) DO UPDATE SET last_id=EXCLUDED.last_id, last_sync_at=now()`, afterID)
	}
	return nil
}

func (im *Importer) upsertDealFieldPage(ctx context.Context, page []map[string]any, byCode map[string]fieldRef) error {
	ids := make([]string, 0, len(page))
	for _, row := range page {
		if id := rowString(row, "ID", "id"); id != "" {
			ids = append(ids, id)
		}
	}
	local := map[string]string{}
	if len(ids) > 0 {
		qrows, err := im.db.Query(ctx, `SELECT bitrix_id, id::text FROM deals WHERE bitrix_id = ANY($1)`, ids)
		if err != nil {
			return err
		}
		for qrows.Next() {
			var bid, id string
			if qrows.Scan(&bid, &id) == nil {
				local[bid] = id
			}
		}
		qrows.Close()
	}

	batch := &pgx.Batch{}
	queued := 0
	for _, row := range page {
		bid := rowString(row, "ID", "id")
		entityID, ok := local[bid]
		if !ok {
			continue
		}
		for code, f := range byCode {
			raw, ok := row[code]
			if !ok {
				continue
			}
			text, js, empty := encodeFieldValue(raw)
			if empty {
				continue
			}
			batch.Queue(`
				INSERT INTO entity_field_values (entity_type, entity_id, field_id, value_text, value_json, updated_at)
				VALUES ('deal', $1::uuid, $2::uuid, $3, $4::jsonb, now())
				ON CONFLICT (entity_type, entity_id, field_id) DO UPDATE SET
					value_text=EXCLUDED.value_text, value_json=EXCLUDED.value_json, updated_at=now()`,
				entityID, f.id, text, js,
			)
			queued++
		}
	}
	if queued == 0 {
		return nil
	}
	br := im.db.SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < queued; i++ {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func userfieldLabel(row map[string]any) string {
	for _, key := range []string{"EDIT_FORM_LABEL", "LIST_COLUMN_LABEL", "editFormLabel"} {
		if s := localized(row[key]); s != "" {
			return s
		}
	}
	return rowString(row, "FIELD_NAME")
}

func localized(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]any:
		for _, k := range []string{"ru", "ru_RU", "RU", "en", "en_US"} {
			if s := asString(t[k]); s != "" {
				return s
			}
		}
	}
	return ""
}

func userfieldOptions(row map[string]any) []map[string]string {
	var out []map[string]string
	switch t := row["LIST"].(type) {
	case []any:
		for _, item := range t {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id := asString(m["ID"])
			val := asString(m["VALUE"])
			if id == "" && val == "" {
				continue
			}
			out = append(out, map[string]string{"id": id, "value": val})
		}
	}
	return out
}

func isMultiple(v any) bool {
	s := strings.ToUpper(asString(v))
	return s == "Y" || s == "1" || s == "TRUE"
}

func encodeFieldValue(v any) (text string, js string, empty bool) {
	if v == nil {
		return "", "null", true
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return "", "null", true
		}
		b, _ := json.Marshal(s)
		return s, string(b), false
	case []any:
		if len(t) == 0 {
			return "", "null", true
		}
		var parts []string
		for _, item := range t {
			if s := firstScalar(item); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return "", "null", true
		}
		b, _ := json.Marshal(t)
		return strings.Join(parts, ", "), string(b), false
	case map[string]any:
		if s := firstScalar(t); s != "" {
			b, _ := json.Marshal(t)
			return s, string(b), false
		}
		if len(t) == 0 {
			return "", "null", true
		}
		b, _ := json.Marshal(t)
		return string(b), string(b), false
	default:
		s := asString(t)
		if s == "" {
			return "", "null", true
		}
		b, _ := json.Marshal(t)
		return s, string(b), false
	}
}

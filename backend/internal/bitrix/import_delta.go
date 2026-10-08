package bitrix

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var webhookURL = regexp.MustCompile(`https://[^\s"]+`)

func redactErr(err error) string {
	if err == nil {
		return ""
	}
	return webhookURL.ReplaceAllString(err.Error(), "[bitrix]")
}

func scopeHint(err error, msg string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "insufficient_scope") {
		return fmt.Errorf("%s", msg)
	}
	return err
}

func (im *Importer) maxBitrixID(ctx context.Context, table string) int64 {
	table = crmTable(table)
	if table == "" {
		return 0
	}
	var id int64
	_ = im.db.QueryRow(ctx, `SELECT coalesce(max(nullif(bitrix_id,'')::bigint), 0) FROM `+table).Scan(&id)
	return id
}

func (im *Importer) localBitrixCount(ctx context.Context, table string) int {
	table = crmTable(table)
	if table == "" {
		return 0
	}
	var n int
	_ = im.db.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE bitrix_id IS NOT NULL`).Scan(&n)
	return n
}

func crmTable(table string) string {
	switch table {
	case "contacts", "leads", "deals", "companies", "tasks", "users":
		return table
	default:
		return ""
	}
}

func (im *Importer) eachNew(ctx context.Context, key, method, table string, params map[string]any, handle func(row map[string]any) error) error {
	already := im.localBitrixCount(ctx, table)
	afterID := im.maxBitrixID(ctx, table)
	target := already
	im.setCount(key, already, already, nil)
	im.emit(key)
	return im.client.EachAfter(ctx, method, params, afterID, func(page []map[string]any, total int) error {
		if target == already {
			target = already + total
		}
		for _, row := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := handle(row); err != nil {
				return err
			}
		}
		already += len(page)
		im.setCount(key, already, target, nil)
		im.emit(key)
		return nil
	})
}

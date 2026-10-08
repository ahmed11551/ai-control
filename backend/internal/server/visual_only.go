package server

import (
	"context"
	"net/http"
	"strings"
)

const visualOnlyClosedMsg = "карточка закрыта"

func (a *App) dealVisualOnly(ctx context.Context, dealID string) bool {
	dealID = strings.TrimSpace(dealID)
	if a == nil || a.db == nil || dealID == "" {
		return false
	}
	var v bool
	err := a.db.QueryRow(ctx, `SELECT coalesce(visual_only,false) FROM deals WHERE id=$1::uuid`, dealID).Scan(&v)
	return err == nil && v
}

func (a *App) leadVisualOnly(ctx context.Context, leadID string) bool {
	leadID = strings.TrimSpace(leadID)
	if a == nil || a.db == nil || leadID == "" {
		return false
	}
	var v bool
	err := a.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM deals d
			WHERE coalesce(d.visual_only,false)
			  AND (d.lead_id=$1::uuid OR d.id=(SELECT converted_deal_id FROM leads WHERE id=$1::uuid))
		)`, leadID).Scan(&v)
	return err == nil && v
}

// rejectVisualOnly закрывает открытие для всех, кроме админа.
func rejectVisualOnly(w http.ResponseWriter, r *http.Request, sealed bool) bool {
	if !sealed || isAdmin(r) {
		return false
	}
	writeJSON(w, 403, map[string]string{"error": visualOnlyClosedMsg})
	return true
}

// rejectVisualOnlyChange запрещает двигать и править закрытую карточку даже админу.
func rejectVisualOnlyChange(w http.ResponseWriter, sealed bool) bool {
	if !sealed {
		return false
	}
	writeJSON(w, 403, map[string]string{"error": visualOnlyClosedMsg})
	return true
}

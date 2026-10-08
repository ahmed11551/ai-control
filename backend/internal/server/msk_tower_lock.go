package server

import (
	"context"
	"fmt"
	"strings"
)

const mskTowerRecallDeniedMsg = "из отдела МСК Башня лиды забирать нельзя"

// isMskTowerDepartmentName — отдел «МСК Башня» / «МСК БАШНЯ» и близкие названия.
func isMskTowerDepartmentName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "ё", "е")
	for strings.Contains(n, "  ") {
		n = strings.ReplaceAll(n, "  ", " ")
	}
	hasTower := strings.Contains(n, "башня") || strings.Contains(n, "tower")
	hasMsk := strings.Contains(n, "мск") || strings.Contains(n, "msk") || strings.Contains(n, "москв")
	return hasTower && hasMsk
}

// departmentLeadsLocked — из отдела нельзя отзывать/уводить лиды наружу.
// Триггер: имя «МСК Башня» или РОП с флагом msk_tower.
func (a *App) departmentLeadsLocked(ctx context.Context, deptID string) bool {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" || a.db == nil {
		return false
	}
	var name string
	var tower bool
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(d.name,''), coalesce(u.msk_tower,false)
		FROM departments d
		LEFT JOIN users u ON u.id=d.head_user_id
		WHERE d.id=$1::uuid`, deptID).Scan(&name, &tower)
	if err != nil {
		return false
	}
	return isMskTowerDepartmentName(name) || tower
}

func (a *App) userDepartmentLeadsLocked(ctx context.Context, userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" || a.db == nil {
		return false
	}
	deptID := a.actorDepartmentID(ctx, userID)
	return a.departmentLeadsLocked(ctx, deptID)
}

// transferLeavesLockedDept — попытка увести лид из МСК Башня в банк или другой отдел.
func (a *App) transferLeavesLockedDept(ctx context.Context, currentOwner, newOwner string) bool {
	currentOwner = strings.TrimSpace(currentOwner)
	newOwner = strings.TrimSpace(newOwner)
	if currentOwner == "" || !a.userDepartmentLeadsLocked(ctx, currentOwner) {
		return false
	}
	if newOwner == "" {
		return true
	}
	curDept := a.actorDepartmentID(ctx, currentOwner)
	newDept := a.actorDepartmentID(ctx, newOwner)
	if curDept == "" {
		return true
	}
	return newDept != curDept
}

func errMskTowerRecallDenied() error {
	return fmt.Errorf("%s", mskTowerRecallDeniedMsg)
}

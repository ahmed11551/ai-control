package server

import (
	"net/http"
	"strings"
)

func roleName(r *http.Request) string {
	role, _ := r.Context().Value(ctxRole).(string)
	return strings.ToLower(strings.TrimSpace(role))
}

func isAdmin(r *http.Request) bool {
	return roleName(r) == "admin"
}

func isROP(r *http.Request) bool {
	return roleName(r) == "rop"
}

func isManager(r *http.Request) bool {
	switch roleName(r) {
	case "manager", "sales": // sales — старое имя менеджера
		return true
	default:
		return false
	}
}

func isLogistRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "logist", "night_logist":
		return true
	default:
		return false
	}
}

func isLogist(r *http.Request) bool {
	return isLogistRole(roleName(r))
}

// canSeeClientPhonesRole — только admin без учёта отдела (legacy / быстрый чек).
// Для API используйте App.userCanSeeClientPhones (флаг departments.see_client_phones).
func canSeeClientPhonesRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "admin")
}

// seeClientPhonesAllowed — чистая матрица: admin всегда; manager/rop — только если у отдела флаг.
func seeClientPhonesAllowed(role string, deptSeePhones bool) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "night_logist":
		return true
	case "manager", "sales", "rop":
		return deptSeePhones
	default:
		return false
	}
}

func canManageStaff(r *http.Request) bool {
	// Управление штатными пользователями, линиями и доступами — только администратор.
	return isAdmin(r)
}

// canBindMax — привязка и отвязка аккаунтов MAX. Только администратор.
func canBindMax(r *http.Request) bool {
	return isAdmin(r)
}

// creatableRoles — кого текущий пользователь может заводить.
func creatableRoles(r *http.Request) []string {
	return creatableRolesFor(roleName(r))
}

func creatableRolesFor(role string) []string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin":
		return []string{"rop", "manager", "logist", "night_logist"}
	case "rop":
		return []string{"manager"}
	default:
		return nil
	}
}

func canCreateRole(actor *http.Request, targetRole string) bool {
	targetRole = strings.ToLower(strings.TrimSpace(targetRole))
	if targetRole == "sales" {
		targetRole = "manager"
	}
	for _, ok := range creatableRoles(actor) {
		if ok == targetRole {
			return true
		}
	}
	return false
}

func normStaffRole(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "sales", "менеджер":
		return "manager"
	case "роп", "head":
		return "rop"
	case "логист":
		return "logist"
	case "night_logist", "ночной логист", "night logist":
		return "night_logist"
	case "customer", "заказчик":
		return "customer"
	case "admin", "administrator", "админ", "администратор":
		return "admin"
	case "manager", "rop", "logist", "support", "marketing":
		return name
	default:
		return name
	}
}

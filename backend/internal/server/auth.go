package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type ctxKey string

const ctxUserID ctxKey = "uid"
const ctxRole ctxKey = "role"

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Login    string `json:"login"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := jsonDecode(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	ident := strings.TrimSpace(in.Login)
	if ident == "" {
		ident = strings.TrimSpace(in.Username)
	}
	if ident == "" {
		ident = strings.TrimSpace(in.Email)
	}
	if ident == "" || in.Password == "" {
		writeJSON(w, 401, map[string]string{"error": "invalid credentials"})
		return
	}
	var id, hash, name, role, ext, email, login string
	var canCall bool
	var ver int
	err := a.db.QueryRow(r.Context(), `
		SELECT u.id::text, u.password_hash, u.full_name, r.name, coalesce(u.phone_extension,''), u.can_call,
		       coalesce(u.token_version,1), coalesce(u.email,''), coalesce(u.login,'')
		FROM users u JOIN roles r ON r.id=u.role_id
		WHERE u.is_active=true
		  AND (
		    lower(coalesce(u.email,''))=lower($1)
		    OR lower(coalesce(u.login,''))=lower($1)
		  )`, ident).
		Scan(&id, &hash, &name, &role, &ext, &canCall, &ver, &email, &login)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		writeJSON(w, 401, map[string]string{"error": "invalid credentials"})
		return
	}
	if a.officeIPDenied(r.Context(), r, role) {
		writeJSON(w, 403, map[string]string{"error": officeIPDeniedMsg})
		return
	}
	if a.userDeptAccessDenied(r.Context(), id, role) {
		writeJSON(w, 403, map[string]string{"error": deptAccessDeniedMsg})
		return
	}
	ttl := time.Duration(jwtTTLHours(role, a.cfg.JWTTTLHours)) * time.Hour
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  id,
		"role": role,
		"ver":  ver,
		"exp":  time.Now().Add(ttl).Unix(),
	})
	s, err := tok.SignedString([]byte(a.cfg.JWTSecret))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "token"})
		return
	}
	siteName, siteCode, _ := a.customerSiteOf(r.Context(), id)
	writeJSON(w, 200, map[string]any{
		"token": s,
		"user": map[string]any{
			"id": id, "email": email, "login": login, "full_name": name, "role": role,
			"extension": ext, "can_call": canCall,
			"see_phones":         a.userCanSeeClientPhones(r.Context(), id, role),
			"creatable_roles":    creatableRolesFor(role),
			"customer_site":      siteName,
			"customer_site_code": siteCode,
			"session_restricted": a.userSessionRestricted(r.Context(), id),
		},
	})
}

func (a *App) authMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		tok, err := jwt.Parse(strings.TrimPrefix(h, "Bearer "), func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("alg")
			}
			return []byte(a.cfg.JWTSecret), nil
		})
		if err != nil || !tok.Valid {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		claims, _ := tok.Claims.(jwt.MapClaims)
		sub, _ := claims["sub"].(string)
		if sub == "" {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		var active bool
		var ver int
		var role string
		err = a.db.QueryRow(r.Context(), `
			SELECT u.is_active, coalesce(u.token_version,1), coalesce(r.name,'')
			FROM users u LEFT JOIN roles r ON r.id=u.role_id
			WHERE u.id=$1`, sub).Scan(&active, &ver, &role)
		if err != nil || !active || jwtClaimVer(map[string]any(claims)) != ver {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		if a.officeIPDenied(r.Context(), r, role) {
			writeJSON(w, 403, map[string]string{"error": officeIPDeniedMsg})
			return
		}
		if a.userDeptAccessDenied(r.Context(), sub, role) {
			writeJSON(w, 403, map[string]string{"error": deptAccessDeniedMsg})
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserID, sub)
		ctx = context.WithValue(ctx, ctxRole, role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	uid := actorID(r)
	var id, email, login, name, role, ext string
	var canCall bool
	if err := a.db.QueryRow(r.Context(), `
		SELECT u.id::text, coalesce(u.email,''), coalesce(u.login,''), u.full_name, r.name,
		       coalesce(u.phone_extension,''), u.can_call
		FROM users u JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1 AND u.is_active=true`, uid).
		Scan(&id, &email, &login, &name, &role, &ext, &canCall); err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	siteName, siteCode, _ := a.customerSiteOf(r.Context(), id)
	writeJSON(w, 200, map[string]any{
		"id": id, "email": email, "login": login, "full_name": name, "role": role, "extension": ext, "can_call": canCall,
		"see_phones":         a.userCanSeeClientPhones(r.Context(), id, role),
		"creatable_roles":    creatableRolesFor(role),
		"can_see_old_leads":  role == "admin" || a.userInTemaDepartment(r.Context(), id),
		"customer_site":      siteName,
		"customer_site_code": siteCode,
		"session_restricted": a.userSessionRestricted(r.Context(), id),
	})
}

func jsonDecode(r *http.Request, dst any) error {
	return readJSON(r, dst)
}

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
	in      chan []byte
}

func NewHub() *Hub {
	return &Hub{clients: map[*websocket.Conn]bool{}, in: make(chan []byte, 512)}
}

func (h *Hub) Run() {
	for msg := range h.in {
		h.mu.RLock()
		dead := make([]*websocket.Conn, 0)
		for c := range h.clients {
			_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
				dead = append(dead, c)
			}
		}
		h.mu.RUnlock()
		if len(dead) > 0 {
			h.mu.Lock()
			for _, c := range dead {
				delete(h.clients, c)
				_ = c.Close()
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) Broadcast(kind string, payload any) {
	b, _ := json.Marshal(map[string]any{"type": kind, "payload": payload})
	select {
	case h.in <- b:
		return
	default:
	}
	// Буфер забит — не теряем критичные autodial-события: подождём кратко.
	select {
	case h.in <- b:
	case <-time.After(200 * time.Millisecond):
		log.Printf("ws hub DROP type=%s (buffer full)", kind)
	}
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (a *App) serveWS(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimSpace(r.URL.Query().Get("token"))
	if tok == "" {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if tok == "" || !a.validWSToken(r.Context(), tok) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	a.hub.mu.Lock()
	a.hub.clients[c] = true
	a.hub.mu.Unlock()
	go func() {
		defer func() {
			a.hub.mu.Lock()
			delete(a.hub.clients, c)
			a.hub.mu.Unlock()
			c.Close()
		}()
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

func (a *App) validWSToken(ctx context.Context, raw string) bool {
	tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("alg")
		}
		return []byte(a.cfg.JWTSecret), nil
	})
	if err != nil || !tok.Valid {
		return false
	}
	claims, _ := tok.Claims.(jwt.MapClaims)
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return false
	}
	var active bool
	var ver int
	var role string
	err = a.db.QueryRow(ctx, `
		SELECT u.is_active, coalesce(u.token_version,1), coalesce(r.name,'')
		FROM users u LEFT JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1`, sub).Scan(&active, &ver, &role)
	if err != nil || !active || jwtClaimVer(map[string]any(claims)) != ver {
		return false
	}
	if a.userDeptAccessDenied(ctx, sub, role) {
		return false
	}
	return true
}

package server

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type voxServiceAccountCreds struct {
	AccountID  json.Number `json:"account_id"`
	KeyID      string      `json:"key_id"`
	PrivateKey string      `json:"private_key"`
}

type voxServiceAccount struct {
	accountID string
	keyID     string
	key       *rsa.PrivateKey

	mu    sync.Mutex
	token string
	exp   time.Time
}

var (
	voxSAOnce sync.Once
	voxSA     *voxServiceAccount
	voxSAErr  error
)

func (a *App) voxServiceAccountAuthHeader() (string, error) {
	sa, err := a.loadVoxServiceAccount()
	if err != nil || sa == nil {
		return "", err
	}
	return sa.bearerToken()
}

func resolveVoxServiceAccountPath(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		configured = "voximplant-service-account.json"
	}
	candidates := []string{configured}
	if !filepath.IsAbs(configured) {
		if wd, err := os.Getwd(); err == nil {
			candidates = append(candidates,
				filepath.Join(wd, configured),
				filepath.Join(wd, "..", configured),
			)
		}
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(p)
			if err == nil {
				return abs
			}
			return p
		}
	}
	return configured
}

func (a *App) loadVoxServiceAccount() (*voxServiceAccount, error) {
	voxSAOnce.Do(func() {
		path := resolveVoxServiceAccountPath(a.cfg.VoxServiceAccountJSON)
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			voxSAErr = fmt.Errorf("vox service account: %w", err)
			return
		}
		var creds voxServiceAccountCreds
		if err := json.Unmarshal(raw, &creds); err != nil {
			voxSAErr = fmt.Errorf("vox service account json: %w", err)
			return
		}
		if strings.TrimSpace(creds.KeyID) == "" || strings.TrimSpace(creds.PrivateKey) == "" {
			voxSAErr = fmt.Errorf("vox service account: нет key_id/private_key")
			return
		}
		key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(creds.PrivateKey))
		if err != nil {
			voxSAErr = fmt.Errorf("vox service account private_key: %w", err)
			return
		}
		voxSA = &voxServiceAccount{
			accountID: strings.TrimSpace(creds.AccountID.String()),
			keyID:     strings.TrimSpace(creds.KeyID),
			key:       key,
		}
		log.Printf("vox service account loaded from %s (account_id=%s)", path, voxSA.accountID)
	})
	return voxSA, voxSAErr
}

func (sa *voxServiceAccount) bearerToken() (string, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	if sa.token != "" && time.Now().Before(sa.exp.Add(-30*time.Second)) {
		return "Bearer " + sa.token, nil
	}
	now := time.Now()
	var iss any = sa.accountID
	if n, err := json.Number(sa.accountID).Int64(); err == nil {
		iss = n
	}
	claims := jwt.MapClaims{
		"iat": now.Unix(),
		"iss": iss,
		"exp": now.Add(50 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = sa.keyID
	tok.Header["typ"] = "JWT"
	signed, err := tok.SignedString(sa.key)
	if err != nil {
		return "", err
	}
	sa.token = signed
	sa.exp = now.Add(50 * time.Minute)
	return "Bearer " + signed, nil
}

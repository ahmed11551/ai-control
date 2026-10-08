package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr               string
	DatabaseURL        string
	RedisURL           string
	JWTSecret          string
	JWTTTLHours        int
	BootstrapEmail     string
	BootstrapPass      string
	SearchEngine       string
	AMIHost            string
	AMIPort            string
	AMIUser            string
	AMISecret          string
	VKToken            string
	VKAccountID        string
	VKWebhookSecret    string
	S3Endpoint         string
	S3Access           string
	S3Secret           string
	S3Bucket           string
	SkipAMI            bool
	BitrixWebhook      string
	CallProvider       string
	TelfinAppID        string
	TelfinAppSecret    string
	UISToken           string
	UISVirtualPhone    string
	CallWebhookSecret  string
	InboxWebhookSecret string
	PublicBaseURL      string
	WappiToken         string
	WappiProfileID     string
	MaxBotToken        string
	MaxBotPoll         bool
	YandexIMAPUser     string
	YandexIMAPPassword string
	YandexIMAPFolder   string
	YandexIMAPPollSec  int
	AvitoClientID      string
	AvitoClientSecret  string
	ExolveAPIKey       string
	ExolveSIPLogin     string
	ExolveSIPPassword  string
	ExolveInboundNums  string
	// ExolveAutodialNums — связующие DID только для скорозвона (не CLI обычных звонков).
	ExolveAutodialNums string
	// ExolveCLINums — опциональный allowlist CLI менеджера; пусто = все DID кроме autodial.
	ExolveCLINums string
	VoxUsername   string
	VoxPassword   string
	// VoxServiceAccountJSON — путь к credentials service account (secure recordings).
	VoxServiceAccountJSON    string
	ExolveCallbackResourceID string
	AutodialRetryMinutes     int
	TelegramBotToken         string
	// TelegramLogistChatIDs — chat_id через запятую (группа/личка); плюс /start в боте.
	TelegramLogistChatIDs string
	// Отдельный бот заявок МСК Башня. Свой токен, не тот же, что у уведомлений «Билет».
	TelegramMskTowerBotToken    string
	TelegramMskTowerOperatorIDs string
	AIControlURL                string
	AIControlToken              string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Load() Config {
	ttl, _ := strconv.Atoi(env("JWT_TTL_HOURS", "12"))
	sipLogin := strings.TrimSpace(env("EXOLVE_SIP_LOGIN", env("VITE_EXOLVE_SIP_LOGIN", "")))
	voxUser := strings.TrimSpace(env("VOX_USERNAME", env("VITE_VOX_USERNAME", "")))
	voxPass := strings.TrimSpace(env("VOX_PASSWORD", env("VITE_VOX_PASSWORD", "")))
	return Config{
		Addr:                        env("HTTP_ADDR", ":8080"),
		DatabaseURL:                 env("DATABASE_URL", "postgres://forge:forge_change_me@localhost:5432/forgecrm?sslmode=disable"),
		RedisURL:                    env("REDIS_URL", "redis://localhost:6379/0"),
		JWTSecret:                   env("JWT_SECRET", "dev-secret-change"),
		JWTTTLHours:                 ttl,
		BootstrapEmail:              env("BOOTSTRAP_ADMIN_EMAIL", "admin@forgecrm.local"),
		BootstrapPass:               env("BOOTSTRAP_ADMIN_PASSWORD", "ChangeMe!123"),
		SearchEngine:                env("SEARCH_ENGINE", "postgres"),
		AMIHost:                     env("AMI_HOST", "127.0.0.1"),
		AMIPort:                     env("AMI_PORT", "5038"),
		AMIUser:                     env("AMI_USER", "forgecrm"),
		AMISecret:                   env("AMI_SECRET", "ami_change_me_now"),
		VKToken:                     env("VK_ADS_TOKEN", ""),
		VKAccountID:                 env("VK_ADS_ACCOUNT_ID", ""),
		VKWebhookSecret:             env("VK_ADS_WEBHOOK_SECRET", "vk_webhook_secret_change_me"),
		S3Endpoint:                  env("S3_ENDPOINT", "http://minio:9000"),
		S3Access:                    env("S3_ACCESS_KEY", "forge_minio"),
		S3Secret:                    env("S3_SECRET_KEY", "forge_minio_change_me"),
		S3Bucket:                    env("S3_BUCKET", "forgecrm"),
		SkipAMI:                     env("SKIP_AMI", "") == "1" || env("SKIP_AMI", "") == "true",
		BitrixWebhook:               strings.TrimSpace(env("BITRIX_WEBHOOK_URL", "")),
		CallProvider:                strings.ToLower(strings.TrimSpace(env("CALL_PROVIDER", "auto"))),
		TelfinAppID:                 strings.TrimSpace(env("TELFIN_APP_ID", "")),
		TelfinAppSecret:             strings.TrimSpace(env("TELFIN_APP_SECRET", "")),
		UISToken:                    strings.TrimSpace(env("UIS_ACCESS_TOKEN", "")),
		UISVirtualPhone:             strings.TrimSpace(env("UIS_VIRTUAL_PHONE", "")),
		CallWebhookSecret:           strings.TrimSpace(env("CALL_WEBHOOK_SECRET", "")),
		InboxWebhookSecret:          strings.TrimSpace(env("INBOX_WEBHOOK_SECRET", env("CALL_WEBHOOK_SECRET", ""))),
		PublicBaseURL:               strings.TrimRight(strings.TrimSpace(env("PUBLIC_BASE_URL", "")), "/"),
		WappiToken:                  strings.TrimSpace(env("WAPPI_TOKEN", "")),
		WappiProfileID:              strings.TrimSpace(env("WAPPI_PROFILE_ID", "")),
		MaxBotToken:                 strings.TrimSpace(env("MAX_BOT_TOKEN", "")),
		MaxBotPoll:                  envTruthy(env("MAX_BOT_POLL", "1")),
		YandexIMAPUser:              strings.TrimSpace(env("YANDEX_IMAP_USER", "")),
		YandexIMAPPassword:          strings.TrimSpace(env("YANDEX_IMAP_PASSWORD", "")),
		YandexIMAPFolder:            strings.TrimSpace(env("YANDEX_IMAP_FOLDER", "INBOX")),
		YandexIMAPPollSec:           envInt(env("YANDEX_IMAP_POLL", "30"), 30),
		AvitoClientID:               strings.TrimSpace(env("AVITO_CLIENT_ID", "")),
		AvitoClientSecret:           strings.TrimSpace(env("AVITO_CLIENT_SECRET", "")),
		ExolveAPIKey:                strings.TrimSpace(env("EXOLVE_API_KEY", "")),
		ExolveSIPLogin:              sipLogin,
		ExolveSIPPassword:           strings.TrimSpace(env("EXOLVE_SIP_PASSWORD", env("VITE_EXOLVE_SIP_PASSWORD", ""))),
		ExolveInboundNums:           strings.TrimSpace(env("EXOLVE_INBOUND_NUMBERS", "")),
		ExolveAutodialNums:          strings.TrimSpace(env("EXOLVE_AUTODIAL_NUMBERS", "")),
		ExolveCLINums:               strings.TrimSpace(env("EXOLVE_CLI_NUMBERS", "")),
		VoxUsername:                 voxUser,
		VoxPassword:                 voxPass,
		VoxServiceAccountJSON:       strings.TrimSpace(env("VOX_SERVICE_ACCOUNT_JSON", "voximplant-service-account.json")),
		ExolveCallbackResourceID:    strings.TrimSpace(env("EXOLVE_CALLBACK_RESOURCE_ID", "")),
		AutodialRetryMinutes:        envInt(env("AUTODIAL_RETRY_MINUTES", "30"), 30),
		TelegramBotToken:            strings.TrimSpace(env("TELEGRAM_BOT_TOKEN", "")),
		TelegramLogistChatIDs:       strings.TrimSpace(env("TELEGRAM_LOGIST_CHAT_IDS", "")),
		TelegramMskTowerBotToken:    strings.TrimSpace(env("TELEGRAM_MSK_TOWER_BOT_TOKEN", "")),
		TelegramMskTowerOperatorIDs: strings.TrimSpace(env("TELEGRAM_MSK_TOWER_OPERATOR_IDS", "")),
		AIControlURL:                strings.TrimRight(strings.TrimSpace(env("AI_CONTROL_URL", "http://127.0.0.1:8091")), "/"),
		AIControlToken:              strings.TrimSpace(env("AI_CONTROL_TOKEN", "")),
	}
}

func (c Config) TelfinReady() bool {
	return c.TelfinAppID != "" && c.TelfinAppSecret != ""
}

func (c Config) UISReady() bool {
	return c.UISToken != ""
}

func (c Config) AMIReady() bool {
	return !c.SkipAMI
}

func (c Config) WappiReady() bool {
	return c.WappiToken != ""
}

func (c Config) MaxBotReady() bool {
	return c.MaxBotToken != ""
}

func (c Config) YandexMailReady() bool {
	return c.YandexIMAPUser != "" && c.YandexIMAPPassword != ""
}

func (c Config) AvitoReady() bool {
	return c.AvitoClientID != "" && c.AvitoClientSecret != ""
}

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envInt(v string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func (c Config) ExolveReady() bool {
	return c.ExolveAPIKey != "" && c.ExolveSIPLogin != ""
}

func (c Config) VoxInboundReady() bool {
	return c.ExolveAPIKey != "" && c.VoxSIPURI() != ""
}

// VoxSIPURI is user@app.account.voximplant.com for Exolve external SIP forwarding.
func (c Config) VoxSIPURI() string {
	u := strings.TrimSpace(c.VoxUsername)
	u = strings.TrimPrefix(strings.ToLower(u), "sip:")
	return u
}

// VoxInboundUser is the short username for VoxEngine.callUser (before @).
func (c Config) VoxInboundUser() string {
	u := c.VoxSIPURI()
	if i := strings.IndexByte(u, '@'); i > 0 {
		return u[:i]
	}
	return u
}

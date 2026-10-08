package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/forgecrm/server/internal/schema"
	"github.com/forgecrm/server/internal/server"
	"github.com/jackc/pgx/v5"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	runtime := filepath.Join(root, ".pg-embed")
	_ = os.MkdirAll(runtime, 0o755)

	port := uint32(55432)
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Username("forge").
		Password("forge").
		Database("forgecrm").
		Port(port).
		RuntimePath(runtime).
		DataPath(filepath.Join(runtime, "data")).
		StartTimeout(2 * time.Minute).
		Logger(os.Stdout))

	log.Println("embedded PostgreSQL :", port)
	pgOwned := true
	if err := pg.Start(); err != nil {
		msg := err.Error()
		// Сирота postgres после прошлого kill API — просто подключаемся.
		if strings.Contains(msg, "already listening") || strings.Contains(msg, "address already in use") {
			log.Println("reusing existing PostgreSQL on", port)
			pgOwned = false
		} else {
			log.Fatal(err)
		}
	}
	if pgOwned {
		defer func() { _ = pg.Stop() }()
	}

	ctx := context.Background()
	url := fmt.Sprintf("postgres://forge:forge@127.0.0.1:%d/forgecrm?sslmode=disable", port)
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		log.Fatal(err)
	}
	var n int
	_ = conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name='roles'`).Scan(&n)
	if n == 0 {
		if _, err := conn.Exec(ctx, schema.LocalSQL); err != nil {
			log.Fatal("migration: ", err)
		}
		log.Println("schema applied")
	} else {
		log.Println("schema already present")
	}
	if _, err := conn.Exec(ctx, schema.CompatSQL); err != nil {
		log.Fatal("compat: ", err)
	}
	_ = conn.Close(ctx)

	// Pull softphone / Exolve secrets from repo root .env without overriding local DB settings below.
	loadDotEnvIfMissing(filepath.Join(root, "..", ".env"), filepath.Join(root, ".env"))

	_ = os.Setenv("DATABASE_URL", url)
	_ = os.Setenv("SKIP_AMI", "1")
	if strings.TrimSpace(os.Getenv("HTTP_ADDR")) == "" {
		_ = os.Setenv("HTTP_ADDR", ":8080")
	}
	_ = os.Setenv("JWT_SECRET", "local-dev-secret-not-for-prod")
	_ = os.Setenv("BOOTSTRAP_ADMIN_EMAIL", "admin@forgecrm.local")
	_ = os.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "ChangeMe!123")
	_ = os.Setenv("REDIS_URL", "redis://127.0.0.1:9/0")

	log.Println("API http://127.0.0.1"+os.Getenv("HTTP_ADDR"), " admin@forgecrm.local / ChangeMe!123")
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}

func loadDotEnvIfMissing(paths ...string) {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)
			if len(val) >= 2 {
				if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
					val = val[1 : len(val)-1]
				}
			}
			if key == "" {
				continue
			}
			// Локально .env важнее унаследованного shell env (иначе залипает старый WAPPI_TOKEN).
			_ = os.Setenv(key, val)
		}
	}
}

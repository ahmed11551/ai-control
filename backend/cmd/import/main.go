package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/forgecrm/server/internal/bitrix"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	webhook := flag.String("webhook", os.Getenv("BITRIX_WEBHOOK_URL"), "входящий вебхук Bitrix24")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "DATABASE_URL")
	entities := flag.String("entities", "", "через запятую: users,departments,pipelines,companies,contacts,leads,deals,tasks,fields")
	inventory := flag.Bool("inventory", false, "только посчитать на портале, не писать в БД")
	flag.Parse()

	if strings.TrimSpace(*webhook) == "" {
		log.Fatal("нужен -webhook или BITRIX_WEBHOOK_URL")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *inventory {
		prog, err := bitrix.Inventory(ctx, *webhook)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("портал:", prog.Portal)
		for k, v := range prog.Counts {
			if v.Error != "" {
				fmt.Printf("  %s: ошибка %s\n", k, v.Error)
				continue
			}
			fmt.Printf("  %s: %d\n", k, v.Total)
		}
		return
	}

	if strings.TrimSpace(*dbURL) == "" {
		log.Fatal("нужен -db или DATABASE_URL")
	}
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	var list []string
	if s := strings.TrimSpace(*entities); s != "" {
		list = strings.Split(s, ",")
	}
	im, err := bitrix.NewImporter(pool, *webhook, list, func(p bitrix.Progress) {
		log.Printf("%s %s", p.Status, p.Phase)
		for k, v := range p.Counts {
			if v.Imported > 0 || v.Error != "" {
				log.Printf("  %s %d/%d %s", k, v.Imported, v.Total, v.Error)
			}
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := im.Run(ctx); err != nil {
		log.Fatal(err)
	}
	log.Println("готово")
}

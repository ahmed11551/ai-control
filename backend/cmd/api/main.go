package main

import (
	"log"
	"os"

	"github.com/forgecrm/server/internal/server"
)

func main() {
	if err := server.Run(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

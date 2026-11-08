// pulso-api migrate CLI. Lo saque a un binario aparte para no cargar
// migrate en el server en runtime.
package main

import (
	"flag"
	"log"
	"os"

	"github.com/MatiasHernanSerrato/pulso-api/internal/config"
	"github.com/MatiasHernanSerrato/pulso-api/internal/db"
)

func main() {
	dir := flag.String("dir", "file://db/migrations", "source url de las migrations")
	cmd := flag.String("cmd", "up", "up | down | version")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	switch *cmd {
	case "up":
		if err := db.MigrateUp(*dir, cfg.DatabaseURL); err != nil {
			log.Fatalf("migrate up: %v", err)
		}
		log.Println("migrations ok")
	case "down":
		if err := db.MigrateDown(*dir, cfg.DatabaseURL); err != nil {
			log.Fatalf("migrate down: %v", err)
		}
		log.Println("rollback ok")
	case "version":
		v, dirty, err := db.MigrateVersion(*dir, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("version: %v", err)
		}
		log.Printf("version=%d dirty=%v", v, dirty)
	default:
		log.Printf("comando desconocido: %s", *cmd)
		os.Exit(2)
	}
}

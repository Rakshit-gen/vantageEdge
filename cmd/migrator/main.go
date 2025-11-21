package main

import (
	"fmt"
	"os"

	"github.com/vantageedge/backend/pkg/config"
	"github.com/vantageedge/backend/pkg/database"
	"github.com/vantageedge/backend/pkg/logger"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: migrator <up|down>")
		os.Exit(1)
	}

	command := os.Args[1]

	// Load config
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log := logger.New("info", "json")

	// Connect to database
	db, err := database.New(&cfg.Database, log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()

	switch command {
	case "up":
		log.Info().Msg("Running migrations up")
		// Migrations are already run via docker-entrypoint-initdb.d
		log.Info().Msg("Migrations completed")
	case "down":
		log.Info().Msg("Rolling back migrations")
		log.Warn().Msg("Migration rollback not implemented - handle manually")
	default:
		log.Fatal().Str("command", command).Msg("Unknown command")
	}
}


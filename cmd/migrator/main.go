package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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

	// Create migrations table if it doesn't exist
	if err := createMigrationsTable(db); err != nil {
		log.Fatal().Err(err).Msg("Failed to create migrations table")
	}

	switch command {
	case "up":
		if err := runMigrationsUp(db, log); err != nil {
			log.Fatal().Err(err).Msg("Failed to run migrations")
		}
		log.Info().Msg("Migrations completed successfully")
	case "down":
		steps := 1
		if len(os.Args) > 2 {
			n, err := strconv.Atoi(os.Args[2])
			if err != nil || n < 1 {
				log.Fatal().Str("arg", os.Args[2]).Msg("Usage: migrator down [n] (n must be a positive integer, default 1)")
			}
			steps = n
		}
		if err := runMigrationsDown(db, log, steps); err != nil {
			log.Fatal().Err(err).Msg("Failed to roll back migrations")
		}
		log.Info().Msg("Rollback completed successfully")
	default:
		log.Fatal().Str("command", command).Msg("Unknown command")
	}
}

func createMigrationsTable(db *database.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)
	`
	_, err := db.Exec(query)
	return err
}

func getAppliedMigrations(db *database.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

func getMigrationFiles(dir string) ([]string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var migrations []string
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".up.sql") {
			migrations = append(migrations, file.Name())
		}
	}

	sort.Strings(migrations)
	return migrations, nil
}

func runMigrationsUp(db *database.DB, log *logger.Logger) error {
	migrationsDir := "./migrations"
	if dir := os.Getenv("MIGRATIONS_DIR"); dir != "" {
		migrationsDir = dir
	}

	// Get list of migration files
	migrationFiles, err := getMigrationFiles(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	if len(migrationFiles) == 0 {
		log.Warn().Msg("No migration files found")
		return nil
	}

	// Get applied migrations
	applied, err := getAppliedMigrations(db)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Run pending migrations
	for _, file := range migrationFiles {
		version := strings.TrimSuffix(file, ".up.sql")

		if applied[version] {
			log.Info().Str("version", version).Msg("Migration already applied, skipping")
			continue
		}

		log.Info().Str("version", version).Msg("Running migration")

		// Read migration file
		path := filepath.Join(migrationsDir, file)
		sql, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		// Begin transaction
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		// Execute migration
		if _, err := tx.Exec(string(sql)); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute migration %s: %w", version, err)
		}

		// Record migration
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", version, err)
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", version, err)
		}

		log.Info().Str("version", version).Msg("Migration applied successfully")
	}

	return nil
}

// runMigrationsDown rolls back up to `steps` applied migrations, most
// recent first, executing each corresponding .down.sql file. This was
// previously a no-op ("handle manually") despite every migration already
// shipping a .down.sql file, so `make migrate-down` silently did nothing —
// a real hazard if someone ran it expecting an actual rollback (e.g.
// during a bad deploy) and got no error, just no effect.
func runMigrationsDown(db *database.DB, log *logger.Logger, steps int) error {
	migrationsDir := "./migrations"
	if dir := os.Getenv("MIGRATIONS_DIR"); dir != "" {
		migrationsDir = dir
	}

	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT $1", steps)
	if err != nil {
		return fmt.Errorf("failed to query applied migrations: %w", err)
	}
	var versions []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(versions) == 0 {
		log.Warn().Msg("No applied migrations to roll back")
		return nil
	}

	for _, version := range versions {
		downFile := filepath.Join(migrationsDir, version+".down.sql")
		sql, err := os.ReadFile(downFile)
		if err != nil {
			return fmt.Errorf("failed to read down migration for %s: %w", version, err)
		}

		log.Info().Str("version", version).Msg("Rolling back migration")

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		if _, err := tx.Exec(string(sql)); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute down migration %s: %w", version, err)
		}

		if _, err := tx.Exec("DELETE FROM schema_migrations WHERE version = $1", version); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to unrecord migration %s: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit rollback of %s: %w", version, err)
		}

		log.Info().Str("version", version).Msg("Migration rolled back successfully")
	}

	return nil
}

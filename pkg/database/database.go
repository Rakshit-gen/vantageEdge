package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/vantageedge/backend/pkg/config"
	"github.com/vantageedge/backend/pkg/logger"
)

type DB struct {
	*sqlx.DB
	logger *logger.Logger
}

func New(cfg *config.DatabaseConfig, log *logger.Logger) (*DB, error) {
	connStr := cfg.ConnectionString()

	db, err := sqlx.Connect("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxConnections)
	db.SetMaxIdleConns(cfg.MaxIdleConnections)
	db.SetConnMaxLifetime(cfg.MaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info().
		Str("host", cfg.Host).
		Int("port", cfg.Port).
		Str("database", cfg.Name).
		Msg("Connected to database")

	return &DB{
		DB:     db,
		logger: log,
	}, nil
}

func (db *DB) Close() error {
	db.logger.Info().Msg("Closing database connection")
	return db.DB.Close()
}

func (db *DB) HealthCheck(ctx context.Context) error {
	return db.PingContext(ctx)
}

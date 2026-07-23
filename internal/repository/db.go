package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/metrics"
)

func recordQueryDuration(queryType string, start time.Time) {
	metrics.DBQueryDuration.WithLabelValues(queryType).Observe(time.Since(start).Seconds())
}

func NewPostgresDB(cfg *config.Config) (*pgxpool.Pool, error) {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.Database.Postgresql.User,
		cfg.Database.Postgresql.Password,
		cfg.Database.Postgresql.Host,
		cfg.Database.Postgresql.Port,
		cfg.Database.Postgresql.Database,
		cfg.Database.Postgresql.SSLMode,
	)

	pgxCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	pgxCfg.MaxConns = int32(cfg.Database.Postgresql.MaxOpenConns)
	pgxCfg.MinConns = int32(cfg.Database.Postgresql.MinIdleConns)
	pgxCfg.MaxConnLifetime = cfg.Database.Postgresql.ConnMaxLifetime
	pgxCfg.MaxConnLifetimeJitter = 5 * time.Minute
	pgxCfg.MaxConnIdleTime = 30 * time.Minute
	pgxCfg.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

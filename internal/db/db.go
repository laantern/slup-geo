// Package db — подключение к PostgreSQL и выполнение SQL-скриптов схемы.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect открывает пул соединений.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("разбор строки подключения: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("создание пула соединений: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("проверка подключения: %w", err)
	}
	return pool, nil
}

// ExecScripts выполняет SQL-скрипты целиком (несколько инструкций в одном скрипте).
// Для этого используется отдельное соединение в simple protocol.
func ExecScripts(ctx context.Context, dsn string, scripts ...string) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("разбор строки подключения: %w", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("подключение для миграций: %w", err)
	}
	defer conn.Close(ctx)

	for i, script := range scripts {
		if _, err := conn.Exec(ctx, script); err != nil {
			return fmt.Errorf("выполнение скрипта %d: %w", i+1, err)
		}
	}
	return nil
}

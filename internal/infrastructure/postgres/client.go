package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig конфигурация пула соединений
type PoolConfig struct {
	DSN      string
	MinConns int32
	MaxConns int32
}

// Client инкапсулирует PostgreSQL соединение
type Client struct {
	pool *pgxpool.Pool
}

// NewClient создает новый PostgreSQL клиент
func NewClient(ctx context.Context, cfg PoolConfig) (*Client, error) {
	pool, err := NewPool(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return &Client{pool: pool}, nil
}

// NewPool создает пул соединений (перенесено из database.go)
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	pgxCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("fail to parse config: %v", err)
	}

	pgxCfg.MinConns = cfg.MinConns
	pgxCfg.MaxConns = cfg.MaxConns

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize connection pool: %v", err)
	}

	return pool, nil
}

// Pool возвращает пул соединений
func (c *Client) Pool() *pgxpool.Pool {
	return c.pool
}

// Close закрывает пул соединений
func (c *Client) Close() {
	c.pool.Close()
}

package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wrapea el pgxpool con un poco de contexto nuestro.
// Lo tenemos aparte asi despues podemos cambiar el driver si hace falta
// sin tocar cada handler.
type Pool struct {
	*pgxpool.Pool
}

// PoolConfig junta los knobs del pool. Los defaults estan pensados
// para una api chica corriendo en fly.io, no para algo pesado.
type PoolConfig struct {
	DatabaseURL     string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	HealthCheck     time.Duration
}

// DefaultPoolConfig devuelve una config razonable para dev.
// En prod sobreescribimos con envs.
func DefaultPoolConfig(dsn string) PoolConfig {
	return PoolConfig{
		DatabaseURL:     dsn,
		MaxConns:        10,
		MinConns:        2,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
		HealthCheck:     time.Minute,
	}
}

// NewPool arma el pool y pinga la db para no arrancar roto.
func NewPool(ctx context.Context, cfg PoolConfig) (*Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	if cfg.MaxConns > 0 {
		pc.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		pc.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		pc.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		pc.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheck > 0 {
		pc.HealthCheckPeriod = cfg.HealthCheck
	}

	p, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.Ping(pingCtx); err != nil {
		p.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &Pool{Pool: p}, nil
}

// Close cierra el pool. Lo hice metodo propio para poder loguear si
// algun dia hace falta.
func (p *Pool) Close() {
	if p == nil || p.Pool == nil {
		return
	}
	p.Pool.Close()
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/jackc/pgx/v5"
)

// Config fixes one API or MCP process's total pool budget across writer and reader.
// Endpoint selection is intentionally limited to one static physical PostgreSQL
// primary and, optionally, one physical streaming standby.
type Config struct {
	WriterDSN          string
	ReadDSN            string
	SamePrimary        bool
	WriterMaxOpenConns int
	ReadMaxOpenConns   int
	WriterMaxIdleConns int
	ReadMaxIdleConns   int
	ConnMaxLifetime    time.Duration
	ConnMaxIdleTime    time.Duration
	PingTimeout        time.Duration
	ReplayTimeout      time.Duration
}

// LoadConfig resolves the optional reader endpoint and validates the shared
// budget before opening either pool. An omitted reader DSN uses the writer.
func LoadConfig(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, fmt.Errorf("postgres environment reader is required")
	}
	writer := strings.TrimSpace(getenv("ESHU_POSTGRES_DSN"))
	if writer == "" {
		writer = strings.TrimSpace(getenv("ESHU_CONTENT_STORE_DSN"))
	}
	if writer == "" {
		return Config{}, fmt.Errorf("set ESHU_POSTGRES_DSN or ESHU_CONTENT_STORE_DSN")
	}
	base, err := runtimecfg.LoadPostgresConfig(func(key string) string {
		switch key {
		case "ESHU_FACT_STORE_DSN", "ESHU_CONTENT_STORE_DSN":
			return ""
		case "ESHU_POSTGRES_DSN":
			return writer
		default:
			return getenv(key)
		}
	})
	if err != nil {
		return Config{}, fmt.Errorf("invalid PostgreSQL pool configuration; check ESHU_POSTGRES_MAX_OPEN_CONNS, ESHU_POSTGRES_MAX_IDLE_CONNS, ESHU_POSTGRES_CONN_MAX_LIFETIME, ESHU_POSTGRES_CONN_MAX_IDLE_TIME, and ESHU_POSTGRES_PING_TIMEOUT")
	}
	if base.MaxOpenConns < 2 {
		return Config{}, fmt.Errorf("ESHU_POSTGRES_MAX_OPEN_CONNS must be at least 2 for separate reader and writer pools")
	}
	read := strings.TrimSpace(getenv("ESHU_POSTGRES_READ_DSN"))
	if read == "" {
		read = writer
	}
	for _, endpoint := range []string{writer, read} {
		if _, err := parseStaticEndpoint(endpoint); err != nil {
			return Config{}, err
		}
	}
	readOpen := base.MaxOpenConns / 2
	if value := strings.TrimSpace(getenv("ESHU_POSTGRES_READ_MAX_OPEN_CONNS")); value != "" {
		readOpen, err = strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("ESHU_POSTGRES_READ_MAX_OPEN_CONNS must be an integer")
		}
	}
	writeOpen := base.MaxOpenConns - readOpen
	if readOpen < 1 || writeOpen < 1 {
		return Config{}, fmt.Errorf("ESHU_POSTGRES_READ_MAX_OPEN_CONNS must leave at least one connection for each pool")
	}
	readIdle := base.MaxIdleConns / 2
	if value := strings.TrimSpace(getenv("ESHU_POSTGRES_READ_MAX_IDLE_CONNS")); value != "" {
		readIdle, err = strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("ESHU_POSTGRES_READ_MAX_IDLE_CONNS must be an integer")
		}
		if readIdle < 0 || readIdle > base.MaxIdleConns || readIdle > readOpen || base.MaxIdleConns-readIdle > writeOpen {
			return Config{}, fmt.Errorf("ESHU_POSTGRES_READ_MAX_IDLE_CONNS must fit both pools and the total idle budget")
		}
	} else {
		if readIdle < base.MaxIdleConns-writeOpen {
			readIdle = base.MaxIdleConns - writeOpen
		}
		if readIdle > readOpen {
			readIdle = readOpen
		}
	}
	return Config{
		WriterDSN: writer, ReadDSN: read, SamePrimary: read == writer,
		WriterMaxOpenConns: writeOpen, ReadMaxOpenConns: readOpen,
		WriterMaxIdleConns: base.MaxIdleConns - readIdle, ReadMaxIdleConns: readIdle,
		ConnMaxLifetime: base.ConnMaxLifetime, ConnMaxIdleTime: base.ConnMaxIdleTime,
		PingTimeout: base.PingTimeout, ReplayTimeout: 2 * time.Second,
	}, nil
}

func hasAlternateHost(cfg *pgx.ConnConfig) bool {
	for _, fallback := range cfg.Fallbacks {
		if fallback.Host != cfg.Host || fallback.Port != cfg.Port {
			return true
		}
	}
	return false
}

func parseStaticEndpoint(dsn string) (*pgx.ConnConfig, error) {
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL endpoint configuration")
	}
	if parsed.Host == "" || hasAlternateHost(parsed) {
		return nil, fmt.Errorf("PostgreSQL endpoint must name one static host; multi-host and failover routing are unsupported")
	}
	return parsed, nil
}

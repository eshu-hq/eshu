// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// Stage identifies the bounded operation whose duration a reader observer
// receives. It never contains SQL text, endpoint names, or user data.
type Stage string

// Outcome classifies success, failure, deadline, or caller cancellation using
// a closed value set with no credentials or unbounded error strings.
type Outcome string

// Reader access stages identify the measured writer and reader operations.
// Each value is a closed, low-cardinality telemetry dimension.
const (
	StageWriterCheckpoint Stage = "writer_checkpoint"
	StageReaderBorrow     Stage = "reader_borrow"
	StageReaderIdentity   Stage = "reader_identity"
	StageReaderReplay     Stage = "reader_replay"
	StageBusinessQuery    Stage = "business_query"
)

// Reader access outcomes classify each observed stage without exposing an
// error string, DSN, SQL statement, or credential as a telemetry dimension.
const (
	OutcomeOK       Outcome = "ok"
	OutcomeError    Outcome = "error"
	OutcomeDeadline Outcome = "deadline"
	OutcomeCanceled Outcome = "canceled"
)

// Observer receives role, stage, outcome, and duration for pool and replay
// diagnostics. Implementations must return promptly and avoid secret labels.
type Observer interface {
	Observe(role string, stage Stage, outcome Outcome, duration time.Duration)
}

// Access owns distinct writer and reader pools for one API or MCP process.
// Writer is exposed for authorization, audit, and mutation paths; business
// reads use Reader, which does not expose its underlying sql.DB.
type Access struct {
	writer        *sql.DB
	reader        *sql.DB
	samePrimary   bool
	replayTimeout time.Duration
	observer      Observer
}

// Open initializes both pools and pings each within the shared startup bound.
func Open(ctx context.Context, cfg Config, observer Observer) (*Access, error) {
	if cfg.WriterDSN == "" || cfg.ReadDSN == "" || cfg.WriterMaxOpenConns < 1 || cfg.ReadMaxOpenConns < 1 ||
		cfg.WriterMaxIdleConns < 0 || cfg.ReadMaxIdleConns < 0 || cfg.WriterMaxIdleConns > cfg.WriterMaxOpenConns ||
		cfg.ReadMaxIdleConns > cfg.ReadMaxOpenConns || cfg.SamePrimary != (cfg.WriterDSN == cfg.ReadDSN) ||
		cfg.ReplayTimeout <= 0 || cfg.PingTimeout <= 0 {
		return nil, errors.New("invalid Postgres reader access configuration")
	}
	writerCfg, err := parseStaticEndpoint(cfg.WriterDSN)
	if err != nil {
		return nil, err
	}
	readCfg, err := parseStaticEndpoint(cfg.ReadDSN)
	if err != nil {
		return nil, err
	}
	// API/MCP writer calls include auth and admin mutations that do not keep
	// infra inventory derived rows in step. They use an ordinary pgx session.
	writer := stdlib.OpenDB(*writerCfg)
	writer.SetMaxOpenConns(cfg.WriterMaxOpenConns)
	writer.SetMaxIdleConns(cfg.WriterMaxIdleConns)
	writer.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	writer.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	if readCfg.RuntimeParams == nil {
		readCfg.RuntimeParams = map[string]string{}
	}
	// The session default is set at startup on every physical connection,
	// including reconnects. A conflicting DSN option cannot turn it off.
	readCfg.RuntimeParams["default_transaction_read_only"] = "on"
	// target_session_attrs=read-write inspects transaction_read_only and would
	// reject this deliberately read-only session even on the intended host.
	// Select by physical role on connection; the request fence checks again.
	if cfg.SamePrimary {
		readCfg.ValidateConnect = pgconn.ValidateConnectTargetSessionAttrsPrimary
	} else {
		readCfg.ValidateConnect = pgconn.ValidateConnectTargetSessionAttrsStandby
	}
	reader := stdlib.OpenDB(*readCfg)
	reader.SetMaxOpenConns(cfg.ReadMaxOpenConns)
	reader.SetMaxIdleConns(cfg.ReadMaxIdleConns)
	reader.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	reader.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	access := &Access{writer: writer, reader: reader, samePrimary: cfg.SamePrimary, replayTimeout: cfg.ReplayTimeout, observer: observer}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := writer.PingContext(pingCtx); err != nil {
		_ = access.Close()
		return nil, fmt.Errorf("writer PostgreSQL ping: %w", err)
	}
	if err := reader.PingContext(pingCtx); err != nil {
		_ = access.Close()
		return nil, fmt.Errorf("reader PostgreSQL ping: %w", err)
	}
	return access, nil
}

// Writer returns the write-capable pool for authorization, audits, and writes.
func (a *Access) Writer() *sql.DB { return a.writer }

// Reader returns the fenced Queryer without exposing the raw reader pool.
func (a *Access) Reader() db.Queryer { return fencedQueryer{access: a} }

// Stats reports both pool states under closed role names.
func (a *Access) Stats() (writer, reader sql.DBStats) { return a.writer.Stats(), a.reader.Stats() }

// Ping checks connectivity of both pools for readiness probes.
func (a *Access) Ping(ctx context.Context) error {
	if err := a.writer.PingContext(ctx); err != nil {
		return fmt.Errorf("writer PostgreSQL unavailable: %w", err)
	}
	if err := a.reader.PingContext(ctx); err != nil {
		return fmt.Errorf("reader PostgreSQL unavailable: %w", err)
	}
	return nil
}

// Close releases both pools, including when one close reports an error.
func (a *Access) Close() error { return errors.Join(a.reader.Close(), a.writer.Close()) }

func (a *Access) observe(role string, stage Stage, started time.Time, err error) {
	if a.observer == nil {
		return
	}
	outcome := OutcomeOK
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		outcome = OutcomeDeadline
	case errors.Is(err, context.Canceled):
		outcome = OutcomeCanceled
	case err != nil:
		outcome = OutcomeError
	}
	a.observer.Observe(role, stage, outcome, time.Since(started))
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
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
	writer             *sql.DB
	reader             *sql.DB
	readerHasFallbacks bool
	samePrimary        bool
	replayTimeout      time.Duration
	observer           Observer
	identity           physicalIdentity
	snapshotSetGate    chan struct{}
	readerPermits      chan struct{}
	querySequence      atomic.Int64
	readerMembers      []physicalReaderMember
	nextReader         atomic.Uint64
}

// Open validates physical writer and reader identity before exposing either pool.
func Open(ctx context.Context, cfg Config, observer Observer) (*Access, error) {
	if cfg.WriterDSN == "" || cfg.ReadDSN == "" || cfg.WriterMaxOpenConns < 1 || cfg.ReadMaxOpenConns < 1 ||
		cfg.WriterMaxIdleConns < 0 || cfg.ReadMaxIdleConns < 0 || cfg.WriterMaxIdleConns > cfg.WriterMaxOpenConns ||
		cfg.ReadMaxIdleConns > cfg.ReadMaxOpenConns || cfg.SamePrimary != (cfg.WriterDSN == cfg.ReadDSN) ||
		cfg.ReplayTimeout <= 0 || cfg.PingTimeout <= 0 {
		return nil, errors.New("invalid Postgres reader access configuration")
	}
	if err := validateExpectedSystemID(cfg.ExpectedSystemID); err != nil {
		return nil, err
	}
	writerCfg, err := parsePhysicalEndpoint(cfg.WriterDSN)
	if err != nil {
		return nil, err
	}
	readCfg, err := parsePhysicalEndpoint(cfg.ReadDSN)
	if err != nil {
		return nil, err
	}
	if len(cfg.ReadMembers) > 0 && (cfg.SamePrimary || len(readCfg.Fallbacks) > 0 || cfg.ReadMaxOpenConns/len(cfg.ReadMembers) < 4) {
		return nil, errors.New("invalid physical reader member configuration")
	}
	if len(cfg.ReadMembers) > 0 {
		if err := validateReaderMembers(cfg.ReadMembers); err != nil {
			return nil, err
		}
	}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	identity, err := bootstrapPhysicalWriter(pingCtx, writerCfg, cfg.ExpectedSystemID)
	if err != nil {
		return nil, privateFailure(failureWriterIdentity, err)
	}
	// The bootstrap connection is closed before either pool is exposed.
	writerCfg.ValidateConnect = writerValidator(identity)
	writer := openWriterPool(writerCfg, cfg.Logger)
	writer.SetMaxOpenConns(cfg.WriterMaxOpenConns)
	writer.SetMaxIdleConns(cfg.WriterMaxIdleConns)
	writer.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	writer.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	if readCfg.RuntimeParams == nil {
		readCfg.RuntimeParams = map[string]string{}
	}
	readCfg.RuntimeParams["default_transaction_read_only"] = "on"
	readCfg.ValidateConnect = readerValidator(identity, cfg.SamePrimary)
	var reader *sql.DB
	var members []physicalReaderMember
	if len(cfg.ReadMembers) > 0 {
		members, err = openReaderMembers(pingCtx, cfg, readCfg, identity)
		if err != nil {
			_ = writer.Close()
			return nil, privateFailure(failureReaderPing, err)
		}
		reader = members[0].pool
	} else {
		reader = stdlib.OpenDB(*readCfg, stdlib.OptionBeforeConnect(stdlib.RandomizeHostOrderFunc))
		reader.SetMaxOpenConns(cfg.ReadMaxOpenConns)
		reader.SetMaxIdleConns(cfg.ReadMaxIdleConns)
		reader.SetConnMaxLifetime(cfg.ConnMaxLifetime)
		reader.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}
	access := &Access{writer: writer, reader: reader, readerMembers: members, readerHasFallbacks: len(readCfg.Fallbacks) > 0, samePrimary: cfg.SamePrimary, replayTimeout: cfg.ReplayTimeout, observer: observer, identity: identity, snapshotSetGate: make(chan struct{}, 1), readerPermits: make(chan struct{}, cfg.ReadMaxOpenConns)}
	access.snapshotSetGate <- struct{}{}
	for range cfg.ReadMaxOpenConns {
		access.readerPermits <- struct{}{}
	}
	if err := writer.PingContext(pingCtx); err != nil {
		_ = access.Close()
		return nil, privateFailure(failureWriterPing, err)
	}
	if err := access.pingReader(pingCtx); err != nil {
		_ = access.Close()
		return nil, privateFailure(failureReaderPing, err)
	}
	return access, nil
}

// Writer returns the write-capable pool for authorization, audits, and writes.
func (a *Access) Writer() *sql.DB { return a.writer }

// Reader returns guarded row, cursor, and snapshot reads without exposing a raw
// pool. Native multi-host fallback DSNs do not advertise snapshot sets;
// explicit direct-member fleets select one physical member per set.
func (a *Access) Reader() db.ReadStore {
	reader := fencedQueryer{access: a}
	if a.readerHasFallbacks {
		return snapshotlessReadStore{ReadStore: reader}
	}
	return reader
}

type snapshotlessReadStore struct{ db.ReadStore }

// Stats reports the writer pool and aggregate reader-member pool state.
func (a *Access) Stats() (writer, reader sql.DBStats) {
	return a.writer.Stats(), a.aggregateReaderStats()
}

// Ping checks the writer and at least one qualified reader for readiness.
func (a *Access) Ping(ctx context.Context) error {
	if err := a.writer.PingContext(ctx); err != nil {
		return privateFailure(failureWriterPing, err)
	}
	return a.pingReader(ctx)
}

func (a *Access) pingReader(ctx context.Context) error {
	if a.readerPermits != nil {
		select {
		case <-a.readerPermits:
			defer func() { a.readerPermits <- struct{}{} }()
		case <-ctx.Done():
			return privateFailure(failureReaderPing, ctx.Err())
		}
	}
	if len(a.readerMembers) > 0 {
		for _, index := range a.memberOrder(1) {
			if a.readerMembers[index].pool.PingContext(ctx) == nil {
				return nil
			}
		}
		return privateFailure(failureReaderPing, ErrReaderUnavailable)
	}
	if err := a.reader.PingContext(ctx); err != nil {
		return privateFailure(failureReaderPing, err)
	}
	return nil
}

// Close releases both pools, including when one close reports an error.
func (a *Access) Close() error {
	if len(a.readerMembers) > 0 {
		return privateFailure(failurePoolClose, errors.Join(closeReaderMembers(a.readerMembers), a.writer.Close()))
	}
	return privateFailure(failurePoolClose, errors.Join(a.reader.Close(), a.writer.Close()))
}

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

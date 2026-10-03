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

type memberAttemptObserver interface {
	ObserveMemberAttempt(ordinal int, outcome Outcome)
}

// ContextObserver is an optional extension of Observer. An Observer that also
// implements it receives the request context through ObserveContext instead of
// Observe, so a span or histogram sample for a stage can join the request that
// paid for it (#7545). An Observer without it keeps receiving Observe, with no
// request identity, exactly as before. ObserveContext carries the same closed
// role, stage, and outcome values and the same secret-free duration; the
// context is for correlation only and an implementation must not read request
// data from it. Access calls exactly one of the two methods per observation.
type ContextObserver interface {
	Observer
	ObserveContext(ctx context.Context, role string, stage Stage, outcome Outcome, duration time.Duration)
}

// Access owns distinct writer and reader pools for one API or MCP process.
// Writer is exposed for authorization, audit, and mutation paths; business
// reads use Reader, which does not expose its underlying sql.DB.
type Access struct {
	writer               *sql.DB
	reader               *sql.DB
	readerHasFallbacks   bool
	samePrimary          bool
	replayTimeout        time.Duration
	pingTimeout          time.Duration
	observer             Observer
	identity             physicalIdentity
	snapshotSetGate      chan struct{}
	readerPermits        chan struct{}
	querySequence        atomic.Int64
	readerMembers        []physicalReaderMember
	readerInventoryCount int
	allocator            *readerAllocator
	nextReader           atomic.Uint64
}

// Open validates physical writer and reader identity before exposing either pool.
func Open(ctx context.Context, cfg Config, observer Observer) (*Access, error) {
	openStarted := time.Now()
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
	if len(cfg.ReadMembers) > 0 && (cfg.SamePrimary || len(readCfg.Fallbacks) > 0 || cfg.ReadMaxOpenConns/len(cfg.ReadMembers) < 4 || cfg.ReadMaxIdleConns/len(cfg.ReadMembers) < 4) {
		return nil, errors.New("invalid physical reader member configuration")
	}
	if len(cfg.ReadMembers) > 0 {
		if err := validateReaderMembers(cfg.ReadMembers); err != nil {
			return nil, err
		}
	}
	totalBudget := cfg.PingTimeout - time.Since(openStarted)
	if callerDeadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(callerDeadline); remaining < totalBudget {
			totalBudget = remaining
		}
	}
	if totalBudget <= 0 {
		return nil, context.DeadlineExceeded
	}
	pingCtx, cancel := context.WithTimeout(ctx, totalBudget)
	defer cancel()
	writerCtx := pingCtx
	stageBudget := totalBudget / 3
	cancelWriter := func() {}
	if len(cfg.ReadMembers) > 0 {
		writerCtx, cancelWriter = context.WithTimeout(pingCtx, stageBudget)
	}
	identity, err := bootstrapPhysicalWriter(writerCtx, writerCfg, cfg.ExpectedSystemID)
	cancelWriter()
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
		members, err = openReaderMembers(pingCtx, stageBudget, cfg, readCfg, identity)
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
	access := &Access{writer: writer, reader: reader, readerMembers: members, readerInventoryCount: len(cfg.ReadMembers), readerHasFallbacks: len(readCfg.Fallbacks) > 0, samePrimary: cfg.SamePrimary, replayTimeout: cfg.ReplayTimeout, pingTimeout: cfg.PingTimeout, observer: observer, identity: identity, snapshotSetGate: make(chan struct{}, 1)}
	access.snapshotSetGate <- struct{}{}
	if len(members) > 0 {
		caps := make([]int, len(members))
		for i := range members {
			caps[i] = members[i].maxOpen
		}
		access.allocator = newReaderAllocator(caps, cfg.ReadMaxOpenConns)
	} else {
		access.readerPermits = make(chan struct{}, cfg.ReadMaxOpenConns)
		for range cfg.ReadMaxOpenConns {
			access.readerPermits <- struct{}{}
		}
	}
	if err := writer.PingContext(pingCtx); err != nil {
		_ = access.Close()
		return nil, privateFailure(failureWriterPing, err)
	}
	readerPingCtx := pingCtx
	if len(members) > 0 {
		readerPingCtx, err = access.ContextWithCheckpoint(pingCtx)
		if err != nil {
			_ = access.Close()
			return nil, privateFailure(failureReaderPing, err)
		}
	}
	if err := access.pingReader(readerPingCtx); err != nil {
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
	if len(a.readerMembers) > 0 {
		bounded, cancel := context.WithTimeout(ctx, a.pingTimeout)
		defer cancel()
		if err := a.writer.PingContext(bounded); err != nil {
			return privateFailure(failureWriterPing, err)
		}
		checkpointCtx, err := a.ContextWithCheckpoint(bounded)
		if err != nil {
			return privateFailure(failureReaderPing, err)
		}
		return a.pingReader(checkpointCtx)
	}
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
		conn, err := a.borrowFleet(ctx)
		if err != nil {
			return privateFailure(failureReaderPing, err)
		}
		return privateFailure(failureReaderPing, conn.Close())
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

// observe reports one finished stage. When ctx carries a db.StageTimings
// accumulator, the guarded-reader stages are summed into it so the request can
// log which stage it paid for (#7545); this happens with or without an
// observer. A missing accumulator and observer cost one ctx.Value lookup.
func (a *Access) observe(ctx context.Context, role string, stage Stage, started time.Time, err error) {
	timings := db.StageTimingsFrom(ctx)
	if timings == nil && a.observer == nil {
		return
	}
	elapsed := time.Since(started)
	if role == "reader" {
		if readerStage, ok := requestReaderStage(stage); ok {
			timings.Add(readerStage, elapsed)
		}
	}
	if a.observer == nil {
		return
	}
	outcome := readerOutcome(err)
	if contextual, ok := a.observer.(ContextObserver); ok {
		contextual.ObserveContext(ctx, role, stage, outcome, elapsed)
		return
	}
	a.observer.Observe(role, stage, outcome, elapsed)
}

func (a *Access) observeMemberAttempt(ordinal int, err error) {
	observer, ok := a.observer.(memberAttemptObserver)
	if !ok {
		return
	}
	outcome := readerOutcome(err)
	switch readerFailureClass(err) {
	case readerFailureFatal:
		outcome = OutcomeError
	case readerFailureCanceled:
		outcome = OutcomeCanceled
	case readerFailureNeutral, readerFailureTransient:
	}
	observer.ObserveMemberAttempt(ordinal, outcome)
}

func readerOutcome(err error) Outcome {
	outcome := OutcomeOK
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		outcome = OutcomeDeadline
	case errors.Is(err, context.Canceled):
		outcome = OutcomeCanceled
	case err != nil:
		outcome = OutcomeError
	}
	return outcome
}

// requestReaderStage maps the four guarded-reader stages to the per-request
// accumulator's closed set. The writer checkpoint stage has no accumulator
// slot: it runs before the reader fence and is not reader time.
func requestReaderStage(stage Stage) (db.ReaderStage, bool) {
	switch stage {
	case StageReaderBorrow:
		return db.ReaderStageBorrow, true
	case StageReaderIdentity:
		return db.ReaderStageIdentity, true
	case StageReaderReplay:
		return db.ReaderStageReplay, true
	case StageBusinessQuery:
		return db.ReaderStageBusinessQuery, true
	default:
		return 0, false
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// writerLineageSQL reads the primary's insert timeline and flushed WAL
// position. Both functions raise an error during recovery, so this statement
// runs only on a connection already proven to be a writable primary; it is
// never part of the shared physicalMetadataSQL that standbys also execute.
const writerLineageSQL = `SELECT pg_walfile_name(pg_current_wal_insert_lsn()), pg_current_wal_flush_lsn()::text`

// errLineageRaced reports that another dial published a different primary
// incarnation while this dial was reading metadata. Its observation is stale,
// so it neither publishes nor latches; wrapping driver.ErrBadConn lets
// database/sql dial again against the newly published identity.
var errLineageRaced = fmt.Errorf("PostgreSQL writer identity changed during validation: %w", driver.ErrBadConn)

// writerIdentity is one accepted primary incarnation and its WAL timeline.
type writerIdentity struct {
	physicalIdentity
	timeline string
}

// lineageObservation is the writer-only lineage evidence read from one dial.
type lineageObservation struct {
	timeline string
	flush    uint64
}

// writerLineage is the one identity shared by the writer validator, the
// same-primary reader validator, and the checkpoint. A dial whose postmaster
// incarnation equals the published one is accepted with no extra query. A
// different incarnation is accepted, and published before the dial returns,
// only when the system identifier, database, insert timeline, and flushed WAL
// watermark prove a restart of the same primary. A timeline or watermark
// failure latches the Access to ErrWrongTopology until the process restarts.
type writerLineage struct {
	current   atomic.Pointer[writerIdentity]
	watermark atomic.Uint64
	latched   atomic.Bool
	// mu serializes only the in-memory decide-and-publish step. It is never
	// held across a network round trip.
	mu     sync.Mutex
	logger *slog.Logger
}

func newWriterLineage(id physicalIdentity, observed lineageObservation, logger *slog.Logger) *writerLineage {
	if logger == nil {
		logger = slog.Default()
	}
	lineage := &writerLineage{logger: logger}
	lineage.current.Store(&writerIdentity{physicalIdentity: id, timeline: observed.timeline})
	lineage.watermark.Store(observed.flush)
	return lineage
}

// identity returns the currently published primary identity. Open always
// builds a lineage; a nil one (a partially built test Access) has the zero
// identity.
func (l *writerLineage) identity() *writerIdentity {
	if l == nil {
		return &writerIdentity{}
	}
	return l.current.Load()
}

// isLatched reports whether a promoted or restored primary latched this Access
// to ErrWrongTopology. A nil lineage has never latched.
func (l *writerLineage) isLatched() bool { return l != nil && l.latched.Load() }

// raiseWatermark moves the flushed-LSN watermark forward and never back.
// Concurrent checkpoints call it, so it is a compare-and-swap max.
func (l *writerLineage) raiseWatermark(lsn uint64) {
	for {
		current := l.watermark.Load()
		if lsn <= current || l.watermark.CompareAndSwap(current, lsn) {
			return
		}
	}
}

// validate checks one freshly dialed primary connection against the published
// identity captured as base before its metadata query ran.
func (l *writerLineage) validate(ctx context.Context, conn *pgconn.PgConn, base *writerIdentity, id physicalIdentity) error {
	if l.isLatched() {
		return ErrWrongTopology
	}
	if id.systemID != base.systemID || id.database != base.database {
		return ErrWrongTopology
	}
	if id.incarnation == base.incarnation {
		return nil
	}
	observed, err := readLineageRaw(ctx, conn)
	if err != nil {
		return fmt.Errorf("writer lineage metadata: %w", err)
	}
	return l.admit(base, id, observed)
}

// admit decides whether a changed incarnation is a restart of the published
// primary and, if so, publishes it. It holds mu only for in-memory work.
func (l *writerLineage) admit(base *writerIdentity, id physicalIdentity, observed lineageObservation) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	current := l.current.Load()
	if current != base {
		if current.incarnation == id.incarnation {
			return nil
		}
		l.logger.Info("postgres writer identity raced", slog.String("event_name", "postgres.writer.lineage"), slog.String("outcome", "raced"))
		return errLineageRaced
	}
	if l.isLatched() {
		return ErrWrongTopology
	}
	watermark := l.watermark.Load()
	if observed.timeline != base.timeline || observed.flush < watermark {
		l.latched.Store(true)
		reason := "timeline"
		if observed.timeline == base.timeline {
			reason = "flush_below_watermark"
		}
		l.logger.Error("postgres writer lineage rejected; access latched to wrong topology",
			slog.String("event_name", "postgres.writer.lineage"), slog.String("outcome", "latched"), slog.String("reason", reason),
			slog.String("previous_incarnation", base.incarnation), slog.String("observed_incarnation", id.incarnation),
			slog.String("previous_timeline", base.timeline), slog.String("observed_timeline", observed.timeline),
			slog.String("watermark_lsn", formatLSN(watermark)), slog.String("observed_flush_lsn", formatLSN(observed.flush)))
		return ErrWrongTopology
	}
	l.raiseWatermark(observed.flush)
	l.current.Store(&writerIdentity{physicalIdentity: id, timeline: observed.timeline})
	l.logger.Info("postgres writer restarted on the same lineage; identity republished",
		slog.String("event_name", "postgres.writer.lineage"), slog.String("outcome", "accepted"),
		slog.String("previous_incarnation", base.incarnation), slog.String("observed_incarnation", id.incarnation),
		slog.String("timeline", observed.timeline), slog.String("observed_flush_lsn", formatLSN(observed.flush)))
	return nil
}

// checkpointTopology asserts that a writer checkpoint was read on the
// published identity. A matching checkpoint raises the flush watermark.
func (l *writerLineage) checkpointTopology(point checkpoint, flush string) error {
	if l.isLatched() {
		return ErrWrongTopology
	}
	current := l.current.Load()
	if point.systemID != current.systemID || point.database != current.database || point.incarnation != current.incarnation {
		return ErrWrongTopology
	}
	lsn, err := parseLSN(flush)
	if err != nil {
		return fmt.Errorf("writer checkpoint flush position: %w", err)
	}
	l.raiseWatermark(lsn)
	return nil
}

func readLineageRaw(ctx context.Context, conn *pgconn.PgConn) (lineageObservation, error) {
	results, err := conn.Exec(ctx, writerLineageSQL).ReadAll()
	if err != nil {
		return lineageObservation{}, err
	}
	if len(results) != 1 || len(results[0].Rows) != 1 || len(results[0].Rows[0]) != 2 {
		return lineageObservation{}, errors.New("invalid PostgreSQL lineage metadata")
	}
	row := results[0].Rows[0]
	return parseLineage(string(row[0]), string(row[1]))
}

func readLineagePGX(ctx context.Context, conn *pgx.Conn) (lineageObservation, error) {
	var walFile, flush string
	if err := conn.QueryRow(ctx, writerLineageSQL).Scan(&walFile, &flush); err != nil {
		return lineageObservation{}, err
	}
	return parseLineage(walFile, flush)
}

// parseLineage takes the timeline from the first eight hexadecimal digits of a
// WAL segment file name. pg_split_walfile_name needs PostgreSQL 15 or newer.
func parseLineage(walFile, flush string) (lineageObservation, error) {
	if len(walFile) != 24 || !isHex(walFile) {
		return lineageObservation{}, errors.New("invalid PostgreSQL WAL file name")
	}
	lsn, err := parseLSN(flush)
	if err != nil {
		return lineageObservation{}, err
	}
	return lineageObservation{timeline: strings.ToUpper(walFile[:8]), flush: lsn}, nil
}

func isHex(value string) bool {
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return value != ""
}

// parseLSN converts PostgreSQL's "high/low" hexadecimal LSN text to a number.
func parseLSN(text string) (uint64, error) {
	high, low, ok := strings.Cut(text, "/")
	if !ok || !isHex(high) || !isHex(low) || len(high) > 8 || len(low) > 8 {
		return 0, errors.New("invalid PostgreSQL LSN")
	}
	hi, err := strconv.ParseUint(high, 16, 32)
	if err != nil {
		return 0, errors.New("invalid PostgreSQL LSN")
	}
	lo, err := strconv.ParseUint(low, 16, 32)
	if err != nil {
		return 0, errors.New("invalid PostgreSQL LSN")
	}
	return hi<<32 | lo, nil
}

func formatLSN(lsn uint64) string { return fmt.Sprintf("%X/%X", lsn>>32, lsn&0xFFFFFFFF) }

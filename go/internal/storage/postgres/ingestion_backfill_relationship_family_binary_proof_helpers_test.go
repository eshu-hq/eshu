// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	relationshipFamilyBinaryProofCandidateDatabase = "ifa_relationship_family_retained_proof"
	relationshipFamilyBinaryProofBaselineDatabase  = "ifa_relationship_family_retained_baseline"
	relationshipFamilyBinaryProofCandidateConfirm  = "isolated-write-proof"
	relationshipFamilyBinaryProofBaselineConfirm   = "isolated-baseline-proof"
)

type relationshipFamilyBinaryProofOverlapTracker struct {
	current atomic.Int64
	maximum atomic.Int64
}

func (t *relationshipFamilyBinaryProofOverlapTracker) begin() {
	active := t.current.Add(1)
	for {
		peak := t.maximum.Load()
		if active <= peak || t.maximum.CompareAndSwap(peak, active) {
			return
		}
	}
}

func (t *relationshipFamilyBinaryProofOverlapTracker) end() {
	t.current.Add(-1)
}

func (t *relationshipFamilyBinaryProofOverlapTracker) active() int {
	return int(t.current.Load())
}

func (t *relationshipFamilyBinaryProofOverlapTracker) peak() int {
	return int(t.maximum.Load())
}

type relationshipFamilyBinaryProofDB struct {
	SQLDB
	queryOverride string
	readCalls     relationshipFamilyBinaryProofOverlapTracker
	readCursors   relationshipFamilyBinaryProofOverlapTracker
	writeTx       relationshipFamilyBinaryProofOverlapTracker
	writeCalls    relationshipFamilyBinaryProofOverlapTracker
	queryTasks    atomic.Int64
	loadedFacts   atomic.Int64
	loadedIDs     relationshipFamilyBinaryProofFactIDs
}

func (db *relationshipFamilyBinaryProofDB) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (Rows, error) {
	if query != listDeferredScopedRelationshipFactRecordsQuery {
		return db.SQLDB.QueryContext(ctx, query, args...)
	}

	db.queryTasks.Add(1)
	db.readCalls.begin()
	delegatedQuery := query
	if db.queryOverride != "" {
		delegatedQuery = db.queryOverride
	}
	rows, err := db.SQLDB.QueryContext(ctx, delegatedQuery, args...)
	db.readCalls.end()
	if err != nil {
		return nil, err
	}

	db.readCursors.begin()
	return &relationshipFamilyBinaryProofRows{
		Rows: rows,
		onScan: func(factID string) error {
			if strings.TrimSpace(factID) == "" {
				return fmt.Errorf("deferred proof query returned an empty fact_id")
			}
			db.loadedFacts.Add(1)
			db.loadedIDs.add(factID)
			return nil
		},
		onClose: db.readCursors.end,
	}, nil
}

type relationshipFamilyBinaryProofFactIDs struct {
	mu         sync.Mutex
	ids        map[string]struct{}
	duplicates int64
}

func (s *relationshipFamilyBinaryProofFactIDs) add(factID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ids == nil {
		s.ids = make(map[string]struct{})
	}
	if _, exists := s.ids[factID]; exists {
		s.duplicates++
		return
	}
	s.ids[factID] = struct{}{}
}

func (s *relationshipFamilyBinaryProofFactIDs) snapshot() (map[string]struct{}, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]struct{}, len(s.ids))
	for factID := range s.ids {
		result[factID] = struct{}{}
	}
	return result, s.duplicates
}

func (db *relationshipFamilyBinaryProofDB) Begin(ctx context.Context) (Transaction, error) {
	tx, err := db.SQLDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	db.writeTx.begin()
	return &relationshipFamilyBinaryProofTx{
		Transaction: tx,
		onDone:      db.writeTx.end,
		writeCalls:  &db.writeCalls,
	}, nil
}

type relationshipFamilyBinaryProofRows struct {
	Rows
	once    sync.Once
	onScan  func(string) error
	onClose func()
}

func (r *relationshipFamilyBinaryProofRows) Scan(dest ...any) error {
	if err := r.Rows.Scan(dest...); err != nil {
		return err
	}
	if len(dest) == 0 {
		return fmt.Errorf("deferred proof query scan has no fact_id destination")
	}
	factID, ok := dest[0].(*string)
	if !ok {
		return fmt.Errorf("deferred proof query fact_id destination has type %T, want *string", dest[0])
	}
	return r.onScan(*factID)
}

func (r *relationshipFamilyBinaryProofRows) Close() error {
	err := r.Rows.Close()
	r.once.Do(r.onClose)
	return err
}

type relationshipFamilyBinaryProofTx struct {
	Transaction
	once       sync.Once
	onDone     func()
	writeCalls *relationshipFamilyBinaryProofOverlapTracker
}

func (tx *relationshipFamilyBinaryProofTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	tx.writeCalls.begin()
	defer tx.writeCalls.end()
	return tx.Transaction.ExecContext(ctx, query, args...)
}

func (tx *relationshipFamilyBinaryProofTx) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (Rows, error) {
	tx.writeCalls.begin()
	defer tx.writeCalls.end()
	return tx.Transaction.QueryContext(ctx, query, args...)
}

func (tx *relationshipFamilyBinaryProofTx) Commit() error {
	err := tx.Transaction.Commit()
	tx.once.Do(tx.onDone)
	return err
}

func (tx *relationshipFamilyBinaryProofTx) Rollback() error {
	err := tx.Transaction.Rollback()
	tx.once.Do(tx.onDone)
	return err
}

func parseRelationshipFamilyBinaryProofPositiveInt(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse positive integer %q: %w", raw, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("value must be positive, got %d", value)
	}
	return value, nil
}

func validateRelationshipFamilyBinaryProofDatabase(database, schema, mode, confirm string) error {
	var wantDatabase, wantConfirm string
	switch mode {
	case "baseline":
		wantDatabase = relationshipFamilyBinaryProofBaselineDatabase
		wantConfirm = relationshipFamilyBinaryProofBaselineConfirm
	case "candidate":
		wantDatabase = relationshipFamilyBinaryProofCandidateDatabase
		wantConfirm = relationshipFamilyBinaryProofCandidateConfirm
	default:
		return fmt.Errorf("binary proof mode = %q, want baseline or candidate", mode)
	}
	if database != wantDatabase {
		return fmt.Errorf("refusing %s binary proof against database %q; want %q", mode, database, wantDatabase)
	}
	if schema != "public" {
		return fmt.Errorf("refusing binary proof in schema %q; want public", schema)
	}
	if confirm != wantConfirm {
		return fmt.Errorf("refusing %s binary proof without confirmation %q", mode, wantConfirm)
	}
	return nil
}

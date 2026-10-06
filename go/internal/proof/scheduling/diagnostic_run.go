// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type diagnosticCapture struct {
	name        string
	rowCount    int
	probeHash   string
	arrivalHash string
	pools       map[poolKey]diagnosticPool
	pages       [2]diagnosticPage
}

func diagnosticDatabaseFailure(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return fmt.Errorf("PostgreSQL SQLSTATE %s; details suppressed", pgErr.Code)
	}
	return errors.New("database operation failed; details suppressed")
}

func replayDiagnosticAssembly(ctx context.Context, payload []byte, assemble func(context.Context, []byte) (diagnosticPage, error)) ([2]diagnosticPage, error) {
	var pages [2]diagnosticPage
	for index := range pages {
		page, err := assemble(ctx, payload)
		if err != nil {
			return pages, fmt.Errorf("direct assembly replay %d: %w", index+1, err)
		}
		pages[index] = page
	}
	return pages, nil
}

func queryAssembledRows(ctx context.Context, query func(context.Context, string, ...any) (pgx.Rows, error), payload []byte) ([]assembledDiagnosticRow, error) {
	// Both proof modes use the unchanged production SQL and typed row scan.
	result, err := query(ctx, codetopicparallel.AssemblySQL(candidateCap), string(payload), 26, 0)
	if err != nil {
		return nil, fmt.Errorf("query direct assembly: %w", diagnosticDatabaseFailure(err))
	}
	defer result.Close()
	rows := make([]assembledDiagnosticRow, 0, 26)
	for result.Next() {
		if len(rows) == 26 {
			return nil, fmt.Errorf("direct assembly exceeded 26-row page bound")
		}
		var row assembledDiagnosticRow
		if err := result.Scan(&row.SourceKind, &row.RepoID, &row.RelativePath, &row.EntityID,
			&row.EntityName, &row.EntityType, &row.Language, &row.StartLine,
			&row.EndLine, &row.MatchedTerms, &row.Score, &row.PoolTruncated); err != nil {
			return nil, fmt.Errorf("scan direct assembly: %w", diagnosticDatabaseFailure(err))
		}
		rows = append(rows, row)
	}
	result.Close()
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("iterate direct assembly: %w", diagnosticDatabaseFailure(err))
	}
	return rows, nil
}

func assembleDiagnosticPage(ctx context.Context, tx pgx.Tx, payload []byte) (diagnosticPage, error) {
	rows, err := queryAssembledRows(ctx, tx.Query, payload)
	if err != nil {
		return diagnosticPage{}, err
	}
	return summarizeDiagnosticPage(rows)
}

func captureDiagnosticRoute(ctx context.Context, txs []pgx.Tx, workload dynamicWorkload, name string, candidate bool) (diagnosticCapture, error) {
	var rows []codetopicparallel.ProbeRow
	var err error
	if candidate {
		rows, err = readDynamicTerms(ctx, txs, workload)
	} else {
		rows, err = readBaselineTerms(ctx, txs, workload)
	}
	if err != nil {
		return diagnosticCapture{}, fmt.Errorf("capture %s probe: %w", name, diagnosticDatabaseFailure(err))
	}
	if len(rows) > 2*candidateCap*len(workload.terms) {
		return diagnosticCapture{}, fmt.Errorf("capture %s exceeded probe-row bound", name)
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return diagnosticCapture{}, fmt.Errorf("encode %s probe payload: %w", name, err)
	}
	if len(payload) > oracleMaxBytes {
		return diagnosticCapture{}, fmt.Errorf("capture %s exceeded payload byte bound", name)
	}
	pools, err := summarizeDiagnosticPools(rows, workload.terms, candidateCap)
	if err != nil {
		return diagnosticCapture{}, fmt.Errorf("summarize %s probe pools: %w", name, err)
	}
	probeHash, err := hashRows(rows)
	if err != nil {
		return diagnosticCapture{}, fmt.Errorf("hash %s probe rows: %w", name, err)
	}
	pages, err := replayDiagnosticAssembly(ctx, payload, func(ctx context.Context, payload []byte) (diagnosticPage, error) {
		return assembleDiagnosticPage(ctx, txs[0], payload)
	})
	if err != nil {
		return diagnosticCapture{}, fmt.Errorf("capture %s page: %w", name, err)
	}
	arrivalSum := sha256.Sum256(payload)
	return diagnosticCapture{
		name:        name,
		rowCount:    len(rows),
		probeHash:   probeHash,
		arrivalHash: hex.EncodeToString(arrivalSum[:]),
		pools:       pools,
		pages:       pages,
	}, nil
}

func writeDiagnosticCapture(out io.Writer, capture diagnosticCapture, terms []string) error {
	if _, err := fmt.Fprintf(out,
		"diagnostic_capture route=%s rows=%d probe_multiset_sha256=%s probe_arrival_sha256=%s replay_equal=%t\n",
		capture.name, capture.rowCount, capture.probeHash, capture.arrivalHash,
		capture.pages[0].fullHash == capture.pages[1].fullHash); err != nil {
		return fmt.Errorf("write diagnostic route summary: %w", err)
	}
	for index, page := range capture.pages {
		if _, err := fmt.Fprintf(out,
			"diagnostic_assembly route=%s replay=%d visible_sha256=%s lookahead_present=%t lookahead_sha256=%s full_sha256=%s\n",
			capture.name, index+1, page.visibleHash, page.lookaheadPresent,
			page.lookaheadHash, page.fullHash); err != nil {
			return fmt.Errorf("write diagnostic assembly summary: %w", err)
		}
	}
	for _, term := range terms {
		for _, kind := range []string{"entity", "file"} {
			pool := capture.pools[poolKey{kind: kind, term: term}]
			if _, err := fmt.Fprintf(out,
				"diagnostic_pool route=%s term=%s source=%s count=%d capped=%t multiset_sha256=%s arrival_sha256=%s\n",
				capture.name, term, kind, pool.count, pool.capped, pool.multisetHash, pool.arrivalHash); err != nil {
				return fmt.Errorf("write diagnostic pool summary: %w", err)
			}
		}
	}
	return nil
}

func writeDiagnosticPair(out io.Writer, left, right diagnosticCapture, collation, provider, locale string) error {
	poolMultisetEqual := true
	poolArrivalEqual := true
	anyCapped := false
	for key, leftPool := range left.pools {
		rightPool := right.pools[key]
		poolMultisetEqual = poolMultisetEqual && leftPool.multisetHash == rightPool.multisetHash
		poolArrivalEqual = poolArrivalEqual && leftPool.arrivalHash == rightPool.arrivalHash
		anyCapped = anyCapped || leftPool.capped || rightPool.capped
	}
	leftPage, rightPage := left.pages[0], right.pages[0]
	lookaheadEqual := leftPage.lookaheadPresent == rightPage.lookaheadPresent && leftPage.lookaheadHash == rightPage.lookaheadHash
	if _, err := fmt.Fprintf(out,
		"diagnostic_pair left=%s right=%s pool_multiset_equal=%t pool_arrival_equal=%t any_pool_capped=%t visible_equal=%t lookahead_equal=%t full_page_equal=%t\n",
		left.name, right.name, poolMultisetEqual, poolArrivalEqual, anyCapped,
		leftPage.visibleHash == rightPage.visibleHash, lookaheadEqual, leftPage.fullHash == rightPage.fullHash); err != nil {
		return fmt.Errorf("write diagnostic pair summary: %w", err)
	}
	_, err := fmt.Fprintln(out, formatDiagnosticFirstDifference(left.name, right.name, leftPage, rightPage, collation, provider, locale))
	if err != nil {
		return fmt.Errorf("write diagnostic first difference: %w", err)
	}
	return nil
}

func runFixedDiagnosticCase(ctx context.Context, connections []*pgx.Conn, workload dynamicWorkload, out io.Writer) (resultErr error) {
	started := time.Now()
	txs := make([]pgx.Tx, 0, 4)
	defer func() {
		resultErr = errors.Join(resultErr, rollbackDynamicTransactions(txs))
	}()
	for _, conn := range connections {
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return fmt.Errorf("begin diagnostic read-only transaction: %w", err)
		}
		txs = append(txs, tx)
	}
	if len(txs) != 4 {
		return fmt.Errorf("diagnostic requires exactly four readers")
	}
	var snapshotID string
	if err := txs[0].QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&snapshotID); err != nil {
		return fmt.Errorf("export diagnostic snapshot: %w", err)
	}
	if !validSnapshotID(snapshotID) {
		return fmt.Errorf("invalid diagnostic snapshot identifier")
	}
	for _, tx := range txs[1:] {
		if _, err := tx.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+snapshotID+"'"); err != nil {
			return fmt.Errorf("import diagnostic snapshot: %w", err)
		}
	}
	for _, tx := range txs {
		if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '5s'"); err != nil {
			return fmt.Errorf("bound diagnostic statement: %w", err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '1s'"); err != nil {
			return fmt.Errorf("bound diagnostic lock wait: %w", err)
		}
	}
	var collation, provider, locale string
	if err := txs[0].QueryRow(ctx, `SELECT datcollate, datlocprovider::text, COALESCE(datlocale, '') FROM pg_database WHERE datname = current_database()`).Scan(&collation, &provider, &locale); err != nil {
		return fmt.Errorf("read diagnostic database collation: %w", err)
	}
	routes := []struct {
		name      string
		candidate bool
	}{
		{name: "baseline1"},
		{name: "candidate1", candidate: true},
		{name: "candidate2", candidate: true},
		{name: "baseline2"},
	}
	captures := make([]diagnosticCapture, 0, len(routes))
	for _, route := range routes {
		capture, err := captureDiagnosticRoute(ctx, txs, workload, route.name, route.candidate)
		if err != nil {
			return err
		}
		captures = append(captures, capture)
	}
	var report bytes.Buffer
	for _, capture := range captures {
		if err := writeDiagnosticCapture(&report, capture, workload.terms); err != nil {
			return fmt.Errorf("write diagnostic capture: %w", err)
		}
		if _, err := fmt.Fprintln(&report, formatDiagnosticFirstDifference(capture.name+"_replay1", capture.name+"_replay2", capture.pages[0], capture.pages[1], collation, provider, locale)); err != nil {
			return fmt.Errorf("buffer diagnostic replay difference: %w", err)
		}
	}
	for _, pair := range [][2]int{{0, 3}, {1, 2}, {0, 1}, {3, 2}} {
		if err := writeDiagnosticPair(&report, captures[pair[0]], captures[pair[1]], collation, provider, locale); err != nil {
			return fmt.Errorf("write diagnostic pair: %w", err)
		}
	}
	if _, err := fmt.Fprintf(&report, "diagnostic_snapshot_age_ms=%d timing=not_run\n", time.Since(started).Milliseconds()); err != nil {
		return fmt.Errorf("buffer diagnostic snapshot age: %w", err)
	}
	if _, err := io.Copy(out, &report); err != nil {
		return fmt.Errorf("publish diagnostic report: %w", err)
	}
	return nil
}

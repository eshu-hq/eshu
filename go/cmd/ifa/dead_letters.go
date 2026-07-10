// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/eshu-hq/eshu/go/internal/ifa"
)

// deadLettersSQL selects the durable dead-letter rows this verb reports.
// Unlike go/cmd/golden-corpus-gate/drains.go's factWorkItemsResidualSQL
// (which deliberately counts 'dead_letter' AS residual — a drained pipeline
// has no dead letters), this verb's own purpose IS to read those rows: Ifá
// P3's failure-path determinism (step 3a,
// docs/internal/design/4389-ifa-conformance-platform.md) needs the dead-letter
// SET itself, not a residual count, to compare across worker counts.
const deadLettersSQL = `
SELECT work_item_id, stage, domain, COALESCE(failure_class, '')
FROM fact_work_items
WHERE status = 'dead_letter'
ORDER BY work_item_id`

// deadLetterQuerier reads the current durable dead-letter set. Defined here
// where it is consumed so tests can fake it without a database, mirroring
// go/cmd/golden-corpus-gate/drains.go's drainQuerier interface.
type deadLetterQuerier interface {
	DeadLetters(ctx context.Context) ([]ifa.DeadLetterRecord, error)
}

// sqlDeadLetterQuerier reads the dead-letter set from Postgres.
type sqlDeadLetterQuerier struct {
	db *sql.DB
}

// DeadLetters runs deadLettersSQL and returns one ifa.DeadLetterRecord per
// row. The SQL already orders by work_item_id, but the caller still runs
// ifa.SortDeadLetterRecords defensively so this verb's determinism does not
// depend on the query's ORDER BY surviving a future edit unnoticed.
func (q sqlDeadLetterQuerier) DeadLetters(ctx context.Context) ([]ifa.DeadLetterRecord, error) {
	rows, err := q.db.QueryContext(ctx, deadLettersSQL)
	if err != nil {
		return nil, fmt.Errorf("query fact_work_items dead letters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []ifa.DeadLetterRecord
	for rows.Next() {
		var r ifa.DeadLetterRecord
		if err := rows.Scan(&r.WorkItemID, &r.Stage, &r.Domain, &r.FailureClass); err != nil {
			return nil, fmt.Errorf("scan fact_work_items dead letter row: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fact_work_items dead letters: %w", err)
	}
	return records, nil
}

// deadLettersOptions holds the parsed command-line inputs for one
// "ifa dead-letters" run.
type deadLettersOptions struct {
	out         string
	postgresDSN string
}

func parseDeadLettersFlags(args []string, stderr io.Writer) (deadLettersOptions, error) {
	fs := flag.NewFlagSet("ifa dead-letters", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o deadLettersOptions
	fs.StringVar(&o.out, "out", "", "path to write the dead-letter set JSON (default: stdout)")
	fs.StringVar(&o.postgresDSN, "postgres-dsn", "", "Postgres DSN to read from (default: ESHU_POSTGRES_DSN/ESHU_FACT_STORE_DSN/ESHU_CONTENT_STORE_DSN)")
	if err := fs.Parse(args); err != nil {
		return deadLettersOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	return o, nil
}

// runDeadLettersCommand implements `ifa dead-letters`: the Ifá P3
// failure-path-determinism read verb (issue #4396, ADR step 3a). It opens
// Postgres using the same DSN precedence "ifa drive" uses
// (driveOpenPostgres), reads the durable fact_work_items dead-letter set, and
// prints it as deterministic sorted JSON — the artifact a determinism-matrix
// runner (or an operator) diffs across independent runs with
// ifa.DeadLetterSetsEqual.
//
// This verb is read-only: it issues one SELECT and no schema DDL or write.
func runDeadLettersCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	o, err := parseDeadLettersFlags(args, stderr)
	if err != nil {
		return err
	}

	db, err := driveOpenPostgres(ctx, o.postgresDSN)
	if err != nil {
		return fmt.Errorf("ifa dead-letters: open postgres: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	return renderDeadLetters(ctx, sqlDeadLetterQuerier{db: db}, o, stdout)
}

// renderDeadLetters queries q, sorts the result deterministically, and writes
// it as indented JSON to o.out or stdout. Split from runDeadLettersCommand so
// tests can exercise the query-to-output path with a fake deadLetterQuerier,
// without a live Postgres connection.
func renderDeadLetters(ctx context.Context, q deadLetterQuerier, o deadLettersOptions, stdout io.Writer) error {
	records, err := q.DeadLetters(ctx)
	if err != nil {
		return fmt.Errorf("ifa dead-letters: %w", err)
	}
	ifa.SortDeadLetterRecords(records)
	if records == nil {
		records = []ifa.DeadLetterRecord{}
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("ifa dead-letters: marshal dead-letter set: %w", err)
	}
	data = append(data, '\n')

	if o.out == "" {
		if _, err := stdout.Write(data); err != nil {
			return fmt.Errorf("ifa dead-letters: write report: %w", err)
		}
		return nil
	}
	// #nosec G306 -- a diagnostic report, not a secret; world-readable is
	// fine and matches this command's other report-output permissions.
	if err := os.WriteFile(o.out, data, 0o644); err != nil {
		return fmt.Errorf("ifa dead-letters: write %s: %w", o.out, err)
	}
	return nil
}

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/jackc/pgx/v5"
)

func p1ExplainQuery() (string, []any) {
	req := codequery.CodeTopicInvestigationRequest{
		Terms: []string{"content", "function", "path", "service"},
	}
	probe, args := codetopicparallel.ProbeSQL(
		req, candidateCap, []string{"eshu_require_content_substring_indexes_ready()"}, nil,
	)
	return "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON, SETTINGS) " + probe, args
}

func runP1Explain(ctx context.Context, config *pgx.ConnConfig, outputPath string) error {
	if outputPath == "" {
		return fmt.Errorf("missing private plan output path")
	}
	conn, err := pgx.ConnectConfig(ctx, config.Copy())
	if err != nil {
		return fmt.Errorf("connect to reader: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := conn.Close(cleanupCtx); err != nil {
			fmt.Fprintf(os.Stderr, "reader close failed: %v\n", err)
		}
	}()
	var recovery bool
	var readOnly string
	if err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery(), current_setting('transaction_read_only')").Scan(&recovery, &readOnly); err != nil {
		return fmt.Errorf("verify reader state: %w", err)
	}
	if err := requireReadOnlyReader(recovery, readOnly); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin read-only transaction: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanupCtx); err != nil {
			fmt.Fprintf(os.Stderr, "read-only rollback failed: %v\n", err)
		}
	}()
	if err := tx.QueryRow(ctx, "SELECT pg_is_in_recovery(), current_setting('transaction_read_only')").Scan(&recovery, &readOnly); err != nil {
		return fmt.Errorf("verify read-only transaction: %w", err)
	}
	if err := requireReadOnlyReader(recovery, readOnly); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '5s'"); err != nil {
		return fmt.Errorf("set statement timeout: %w", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '1s'"); err != nil {
		return fmt.Errorf("set lock timeout: %w", err)
	}
	query, args := p1ExplainQuery()
	queryHash := sha256.Sum256([]byte(query))
	fmt.Printf("p1_query_sha256=%x terms=content,function,path,service cap=%d\n", queryHash, candidateCap)
	var plan []byte
	if err := tx.QueryRow(ctx, query, args...).Scan(&plan); err != nil {
		return fmt.Errorf("explain p1: %w", err)
	}
	if !json.Valid(plan) {
		return fmt.Errorf("invalid EXPLAIN JSON")
	}
	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create private plan artifact: %w", err)
	}
	if _, err := f.Write(append(plan, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("write private plan artifact: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close private plan artifact: %w", err)
	}
	fmt.Printf("p1_explain_saved_bytes=%d\n", len(plan))
	return nil
}

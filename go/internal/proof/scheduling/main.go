// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/jackc/pgx/v5"
)

const candidateCap = 250

var terms = []string{
	"config", "content", "deployment", "environment",
	"file", "function", "handler", "module",
	"package", "path", "repo", "repository",
	"resource", "service", "source", "system",
}

func partitionTerms(input []string, count int) [][]string {
	groups := make([][]string, count)
	for i, term := range input {
		groups[i%count] = append(groups[i%count], term)
	}
	return groups
}

func hashRows(rows []codetopicparallel.ProbeRow) (string, error) {
	encoded := make([]string, 0, len(rows))
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			return "", fmt.Errorf("marshal probe row: %w", err)
		}
		encoded = append(encoded, string(data))
	}
	slices.Sort(encoded)
	sum := sha256.Sum256([]byte(strings.Join(encoded, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

func exactDifference(ctx context.Context, tx pgx.Tx, left, right []codetopicparallel.ProbeRow) (int64, int64, error) {
	leftPayload, err := json.Marshal(left)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal left probe rows: %w", err)
	}
	rightPayload, err := json.Marshal(right)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal right probe rows: %w", err)
	}
	const query = `WITH left_rows AS (
		SELECT * FROM jsonb_to_recordset($1::jsonb) AS p(
			source_kind text, matched_term text, repo_id text, relative_path text,
			entity_id text, entity_name text, entity_type text, language text,
			start_line bigint, end_line bigint)
	), right_rows AS (
		SELECT * FROM jsonb_to_recordset($2::jsonb) AS p(
			source_kind text, matched_term text, repo_id text, relative_path text,
			entity_id text, entity_name text, entity_type text, language text,
			start_line bigint, end_line bigint)
	)
	SELECT (SELECT count(*) FROM (SELECT * FROM left_rows EXCEPT ALL SELECT * FROM right_rows) d),
	       (SELECT count(*) FROM (SELECT * FROM right_rows EXCEPT ALL SELECT * FROM left_rows) d)`
	var leftOnly, rightOnly int64
	if err := tx.QueryRow(ctx, query, string(leftPayload), string(rightPayload)).Scan(&leftOnly, &rightOnly); err != nil {
		return leftOnly, rightOnly, fmt.Errorf("compare persisted probe rows: %w", err)
	}
	return leftOnly, rightOnly, nil
}

func hashStrings(rows []string) string {
	data, _ := json.Marshal(rows)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func runGroupFiltered(ctx context.Context, tx pgx.Tx, group []string, filters []string, baseArgs []any) ([]codetopicparallel.ProbeRow, time.Duration, error) {
	req := codequery.CodeTopicInvestigationRequest{Terms: group}
	query, args := codetopicparallel.ProbeSQL(req, candidateCap, filters, baseArgs)
	started := time.Now()
	result, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, time.Since(started), err
	}
	defer result.Close()
	rows := make([]codetopicparallel.ProbeRow, 0)
	for result.Next() {
		var row codetopicparallel.ProbeRow
		if err := result.Scan(&row.SourceKind, &row.MatchedTerm, &row.RepoID, &row.RelativePath,
			&row.EntityID, &row.EntityName, &row.EntityType, &row.Language,
			&row.StartLine, &row.EndLine); err != nil {
			return nil, time.Since(started), err
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		return nil, time.Since(started), err
	}
	return rows, time.Since(started), nil
}

func assemblyHash(ctx context.Context, tx pgx.Tx, rows []codetopicparallel.ProbeRow) (string, error) {
	payload, err := json.Marshal(rows)
	if err != nil {
		return "", fmt.Errorf("marshal rows for assembly: %w", err)
	}
	query := "SELECT to_jsonb(t)::text FROM (" +
		codetopicparallel.AssemblySQL(candidateCap) + ") t"
	result, err := tx.Query(ctx, query, string(payload), 26, 0)
	if err != nil {
		return "", fmt.Errorf("query assembled page: %w", err)
	}
	defer result.Close()
	page := make([]string, 0, 26)
	for result.Next() {
		var row string
		if err := result.Scan(&row); err != nil {
			return "", fmt.Errorf("scan assembled page row: %w", err)
		}
		page = append(page, row)
	}
	if err := result.Err(); err != nil {
		return "", fmt.Errorf("iterate assembled page rows: %w", err)
	}
	return hashStrings(page), nil
}

func validSnapshotID(id string) bool {
	if id == "" {
		return false
	}
	for _, char := range id {
		if !strings.ContainsRune("0123456789abcdefABCDEF-", char) {
			return false
		}
	}
	return true
}

func validateProofMode(mode string) error {
	if mode != "fixed_canonical" && mode != "fixed_diagnostic" {
		return fmt.Errorf("only fixed_canonical and fixed_diagnostic proof modes are supported")
	}
	return nil
}

func main() {
	secret, err := io.ReadAll(io.LimitReader(os.Stdin, 8192))
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read credential")
		os.Exit(1)
	}
	config, err := pgx.ParseConfig(strings.TrimSpace(string(secret)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid credential")
		os.Exit(1)
	}
	mode := os.Getenv("ESHU7033_MODE")
	if err := validateProofMode(mode); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := configureProofTargetPort(config, mode, os.Getenv("ESHU7033_SOCKET_DIR"), os.Getenv("ESHU7033_FIXED_PORT")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	var runErr error
	if mode == "fixed_diagnostic" {
		runErr = runFixedDiagnostic(ctx, config, os.Getenv("ESHU7033_EXPECTED_DATABASE"))
	} else {
		runErr = runFixedCanonical(ctx, config, os.Getenv("ESHU7033_EXPECTED_DATABASE"))
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "fixed proof stopped: %v\n", runErr)
		os.Exit(1)
	}
}

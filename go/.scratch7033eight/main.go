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

func balancedGroups() [][]string {
	return [][]string{
		{"content", "module", "path", "system"},
		{"config", "function", "package", "service"},
		{"environment", "handler", "repository", "source"},
		{"deployment", "file", "repo", "resource"},
	}
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

func runGroup(ctx context.Context, tx pgx.Tx, group []string) ([]codetopicparallel.ProbeRow, time.Duration, error) {
	return runGroupFiltered(ctx, tx, group, nil, nil)
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

func readGroups(ctx context.Context, tx pgx.Tx, count int, reverse bool) ([]codetopicparallel.ProbeRow, []time.Duration, error) {
	return readTermGroups(ctx, tx, partitionTerms(terms, count), reverse)
}

func readTermGroups(ctx context.Context, tx pgx.Tx, groups [][]string, reverse bool) ([]codetopicparallel.ProbeRow, []time.Duration, error) {
	rows := make([]codetopicparallel.ProbeRow, 0, 8000)
	timings := make([]time.Duration, len(groups))
	for step := range groups {
		index := step
		if reverse {
			index = len(groups) - step - 1
		}
		groupRows, elapsed, err := runGroup(ctx, tx, groups[index])
		if err != nil {
			return nil, nil, fmt.Errorf("partition %d: %w", index, err)
		}
		rows = append(rows, groupRows...)
		timings[index] = elapsed
	}
	return rows, timings, nil
}

func run(ctx context.Context, conn *pgx.Conn) error {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin proof snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '5s'"); err != nil {
		return fmt.Errorf("bound proof statement: %w", err)
	}
	started := time.Now()
	baselineRows, baselineFirst, err := readGroups(ctx, tx, 4, false)
	if err != nil {
		return err
	}
	groups := partitionTerms(terms, 8)
	mode := "eight"
	if os.Getenv("ESHU7033_MODE") == "balanced" {
		mode = "balanced"
		groups = balancedGroups()
	}
	candidateRows, first, err := readTermGroups(ctx, tx, groups, false)
	if err != nil {
		return err
	}
	fourHash, err := hashRows(baselineRows)
	if err != nil {
		return err
	}
	eightHash, err := hashRows(candidateRows)
	if err != nil {
		return err
	}
	leftOnly, rightOnly, err := exactDifference(ctx, tx, baselineRows, candidateRows)
	if err != nil {
		return err
	}
	fmt.Printf("mode=%s snapshot_parity probe_baseline_rows=%d probe_candidate_rows=%d left_only=%d right_only=%d hash_equal=%t\n",
		mode, len(baselineRows), len(candidateRows), leftOnly, rightOnly, fourHash == eightHash)
	if leftOnly != 0 || rightOnly != 0 || fourHash != eightHash {
		return fmt.Errorf("probe rows differ; stopping before timing pass")
	}
	fourPageHash, err := assemblyHash(ctx, tx, baselineRows)
	if err != nil {
		return err
	}
	eightPageHash, err := assemblyHash(ctx, tx, candidateRows)
	if err != nil {
		return err
	}
	fmt.Printf("snapshot_parity assembled_page_equal=%t\n", fourPageHash == eightPageHash)
	if fourPageHash != eightPageHash {
		return fmt.Errorf("assembled pages differ; stopping before timing pass")
	}
	_, second, err := readTermGroups(ctx, tx, groups, true)
	if err != nil {
		return err
	}
	for i := range first {
		fmt.Printf("partition=%d terms=%s first_ms=%.3f reverse_ms=%.3f\n", i,
			strings.Join(groups[i], ","),
			float64(first[i].Microseconds())/1000, float64(second[i].Microseconds())/1000)
	}
	if mode == "balanced" {
		_, baselineSecond, err := readGroups(ctx, tx, 4, true)
		if err != nil {
			return err
		}
		for i := range baselineFirst {
			fmt.Printf("baseline_partition=%d first_ms=%.3f reverse_ms=%.3f\n", i,
				float64(baselineFirst[i].Microseconds())/1000, float64(baselineSecond[i].Microseconds())/1000)
		}
	}
	fmt.Printf("snapshot_age_ms=%d\n", time.Since(started).Milliseconds())
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
	if err := configureProofTargetPort(config, mode, os.Getenv("ESHU7033_SOCKET_DIR"), os.Getenv("ESHU7033_FIXED_PORT")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	timeout := 60 * time.Second
	if os.Getenv("ESHU7033_MODE") == "diagnostic_punctuation" || os.Getenv("ESHU7033_MODE") == "timing_canonical" || os.Getenv("ESHU7033_MODE") == "fixed_canonical" {
		timeout = 75 * time.Second
	} else if os.Getenv("ESHU7033_MODE") == "explain_p1" {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if os.Getenv("ESHU7033_MODE") == "fixed_canonical" {
		if err := runFixedCanonical(ctx, config, os.Getenv("ESHU7033_EXPECTED_DATABASE")); err != nil {
			fmt.Fprintf(os.Stderr, "fixed canonical proof stopped: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if mode := os.Getenv("ESHU7033_MODE"); mode == "parallel" || mode == "parallel8" {
		candidateCount := 4
		if mode == "parallel8" {
			candidateCount = 8
		}
		if err := runParallel(ctx, config, candidateCount); err != nil {
			fmt.Fprintf(os.Stderr, "parallel proof stopped: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if os.Getenv("ESHU7033_MODE") == "deterministic" {
		conn, err := pgx.ConnectConfig(ctx, config)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot connect to reader")
			os.Exit(1)
		}
		defer func() { _ = conn.Close(context.Background()) }()
		if err := runDeterministicScreen(ctx, conn); err != nil {
			fmt.Fprintf(os.Stderr, "deterministic proof stopped: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if os.Getenv("ESHU7033_MODE") == "diagnostic_punctuation" {
		if err := runDynamicScreen(ctx, config, "punctuation", false); err != nil {
			fmt.Fprintf(os.Stderr, "punctuation diagnostic stopped: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if os.Getenv("ESHU7033_MODE") == "timing_canonical" {
		if err := runDynamicScreen(ctx, config, "canonical", true); err != nil {
			fmt.Fprintf(os.Stderr, "canonical timing proof stopped: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if os.Getenv("ESHU7033_MODE") == "explain_p1" {
		if err := runP1Explain(ctx, config, os.Getenv("ESHU7033_PLAN_PATH")); err != nil {
			fmt.Fprintf(os.Stderr, "p1 plan diagnostic stopped: %v\n", err)
			os.Exit(1)
		}
		return
	}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot connect to reader")
		os.Exit(1)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if err := run(ctx, conn); err != nil {
		fmt.Fprintf(os.Stderr, "proof stopped: %v\n", err)
		os.Exit(1)
	}
}

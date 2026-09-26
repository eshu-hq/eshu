// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

// secretPlanParamType maps a builder argument to its Postgres parameter type:
// the builder emits only strings, ints, and one repository grant array.
func secretPlanParamType(arg any) string {
	switch arg.(type) {
	case string:
		return "text"
	case int:
		return "integer"
	default:
		return "text[]"
	}
}

func explainGenericSecretPlan(
	ctx context.Context, conn *sql.Conn, name string, req codequery.HardcodedSecretInvestigationRequest,
) (string, error) {
	query, args := hardcodedSecretInvestigationQuery(req)
	types := make([]string, len(args))
	for i, arg := range args {
		types[i] = secretPlanParamType(arg)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PREPARE %s (%s) AS %s", name, strings.Join(types, ", "), query)); err != nil {
		return "", fmt.Errorf("prepare %s: %w", name, err)
	}
	literals := make([]string, len(args))
	for i, arg := range args {
		switch v := arg.(type) {
		case string:
			literals[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
		case int:
			literals[i] = fmt.Sprint(v)
		default:
			literals[i] = "ARRAY['repo-1','repo-2']"
		}
	}
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("EXPLAIN (COSTS OFF) EXECUTE %s(%s)", name, strings.Join(literals, ", ")))
	if err != nil {
		return "", fmt.Errorf("explain %s: %w", name, err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}
		plan.WriteString(line + "\n")
	}
	return plan.String(), rows.Err()
}

// TestHardcodedSecretInvestigationGenericPlansUsePrimaryKeyLive proves the
// read stays an ordered primary-key scan that stops at LIMIT even under a
// forced generic plan, which pgx statement caching can reach. It also proves
// no plan touches content_files (#7125). The side table is loaded through the
// insert trigger on 20,000 files so the planner sees a realistic cardinality.
func TestHardcodedSecretInvestigationGenericPlansUsePrimaryKeyLive(t *testing.T) {
	ctx, db := openSecretProofDatabase(t)
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT 'repo-' || (i % 40), 'src/f_' || lpad(i::text, 6, '0') || CASE WHEN i % 11 = 0 THEN '_test.go' ELSE '.go' END,
       CASE WHEN i % 7 = 0 THEN E'package x\npassword = "value' || i || E'abcdef"\nAKIA' || lpad(i::text, 16, '0')
            ELSE E'package x\nfunc F() {}' END,
       md5(i::text), 3, CASE WHEN i % 5 = 0 THEN 'python' ELSE 'go' END, now()
FROM generate_series(1, 20000) AS i`); err != nil {
		t.Fatalf("seed content_files: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE content_file_secret_lines`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	var rows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_file_secret_lines`).Scan(&rows); err != nil || rows < 4000 {
		t.Fatalf("side table rows = %d (err %v), want a realistic load", rows, err)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatal(err)
	}
	for i, item := range []secretProofRequest{
		{"default", codequery.HardcodedSecretInvestigationRequest{Limit: 26}},
		{"repo scoped", codequery.HardcodedSecretInvestigationRequest{RepoID: "repo-7", Limit: 26}},
		{"grant scoped", codequery.HardcodedSecretInvestigationRequest{AllowedRepositoryIDs: []string{"repo-1", "repo-2"}, Limit: 26}},
		{"rare kind", codequery.HardcodedSecretInvestigationRequest{FindingKinds: []string{"slack_token"}, Limit: 201}},
		{"language", codequery.HardcodedSecretInvestigationRequest{Language: "python", Limit: 26}},
		{"include suppressed offset", codequery.HardcodedSecretInvestigationRequest{IncludeSuppressed: true, Offset: 1000, Limit: 201}},
	} {
		plan, err := explainGenericSecretPlan(ctx, conn, fmt.Sprintf("secret_plan_%d", i), item.req)
		if err != nil {
			t.Fatalf("%s: %v", item.name, err)
		}
		if !strings.HasPrefix(plan, "Limit\n") {
			t.Errorf("%s generic plan is not a Limit at the root:\n%s", item.name, plan)
		}
		if !strings.Contains(plan, "Index Scan using content_file_secret_lines_pkey") {
			t.Errorf("%s generic plan does not use the primary key:\n%s", item.name, plan)
		}
		// An Incremental Sort over the primary-key prefix (the inert finding_kind
		// tiebreak) streams and stops at LIMIT; a full Sort or Top-N would read
		// every finding first.
		for _, forbidden := range []string{"content_files", "Seq Scan", "->  Sort", "Bitmap"} {
			if strings.Contains(plan, forbidden) {
				t.Errorf("%s generic plan contains %q:\n%s", item.name, forbidden, plan)
			}
		}
	}
}

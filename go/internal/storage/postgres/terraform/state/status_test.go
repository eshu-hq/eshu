// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package statestore

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestListTerraformStateLastSerialsParsesGenerationID(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	queryer := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{
				Data: [][]any{
					{
						"hash-aaa",
						"s3",
						"lineage-aaa",
						"42",
						"terraform_state:state_snapshot:s3:hash-aaa:lineage-aaa:serial:42",
						sql.NullTime{Time: observedAt, Valid: true},
					},
					{
						"hash-bbb",
						"local",
						"lineage-bbb",
						"7",
						"terraform_state:state_snapshot:local:hash-bbb:lineage-bbb:serial:7",
						sql.NullTime{Time: observedAt.Add(-time.Hour), Valid: true},
					},
				},
			},
		},
	}

	rows, err := listTerraformStateLastSerials(context.Background(), queryer)
	if err != nil {
		t.Fatalf("listTerraformStateLastSerials() error = %v, want nil", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].SafeLocatorHash != "hash-aaa" || rows[0].Serial != 42 {
		t.Fatalf("rows[0] = %+v, want hash-aaa serial=42", rows[0])
	}
	if rows[1].SafeLocatorHash != "hash-bbb" || rows[1].Serial != 7 {
		t.Fatalf("rows[1] = %+v, want hash-bbb serial=7", rows[1])
	}
}

func TestListTerraformStateRecentWarningsBoundsLimit(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)
	queryer := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{
				Data: [][]any{
					{
						"hash-1",
						"s3",
						"state_in_vcs",
						"approved_local",
						"info",
						"accepted_guardrail",
						"git_local_file",
						"state_snapshot:s3:hash-1",
						"terraform_state:state_snapshot:s3:hash-1:lineage-1:serial:5",
						sql.NullTime{Time: observedAt, Valid: true},
					},
					{
						"hash-1",
						"s3",
						"output_value_dropped",
						"sensitive_composite_output",
						"info",
						"accepted_guardrail",
						"outputs.x",
						"state_snapshot:s3:hash-1",
						"terraform_state:state_snapshot:s3:hash-1:lineage-1:serial:5",
						sql.NullTime{Time: observedAt.Add(time.Minute), Valid: true},
					},
				},
			},
		},
	}

	rows, err := listTerraformStateRecentWarnings(context.Background(), queryer, 50)
	if err != nil {
		t.Fatalf("listTerraformStateRecentWarnings() error = %v, want nil", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].WarningKind != "state_in_vcs" {
		t.Fatalf("rows[0].WarningKind = %q, want state_in_vcs", rows[0].WarningKind)
	}
	if rows[0].Severity != "info" || rows[0].Actionability != "accepted_guardrail" {
		t.Fatalf("rows[0] classification = %q/%q, want info/accepted_guardrail", rows[0].Severity, rows[0].Actionability)
	}
	if rows[0].SourceHandle != "state_snapshot:s3:hash-1" {
		t.Fatalf("rows[0].SourceHandle = %q, want state_snapshot:s3:hash-1", rows[0].SourceHandle)
	}
	if len(queryer.Queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queryer.Queries))
	}
	if !strings.Contains(queryer.Queries[0].Query, "rank <= $1") {
		t.Fatalf("expected limit binding in query: %s", queryer.Queries[0].Query)
	}
}

func TestListTerraformStateRecentWarningsIncludesGitBackendExpressionWarnings(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 13, 15, 30, 0, 0, time.UTC)
	queryer := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{
				Data: [][]any{
					{
						"repository:r_12345678:env/backend.tf",
						"git",
						"unresolved_backend_expression",
						"missing_variable_default",
						"blocking",
						"blocking_evidence",
						"terraform_backend",
						"env/backend.tf",
						"git:repository:r_12345678:run-backend-warning",
						sql.NullTime{Time: observedAt, Valid: true},
					},
				},
			},
		},
	}

	rows, err := listTerraformStateRecentWarnings(context.Background(), queryer, 50)
	if err != nil {
		t.Fatalf("listTerraformStateRecentWarnings() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.SafeLocatorHash != "repository:r_12345678:env/backend.tf" ||
		row.BackendKind != "git" ||
		row.WarningKind != "unresolved_backend_expression" ||
		row.Reason != "missing_variable_default" ||
		row.SourceHandle != "env/backend.tf" {
		t.Fatalf("row = %+v, want Git backend-expression warning", row)
	}
	if len(queryer.Queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queryer.Queries))
	}
	query := queryer.Queries[0].Query
	for _, want := range []string{
		"collector_kind IN ('terraform_state', 'git')",
		"scope_kind IN ('state_snapshot', 'repository')",
		"unresolved_backend_expression",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("terraformStateRecentWarningsQuery missing %q:\n%s", want, query)
		}
	}
}

func TestListTerraformStateRecentWarningsAppliesContractDefaultLimit(t *testing.T) {
	t.Parallel()

	queryer := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{}}}}
	if _, err := listTerraformStateRecentWarnings(context.Background(), queryer, 0); err != nil {
		t.Fatalf("listTerraformStateRecentWarnings() error = %v, want nil", err)
	}
	if statuspkg.MaxTerraformStateRecentWarnings <= 0 {
		t.Fatalf("MaxTerraformStateRecentWarnings = %d, want positive bound", statuspkg.MaxTerraformStateRecentWarnings)
	}
}

func TestListTerraformStateLastSerialsSkipsMalformedRows(t *testing.T) {
	t.Parallel()

	queryer := &fake.ExecQueryer{QueryResponses: []fake.Rows{{
		Data: [][]any{
			{"hash-good", "s3", "lineage", "12", "terraform_state:state_snapshot:s3:hash-good:lineage:serial:12", sql.NullTime{Time: time.Date(2026, 5, 3, 1, 0, 0, 0, time.UTC), Valid: true}},
			{"hash-bad", "s3", "lineage", "not-a-number", "terraform_state:state_snapshot:s3:hash-bad:lineage:serial:bogus", sql.NullTime{Time: time.Date(2026, 5, 3, 2, 0, 0, 0, time.UTC), Valid: true}},
		},
	}}}
	rows, err := listTerraformStateLastSerials(context.Background(), queryer)
	if err != nil {
		t.Fatalf("listTerraformStateLastSerials() error = %v, want nil", err)
	}
	if len(rows) != 1 || rows[0].SafeLocatorHash != "hash-good" {
		t.Fatalf("rows = %+v, want only hash-good", rows)
	}
}

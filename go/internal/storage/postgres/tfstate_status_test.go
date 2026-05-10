package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestListTerraformStateLastSerialsParsesGenerationID(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{
						"hash-aaa",
						"s3",
						"lineage-aaa",
						"42",
						"terraform_state:state_snapshot:s3:hash-aaa:lineage-aaa:serial:42",
						observedAt,
					},
					{
						"hash-bbb",
						"local",
						"lineage-bbb",
						"7",
						"terraform_state:state_snapshot:local:hash-bbb:lineage-bbb:serial:7",
						observedAt.Add(-time.Hour),
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
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{
						"hash-1",
						"s3",
						"state_in_vcs",
						"approved_local",
						"git_local_file",
						"terraform_state:state_snapshot:s3:hash-1:lineage-1:serial:5",
						observedAt,
					},
					{
						"hash-1",
						"s3",
						"output_value_dropped",
						"sensitive_composite_output",
						"outputs.x",
						"terraform_state:state_snapshot:s3:hash-1:lineage-1:serial:5",
						observedAt.Add(time.Minute),
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
	if len(queryer.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queryer.queries))
	}
	if !strings.Contains(queryer.queries[0], "rank <= $1") {
		t.Fatalf("expected limit binding in query: %s", queryer.queries[0])
	}
}

func TestListTerraformStateRecentWarningsAppliesContractDefaultLimit(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{}}}}
	if _, err := listTerraformStateRecentWarnings(context.Background(), queryer, 0); err != nil {
		t.Fatalf("listTerraformStateRecentWarnings() error = %v, want nil", err)
	}
	if statuspkg.MaxTerraformStateRecentWarnings <= 0 {
		t.Fatalf("MaxTerraformStateRecentWarnings = %d, want positive bound", statuspkg.MaxTerraformStateRecentWarnings)
	}
}

func TestListTerraformStateLastSerialsSkipsMalformedRows(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{
		rows: [][]any{
			{"hash-good", "s3", "lineage", "12", "terraform_state:state_snapshot:s3:hash-good:lineage:serial:12", time.Date(2026, 5, 3, 1, 0, 0, 0, time.UTC)},
			{"hash-bad", "s3", "lineage", "not-a-number", "terraform_state:state_snapshot:s3:hash-bad:lineage:serial:bogus", time.Date(2026, 5, 3, 2, 0, 0, 0, time.UTC)},
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

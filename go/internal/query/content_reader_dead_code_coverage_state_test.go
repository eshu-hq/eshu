// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
)

func coverageGapRow(id, state string, generation any) []driver.Value {
	return []driver.Value{id, state, generation}
}

func readCoverageGaps(t *testing.T, request code.CrossRepoDeadCodeCoverageRequest, rows ...[]driver.Value) code.CrossRepoDeadCodeCoverage {
	t.Helper()

	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: coverageColumns, rows: rows},
	})
	got, err := NewContentReader(db).CrossRepoDeadCodeConsumerCoverage(context.Background(), request)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerCoverage() error = %v, want nil", err)
	}
	return got
}

// Each gap carries why it is a gap, the generation being waited for, and
// whether waiting can fix it (#7547). A repository with no active scope has no
// generation and is not retryable.
func TestCrossRepoDeadCodeConsumerCoverageReportsStateGenerationAndRetryable(t *testing.T) {
	t.Parallel()

	got := readCoverageGaps(t,
		code.CrossRepoDeadCodeCoverageRequest{RepositoryIDs: []string{"a", "b", "c", "d"}, RequireActiveScope: true},
		coverageGapRow("a", "no_snapshot_yet", "gen-a"),
		coverageGapRow("b", "truncated", "gen-b"),
		coverageGapRow("c", "older_epoch", "gen-c"),
		coverageGapRow("d", "no_active_scope", nil),
	)
	want := []code.CrossRepoDeadCodeCoverageGap{
		{RepositoryID: "a", State: "no_snapshot_yet", GenerationID: "gen-a", Retryable: true},
		{RepositoryID: "b", State: "truncated", GenerationID: "gen-b", Retryable: false},
		{RepositoryID: "c", State: "older_epoch", GenerationID: "gen-c", Retryable: true},
		{RepositoryID: "d", State: "no_active_scope", GenerationID: "", Retryable: false},
	}
	if len(got.Gaps) != len(want) {
		t.Fatalf("gaps = %#v, want %#v", got.Gaps, want)
	}
	for i := range want {
		if got.Gaps[i] != want[i] {
			t.Errorf("gap[%d] = %#v, want %#v", i, got.Gaps[i], want[i])
		}
	}
}

// A state the reader does not know is refused, never reported as a gap with a
// made-up retry promise.
func TestCrossRepoDeadCodeConsumerCoverageRefusesAnUnknownState(t *testing.T) {
	t.Parallel()

	for name, state := range map[string]driver.Value{"unknown": "bogus", "null": nil} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
				{columns: coverageColumns, rows: [][]driver.Value{{"a", state, "gen-a"}}},
			})
			_, err := NewContentReader(db).CrossRepoDeadCodeConsumerCoverage(context.Background(),
				code.CrossRepoDeadCodeCoverageRequest{AllRepositories: true})
			if err == nil {
				t.Fatalf("error = nil, want a refusal for state %v", state)
			}
		})
	}
}

// One repository can sit behind several scopes. The statement returns a row per
// scope for the all-repositories read, so the reader picks one: a
// non-retryable scope first (waiting cannot fix the repository while any scope
// is truncated), then the lowest generation id. The pick must not depend on the
// order the rows arrive in.
func TestCrossRepoDeadCodeConsumerCoverageMultiScopePick(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		rows [][]driver.Value
		want code.CrossRepoDeadCodeCoverageGap
	}{
		{
			name: "truncated beats a lower-generation retryable scope",
			rows: [][]driver.Value{
				coverageGapRow("m", "no_snapshot_yet", "gen-1"),
				coverageGapRow("m", "truncated", "gen-2"),
			},
			want: code.CrossRepoDeadCodeCoverageGap{RepositoryID: "m", State: "truncated", GenerationID: "gen-2"},
		},
		{
			name: "truncated beats a retryable scope in the other row order",
			rows: [][]driver.Value{
				coverageGapRow("m", "truncated", "gen-2"),
				coverageGapRow("m", "older_epoch", "gen-1"),
			},
			want: code.CrossRepoDeadCodeCoverageGap{RepositoryID: "m", State: "truncated", GenerationID: "gen-2"},
		},
		{
			name: "among retryable scopes the lowest generation wins",
			rows: [][]driver.Value{
				coverageGapRow("m", "older_epoch", "gen-9"),
				coverageGapRow("m", "no_snapshot_yet", "gen-3"),
			},
			want: code.CrossRepoDeadCodeCoverageGap{RepositoryID: "m", State: "no_snapshot_yet", GenerationID: "gen-3", Retryable: true},
		},
		{
			name: "among truncated scopes the lowest generation wins",
			rows: [][]driver.Value{
				coverageGapRow("m", "truncated", "gen-7"),
				coverageGapRow("m", "truncated", "gen-4"),
			},
			want: code.CrossRepoDeadCodeCoverageGap{RepositoryID: "m", State: "truncated", GenerationID: "gen-4"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := readCoverageGaps(t, code.CrossRepoDeadCodeCoverageRequest{AllRepositories: true}, tc.rows...)
			if len(got.Gaps) != 1 || got.Gaps[0] != tc.want {
				t.Fatalf("gaps = %#v, want one gap %#v", got.Gaps, tc.want)
			}
		})
	}
}

// Retryable is the agent's "can waiting help" answer: true only when every gap
// is known and retryable. A cut list hides gaps that may not be retryable.
func TestCrossRepoDeadCodeCoverageRetryableHint(t *testing.T) {
	t.Parallel()

	retryable := code.CrossRepoDeadCodeCoverageGap{RepositoryID: "a", State: "no_snapshot_yet", Retryable: true}
	truncated := code.CrossRepoDeadCodeCoverageGap{RepositoryID: "b", State: "truncated"}
	for name, tc := range map[string]struct {
		coverage code.CrossRepoDeadCodeCoverage
		want     bool
	}{
		"all retryable":                    {code.CrossRepoDeadCodeCoverage{Gaps: []code.CrossRepoDeadCodeCoverageGap{retryable}}, true},
		"one not retryable":                {code.CrossRepoDeadCodeCoverage{Gaps: []code.CrossRepoDeadCodeCoverageGap{retryable, truncated}}, false},
		"retryable but the list was cut":   {code.CrossRepoDeadCodeCoverage{Gaps: []code.CrossRepoDeadCodeCoverageGap{retryable}, IncompleteTruncated: true}, false},
		"complete has nothing to wait for": {code.CrossRepoDeadCodeCoverage{}, false},
	} {
		if got := tc.coverage.Retryable(); got != tc.want {
			t.Errorf("%s: Retryable() = %v, want %v", name, got, tc.want)
		}
	}
}

// The statements return the state and generation from the same probes they
// already ran: the generation is the scope's active generation, the state is a
// CASE over the watermark that joined already, and no new table appears.
func TestCrossRepoDeadCodeConsumerCoverageStatementsReturnStateAndGeneration(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ query, epoch string }{
		"named": {deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery, "$3"},
		"all":   {deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery, "$1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, want := range []string{
				"scope.active_generation_id AS generation_id",
				"WHEN watermark.scope_id IS NULL THEN 'no_snapshot_yet'",
				"WHEN watermark.truncated THEN 'truncated'",
				"WHEN watermark.verdict_schema_epoch < " + tc.epoch + "::integer THEN 'older_epoch'",
				"AS state",
			} {
				if !strings.Contains(tc.query, want) {
					t.Errorf("%s coverage SQL is missing %q", name, want)
				}
			}
			// Precedence inside one watermark: missing, then truncated, then
			// older epoch. A truncated bit outranks the epoch test.
			missing := strings.Index(tc.query, "'no_snapshot_yet'")
			truncated := strings.Index(tc.query, "'truncated'")
			older := strings.Index(tc.query, "'older_epoch'")
			if missing < 0 || missing >= truncated || truncated >= older {
				t.Errorf("%s coverage SQL state precedence is not no_snapshot_yet < truncated < older_epoch", name)
			}
			if got := strings.Count(tc.query, "FROM shared_projection_intents"); got != 0 {
				t.Errorf("%s coverage SQL reads shared_projection_intents directly %d times", name, got)
			}
		})
	}
	named := deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery
	for _, want := range []string{
		"SELECT DISTINCT ON (repository_id)",
		"'no_active_scope'",
		"ORDER BY repository_id,",
	} {
		if !strings.Contains(named, want) {
			t.Errorf("named coverage SQL is missing %q", want)
		}
	}
	if strings.Contains(deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery, "ORDER BY") {
		t.Errorf("all-repositories coverage SQL has an ORDER BY; its LIMIT must stop the scan early")
	}
}

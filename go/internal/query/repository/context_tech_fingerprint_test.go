// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// fingerprintReader answers the language-distribution and source-tool
// breakdown queries from fixed rows so QueryServiceTechFingerprint is
// exercised without a backend.
func fingerprintReader() graph.FakeRepoGraphReader {
	return graph.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "f.language IS NOT NULL"):
				return []map[string]any{
					{"language": "go", "file_count": int64(12)},
					{"language": "yaml", "file_count": int64(3)},
					{"language": "", "file_count": int64(1)},
				}, nil
			case strings.Contains(cypher, "rel.source_tool IS NOT NULL"):
				return []map[string]any{
					{"source_tool": "terraform", "edge_count": int64(5)},
				}, nil
			default:
				return nil, nil
			}
		},
	}
}

// TestQueryServiceTechFingerprintAggregatesBreakdowns pins that the service
// tech fingerprint rolls the language and source-tool rows into compact maps
// with no degraded reasons on a healthy read. Rows with an empty key are
// skipped by the breakdown builders.
func TestQueryServiceTechFingerprintAggregatesBreakdowns(t *testing.T) {
	t.Parallel()

	languages, tools, degraded := QueryServiceTechFingerprint(
		context.Background(), fingerprintReader(), map[string]any{"repo_id": "repository:r_fp"})

	if len(degraded) != 0 {
		t.Fatalf("degraded = %#v, want none", degraded)
	}
	if got, want := languages["go"], 12; got != want {
		t.Fatalf(`languages["go"] = %d, want %d`, got, want)
	}
	if got, want := languages["yaml"], 3; got != want {
		t.Fatalf(`languages["yaml"] = %d, want %d`, got, want)
	}
	if _, ok := languages[""]; ok {
		t.Fatalf("languages has empty key: %#v", languages)
	}
	if got, want := tools["terraform"], 5; got != want {
		t.Fatalf(`tools["terraform"] = %d, want %d`, got, want)
	}
}

// TestQueryServiceTechFingerprintEmptyWithoutRepoID pins that a service
// with no resolved repo_id yields an empty fingerprint instead of querying.
func TestQueryServiceTechFingerprintEmptyWithoutRepoID(t *testing.T) {
	t.Parallel()

	languages, tools, degraded := QueryServiceTechFingerprint(
		context.Background(), fingerprintReader(), map[string]any{})

	if languages != nil || tools != nil || degraded != nil {
		t.Fatalf("got (%#v, %#v, %#v), want all nil without repo_id", languages, tools, degraded)
	}
}

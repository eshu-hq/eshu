// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	cloudresource "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/resource"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// recordingSearchRunner records every probe statement the production
// readiness path sends, so the pin below reads the recorded text and never
// a copy of it.
type recordingSearchRunner struct {
	calls []recordedSearchCall
}

type recordedSearchCall struct {
	cypher string
	params map[string]any
}

func (r *recordingSearchRunner) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.calls = append(r.calls, recordedSearchCall{cypher: cypher, params: params})
	return nil, nil
}

// probeMissingDiffs runs the production infra-search readiness probes
// through a recording runner and fingerprints every recorded statement as
// a one-sided divergence, the shape the differential oracle reports for a
// Neo4j-only statement.
func probeMissingDiffs(t *testing.T) []backendconformance.DifferentialDifference {
	t.Helper()
	runner := &recordingSearchRunner{}
	if _, err := search.IndexState(context.Background(), runner); err != nil {
		t.Fatalf("IndexState: %v", err)
	}
	if _, err := search.CountUnkeyedSearchable(context.Background(), runner); err != nil {
		t.Fatalf("CountUnkeyedSearchable: %v", err)
	}
	if len(runner.calls) == 0 {
		t.Fatal("readiness probes recorded no statements")
	}
	var diffs []backendconformance.DifferentialDifference
	for _, call := range runner.calls {
		fingerprint, err := backendconformance.FingerprintStatement(call.cypher, call.params)
		if err != nil {
			t.Fatalf("FingerprintStatement: %v", err)
		}
		diffs = append(diffs, backendconformance.DifferentialDifference{
			Fingerprint: fingerprint,
			Kind:        backendconformance.DivergenceMissing,
			Detail:      "recorded on one backend only",
		})
	}
	return diffs
}

// writerDialectMissingDiffs drives the production CloudResource writer in
// both infra-search key dialects through a recording executor and
// fingerprints each emitted statement as a one-sided divergence.
func writerDialectMissingDiffs(t *testing.T) []backendconformance.DifferentialDifference {
	t.Helper()
	rows := []map[string]any{{
		"uid":                 "uid-1",
		"arn":                 "arn:aws:ec2:us-east-1:111122223333:vpc/vpc-1",
		"resource_id":         "vpc-1",
		"resource_type":       "aws_ec2_vpc",
		"name":                "main",
		"state":               "available",
		"account_id":          "111122223333",
		"region":              "us-east-1",
		"service_kind":        "vpc",
		"correlation_anchors": []string{"vpc-1"},
		"source_fact_id":      "fact",
		"stable_fact_key":     "key",
		"source_system":       "aws",
		"source_record_id":    "rec",
		"source_confidence":   "reported",
		"collector_kind":      "aws",
	}}
	var diffs []backendconformance.DifferentialDifference
	for _, dialect := range []search.Dialect{search.Off, search.On} {
		exec := &cleanupRecordingExecutor{}
		writer := cloudresource.NewCloudResourceNodeWriterWithDialect(exec, 500, dialect)
		if err := writer.WriteCloudResourceNodes(context.Background(), rows, "reducer/aws-resources"); err != nil {
			t.Fatalf("WriteCloudResourceNodes(%v): %v", dialect, err)
		}
		if len(exec.calls) == 0 {
			t.Fatalf("writer dialect %v emitted no statements", dialect)
		}
		for _, stmt := range exec.calls {
			fingerprint, err := backendconformance.FingerprintStatement(stmt.Cypher, stmt.Parameters)
			if err != nil {
				t.Fatalf("FingerprintStatement: %v", err)
			}
			diffs = append(diffs, backendconformance.DifferentialDifference{
				Fingerprint: fingerprint,
				Kind:        backendconformance.DivergenceMissing,
				Detail:      "recorded on one backend only",
			})
		}
	}
	return diffs
}

// TestAllowlistExcusesInfraSearchReadinessProbes pins the #8689 probe
// entries to the production readiness path: every recorded IndexState and
// CountUnkeyedSearchable statement must excuse as a missing-kind
// divergence. A probe-text change without an allowlist refresh fails here
// instead of red-ing the quorum gate on main.
func TestAllowlistExcusesInfraSearchReadinessProbes(t *testing.T) {
	t.Parallel()
	allow := loadBackendDivergenceAllowlist(t)
	diffs := probeMissingDiffs(t)
	if len(diffs) == 0 {
		t.Fatal("no probe divergences to pin")
	}
	for _, diff := range diffs {
		if !anyEntryMatches(allow, diff) {
			t.Errorf("no allowlist entry excuses the readiness probe %.120q; refresh specs/backend-divergence-allowlist.v1.yaml", diff.Fingerprint.Statement)
		}
	}
}

// TestAllowlistExcusesInfraSearchKeyWriterDialects pins the #8689 writer
// entries to the production writer: the NornicDB form (key clause omitted)
// and the Neo4j form (key clause appended) must each excuse as a
// missing-kind divergence.
func TestAllowlistExcusesInfraSearchKeyWriterDialects(t *testing.T) {
	t.Parallel()
	allow := loadBackendDivergenceAllowlist(t)
	for _, diff := range writerDialectMissingDiffs(t) {
		if !anyEntryMatches(allow, diff) {
			t.Errorf("no allowlist entry excuses the writer statement %.120q; refresh specs/backend-divergence-allowlist.v1.yaml", diff.Fingerprint.Statement)
		}
	}
}

// TestAllowlistExcusesInfraSearchKeyOnlyAsMissing is the seeded violation:
// the #8689 entries excuse only the one-sided recording. A results or
// failures divergence on a registered probe or writer statement must still
// fail, and so must a drifted statement (key clause stripped from a Neo4j
// form or added to a NornicDB form).
func TestAllowlistExcusesInfraSearchKeyOnlyAsMissing(t *testing.T) {
	t.Parallel()
	allow := loadBackendDivergenceAllowlist(t)
	probes := probeMissingDiffs(t)
	writers := writerDialectMissingDiffs(t)
	if len(probes) == 0 || len(writers) == 0 {
		t.Fatal("no divergences to bound")
	}
	registered := append(probes, writers...)
	for _, diff := range registered {
		for _, kind := range []string{backendconformance.DivergenceResults, backendconformance.DivergenceFailures} {
			if anyEntryMatches(allow, backendconformance.DifferentialDifference{Fingerprint: diff.Fingerprint, Kind: kind}) {
				t.Errorf("%s divergence on %.80q was excused; entries must cover only the missing kind", kind, diff.Fingerprint.Statement)
			}
		}
	}
	for _, diff := range writers {
		drifted := diff.Fingerprint
		if strings.Contains(drifted.Statement, "infra_search_joined_key") {
			// A Neo4j form with a renamed key variable is neither the
			// registered Neo4j form nor the NornicDB form.
			drifted.Statement = strings.Replace(drifted.Statement, "infra_search_joined_key", "infra_search_joined_key_drifted", 1)
		} else {
			drifted.Statement += " WITH r, 1 AS drifted_key"
		}
		if drifted.Statement == diff.Fingerprint.Statement {
			t.Fatal("seeded drift did not change the statement")
		}
		if anyEntryMatches(allow, backendconformance.DifferentialDifference{Fingerprint: drifted, Kind: backendconformance.DivergenceMissing}) {
			t.Errorf("a drifted writer statement was excused: %.120q", drifted.Statement)
		}
	}
}

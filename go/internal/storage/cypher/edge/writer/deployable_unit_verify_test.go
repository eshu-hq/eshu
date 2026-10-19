// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	deployableUnitVerifyMetric       = "eshu_dp_deployable_unit_edge_verify_total"
	deployableUnitTestEvidenceSource = "reducer/deployable-unit-correlation"
)

// graphStateProbeExecutor answers the verification probes from a fixed graph
// state: the set of present Repository ids and present (source, target) edges.
type graphStateProbeExecutor struct {
	repos    map[string]bool
	edges    map[[2]string]bool
	probeErr error
	probes   []sourcecypher.Statement
	writes   int
}

func (e *graphStateProbeExecutor) Execute(context.Context, sourcecypher.Statement) error {
	e.writes++
	return nil
}

func (e *graphStateProbeExecutor) ExecuteProbe(_ context.Context, stmt sourcecypher.Statement) (bool, error) {
	e.probes = append(e.probes, stmt)
	if e.probeErr != nil {
		return false, e.probeErr
	}
	if strings.Contains(stmt.Cypher, "CORRELATES_DEPLOYABLE_UNIT") {
		s, _ := stmt.Parameters["source_repo_id"].(string)
		t, _ := stmt.Parameters["target_repo_id"].(string)
		return e.edges[[2]string{s, t}], nil
	}
	id, _ := stmt.Parameters["repo_id"].(string)
	return e.repos[id], nil
}

// plainExecutor has no probe capability.
type plainExecutor struct{}

func (plainExecutor) Execute(context.Context, sourcecypher.Statement) error { return nil }

func verifyWriter(t *testing.T, exec sourcecypher.Executor) (*EdgeWriter, *sdkmetric.ManualReader, *bytes.Buffer) {
	t.Helper()
	instruments, reader := newStubMetrics(t)
	var logs bytes.Buffer
	w := NewEdgeWriter(exec, 0)
	w.Instruments = instruments
	w.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return w, reader, &logs
}

func verifyOutcomeCount(t *testing.T, reader *sdkmetric.ManualReader, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != deployableUnitVerifyMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", deployableUnitVerifyMetric, m.Data)
			}
			for _, point := range sum.DataPoints {
				if attributesMatch(point.Attributes, map[string]string{"outcome": outcome}) {
					total += point.Value
				}
			}
		}
	}
	return total
}

func verifyClassOf(err error) string {
	class, _ := failureClassOf(err)
	return class
}

func verifyOne(w *EdgeWriter, rows ...sharedintent.Row) error {
	if len(rows) == 0 {
		rows = []sharedintent.Row{validDeployableUnitRow()}
	}
	return w.VerifyDeployableUnitEdges(context.Background(), rows, deployableUnitTestEvidenceSource)
}

func TestVerifyDeployableUnitEdgesPresent(t *testing.T) {
	t.Parallel()

	exec := &graphStateProbeExecutor{
		repos: map[string]bool{"repo-app": true, "repo-deploy": true},
		edges: map[[2]string]bool{{"repo-app", "repo-deploy"}: true},
	}
	w, reader, _ := verifyWriter(t, exec)
	row := validDeployableUnitRow()

	// The same pair twice costs one probe: MERGE folds it into one edge.
	if err := verifyOne(w, row, row); err != nil {
		t.Fatalf("VerifyDeployableUnitEdges() error = %v, want nil", err)
	}
	if len(exec.probes) != 1 {
		t.Fatalf("probes = %d, want 1 edge probe for one distinct present edge", len(exec.probes))
	}
	if got := verifyOutcomeCount(t, reader, "present"); got != 1 {
		t.Fatalf("present outcome count = %d, want 1", got)
	}
	if exec.writes != 0 {
		t.Fatalf("verification ran %d write statement(s), want 0", exec.writes)
	}
}

// TestVerifyDeployableUnitEdgeProbeShape pins the probe shape: one
// single-pattern MATCH per edge, no multi-clause MATCH, no inline
// relationship properties (docs/public/reference/nornicdb-pitfalls.md), the
// evidence source in WHERE, read-only.
func TestVerifyDeployableUnitEdgeProbeShape(t *testing.T) {
	t.Parallel()

	stmt := buildDeployableUnitEdgeProbe("repo-app", "repo-deploy", deployableUnitTestEvidenceSource)
	want := "MATCH (:Repository {id: $source_repo_id})-[rel:CORRELATES_DEPLOYABLE_UNIT]->(:Repository {id: $target_repo_id})\n" +
		"WHERE rel.evidence_source = $evidence_source\nRETURN 1 LIMIT 1"
	if stmt.Cypher != want {
		t.Fatalf("edge probe =\n%s\nwant\n%s", stmt.Cypher, want)
	}
	if stmt.Operation != sourcecypher.OperationCanonicalProbe {
		t.Fatalf("edge probe operation = %q, want %q", stmt.Operation, sourcecypher.OperationCanonicalProbe)
	}
	if got := stmt.Parameters["evidence_source"]; got != deployableUnitTestEvidenceSource {
		t.Fatalf("edge probe evidence_source = %v", got)
	}
	node := buildRepositoryPresenceProbe("repo-deploy")
	if node.Cypher != "MATCH (:Repository {id: $repo_id})\nRETURN 1 LIMIT 1" || node.Parameters["repo_id"] != "repo-deploy" {
		t.Fatalf("node probe = %q %v", node.Cypher, node.Parameters)
	}
}

func TestVerifyDeployableUnitEdgesMissingTargetIsNonCountingTimingState(t *testing.T) {
	t.Parallel()

	exec := &graphStateProbeExecutor{repos: map[string]bool{"repo-app": true}}
	w, reader, logs := verifyWriter(t, exec)

	err := verifyOne(w)
	if got := verifyClassOf(err); got != reducer.SharedEdgeTargetNotReadyFailureClass {
		t.Fatalf("missing-target error = %v (class %q), want class %q", err, got, reducer.SharedEdgeTargetNotReadyFailureClass)
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("missing-target error = %v, want retryable", err)
	}
	var sampled interface{ SampleTarget() (string, string) }
	if !errors.As(err, &sampled) {
		t.Fatalf("missing-target error = %v, want a SampleTarget for the #7268 bound WARN", err)
	}
	if repo, _ := sampled.SampleTarget(); repo != "repo-deploy" {
		t.Fatalf("sample repo = %q, want the absent endpoint repo-deploy", repo)
	}
	if got := verifyOutcomeCount(t, reader, "missing_target"); got != 1 {
		t.Fatalf("missing_target outcome count = %d, want 1", got)
	}
	for _, want := range []string{`"verdict":"missing_target"`, `"source_repo_id":"repo-app"`, `"target_repo_id":"repo-deploy"`, `"sample_intent_id":"i-du-1"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("verify WARN missing %s:\n%s", want, logs.String())
		}
	}
}

func TestVerifyDeployableUnitEdgesMissingSourceIsTimingStateToo(t *testing.T) {
	t.Parallel()

	exec := &graphStateProbeExecutor{repos: map[string]bool{"repo-deploy": true}}
	w, _, _ := verifyWriter(t, exec)
	if got := verifyClassOf(verifyOne(w)); got != reducer.SharedEdgeTargetNotReadyFailureClass {
		t.Fatalf("missing-source error class = %q, want %q", got, reducer.SharedEdgeTargetNotReadyFailureClass)
	}
}

func TestVerifyDeployableUnitEdgesMissingEdgeWithBothNodesCounts(t *testing.T) {
	t.Parallel()

	exec := &graphStateProbeExecutor{repos: map[string]bool{"repo-app": true, "repo-deploy": true}}
	w, reader, logs := verifyWriter(t, exec)

	err := verifyOne(w)
	if got := verifyClassOf(err); got != reducer.DeployableUnitEdgeWriteUnverifiedFailureClass {
		t.Fatalf("missing-edge error = %v (class %q), want class %q", err, got, reducer.DeployableUnitEdgeWriteUnverifiedFailureClass)
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("missing-edge error = %v, want retryable", err)
	}
	if got := verifyOutcomeCount(t, reader, "missing_edge"); got != 1 {
		t.Fatalf("missing_edge outcome count = %d, want 1", got)
	}
	if !strings.Contains(logs.String(), `"verdict":"missing_edge"`) {
		t.Fatalf("verify WARN missing the missing_edge verdict:\n%s", logs.String())
	}
}

func TestVerifyDeployableUnitEdgesProbeErrorCountsWithoutClass(t *testing.T) {
	t.Parallel()

	exec := &graphStateProbeExecutor{probeErr: errors.New("graph timeout")}
	w, reader, _ := verifyWriter(t, exec)

	err := verifyOne(w)
	if err == nil || verifyClassOf(err) != "" {
		t.Fatalf("probe error = %v (class %q), want an unclassed counting error", err, verifyClassOf(err))
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("probe error = %v, want retryable", err)
	}
	if got := verifyOutcomeCount(t, reader, "probe_error"); got != 1 {
		t.Fatalf("probe_error outcome count = %d, want 1", got)
	}
}

// Rows the write path cannot route are never written, so they must not be
// verified either: they would always read as missing edges.
func TestVerifyDeployableUnitEdgesSkipsUnroutableRows(t *testing.T) {
	t.Parallel()

	exec := &graphStateProbeExecutor{}
	w, _, _ := verifyWriter(t, exec)
	row := validDeployableUnitRow()
	delete(row.Payload, "deployable_unit_key")
	if err := verifyOne(w, row); err != nil {
		t.Fatalf("VerifyDeployableUnitEdges(unroutable) error = %v, want nil", err)
	}
	if len(exec.probes) != 0 {
		t.Fatalf("probes = %d, want 0 for an unroutable row", len(exec.probes))
	}
}

func TestVerifyDeployableUnitEdgesWithoutProbeCapabilityIsUnavailable(t *testing.T) {
	t.Parallel()

	w, _, _ := verifyWriter(t, plainExecutor{})
	if err := verifyOne(w); !errors.Is(err, reducer.ErrDeployableUnitEdgeVerificationUnavailable) {
		t.Fatalf("no-probe error = %v, want ErrDeployableUnitEdgeVerificationUnavailable", err)
	}
}

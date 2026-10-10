// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/graph/capture"
)

// recordedWrite is one statement execution as the capture sink stores it.
type recordedWrite struct {
	statement  string
	parameters string
	callsite   string
}

// canonicalWrites mirror the shapes the production writers send: a MERGE on a
// uid label that also sets id, a MERGE keyed by id on an id-constrained label,
// and a dynamic property map on a uid label.
var canonicalWrites = []recordedWrite{
	{"UNWIND $rows AS row MERGE (n:Function {uid: row.uid}) SET n.id = row.uid, n.name = row.name", `{"rows":[]}`, "storage/cypher/canonical_node_cypher.go:build"},
	{"MERGE (r:Repository {id: $repo_id}) SET r.name = $name", `{"repo_id":"r"}`, "storage/cypher/canonical.go:build"},
	{"UNWIND $rows AS row MERGE (n:Class {uid: row.entity_id}) SET n.id = row.entity_id, n += row.properties", `{"rows":[]}`, "storage/cypher/semantic_entity_statements.go:build"},
	{"MATCH (n:Function {uid: $uid}) RETURN n", `{"uid":"u"}`, ""},
}

// writeWriterRecordings records writes for one backend through the real
// capture sink, so the phase test reads the on-disk format.
func writeWriterRecordings(t *testing.T, backend string, writes []recordedWrite) string {
	t.Helper()
	dir := t.TempDir()
	sink, err := capture.OpenDir(dir, backend, "testbin")
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	for _, w := range writes {
		record := backendconformance.DifferentialRecord{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: w.statement, Parameters: w.parameters},
			Backend:     backend,
			Callsite:    w.callsite,
		}
		if err := sink.Append(record); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dir
}

func runWriterCoveragePhase(t *testing.T, dirs string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"-phase=writer-coverage", "-coverage-dirs=" + dirs}, os.Getenv, &stdout, &stderr)
	return stdout.String(), err
}

// The writer-coverage phase is opt-in like statement-coverage: it needs
// capture directories that no single-backend B-7 invocation produces.
func TestWriterCoveragePhaseSetOptIn(t *testing.T) {
	if !phaseSet("writer-coverage")["writer-coverage"] {
		t.Error("phaseSet(writer-coverage) does not include writer-coverage")
	}
	if phaseSet("all")["writer-coverage"] {
		t.Error("phaseSet(all) must not include the opt-in writer-coverage phase")
	}
	if needsSnapshot(phaseSet("writer-coverage")) {
		t.Error("writer-coverage reads recordings, not the golden snapshot")
	}
}

// GREEN: the clean tree's recorded writers all name an anchor label.
func TestWriterCoverageCleanReplayPasses(t *testing.T) {
	neo4j := writeWriterRecordings(t, "neo4j", canonicalWrites)
	nornic := writeWriterRecordings(t, "nornicdb", canonicalWrites)
	if out, err := runWriterCoveragePhase(t, neo4j+","+nornic); err != nil {
		t.Fatalf("clean replay failed the writer-coverage gate: %v\n%s", err, out)
	}
}

// RED: a planted `MERGE (n:Unconstrained {id: ...})` in the replay fails the
// gate and the report names the statement and its builder.
func TestWriterCoveragePlantedUnconstrainedWriterFails(t *testing.T) {
	planted := append(append([]recordedWrite{}, canonicalWrites...),
		recordedWrite{"MERGE (n:Unconstrained {id: $entity_id}) SET n.name = $name", `{"entity_id":"e"}`, "planted/writer.go:build"})
	neo4j := writeWriterRecordings(t, "neo4j", planted)
	out, err := runWriterCoveragePhase(t, neo4j)
	if err == nil {
		t.Fatalf("a planted unconstrained id writer passed the gate:\n%s", out)
	}
	for _, want := range []string{"writer-coverage", "Unconstrained", "planted/writer.go:build"} {
		if !strings.Contains(out, want) {
			t.Errorf("report does not name %q:\n%s", want, out)
		}
	}
}

// A dynamic `SET n += $props` on an uncovered label fails closed unless the
// recorded parameters prove the map has no id key.
func TestWriterCoverageDynamicMapNeedsParameterProof(t *testing.T) {
	stmt := "UNWIND $rows AS row MERGE (n:Unconstrained {uid: row.uid}) SET n += row.props"
	unproven := append(append([]recordedWrite{}, canonicalWrites...),
		recordedWrite{stmt, `{"rows":[{"uid":"u","props":{"id":"u"}}]}`, "planted/dynamic.go:build"})
	if out, err := runWriterCoveragePhase(t, writeWriterRecordings(t, "neo4j", unproven)); err == nil {
		t.Fatalf("a dynamic map carrying an id key passed the gate:\n%s", out)
	}
	proven := append(append([]recordedWrite{}, canonicalWrites...),
		recordedWrite{stmt, `{"rows":[{"uid":"u","props":{"name":"a"}}]}`, "planted/dynamic.go:build"})
	if out, err := runWriterCoveragePhase(t, writeWriterRecordings(t, "neo4j", proven)); err != nil {
		t.Fatalf("a dynamic map with no id key failed the gate: %v\n%s", err, out)
	}
}

// An empty capture proves nothing, and so does a capture that never sees an
// id write: either would pass a broken analyzer.
func TestWriterCoverageVacuousCapturesFail(t *testing.T) {
	empty := t.TempDir()
	if _, err := runWriterCoveragePhase(t, empty); err == nil {
		t.Error("an empty capture passed the gate")
	}
	readsOnly := writeWriterRecordings(t, "neo4j", []recordedWrite{{"MATCH (n:Function {uid: $uid}) RETURN n", `{"uid":"u"}`, ""}})
	if _, err := runWriterCoveragePhase(t, readsOnly); err == nil {
		t.Error("a capture with no id write passed the gate")
	}
}

func TestWriterCoverageRequiresCoverageDirs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), []string{"-phase=writer-coverage"}, os.Getenv, &stdout, &stderr); err == nil {
		t.Fatal("writer-coverage without -coverage-dirs succeeded")
	}
}

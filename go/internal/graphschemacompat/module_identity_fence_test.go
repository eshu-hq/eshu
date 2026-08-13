// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphschemacompat

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// TestPreModuleIdentityWriterIsRefused proves the thing the #6102 review asked
// for: after the Module (name, lang) identity cutover is applied, a writer
// still running the previous release is refused, not merely handed a different
// fingerprint value.
//
// The rollout this covers is the dangerous one. A new bootstrap applies the
// cutover schema and a new writer mints Module{name:"time", lang:"go"} and
// Module{name:"time", lang:"python"}. An old pod is still running, and its
// import-edge write is `MATCH (m:Module {name: row.module_name})`, which binds
// both nodes and attaches a Go file's IMPORTS edge to the Python module. The
// only thing standing between that pod and the graph is this admission
// decision, so the test drives the real decision -- writerAdmitted, the
// function RequireCompatible itself calls -- with the exact marker MarkApplied
// writes for the cutover schema.
func TestPreModuleIdentityWriterIsRefused(t *testing.T) {
	t.Parallel()

	for _, backend := range []graph.SchemaBackend{graph.SchemaBackendNeo4j, graph.SchemaBackendNornicDB} {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()

			applied, err := graph.SchemaApplicationForBackend(backend)
			if err != nil {
				t.Fatalf("SchemaApplicationForBackend(%q) error = %v, want nil", backend, err)
			}
			stale, err := graph.PreModuleIdentitySchemaApplication(backend)
			if err != nil {
				t.Fatalf("PreModuleIdentitySchemaApplication(%q) error = %v, want nil", backend, err)
			}

			if stale.Fingerprint == applied.Fingerprint {
				t.Fatalf("pre-cutover fingerprint equals the applied fingerprint (%q); "+
					"the identity cutover did not move the digest, so nothing fences the old writer",
					applied.Fingerprint)
			}
			if writerAdmitted(stale.Fingerprint, applied.Fingerprint, applied.CompatibleFingerprints) {
				t.Fatalf("a writer expecting the pre-cutover fingerprint %q was ADMITTED against the "+
					"applied cutover schema %q (compatible=%v); it would attach IMPORTS edges to the "+
					"wrong language's Module node",
					stale.Fingerprint, applied.Fingerprint, applied.CompatibleFingerprints)
			}
		})
	}
}

// TestRequireCompatibleRefusesCutoverWriterOnPreCutoverSchema covers the other
// rollout order end to end, through RequireCompatible and its Postgres read: a
// writer carrying the cutover identity starting against a data plane whose
// marker is still the pre-cutover schema. That writer would MERGE on
// (name, lang) while an unmigrated peer still MERGEs on name alone, so it is
// refused too.
func TestRequireCompatibleRefusesCutoverWriterOnPreCutoverSchema(t *testing.T) {
	t.Parallel()

	backend := graph.SchemaBackendNornicDB
	stale, err := graph.PreModuleIdentitySchemaApplication(backend)
	if err != nil {
		t.Fatalf("PreModuleIdentitySchemaApplication() error = %v, want nil", err)
	}
	compatible, err := jsonStringArray(stale.CompatibleFingerprints)
	if err != nil {
		t.Fatalf("encode compatible fingerprints: %v", err)
	}
	db := &fakeGraphSchemaQueryer{
		rows: fakeGraphSchemaRows{
			values: [][]any{{stale.Fingerprint, []byte(compatible)}},
		},
	}

	_, err = RequireCompatible(context.Background(), db, backend)
	if err == nil {
		t.Fatal("RequireCompatible() error = nil, want the cutover writer refused " +
			"against the pre-cutover schema marker")
	}
	if !strings.Contains(err.Error(), "graph schema incompatible") {
		t.Fatalf("RequireCompatible() error = %q, want a graph schema incompatible error", err)
	}
}

// jsonStringArray renders fingerprints the way MarkApplied stores them, so the
// fake marker row is byte-shaped like a real one.
func jsonStringArray(values []string) (string, error) {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("%q", value))
	}
	return "[" + strings.Join(quoted, ",") + "]", nil
}

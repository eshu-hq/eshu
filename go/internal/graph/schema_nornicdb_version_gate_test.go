// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// versionGateRecordingExecutor is a schemaRecordingExecutor that also reports
// a configured backend version, proving the refusal cases against the real
// admission decision.
type versionGateRecordingExecutor struct {
	schemaRecordingExecutor
	version string
	err     error
}

func (e *versionGateRecordingExecutor) QueryBackendVersion(context.Context) (string, error) {
	if e.err != nil {
		return "", e.err
	}
	return e.version, nil
}

// TestNornicDBSchemaApplyRefusesPre15Backend is the #11535 P2 regression: the
// 1.5.x-gated statement list (uid constraints whose backing index only 1.5.x
// creates, plus the DROP guards for the retired lookup indexes) must never
// execute against a pre-1.5 backend, where the constraint creates no backing
// index and the drops would leave uid reads unindexed. The probe reports the
// refusal before any DDL statement reaches the executor.
func TestNornicDBSchemaApplyRefusesPre15Backend(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"NornicDB v1.3.3",
		"1.4.9",
		"NornicDB v1.4.12",
		"NornicDB v0.9.0",
	} {
		executor := &versionGateRecordingExecutor{version: version}
		err := EnsureSchemaWithBackendStrict(context.Background(), executor, nil, SchemaBackendNornicDB)
		if err == nil {
			t.Fatalf("EnsureSchemaWithBackendStrict(%q) error = nil, want pre-1.5 refusal", version)
		}
		if !strings.Contains(err.Error(), version) {
			t.Fatalf("EnsureSchemaWithBackendStrict(%q) error = %q, want the reported version named", version, err.Error())
		}
		if got := len(executor.calls); got != 0 {
			t.Fatalf("EnsureSchemaWithBackendStrict(%q) executed %d statements, want 0 (refuse before DDL)", version, got)
		}
	}
}

// TestNornicDBSchemaApplyFailsClosedOnUnparsableVersion proves the probe
// refuses when the backend does not answer with a parseable version: an empty
// report, a query error, and a string with no major.minor pair must all stop
// the apply with zero DDL executed.
func TestNornicDBSchemaApplyFailsClosedOnUnparsableVersion(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		executor := &versionGateRecordingExecutor{version: ""}
		err := EnsureSchemaWithBackendStrict(context.Background(), executor, nil, SchemaBackendNornicDB)
		if err == nil {
			t.Fatal("EnsureSchemaWithBackendStrict(empty version) error = nil, want fail-closed refusal")
		}
		if got := len(executor.calls); got != 0 {
			t.Fatalf("EnsureSchemaWithBackendStrict(empty version) executed %d statements, want 0", got)
		}
	})

	t.Run("query error", func(t *testing.T) {
		t.Parallel()
		executor := &versionGateRecordingExecutor{err: errors.New("simulated version query failure")}
		err := EnsureSchemaWithBackendStrict(context.Background(), executor, nil, SchemaBackendNornicDB)
		if err == nil {
			t.Fatal("EnsureSchemaWithBackendStrict(version query error) error = nil, want fail-closed refusal")
		}
		if got := len(executor.calls); got != 0 {
			t.Fatalf("EnsureSchemaWithBackendStrict(version query error) executed %d statements, want 0", got)
		}
	})

	for _, version := range []string{"NornicDB", "hotfix", "version one point five"} {
		executor := &versionGateRecordingExecutor{version: version}
		err := EnsureSchemaWithBackendStrict(context.Background(), executor, nil, SchemaBackendNornicDB)
		if err == nil {
			t.Fatalf("EnsureSchemaWithBackendStrict(%q) error = nil, want fail-closed refusal", version)
		}
		if got := len(executor.calls); got != 0 {
			t.Fatalf("EnsureSchemaWithBackendStrict(%q) executed %d statements, want 0", version, got)
		}
	}
}

// versionlessRecordingExecutor implements only CypherExecutor, proving the
// missing-querier refusal against the real admission decision. It must never
// gain a QueryBackendVersion method.
type versionlessRecordingExecutor struct {
	calls int
}

func (e *versionlessRecordingExecutor) ExecuteCypher(context.Context, CypherStatement) error {
	e.calls++
	return nil
}

// TestNornicDBSchemaApplyRefusesExecutorWithoutVersion proves the probe fails
// closed when the executor cannot report a version at all: an unproven
// backend is treated as pre-1.5, never as compatible.
func TestNornicDBSchemaApplyRefusesExecutorWithoutVersion(t *testing.T) {
	t.Parallel()

	executor := &versionlessRecordingExecutor{}
	err := EnsureSchemaWithBackendStrict(context.Background(), executor, nil, SchemaBackendNornicDB)
	if err == nil {
		t.Fatal("EnsureSchemaWithBackendStrict(executor without version) error = nil, want fail-closed refusal")
	}
	if executor.calls != 0 {
		t.Fatalf("EnsureSchemaWithBackendStrict(executor without version) executed %d statements, want 0", executor.calls)
	}
}

// TestNornicDBSchemaApplyAdmits15xBackend proves the gate opens for a 1.5.x
// self-report: the full statement list still applies (a recording executor
// fails nothing here, so strict apply succeeds).
func TestNornicDBSchemaApplyAdmits15xBackend(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"NornicDB v1.5.2-hotfix", "1.5.0", "NornicDB v1.5.0", "2.0.0"} {
		executor := &versionGateRecordingExecutor{version: version}
		if err := EnsureSchemaWithBackendStrict(context.Background(), executor, nil, SchemaBackendNornicDB); err != nil {
			t.Fatalf("EnsureSchemaWithBackendStrict(%q) error = %v, want nil", version, err)
		}
		if got := len(executor.calls); got == 0 {
			t.Fatalf("EnsureSchemaWithBackendStrict(%q) executed 0 statements, want the full list", version)
		}
	}
}

// TestNeo4jSchemaApplyNeedsNoBackendVersion proves the Neo4j path is
// untouched: an executor that cannot report any version still applies the
// Neo4j statement list exactly as before.
func TestNeo4jSchemaApplyNeedsNoBackendVersion(t *testing.T) {
	t.Parallel()

	executor := &schemaRecordingExecutor{}
	if err := EnsureSchemaWithBackendStrict(context.Background(), executor, nil, SchemaBackendNeo4j); err != nil {
		t.Fatalf("EnsureSchemaWithBackendStrict(neo4j) error = %v, want nil", err)
	}
	if got := len(executor.calls); got == 0 {
		t.Fatal("EnsureSchemaWithBackendStrict(neo4j) executed 0 statements, want the full list")
	}
}

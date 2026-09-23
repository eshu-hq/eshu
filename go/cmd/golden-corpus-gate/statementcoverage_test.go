// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/graph/capture"
	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

// writeCoverageManifest records a two-builder manifest: one templated
// builder the fixture recordings execute, one fragmented builder they do
// not, so the clean and failing invocations share one fixture.
func writeCoverageManifest(t *testing.T) string {
	t.Helper()
	manifest := queryplan.BuilderManifest{
		Version: 1,
		Builders: []queryplan.StatementBuilderCoverage{
			{
				File: "writer.go",
				Builders: []queryplan.StatementBuilder{
					{
						Symbol:       "buildUpsert",
						Count:        1,
						Operation:    "sourcecypher.OperationCanonicalUpsert",
						Variants:     []queryplan.StatementVariant{{Template: "MERGE (n:File {path: $path})"}},
						SourceDigest: "aaa",
					},
					{
						Symbol:    "buildLabel",
						Count:     1,
						Operation: "sourcecypher.OperationCanonicalUpsert",
						Variants: []queryplan.StatementVariant{{Fragments: []string{
							"MERGE (n:Label) RETURN",
							"n",
						}}},
						SourceDigest: "bbb",
					},
				},
			},
		},
	}
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "statement-builders.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	return path
}

// writeCoverageDir records one backend's write executions into dir using
// the real capture sink, so the gate phase test exercises the on-disk
// format. includeLabel controls whether the fragmented builder's text is
// among them.
func writeCoverageDir(t *testing.T, backend string, includeLabel bool) string {
	t.Helper()
	dir := t.TempDir()
	sink, err := capture.OpenDir(dir, backend, "testbin")
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	records := []backendconformance.DifferentialRecord{
		{
			Fingerprint: backendconformance.DifferentialFingerprint{
				Statement:  "MERGE (n:File {path: $path})",
				Parameters: `{"path":"a"}`,
			},
			Backend: backend,
		},
	}
	if includeLabel {
		records = append(records, backendconformance.DifferentialRecord{
			Fingerprint: backendconformance.DifferentialFingerprint{
				Statement:  "MERGE (n:Label) RETURN n",
				Parameters: `{}`,
			},
			Backend: backend,
		})
	}
	for _, record := range records {
		if err := sink.Append(record); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dir
}

func runStatementCoveragePhase(t *testing.T, manifest, dirs string) error {
	t.Helper()
	var stdout, stderr bytes.Buffer
	return run(context.Background(), []string{
		"-phase=statement-coverage",
		"-coverage-manifest=" + manifest,
		"-coverage-dirs=" + dirs,
	}, os.Getenv, &stdout, &stderr)
}

// The statement-coverage phase is opt-in like backend-diff: an explicit
// -phase=statement-coverage runs it, but -phase=all must not, since it
// needs capture directories no existing B-7 invocation provides.
func TestStatementCoveragePhaseSetOptIn(t *testing.T) {
	if !phaseSet("statement-coverage")["statement-coverage"] {
		t.Errorf("phaseSet(statement-coverage) does not include statement-coverage")
	}
	if phaseSet("all")["statement-coverage"] {
		t.Errorf("phaseSet(all) must not include the opt-in statement-coverage phase")
	}
}

func TestRunStatementCoverageClean(t *testing.T) {
	manifest := writeCoverageManifest(t)
	nornicdb := writeCoverageDir(t, "nornicdb", true)
	neo4j := writeCoverageDir(t, "neo4j", true)
	if err := runStatementCoveragePhase(t, manifest, nornicdb+","+neo4j); err != nil {
		t.Errorf("covered manifest failed the gate: %v", err)
	}
}

func TestRunStatementCoverageNeverExecuted(t *testing.T) {
	manifest := writeCoverageManifest(t)
	nornicdb := writeCoverageDir(t, "nornicdb", false)
	neo4j := writeCoverageDir(t, "neo4j", false)
	if err := runStatementCoveragePhase(t, manifest, nornicdb+","+neo4j); err == nil {
		t.Errorf("unexecuted builder passed the gate")
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration || queryplan_profile_live

package query

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

// The pilot has no production query rewrite. These helpers bind paired runs
// to identical shipped source on the base and candidate, rather than calling
// a candidate query a baseline without checking its provenance.
func methodologyGitCommit(t *testing.T, ref string) string {
	t.Helper()
	output, err := exec.Command("git", "rev-parse", "--verify", ref+"^{commit}").Output()
	if err != nil {
		t.Fatalf("resolve proof commit %s: %v", ref, err)
	}
	return strings.TrimSpace(string(output))
}

func methodologyVerifySameProduction(t *testing.T, base string, paths []string) {
	t.Helper()
	for _, path := range paths {
		baseline, err := exec.Command("git", "show", base+":"+path).Output()
		if err != nil {
			t.Fatalf("read base production source %s: %v", path, err)
		}
		candidate, err := os.ReadFile(filepath.Join("../../..", path))
		if err != nil {
			t.Fatal(err)
		}
		if string(baseline) != string(candidate) {
			t.Fatalf("production source %s differs from base: this unchanged-query paired runner cannot measure a rewrite; run separate base/candidate binaries", path)
		}
	}
}

func methodologyBuild(t *testing.T, commit string, schema, migrations, indexes []string) queryplan.PilotBuildIdentity {
	t.Helper()
	return queryplan.PilotBuildIdentity{
		Commit:    commit,
		SchemaDDL: schema, SchemaSHA256: queryplan.PilotDefinitionsSHA256(schema),
		Migrations: migrations, MigrationsSHA256: queryplan.PilotDefinitionsSHA256(migrations),
		IndexDDL: indexes, IndexesSHA256: queryplan.PilotDefinitionsSHA256(indexes),
	}
}

func methodologyBinaryHash(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func methodologySourceHash(t *testing.T, paths []string) string {
	t.Helper()
	digest := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = digest.Write([]byte(path + "\x00"))
		_, _ = digest.Write(data)
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

func methodologyWriteArtifact(t *testing.T, path string, artifact queryplan.PilotEvidenceArtifact) {
	t.Helper()
	if path == "" {
		t.Fatal("proof runner must supply a machine-readable artifact path")
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

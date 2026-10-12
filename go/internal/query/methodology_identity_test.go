// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration || queryplan_profile_live

package query

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

// The pilot has no production query rewrite. These helpers bind paired runs
// to identical shipped source on the frozen base and candidate.
func methodologyComparisonIdentity(t *testing.T) queryplan.PilotComparisonIdentity {
	t.Helper()
	identity, err := queryplan.LoadPilotComparisonIdentity(os.Getenv("ESHU_QUERY_METHODOLOGY_IDENTITY"))
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.VerifyCandidateCheckout(); err != nil {
		t.Fatal(err)
	}
	return identity
}

func methodologyRepositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, ".git")); err == nil {
			return directory
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect methodology repository root: %v", err)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatalf("methodology repository root not found from %s", directory)
		}
		directory = parent
	}
}

func methodologyVerifySameProduction(t *testing.T, base, candidate string, paths []string) {
	t.Helper()
	root := methodologyRepositoryRoot(t)
	for _, path := range paths {
		baseEntry, err := exec.Command("git", "ls-tree", "--full-tree", base, "--", path).Output()
		if err != nil {
			t.Fatalf("read base production entry %s: %v", path, err)
		}
		candidateEntry, err := exec.Command("git", "ls-tree", "--full-tree", candidate, "--", path).Output()
		if err != nil {
			t.Fatalf("read candidate production entry %s: %v", path, err)
		}
		if len(baseEntry) == 0 || !bytes.Equal(baseEntry, candidateEntry) {
			t.Fatalf("production source %s committed mode or blob differs from base", path)
		}
		baseline, err := exec.Command("git", "show", base+":"+path).Output()
		if err != nil {
			t.Fatalf("read base production source %s: %v", path, err)
		}
		committed, err := exec.Command("git", "show", candidate+":"+path).Output()
		if err != nil {
			t.Fatalf("read candidate production source %s: %v", path, err)
		}
		working, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(baseline, committed) || !bytes.Equal(committed, working) {
			t.Fatalf("production source %s differs from base: this unchanged-query paired runner cannot measure a rewrite; run separate base/candidate binaries", path)
		}
	}
}

// methodologyVerifySameProductionTree binds every matching source at both
// revisions, including files added or removed since the base revision.
func methodologyVerifySameProductionTree(t *testing.T, base, candidate, directory, pattern string) {
	t.Helper()
	root := methodologyRepositoryRoot(t)
	entries := func(commit string) map[string]string {
		t.Helper()
		output, err := exec.Command("git", "ls-tree", "-r", "--full-tree", "-z", commit, "--", directory).Output()
		if err != nil {
			t.Fatalf("list production sources at %s in %s: %v", commit, directory, err)
		}
		matchedEntries := make(map[string]string)
		for _, record := range bytes.Split(bytes.TrimSuffix(output, []byte{0}), []byte{0}) {
			if len(record) == 0 {
				continue
			}
			parts := bytes.SplitN(record, []byte{'\t'}, 2)
			if len(parts) != 2 {
				t.Fatalf("malformed git tree entry at %s: %q", commit, record)
			}
			path := string(parts[1])
			name := filepath.Base(path)
			matched, err := filepath.Match(pattern, name)
			if err != nil {
				t.Fatal(err)
			}
			if matched && !strings.HasSuffix(name, "_test.go") {
				matchedEntries[path] = string(parts[0])
			}
		}
		return matchedEntries
	}
	baseEntries, candidateEntries := entries(base), entries(candidate)
	paths := make(map[string]struct{})
	for path := range baseEntries {
		paths[path] = struct{}{}
	}
	for path := range candidateEntries {
		paths[path] = struct{}{}
	}
	current, err := filepath.Glob(filepath.Join(root, directory, pattern))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range current {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		paths[filepath.ToSlash(relative)] = struct{}{}
	}
	selected := make([]string, 0, len(paths))
	for path := range paths {
		selected = append(selected, path)
	}
	sort.Strings(selected)
	if len(selected) == 0 {
		t.Fatalf("no production sources match %s/%s", directory, pattern)
	}
	for _, path := range selected {
		before, baseExists := baseEntries[path]
		after, candidateExists := candidateEntries[path]
		if !baseExists || !candidateExists {
			t.Fatalf("production source %s inventory differs: base=%t candidate=%t", path, baseExists, candidateExists)
		}
		if before != after {
			t.Fatalf("production source %s committed mode or blob differs from base", path)
		}
	}
	methodologyVerifySameProduction(t, base, candidate, selected)
}

func TestMethodologyProductionSourceTreesMatchBase(t *testing.T) {
	if os.Getenv("ESHU_QUERY_METHODOLOGY_IDENTITY") == "" && os.Getenv("ESHU_QUERY_METHODOLOGY_REQUIRED") != "1" {
		t.Skip("dedicated methodology runner supplies frozen comparison identity")
	}
	identity := methodologyComparisonIdentity(t)
	methodologyVerifySameProductionTree(t, identity.Base, identity.Candidate, "go/internal/graph", "schema*.go")
	methodologyVerifySameProductionTree(t, identity.Base, identity.Candidate, "go/internal/storage/postgres/migrations", "*.sql")
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

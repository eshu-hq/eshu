// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestShellExecFamilyOduResolvesItsExpectedEdgeSet proves the EXTRACTOR:
// reducer.ExtractShellExecRows, run over the cataloged Odù, reproduces the
// hand-derived edge set exactly.
func TestShellExecFamilyOduResolvesItsExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.CatalogByName()[ifa.ShellExecFamilyOduName]
	if odu.Name == "" {
		t.Fatalf("Odù %q is not in the catalog", ifa.ShellExecFamilyOduName)
	}

	ok, detail := resolveShellExecMaterializedEdges(odu, ifa.ShellExecFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("shell exec family Odù does not resolve: %s", detail)
	}
	if !strings.Contains(detail, "expected 2-edge set exactly") {
		t.Errorf("detail = %q, want it to name the exact edge count it proved", detail)
	}
	t.Logf("%s", detail)
}

// TestShellExecFamilyResolvesThroughTheManifestResolver proves the vacuity
// guard is reachable the way the gate reaches it -- by surface name through
// MaterializedEdgeOduResolver. A guard nothing dispatches to is not coverage.
func TestShellExecFamilyResolvesThroughTheManifestResolver(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	resolver := MaterializedEdgeOduResolver{Catalog: ifa.CatalogByName(), RepoRoot: repoRoot}

	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + "shell_exec",
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          ifa.ShellExecFamilyOduName,
	})
	if !ok {
		t.Fatalf("resolver.Resolve for shell_exec: %s", detail)
	}
}

// TestShellExecFamilyExpectedSetRejectsAnExtraEdge is the teeth test in the
// direction a "contains all expected" check would miss: an edge nobody
// derived must fail as loudly as a missing one, or the graph can grow
// silently.
func TestShellExecFamilyExpectedSetRejectsAnExtraEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.CatalogByName()[ifa.ShellExecFamilyOduName]

	expected, err := LoadExpectedEdges(ifa.ShellExecFamilyExpectedEdgesPath(repoRoot), shellExecFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	path := writeShellExecExpectedEdgesFixture(t, expected[:len(expected)-1])

	ok, detail := resolveShellExecMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a short expected set; an edge nobody derived went unreported")
	}
	if !strings.Contains(detail, "EXTRA") {
		t.Errorf("detail = %q, want it to name the EXTRA edge", detail)
	}
}

// TestShellExecFamilyExpectedSetRejectsAMissingEdge is the other direction: an
// expectation naming an edge the extractor does not produce must fail, or the
// fixture could quietly outrun the code.
func TestShellExecFamilyExpectedSetRejectsAMissingEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.CatalogByName()[ifa.ShellExecFamilyOduName]

	expected, err := LoadExpectedEdges(ifa.ShellExecFamilyExpectedEdgesPath(repoRoot), shellExecFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	padded := append(append([]ExpectedEdge{}, expected...), ExpectedEdge{
		RelationshipType: "EXECUTES_SHELL",
		SourceEntityID:   ifa.ShellExecFamilyDeployFunctionUID,
		TargetEntityID:   "shell-command:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	})
	path := writeShellExecExpectedEdgesFixture(t, padded)

	ok, detail := resolveShellExecMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an expectation the extractor does not satisfy")
	}
	if !strings.Contains(detail, "MISSING") {
		t.Errorf("detail = %q, want it to name the MISSING edge", detail)
	}
}

// TestShellExecFamilyExpectedSetIsVacuityGuarded proves the guard FAILS on an
// empty expected-edge fixture rather than passing vacuously against any
// graph -- the #6001 proof requirement. loadSQLRelationshipExpectedEdges
// (LoadExpectedEdges's underlying loader, shared by every single-type family)
// already rejects a zero-edge file at load time; this test proves that
// rejection actually reaches resolveShellExecMaterializedEdges's caller, not
// merely that the loader has the check.
func TestShellExecFamilyExpectedSetIsVacuityGuarded(t *testing.T) {
	t.Parallel()
	odu := ifa.CatalogByName()[ifa.ShellExecFamilyOduName]

	path := filepath.Join(t.TempDir(), "empty-expected-edges.json")
	if err := os.WriteFile(path, []byte(`{"odu":"odu:ifa-shell-exec-family","edges":[]}`), 0o600); err != nil {
		t.Fatalf("write empty expected-edges fixture: %v", err)
	}

	ok, detail := resolveShellExecMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an empty expected-edge fixture; the gate would be vacuous")
	}
	if detail == "" {
		t.Error("guard returned no detail for the empty-fixture rejection")
	}
	t.Logf("%s", detail)
}

// TestShellExecFamilyMissingRegistryTypeIsCaught proves
// missingShellExecExpectedTypes fires when the fixture drops the family's
// only registry type -- the exhaustiveness check, not the exact-set check.
func TestShellExecFamilyMissingRegistryTypeIsCaught(t *testing.T) {
	t.Parallel()
	odu := ifa.CatalogByName()[ifa.ShellExecFamilyOduName]

	path := filepath.Join(t.TempDir(), "wrong-type-expected-edges.json")
	raw := `{"odu":"odu:ifa-shell-exec-family","edges":[{"relationship_type":"NOT_EXECUTES_SHELL","source_entity_id":"a","target_entity_id":"b"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ok, detail := resolveShellExecMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a fixture naming no EXECUTES_SHELL edge")
	}
	if !strings.Contains(detail, "EXECUTES_SHELL") {
		t.Errorf("detail = %q, want it to name the missing EXECUTES_SHELL registry type", detail)
	}
}

// TestShellExecCanonicalEntityIDLiterals independently reproduces
// shell_exec_family_odu.go's pinned Function canonical uid literals against
// the real content.CanonicalEntityID function, mirroring
// TestRationaleCanonicalTargetIDLiterals. A wrong literal here would mean the
// fixture's parsed_file_data.functions[].uid values do not match what the
// projector's canonical entity writer actually derives for the same
// (repo, path, "Function", name, line) tuple -- silently no-opping the live
// edge write's source MATCH even though the pure guard above stays green.
func TestShellExecCanonicalEntityIDLiterals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		name string
		line int
		want string
	}{
		{ifa.ShellExecFamilyDeployPath, ifa.ShellExecFamilyDeployFunctionName, ifa.ShellExecFamilyDeployFunctionLine, ifa.ShellExecFamilyDeployFunctionUID},
		{ifa.ShellExecFamilyCleanupPath, ifa.ShellExecFamilyCleanupFunctionName, ifa.ShellExecFamilyCleanupFunctionLine, ifa.ShellExecFamilyCleanupFunctionUID},
		{ifa.ShellExecFamilyOrphanPath, ifa.ShellExecFamilyOrphanFunctionName, ifa.ShellExecFamilyOrphanFunctionLine, ifa.ShellExecFamilyOrphanFunctionUID},
		{ifa.ShellExecFamilySilentPath, ifa.ShellExecFamilySilentFunctionName, ifa.ShellExecFamilySilentFunctionLine, ifa.ShellExecFamilySilentFunctionUID},
	}
	for _, test := range tests {
		got := content.CanonicalEntityID(ifa.ShellExecFamilyRepoID, test.path, "Function", test.name, test.line)
		if got != test.want {
			t.Errorf("CanonicalEntityID(%q, %q, Function, %q, %d) = %q, want %q", ifa.ShellExecFamilyRepoID, test.path, test.name, test.line, got, test.want)
		}
	}
}

// shellExecTargetIDReference is an independent copy of
// go/internal/reducer/shell_exec_materialization.go's unexported
// shellExecTargetID, held here so this test can reproduce the writer's
// sha256 target uid without importing an unexported reducer symbol. Any
// drift between this copy and the production function is caught by
// TestShellExecFamilyOduResolvesItsExpectedEdgeSet, which runs the REAL
// reducer.ExtractShellExecRows (not this copy) against the same fixture and
// asserts its output matches these same literals -- so a change to the
// production hash function that this copy failed to mirror would surface
// there as a MISSING/EXTRA mismatch, not as a false-passing pair of
// self-consistent copies.
func shellExecTargetIDReference(repoID, sourcePath, functionEntityID string, lineNumber int, api string) string {
	hash := sha256.New()
	for _, part := range []string{repoID, sourcePath, functionEntityID, strconv.Itoa(lineNumber), strings.TrimSpace(api)} {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return "shell-command:" + hex.EncodeToString(hash.Sum(nil))
}

// TestShellExecTargetIDLiteralsMatchTheWriterFunction independently
// reproduces shell_exec_family_odu.go's two pinned ShellCommand target uid
// literals via shellExecTargetIDReference, proving they were computed from
// the real hashing algorithm rather than hand-typed.
func TestShellExecTargetIDLiteralsMatchTheWriterFunction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		lineNumber int
		api        string
		want       string
	}{
		{"line5 os.system", 5, "os.system", ifa.ShellExecFamilyDeployTarget1},
		{"line6 subprocess.run", 6, "subprocess.run", ifa.ShellExecFamilyDeployTarget2},
	}
	// sourcePath is parsed_file_data.path (the full local-path-prefixed
	// path), not the file fact's relative_path -- see
	// ifa.ShellExecFamilyDeployTarget1's doc comment (shell_exec_family_odu.go)
	// for why ExtractShellExecRows's fallback always resolves to this value
	// for this fixture.
	sourcePath := ifa.ShellExecFamilyLocalPath + "/" + ifa.ShellExecFamilyDeployPath
	for _, test := range tests {
		got := shellExecTargetIDReference(ifa.ShellExecFamilyRepoID, sourcePath, ifa.ShellExecFamilyDeployFunctionUID, test.lineNumber, test.api)
		if got != test.want {
			t.Errorf("%s: shellExecTargetID(...) = %q, want %q", test.name, got, test.want)
		}
		if len(strings.TrimPrefix(got, "shell-command:")) != sha256.Size*2 {
			t.Errorf("%s: target uid hex length = %d, want %d (sha256.Size*2)", test.name, len(strings.TrimPrefix(got, "shell-command:")), sha256.Size*2)
		}
	}
}

func writeShellExecExpectedEdgesFixture(t *testing.T, edges []ExpectedEdge) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(`{"odu":"odu:ifa-shell-exec-family","edges":[`)
	for i, e := range edges {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"relationship_type":"` + e.RelationshipType + `","source_entity_id":"` + e.SourceEntityID + `","target_entity_id":"` + e.TargetEntityID + `"}`)
	}
	b.WriteString(`]}`)
	path := filepath.Join(t.TempDir(), "expected-edges.json")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write expected edges: %v", err)
	}
	return path
}

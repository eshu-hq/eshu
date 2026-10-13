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
	// The empty declaration keeps every existing caller (the production-tree
	// guard and the postgres artifact writer) on the byte-identical strict
	// pin. Only the graph artifact writer passes the committed rewrite
	// declaration, so a rewrite in one backend never relaxes another.
	methodologyVerifySameProductionWithDeclaration(t, base, candidate, paths, methodologyRewriteDeclaration{})
}

// methodologyRewriteDeclaration is the author-side recalibration procedure
// for a query rewrite PR. The unchanged-query pin below cannot measure a
// rewrite: the paired Base/Candidate sides execute the same test binary, so
// identical sources are what make a Base-vs-Candidate comparison honest. A
// rewrite PR therefore declares the pinned reader files it intentionally
// changes (issue + reason + exact paths). The declaration activates only
// when the diff touches declared scope; while active the pin requires the
// diff to equal the declared set exactly, and declared files additionally
// bind the working tree to the candidate blob, because the measured binary
// executes working-tree code. An undeclared differing file, a
// declared-but-identical file in an active run, and a declared path outside
// the pinned set all fail closed. Past the rewrite's merge the declared
// files agree again, the declaration goes inert, and the pin behaves
// byte-for-byte as before it existed. A PR with no rewrite leaves the zero
// declaration in place.
type methodologyRewriteDeclaration struct {
	Issue  int
	Reason string
	Paths  []string
}

// methodologyCommittedRewriteDeclaration is the single declaration the
// repository carries. A rewrite PR edits these paths; any other PR leaves
// the zero value alone.
var methodologyCommittedRewriteDeclaration = methodologyRewriteDeclaration{
	Issue:  7348,
	Reason: "TS/JS package lift of file_import_cycles changes these readers by design; the paired Base/Candidate sides in this run both measure candidate code for the declared scope.",
	Paths: []string{
		"go/internal/query/codemodel/code_import_dependencies_queries.go",
		"go/internal/query/codemodel/code_import_dependencies_rows.go",
		"go/internal/query/codemodel/code_import_dependencies_response.go",
		"go/internal/query/codemodel/code_import_dependencies_enumeration.go",
	},
}

func methodologyVerifySameProductionWithDeclaration(t *testing.T, base, candidate string, paths []string, decl methodologyRewriteDeclaration) {
	t.Helper()
	root := methodologyRepositoryRoot(t)
	if len(decl.Paths) == 0 {
		// No declared rewrite: the historical strict pin, message for
		// message. Any differing file fails at first sight.
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
		return
	}
	type blobID struct {
		base, candidate string
	}
	differing := make(map[string]blobID)
	for _, path := range paths {
		baseEntry, err := exec.Command("git", "ls-tree", "--full-tree", base, "--", path).Output()
		if err != nil {
			t.Fatalf("read base production entry %s: %v", path, err)
		}
		candidateEntry, err := exec.Command("git", "ls-tree", "--full-tree", candidate, "--", path).Output()
		if err != nil {
			t.Fatalf("read candidate production entry %s: %v", path, err)
		}
		if len(baseEntry) != 0 && bytes.Equal(baseEntry, candidateEntry) {
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
			continue
		}
		differing[path] = blobID{base: methodologyTreeBlobSHA(baseEntry), candidate: methodologyTreeBlobSHA(candidateEntry)}
	}
	pinned := make(map[string]bool, len(paths))
	for _, path := range paths {
		pinned[path] = true
	}
	declared := make(map[string]bool)
	for _, path := range decl.Paths {
		if !pinned[path] {
			t.Fatalf("rewrite declaration (issue #%d) names %s which is not a pinned production source", decl.Issue, path)
		}
		declared[path] = true
	}
	// The declaration activates only when the diff touches declared scope.
	// Past its merge the declared files agree again, the declaration goes
	// inert, and agreeing runs pass exactly as before it existed. A future
	// rewrite that touches declared scope without updating the declaration
	// still fails below, so inert never means excused.
	active := false
	for path := range differing {
		if declared[path] {
			active = true
			break
		}
	}
	if !active {
		names := make([]string, 0, len(differing))
		for path := range differing {
			names = append(names, path)
		}
		sort.Strings(names)
		for _, path := range names {
			t.Fatalf("production source %s committed mode or blob differs from base", path)
		}
		if len(decl.Paths) != 0 {
			t.Logf("rewrite declaration issue #%d not triggered: no declared file differs", decl.Issue)
		}
		return
	}
	for path := range differing {
		if !declared[path] {
			t.Fatalf("production source %s differs from base and is not covered by the issue #%d rewrite declaration; declare the rewrite or revert the file", path, decl.Issue)
		}
	}
	for path := range declared {
		if _, ok := differing[path]; !ok {
			t.Fatalf("rewrite declaration (issue #%d) names unchanged production source %s; narrow the declaration to the files this change rewrites", decl.Issue, path)
		}
	}
	names := make([]string, 0, len(differing))
	for path := range differing {
		names = append(names, path)
	}
	sort.Strings(names)
	for _, path := range names {
		ids := differing[path]
		t.Logf("rewrite declaration issue #%d: %s base=%s candidate=%s: %s", decl.Issue, decl.Reason, ids.base, ids.candidate, path)
	}
	// Bind the working tree exactly as the strict pin does: the measured
	// test binary executes working-tree code, so an uncommitted edit to a
	// declared reader would measure unpinned code behind a passing pin.
	for _, path := range names {
		committed, err := exec.Command("git", "show", candidate+":"+path).Output()
		if err != nil {
			t.Fatalf("read candidate production source %s: %v", path, err)
		}
		working, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(working, committed) {
			t.Fatalf("production source %s working tree differs from candidate %s: commit the file before measuring; the pin cannot bind uncommitted code", path, candidate)
		}
	}
}

// methodologyTreeBlobSHA parses the blob SHA out of one `git ls-tree`
// output line, or reports absent for a missing entry.
func methodologyTreeBlobSHA(entry []byte) string {
	fields := bytes.Fields(entry)
	if len(fields) < 3 {
		return "absent"
	}
	return string(fields[2])
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
	common := make([]string, 0, len(selected))
	added := make([]string, 0)
	for _, path := range selected {
		before, baseExists := baseEntries[path]
		after, candidateExists := candidateEntries[path]
		switch {
		case baseExists && candidateExists:
			if before != after {
				t.Fatalf("production source %s committed mode or blob differs from base", path)
			}
			common = append(common, path)
		case !baseExists && candidateExists:
			// A committed additive production source is admitted: additive
			// evolution keeps every base file byte-identical. For migrations
			// the schema gates that own additive files
			// (migration-immutability, the checksum manifest, and the live
			// migration proof) verify the addition itself; for graph schema
			// files the same property holds (no base file changes, addition
			// reviewed as a new file), with a dedicated pin tracked in
			// #8073. See #8072.
			added = append(added, path)
		default:
			t.Fatalf("production source %s inventory differs: base=%t candidate=%t", path, baseExists, candidateExists)
		}
	}
	methodologyVerifySameProduction(t, base, candidate, common)
	methodologyVerifyAddedProductionClean(t, root, candidate, added)
}

// methodologyVerifyAddedProductionClean binds an admitted additive path to
// the exact committed candidate blob: the paired runner measures the working
// tree, so a committed addition dirtied after commit must fail rather than
// pass on the committed inventory alone.
func methodologyVerifyAddedProductionClean(t *testing.T, root, candidate string, added []string) {
	t.Helper()
	for _, path := range added {
		working, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("production source %s working tree unreadable: %v", path, err)
		}
		committed, err := exec.Command("git", "-C", root, "show", candidate+":"+path).Output()
		if err != nil {
			t.Fatalf("read candidate production entry %s: %v", path, err)
		}
		if !bytes.Equal(working, committed) {
			t.Fatalf("production source %s working tree differs from committed candidate", path)
		}
	}
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

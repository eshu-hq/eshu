// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// The #6541 directory language query decides grant membership from the
// Directory's own `repo_id` property and counts the files CONTAINS-linked to
// it, without re-checking any File. That is only sound while a Directory the
// projector writes and the Files it links carry one repository, so the
// invariant these tests pin is the query's grant enforcement, not a projector
// detail. See buildDirectoryCypher in go/internal/query/language/cypher.go.

// ownershipScope is a repository scope rooted at /repos/alpha, next to a
// sibling repository at /repos/beta that this projection must never write into.
func ownershipScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "scope-alpha",
		SourceSystem:  "test",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "part-alpha",
		Metadata: map[string]string{
			"repo_id":   "repo-alpha",
			"repo_path": "/repos/alpha",
		},
	}
}

func ownershipGeneration() scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "gen-alpha",
		ScopeID:      "scope-alpha",
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func ownershipRepositoryFact() facts.Envelope {
	return facts.Envelope{
		FactID:   "r-alpha",
		ScopeID:  "scope-alpha",
		FactKind: "repository",
		Payload: map[string]any{
			"repo_id": "repo-alpha",
			"name":    "alpha",
			"path":    "/repos/alpha",
		},
	}
}

func ownershipFileFact(factID, relativePath string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		ScopeID:  "scope-alpha",
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          "repo-alpha",
			"path":             "/repos/alpha/" + relativePath,
			"relative_path":    relativePath,
			"name":             relativePath[strings.LastIndex(relativePath, "/")+1:],
			"language":         "go",
			"parsed_file_data": map[string]any{},
		},
	}
}

// TestCanonicalDirectoryChainStaysInsideTheRepositoryRoot is the failing
// regression for the ownership gap. A file fact whose relative_path climbs out
// of the repository root used to walk the directory chain out with it: the
// projector emitted Directory rows for a SIBLING repository's paths stamped
// with THIS repository's repo_id, and linked this repository's file to them.
// Both halves break the directory query's grant: the sibling's directory is
// re-pointed away from the caller granted it, and is counted for the caller
// granted this repository instead.
//
// Production discovery cannot emit such a relative_path -- it derives every one
// through filepath.Rel under the repository root -- so this is a guard against
// a malformed or hostile fact, not a reproduction of an observed run.
func TestCanonicalDirectoryChainStaysInsideTheRepositoryRoot(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		ownershipRepositoryFact(),
		ownershipFileFact("f-ok", "src/api/handler.go"),
		ownershipFileFact("f-escape", "../beta/src/leak.go"),
	}

	mat, _ := buildCanonicalMaterialization(ownershipScope(), ownershipGeneration(), envelopes)

	const root = "/repos/alpha"
	for _, d := range mat.Directories {
		if !strings.HasPrefix(d.Path, root+"/") {
			t.Errorf(
				"directory %q is outside repository root %q but carries repo_id %q; "+
					"a Directory stamped with this repository owns another repository's path",
				d.Path, root, d.RepoID,
			)
		}
		if d.ParentPath != root && !strings.HasPrefix(d.ParentPath, root+"/") {
			t.Errorf(
				"directory %q has parent %q outside repository root %q",
				d.Path, d.ParentPath, root,
			)
		}
	}

	for _, f := range mat.Files {
		if f.DirPath == root {
			continue
		}
		if !strings.HasPrefix(f.DirPath, root+"/") {
			t.Errorf(
				"file %q is linked to directory %q outside repository root %q",
				f.Path, f.DirPath, root,
			)
		}
	}
}

// TestCanonicalFileRowsShareTheirDirectoryRowsRepositoryID pins the invariant
// the directory query reads: every File the projector CONTAINS-links to a
// Directory carries that Directory's repo_id, so counting files under a
// Directory admitted by repo_id counts only that repository's files.
func TestCanonicalFileRowsShareTheirDirectoryRowsRepositoryID(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		ownershipRepositoryFact(),
		ownershipFileFact("f-1", "src/api/handler.go"),
		ownershipFileFact("f-2", "src/api/router.go"),
		ownershipFileFact("f-3", "cmd/tool/main.go"),
		ownershipFileFact("f-root", "README.go"),
		ownershipFileFact("f-escape", "../beta/src/leak.go"),
	}

	mat, _ := buildCanonicalMaterialization(ownershipScope(), ownershipGeneration(), envelopes)

	repoIDByDirectoryPath := make(map[string]string, len(mat.Directories))
	for _, d := range mat.Directories {
		repoIDByDirectoryPath[d.Path] = d.RepoID
	}

	if len(mat.Files) == 0 {
		t.Fatal("no files materialized; the assertions below would pass vacuously")
	}
	for _, f := range mat.Files {
		// Repository-root files carry no Directory CONTAINS edge; the canonical
		// writer links them to the Repository directly.
		if f.DirPath == "/repos/alpha" {
			continue
		}
		dirRepoID, ok := repoIDByDirectoryPath[f.DirPath]
		if !ok {
			t.Errorf("file %q names directory %q, which this projection never materialized", f.Path, f.DirPath)
			continue
		}
		if dirRepoID != f.RepoID {
			t.Errorf(
				"file %q (repo_id %q) is linked to directory %q (repo_id %q): "+
					"a directory query scoped to %q would count a file it was not granted",
				f.Path, f.RepoID, f.DirPath, dirRepoID, dirRepoID,
			)
		}
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/decode"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
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

	mat, _ := BuildMaterialization(ownershipScope(), ownershipGeneration(), envelopes)

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

// TestCanonicalEscapingRelativePathIsQuarantinedNotSilentlyDropped is the
// failing regression for the missing operator signal on the guard above.
//
// isRepositoryLocalRelativePath discards a file fact that WOULD have produced
// graph rows. Dropped with no counter and no log, that fact is
// indistinguishable at 3 AM from one the collector never emitted: "this file is
// missing from the graph" has no evidence behind it either way. The package
// already owns the visible dead-letter for a fact an extractor refuses --
// QuarantinedFact carried out of BuildMaterialization and recorded by
// RecordQuarantinedFacts as the eshu_dp_projector_input_invalid_facts_total
// increment plus a structured error log -- so this asserts the escaping fact
// takes that established path rather than a bare continue.
func TestCanonicalEscapingRelativePathIsQuarantinedNotSilentlyDropped(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		ownershipRepositoryFact(),
		ownershipFileFact("f-ok", "src/api/handler.go"),
		ownershipFileFact("f-escape", "../beta/src/leak.go"),
	}

	mat, quarantined := BuildMaterialization(ownershipScope(), ownershipGeneration(), envelopes)

	if len(mat.Files) != 1 {
		t.Fatalf("len(mat.Files) = %d, want 1: the escaping fact must still be dropped", len(mat.Files))
	}

	var escaped *decode.QuarantinedFact
	for i := range quarantined {
		if quarantined[i].FactID == "f-ok" {
			t.Errorf("valid fact f-ok was quarantined: %+v", quarantined[i])
		}
		if quarantined[i].FactID == "f-escape" {
			escaped = &quarantined[i]
		}
	}
	if escaped == nil {
		t.Fatalf(
			"file fact f-escape was dropped with no QuarantinedFact (quarantined = %+v); "+
				"RecordQuarantinedFacts emits no counter and no log for it, so an "+
				"operator cannot tell a dropped file from one never emitted",
			quarantined,
		)
	}
	if escaped.Field != "relative_path" {
		t.Errorf("quarantined field = %q, want %q: the log must name the field that was invalid", escaped.Field, "relative_path")
	}
	if escaped.Classification != factschema.ClassificationInputInvalid {
		t.Errorf(
			"quarantined classification = %q, want %q: RecordQuarantinedFacts labels the counter by it",
			escaped.Classification, factschema.ClassificationInputInvalid,
		)
	}
	if stage := decode.QuarantinedFactStage(escaped.FactKind); stage != decode.CodegraphCanonicalStage {
		t.Errorf(
			"QuarantinedFactStage(%q) = %q, want %q: the dead-letter must be attributed to the extractor that dropped it",
			escaped.FactKind, stage, decode.CodegraphCanonicalStage,
		)
	}
}

// TestCanonicalFileRowsShareTheirDirectoryRowsRepositoryID is the positive pin
// for the guard above: every File the projector emits names a Directory this
// same projection materialized, so no file is left pointing at a directory row
// that was rejected or never written.
//
// The load-bearing assertion is the `ok` lookup. The repo_id comparison after
// it cannot fail within one materialization -- buildDirectoryChain and
// extractFilesWithQuarantine are handed the same repoID value -- so it does not
// today prove the directory query's grant premise; it pins the invariant
// against a future change that derives the two repo_ids from different sources.
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

	mat, _ := BuildMaterialization(ownershipScope(), ownershipGeneration(), envelopes)

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

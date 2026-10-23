// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"testing"
	"time"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestCanonicalNodeWriterPinsChainedDeltaWriterOrder pins the #8780 writer
// order for a cumulative delta over an active delta. The graph holds d1's
// tree: a.go's entity stamped d1 (aChanged), e.go added by d1. The cumulative
// d2 overlays F (a, b, c, d): a.go reverts to F's bytes, so its entity comes
// from the base row and must be upserted under d2 before any entity retract
// (the retract removes generation_id <> d2 under a.go, deleting d1's stale
// aChanged); e.go is deleted since the active commit, so the deleted-file
// retract names e.go only.
func TestCanonicalNodeWriterPinsChainedDeltaWriterOrder(t *testing.T) {
	t.Parallel()
	repo, err := repositoryidentity.MetadataFor("proof-8780", "/repos/proof-8780", "https://github.com/eshu-proof/proof-8780.git")
	if err != nil {
		t.Fatalf("MetadataFor() error = %v", err)
	}
	repoPath := repo.LocalPath
	const scopeID = "git-repository-scope:chain"
	atF := time.Date(2026, 10, 23, 11, 0, 0, 0, time.UTC)
	atD2 := atF.Add(2 * time.Hour)
	fileFacts := func(gen string, at time.Time, rel, entity string) []facts.Envelope {
		return []facts.Envelope{
			gitcontent.FileFactEnvelope(repoPath, repo.ID, scopeID, gen, at, map[string]any{"path": repoPath + "/" + rel, "language": "go"}, false),
			gitcontent.ContentFactEnvelope(repoPath, repo.ID, scopeID, gen, at, model.ContentFileSnapshot{
				RelativePath: rel, Body: "package p\nfunc " + entity + "() {}\n", Digest: "d-" + gen + "-" + rel, Language: "go",
			}),
			gitcontent.ContentEntityFactEnvelope(repoPath, repo.ID, scopeID, gen, at, model.ContentEntitySnapshot{
				EntityID: "fn-" + gen + "-" + entity, RelativePath: rel, EntityType: "Function", EntityName: entity,
				StartLine: 2, EndLine: 2, Language: "go", IndexedAt: at,
			}),
		}
	}
	base := []facts.Envelope{gitcontent.RepositoryFactEnvelope(repoPath, repo, "run-f", scopeID, "gen-f", atF, 4, nil, false, "main", nil, false, nil, nil, false, false)}
	for _, name := range []string{"a", "b", "c", "d"} {
		base = append(base, fileFacts("gen-f", atF, name+".go", name)...)
	}
	delta := []facts.Envelope{gitcontent.RepositoryFactEnvelope(repoPath, repo, "run-d2", scopeID, "gen-d2", atD2, 2, nil, false, "main", nil,
		true, []string{"b.go", "f.go"}, []string{"e.go"}, false, false)}
	delta = append(delta, fileFacts("gen-d2", atD2, "b.go", "bChanged")...)
	delta = append(delta, fileFacts("gen-d2", atD2, "f.go", "f")...)
	tombstone := model.FactEnvelope("file", scopeID, "gen-d2", atD2, "file:"+repo.ID+":e.go", map[string]any{
		"graph_id": repo.ID + ":e.go", "graph_kind": "file", "repo_id": repo.ID, "relative_path": "e.go", "is_dependency": false,
	}, repoPath+"/e.go")
	tombstone.IsTombstone = true
	delta = append(delta, tombstone)

	union := facts.OverlayDeltaBase(base, delta, facts.ParseDeltaScope(delta))
	sc := scope.IngestionScope{
		ScopeID: scopeID, SourceSystem: "git", ScopeKind: scope.KindRepository, CollectorKind: scope.CollectorGit,
		PartitionKey: repo.ID, PreviousGenerationExists: true, ActiveGenerationID: "gen-d1",
		Metadata: map[string]string{"repo_id": repo.ID, "local_path": repoPath},
	}
	gen := scope.ScopeGeneration{
		GenerationID: "gen-d2", ScopeID: scopeID, ObservedAt: atD2, IngestedAt: atD2, Status: scope.GenerationStatusPending,
		TriggerKind: scope.TriggerKindSnapshot, IsDelta: true, DeltaBaselineCommitSHA: "d1", DeltaUnionBaseCommitSHA: "f",
		DeltaUnionBaseGenerationID: "gen-f",
	}
	mat, _, err := canonical.BuildMaterialization(sc, gen, union)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v", err)
	}
	exec := &mockExecutor{}
	if err := NewCanonicalNodeWriter(exec, 500, nil).Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	phaseOf := func(call Statement) string {
		phase, _ := call.Parameters[StatementMetadataPhaseKey].(string)
		return phase
	}
	ePath, aPath := repoPath+"/e.go", repoPath+"/a.go"
	firstEntityRetract, restampedA := -1, -1
	for i, call := range exec.calls {
		paths, _ := call.Parameters["file_paths"].([]string)
		_, hasGeneration := call.Parameters["generation_id"]
		if i == 0 && (phaseOf(call) != "retract" || hasGeneration || !reflect.DeepEqual(sortedStrings(paths), []string{ePath})) {
			t.Fatalf("statement 0 = %s %v, want the deleted-file retract naming only %s", phaseOf(call), paths, ePath)
		}
		if phaseOf(call) == "entity_retract" && firstEntityRetract < 0 {
			firstEntityRetract = i
			if got := call.Parameters["generation_id"]; got != "gen-d2" {
				t.Fatalf("entity retract #%d generation_id = %v, want gen-d2", i, got)
			}
		}
		if phaseOf(call) != "entities" || call.Parameters["_eshu_entity_label"] != "Function" {
			continue
		}
		rows, _ := call.Parameters["rows"].([]map[string]any)
		for _, row := range rows {
			props, _ := row["props"].(map[string]any)
			if props["name"] == "a" && props["path"] == aPath {
				if props["generation_id"] != "gen-d2" {
					t.Fatalf("a.go base entity props = %v, want generation_id gen-d2", props)
				}
				restampedA = i
			}
		}
	}
	if restampedA < 0 || firstEntityRetract < 0 || restampedA > firstEntityRetract {
		t.Fatalf("a.go entity upsert at #%d, first entity retract at #%d; the upsert must come first", restampedA, firstEntityRetract)
	}
	if !reflect.DeepEqual(mat.DeltaDeletedFilePaths, []string{ePath}) {
		t.Fatalf("delta deleted file paths = %v, want only %s", mat.DeltaDeletedFilePaths, ePath)
	}
}

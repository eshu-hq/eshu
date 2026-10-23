// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/code"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// #8779: a delta projected over its #7797 union base writes the full tree but
// derives reducer intents from its own facts only. The base rows below are the
// ones the git collector emits on a full generation and withholds on a delta
// (collector/repo/git/fact_builder.go): the workload_identity shared_followup
// and the code_dataflow_scanned marker. Under the delta they would fire
// workload_identity and the three value-flow domains, whose handlers load the
// delta's facts alone and retract the whole scope.

const (
	unionIntentScopeID = "git-repository-scope:union-intents-8779"
	unionIntentDeltaID = "gen-8779-delta-b"
	unionIntentBaseID  = "gen-8779-base-a"
)

// unionIntentInput builds a union-shaped projection input: base facts for the
// untouched other.go plus the base-only follow-ups, then the delta's own facts
// for keep.go, including a follow-up the collector does emit on a delta.
func unionIntentInput(t *testing.T) (scope.IngestionScope, scope.ScopeGeneration, []facts.Envelope) {
	t.Helper()
	repo, err := repositoryidentity.MetadataFor("union-intents-8779", "/repos/union-intents-8779",
		"https://github.com/eshu-proof/union-intents-8779.git")
	if err != nil {
		t.Fatalf("repository identity: %v", err)
	}
	repoPath := repo.LocalPath
	baseAt := time.Date(2026, time.October, 22, 11, 0, 0, 0, time.UTC)
	deltaAt := baseAt.Add(time.Hour)
	scopeValue := scope.IngestionScope{
		ScopeID: unionIntentScopeID, SourceSystem: "git", ScopeKind: scope.KindRepository,
		CollectorKind: scope.CollectorGit, PartitionKey: repo.ID,
		Metadata: map[string]string{"repo_id": repo.ID, "local_path": repoPath},
	}
	generation := scope.ScopeGeneration{
		GenerationID: unionIntentDeltaID, ScopeID: unionIntentScopeID,
		ObservedAt: deltaAt, IngestedAt: deltaAt, Status: scope.GenerationStatusPending,
		TriggerKind: scope.TriggerKindSnapshot, IsDelta: true,
		DeltaBaselineCommitSHA:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa8779",
		DeltaUnionBaseGenerationID: unionIntentBaseID,
	}
	fileFacts := func(generationID string, observed time.Time, rel, body, entity string) []facts.Envelope {
		return []facts.Envelope{
			gitcontent.FileFactEnvelope(repoPath, repo.ID, unionIntentScopeID, generationID, observed,
				map[string]any{"path": repoPath + "/" + rel, "language": "go"}, false),
			gitcontent.ContentFactEnvelope(repoPath, repo.ID, unionIntentScopeID, generationID, observed,
				model.ContentFileSnapshot{RelativePath: rel, Body: body, Digest: "digest-" + generationID + "-" + rel, Language: "go"}),
			gitcontent.ContentEntityFactEnvelope(repoPath, repo.ID, unionIntentScopeID, generationID, observed,
				model.ContentEntitySnapshot{
					EntityID: "fn-" + generationID + "-" + entity, RelativePath: rel, EntityType: "Function",
					EntityName: entity, StartLine: 2, EndLine: 2, Language: "go", IndexedAt: observed,
				}),
		}
	}
	input := fileFacts(unionIntentBaseID, baseAt, "other.go", "package p\nfunc other() {}\n", "other")
	input = append(input,
		model.FactEnvelope("shared_followup", unionIntentScopeID, unionIntentBaseID, baseAt,
			"shared_followup:"+repo.ID+":workload_identity", map[string]any{
				"reducer_domain": "workload_identity", "entity_key": "workload:" + repo.ID,
				"reason": "repository snapshot emitted shared workload identity follow-up", "repo_id": repo.ID,
			}, repoPath),
		model.FactEnvelope(code.DataflowScannedFactKind, unionIntentScopeID, unionIntentBaseID, baseAt,
			code.DataflowScannedFactKind+":"+repo.ID, map[string]any{
				"reason": "value-flow gate scanned the repository snapshot", "repo_id": repo.ID,
			}, repoPath),
		gitcontent.RepositoryFactEnvelope(repoPath, repo, "run-delta", unionIntentScopeID, unionIntentDeltaID, deltaAt, 1,
			nil, false, "main", nil, true, []string{"keep.go"}, nil, false, false), // delta, reconcile, emptyScope
		model.FactEnvelope("shared_followup", unionIntentScopeID, unionIntentDeltaID, deltaAt,
			"shared_followup:"+repo.ID+":rationale_materialization", map[string]any{
				"reducer_domain": "rationale_materialization", "entity_key": "repo:" + repo.ID,
				"reason": "repository snapshot emitted rationale follow-up", "repo_id": repo.ID,
			}, repoPath),
	)
	input = append(input, fileFacts(unionIntentDeltaID, deltaAt, "keep.go", "package p\nfunc keepNew() {}\n", "keepNew")...)
	return scopeValue, generation, input
}

func TestProjectDeltaOverUnionBaseDerivesIntentsFromDeltaFactsOnly(t *testing.T) {
	t.Parallel()
	scopeValue, generation, input := unionIntentInput(t)
	canonicalWriter := &recordingCanonicalWriter{}
	contentWriter := &recordingContentWriter{}
	intentWriter := &recordingIntentWriter{}
	runtime := Runtime{CanonicalWriter: canonicalWriter, ContentWriter: contentWriter, IntentWriter: intentWriter}

	if _, err := runtime.Project(context.Background(), scopeValue, generation, input); err != nil {
		t.Fatalf("Project(delta over union base) error = %v, want nil", err)
	}

	var domains []string
	for _, batch := range intentWriter.calls {
		for _, intent := range batch {
			if intent.GenerationID != unionIntentDeltaID || intent.ScopeID != unionIntentScopeID {
				t.Fatalf("intent %s attributed to %s/%s, want %s/%s", intent.Domain,
					intent.ScopeID, intent.GenerationID, unionIntentScopeID, unionIntentDeltaID)
			}
			domains = append(domains, string(intent.Domain))
		}
	}
	sort.Strings(domains)
	t.Logf("reducer intents: %v", domains)
	for _, banned := range []string{"workload_identity", "code_taint_evidence", "code_interproc_evidence", "code_function_summary"} {
		for _, domain := range domains {
			if domain == banned {
				t.Fatalf("base-only follow-up %q fired under the delta (intents: %v)", banned, domains)
			}
		}
	}
	if !containsString(domains, "rationale_materialization") {
		t.Fatalf("the delta's own rationale follow-up did not fire (intents: %v)", domains)
	}

	if len(canonicalWriter.calls) != 1 {
		t.Fatalf("canonical writes = %d, want 1", len(canonicalWriter.calls))
	}
	mat := canonicalWriter.calls[0]
	var files []string
	for _, file := range mat.Files {
		files = append(files, file.RelativePath)
	}
	sort.Strings(files)
	if got, want := strings.Join(files, ","), "keep.go,other.go"; got != want || mat.GenerationID != unionIntentDeltaID || !mat.DeltaProjection {
		t.Fatalf("canonical files = %s, generation = %q, delta = %v; want %s, %q, true",
			got, mat.GenerationID, mat.DeltaProjection, want, unionIntentDeltaID)
	}
	if len(contentWriter.calls) != 1 {
		t.Fatalf("content writes = %d, want 1", len(contentWriter.calls))
	}
	var paths []string
	for _, record := range contentWriter.calls[0].Records {
		paths = append(paths, record.Path)
	}
	sort.Strings(paths)
	if got, want := strings.Join(paths, ","), "keep.go,other.go"; got != want {
		t.Fatalf("content records = %s, want %s (the base's untouched other.go must be written)", got, want)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

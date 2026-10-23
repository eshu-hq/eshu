// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8779 disposable PostgreSQL proof that a git delta generation projects over its #7797 union base through the production projector service (ProjectorQueue claim, union FactStore load, runtime Project, content writer, reducer queue, Ack activation) without base facts tripping the fact boundary check or re-firing base-only reducer follow-ups under either generation; isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

// Live proof for #8779 (ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN, a disposable
// database). A full generation A is committed through the production
// IngestionStore with the git collector's own envelope builders and projected
// through the production projector Service; a delta B diffed from A is then
// committed and projected the same way. The union FactStore loads B overlaid
// on A, so the projection input carries base rows that keep A's generation id.
// The delta must project and activate, the content store and the canonical
// materialization must hold the union tree, and the reducer intents must
// match what B's own facts derive: base-only follow-ups (the shared_followup
// rows and the dataflow-scanned marker a delta never emits, see
// internal/collector/repo/git/fact_builder.go) must not re-fire.

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/code"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	deltaProjCommitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa8779"
	deltaProjCommitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb8779"
	deltaProjGenA    = "gen-8779-full-a"
	deltaProjGenB    = "gen-8779-delta-b"
)

// deltaProjFile is one file a proof generation carries.
type deltaProjFile struct {
	rel, body, entity string
}

// deltaProjGen is one proof generation: its files, and for a delta the
// changed and deleted paths relative to its baseline.
type deltaProjGen struct {
	id, commit, baseline string
	delta                bool
	files                []deltaProjFile
	changed, deleted     []string
}

var (
	// deltaProjFullA is the base tree {keep.go, w.go, other.go}.
	deltaProjFullA = deltaProjGen{
		id: deltaProjGenA, commit: deltaProjCommitA,
		files: []deltaProjFile{
			{"keep.go", "package p\nfunc keep() {}\n", "keep"},
			{"w.go", "package p\nfunc w() {}\n", "w"},
			{"other.go", "package p\nfunc other() {}\n", "other"},
		},
	}
	// deltaProjDeltaB modifies keep.go, adds x.go, deletes w.go, and leaves
	// other.go untouched, so the union tree is {keep.go, other.go, x.go}.
	deltaProjDeltaB = deltaProjGen{
		id: deltaProjGenB, commit: deltaProjCommitB, baseline: deltaProjCommitA, delta: true,
		files: []deltaProjFile{
			{"keep.go", "package p\nfunc keepNew() {}\n", "keepNew"},
			{"x.go", "package p\nfunc x() {}\n", "x"},
		},
		changed: []string{"keep.go", "x.go"},
		deleted: []string{"w.go"},
	}
)

// deltaProjHarness drives the production projector service over one
// bootstrapped proof schema.
type deltaProjHarness struct {
	t         *testing.T
	proof     *reprojectProof
	repo      repositoryidentity.Metadata
	canonical *recordingCanonicalWriter
	observed  time.Time
}

func newDeltaProjHarness(t *testing.T) *deltaProjHarness {
	t.Helper()
	proof := openReprojectProof(t)
	repo, err := repositoryidentity.MetadataFor("proof-8779", "/repos/proof-8779",
		"https://github.com/eshu-proof/proof-8779.git")
	if err != nil {
		t.Fatalf("repository identity: %v", err)
	}
	// Mirrors the git collector's buildScope for a default-branch repository,
	// as the cmd/projector overlay harness does.
	proof.scope = scope.IngestionScope{
		ScopeID: "git-repository-scope:" + repo.ID, SourceSystem: "git", ScopeKind: scope.KindRepository,
		CollectorKind: scope.CollectorGit, PartitionKey: repo.ID,
		Metadata: map[string]string{
			"repo_id": repo.ID, "repo_name": repo.Name, "source_key": repo.ID,
			"remote_url": repo.RemoteURL, "local_path": repo.LocalPath,
		},
	}
	return &deltaProjHarness{
		t: t, proof: proof, repo: repo, canonical: &recordingCanonicalWriter{},
		observed: time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
	}
}

// commit stores gen through the production IngestionStore. Each generation is
// observed one minute after the previous one, so base rows sort before delta
// rows in the union's (observed_at, fact_id) order.
func (h *deltaProjHarness) commit(gen deltaProjGen) {
	h.t.Helper()
	h.observed = h.observed.Add(time.Minute)
	observed := h.observed
	scopeID := h.proof.scope.ScopeID
	generation := scope.ScopeGeneration{
		GenerationID: gen.id, ScopeID: scopeID, ObservedAt: observed, IngestedAt: observed,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint: "hint-" + gen.id, SourceCommitSHA: gen.commit, IsDelta: gen.delta,
		DeltaBaselineCommitSHA: gen.baseline,
	}
	repoPath := h.repo.LocalPath
	envelopes := []facts.Envelope{gitcontent.RepositoryFactEnvelope(
		repoPath, h.repo, "run-"+gen.id, scopeID, gen.id, observed, len(gen.files),
		nil, false, "main", nil, gen.delta, gen.changed, gen.deleted, false, false, // reconcile, emptyScope
	)}
	for _, file := range gen.files {
		envelopes = append(envelopes,
			gitcontent.FileFactEnvelope(repoPath, h.repo.ID, scopeID, gen.id, observed, map[string]any{
				"path": repoPath + "/" + file.rel, "language": "go",
			}, false),
			gitcontent.ContentFactEnvelope(repoPath, h.repo.ID, scopeID, gen.id, observed, model.ContentFileSnapshot{
				RelativePath: file.rel, Body: file.body, Digest: fmt.Sprintf("digest-%s-%s", gen.id, file.rel),
				Language: "go", CommitSHA: gen.commit,
			}),
			gitcontent.ContentEntityFactEnvelope(repoPath, h.repo.ID, scopeID, gen.id, observed, model.ContentEntitySnapshot{
				EntityID: "fn-8779-" + gen.id + "-" + file.entity, RelativePath: file.rel, EntityType: "Function",
				EntityName: file.entity, StartLine: 2, EndLine: 2, Language: "go", IndexedAt: observed,
			}),
		)
	}
	for _, rel := range gen.deleted {
		// The delta collector's tombstones for a deleted path, as the
		// cmd/projector union harness builds them.
		fileTombstone := model.FactEnvelope("file", scopeID, gen.id, observed, "file:"+h.repo.ID+":"+rel,
			map[string]any{
				"graph_id": h.repo.ID + ":" + rel, "graph_kind": "file", "repo_id": h.repo.ID,
				"relative_path": rel, "is_dependency": false,
			}, repoPath+"/"+rel)
		fileTombstone.IsTombstone = true
		contentTombstone := model.FactEnvelope("content", scopeID, gen.id, observed, "content:"+h.repo.ID+":"+rel,
			map[string]any{"content_path": rel, "content_digest": "", "repo_id": h.repo.ID}, repoPath+"/"+rel)
		contentTombstone.IsTombstone = true
		envelopes = append(envelopes, fileTombstone, contentTombstone)
	}
	if !gen.delta {
		envelopes = append(envelopes, deltaProjFullOnlyFollowups(repoPath, h.repo.ID, scopeID, gen.id, observed)...)
	}
	stream := make(chan facts.Envelope, len(envelopes))
	for _, envelope := range envelopes {
		stream <- envelope
	}
	close(stream)
	if err := h.proof.store.CommitScopeGeneration(context.Background(), h.proof.scope, generation, stream); err != nil {
		h.t.Fatalf("commit %s: %v", gen.id, err)
	}
}

// deltaProjFullOnlyFollowups builds two facts the git collector emits on a
// full generation only (fact_builder.go returns before them on a delta): the
// workload_identity shared_followup and the value-flow dataflow-scanned
// marker. Field names and stable keys copy workloadIdentityFactEnvelope and
// dataflowScannedFactEnvelope in internal/collector/repo/git/followup_facts.go.
// Their stable keys never appear in a delta, so the union keeps both base rows.
func deltaProjFullOnlyFollowups(repoPath, repoID, scopeID, generationID string, observed time.Time) []facts.Envelope {
	return []facts.Envelope{
		model.FactEnvelope("shared_followup", scopeID, generationID, observed,
			"shared_followup:"+repoID+":workload_identity", map[string]any{
				"reducer_domain": "workload_identity",
				"entity_key":     "workload:" + repoID,
				"reason":         "repository snapshot emitted shared workload identity follow-up",
				"repo_id":        repoID,
			}, repoPath),
		model.FactEnvelope(code.DataflowScannedFactKind, scopeID, generationID, observed,
			code.DataflowScannedFactKind+":"+repoID, map[string]any{
				"reason":  "value-flow gate scanned the repository snapshot",
				"repo_id": repoID,
			}, repoPath),
	}
}

// project drains the projector queue through the production Service: the
// ProjectorQueue claims, fences, marks, and acks; the union FactStore loads;
// the runtime projects onto the recording canonical writer, the production
// content writer, and the production reducer queue.
func (h *deltaProjHarness) project() {
	h.t.Helper()
	database := SQLDB{DB: h.proof.db}
	queue := NewProjectorQueue(database, "proof-8779-projector", time.Minute)
	service := projector.Service{
		PollInterval: time.Millisecond,
		WorkSource:   queue,
		FactStore:    NewFactStore(database),
		Runner: runtime.Runtime{
			CanonicalWriter: h.canonical,
			ContentWriter:   NewContentWriter(database),
			IntentWriter:    NewReducerQueue(database, "proof-8779-reducer", time.Minute),
		},
		WorkSink:           queue,
		Heartbeater:        queue,
		DeltaBaselineFence: queue,
		WriteMarker:        queue,
		Wait:               func(context.Context, time.Duration) error { return context.Canceled },
	}
	start := time.Now()
	err := service.Run(context.Background())
	h.t.Logf("projector service drained in %s (run error: %v)", time.Since(start), err)
}

// projectorRow is one generation's projector work row and generation status.
type projectorRow struct {
	workStatus, failureClass, failureMessage, generationStatus string
}

func (h *deltaProjHarness) projectorRow(generationID string) projectorRow {
	h.t.Helper()
	var row projectorRow
	if err := h.proof.db.QueryRowContext(context.Background(), `
SELECT w.status, COALESCE(w.failure_class, ''), COALESCE(w.failure_message, ''), g.status
FROM fact_work_items w
JOIN scope_generations g ON g.generation_id = w.generation_id
WHERE w.stage = 'projector' AND w.generation_id = $1`, generationID).Scan(
		&row.workStatus, &row.failureClass, &row.failureMessage, &row.generationStatus); err != nil {
		h.t.Fatalf("read projector row %s: %v", generationID, err)
	}
	return row
}

// reducerRows returns "<generation_id> <domain>" for every reducer work row.
func (h *deltaProjHarness) reducerRows() []string {
	h.t.Helper()
	rows, err := h.proof.db.QueryContext(context.Background(), `
SELECT generation_id, domain FROM fact_work_items
WHERE stage = 'reducer' AND scope_id = $1 ORDER BY generation_id, domain, work_item_id`, h.proof.scope.ScopeID)
	if err != nil {
		h.t.Fatalf("read reducer rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var generationID, domain string
		if err := rows.Scan(&generationID, &domain); err != nil {
			h.t.Fatalf("scan reducer row: %v", err)
		}
		out = append(out, generationID+" "+domain)
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("read reducer rows: %v", err)
	}
	return out
}

// contentFiles returns the content store's file set for the proof repository.
func (h *deltaProjHarness) contentFiles() []string {
	h.t.Helper()
	return h.queryStrings(`SELECT relative_path FROM content_files WHERE repo_id = $1 ORDER BY relative_path`, h.repo.ID)
}

func (h *deltaProjHarness) queryStrings(query string, args ...any) []string {
	h.t.Helper()
	rows, err := h.proof.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		h.t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var value sql.NullString
		if err := rows.Scan(&value); err != nil {
			h.t.Fatalf("scan %q: %v", query, err)
		}
		out = append(out, value.String)
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("query %q: %v", query, err)
	}
	return out
}

// TestDeltaGenerationProjectsOverUnionBaseLive is the #8779 acceptance proof.
// On main the first base fact in the union fails validateFactBoundary and the
// delta dead-letters as projection_bug, exactly as production reported.
func TestDeltaGenerationProjectsOverUnionBaseLive(t *testing.T) {
	h := newDeltaProjHarness(t)

	h.commit(deltaProjFullA)
	h.project()
	if row := h.projectorRow(deltaProjGenA); row.generationStatus != "active" {
		t.Fatalf("base full A = %+v, want an active generation before the delta", row)
	}
	reducerBefore := h.reducerRows()
	canonicalBefore := len(h.canonical.calls)

	h.commit(deltaProjDeltaB)
	h.project()

	row := h.projectorRow(deltaProjGenB)
	if row.workStatus != "succeeded" || row.generationStatus != "active" {
		t.Fatalf("delta B work status = %q, generation status = %q, failure_class = %q, failure_message = %q; "+
			"want the delta projected over its union base and activated",
			row.workStatus, row.generationStatus, row.failureClass, row.failureMessage)
	}
	if base := h.projectorRow(deltaProjGenA); base.generationStatus != "superseded" {
		t.Fatalf("base full A generation status = %q after the delta activated, want superseded", base.generationStatus)
	}

	// Canonical: the delta's materialization carries the union tree, stamped
	// with the delta generation and still file-scoped for retraction.
	if len(h.canonical.calls) <= canonicalBefore {
		t.Fatal("delta B wrote no canonical materialization")
	}
	mat := h.canonical.calls[len(h.canonical.calls)-1]
	if mat.GenerationID != deltaProjGenB || !mat.DeltaProjection {
		t.Fatalf("canonical generation = %q, delta projection = %v; want %q and true",
			mat.GenerationID, mat.DeltaProjection, deltaProjGenB)
	}
	files := make([]string, 0, len(mat.Files))
	for _, file := range mat.Files {
		files = append(files, file.RelativePath)
	}
	sort.Strings(files)
	if got, want := strings.Join(files, ","), "keep.go,other.go,x.go"; got != want {
		t.Fatalf("canonical files = %s, want %s (the base's untouched other.go must survive)", got, want)
	}

	// Content: the store holds the union tree; w.go's tombstone removed it.
	if got, want := strings.Join(h.contentFiles(), ","), "keep.go,other.go,x.go"; got != want {
		t.Fatalf("content_files = %s, want %s", got, want)
	}

	// Reducer intents: a delta projection derives the intents its own facts
	// derive. No new row may name the base generation, and base-only
	// follow-ups must not re-fire under the delta.
	var added []string
	beforeSet := make(map[string]int, len(reducerBefore))
	for _, entry := range reducerBefore {
		beforeSet[entry]++
	}
	for _, entry := range h.reducerRows() {
		if beforeSet[entry] > 0 {
			beforeSet[entry]--
			continue
		}
		added = append(added, entry)
	}
	t.Logf("reducer rows added by delta B: %v", added)
	for _, entry := range added {
		generationID, domain, _ := strings.Cut(entry, " ")
		if generationID != deltaProjGenB {
			t.Fatalf("delta projection enqueued reducer row %q for generation %q, want only %q", domain, generationID, deltaProjGenB)
		}
		if domain == "workload_identity" || domain == "code_taint_evidence" {
			t.Fatalf("delta projection re-fired base-only follow-up %q under the delta generation", domain)
		}
	}
}

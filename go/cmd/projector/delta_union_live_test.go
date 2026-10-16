// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// Live proof for the #7797 delta-base union end to end: a delta generation
// projected onto an empty graph restores the full repository tree, and a
// delta whose base facts are gone refuses without a partial write.
//
// The tests reuse the superseded_delta_overlay harness (trees below mirror
// its A/B shape): generations commit through the production IngestionStore
// with the collector's own envelope builders, and the production projector
// Service (with the union FactStore from buildProjectorService) projects
// onto the production canonical writer. Opt-in: live Postgres
// (ESHU_7389_PROOF_POSTGRES_DSN, a disposable database the harness resets)
// and live Neo4j (ESHU_NEO4J_URI/USERNAME/PASSWORD/DATABASE).
//
// Graph proof is direct node/edge inspection (File nodes via REPO_CONTAINS,
// per-file Function counts via the File-CONTAINS-Function shape the deadcode
// query surface runs in production). Query proof is the content store
// agreeing with the graph, plus the union outcome counter.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	unionCommitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
	unionCommitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb2"
)

var (
	unionGenA = overlayGen{
		id: "gen-union-a", commit: unionCommitA,
		files: []overlayFile{
			{"keep.go", "package p\nfunc keep() {}\n"},
			{"w.go", "package p\nfunc w() {}\n"},
			{"other.go", "package p\nfunc other() {}\n"},
		},
	}
	unionGenB = overlayGen{
		id: "gen-union-b", commit: unionCommitB, baseline: unionCommitA, delta: true,
		files: []overlayFile{
			{"keep.go", "package p\nfunc keep() {}\nfunc keepNew() {}\n"},
			{"x.go", "package p\nfunc x() {}\n"},
		},
		changed: []string{"keep.go", "x.go"},
		deleted: []string{"w.go"},
	}
)

// unionEntity is one content_entity fact a union generation carries.
type unionEntity struct {
	rel, name, id string
}

var (
	unionEntitiesA = []unionEntity{
		{"keep.go", "keep", "fn-union-keep-a"},
		{"w.go", "w", "fn-union-w-a"},
		{"other.go", "other", "fn-union-other"},
	}
	unionEntitiesB = []unionEntity{
		{"keep.go", "keepNew", "fn-union-keep-b"},
		{"x.go", "x", "fn-union-x-b"},
	}
)

// commitUnionGen persists gen exactly like overlayHarness.commit, plus one
// content_entity fact per unionEntity, in the same commit stream so the
// projection reads files and entities together.
func commitUnionGen(ctx context.Context, h *overlayHarness, gen overlayGen, entities []unionEntity) error {
	h.t.Helper()
	h.mu.Lock()
	h.seq = h.seq.Add(time.Second)
	observed := h.seq
	h.mu.Unlock()
	generation := scope.ScopeGeneration{
		GenerationID: gen.id, ScopeID: h.scope.ScopeID, ObservedAt: observed, IngestedAt: observed,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint: overlayContentHint(gen), SourceCommitSHA: gen.commit, IsDelta: gen.delta,
		DeltaBaselineCommitSHA: gen.baseline, ReconcileReason: h.reasonFor(ctx, gen),
	}
	h.mu.Lock()
	h.gens[gen.id] = generation
	h.mu.Unlock()
	repoPath := h.repo.LocalPath
	envelopes := []facts.Envelope{gitcontent.RepositoryFactEnvelope(
		repoPath, h.repo, "run-"+gen.id, h.scope.ScopeID, gen.id, observed, len(gen.files),
		nil, false, "main", nil, gen.delta, gen.changed, gen.deleted, false,
	)}
	for _, file := range gen.files {
		envelopes = append(envelopes,
			gitcontent.FileFactEnvelope(repoPath, h.repo.ID, h.scope.ScopeID, gen.id, observed, map[string]any{
				"path": repoPath + "/" + file.rel, "language": "go",
			}, false),
			gitcontent.ContentFactEnvelope(repoPath, h.repo.ID, h.scope.ScopeID, gen.id, observed, model.ContentFileSnapshot{
				RelativePath: file.rel, Body: file.body, Digest: fmt.Sprintf("digest-%s-%s", gen.id, file.rel),
				Language: "go", CommitSHA: gen.commit,
			}),
		)
	}
	for _, entity := range entities {
		envelopes = append(envelopes,
			gitcontent.ContentEntityFactEnvelope(repoPath, h.repo.ID, h.scope.ScopeID, gen.id, observed, model.ContentEntitySnapshot{
				EntityID: entity.id, RelativePath: entity.rel, EntityType: "Function",
				EntityName: entity.name, StartLine: 1, EndLine: 3, Language: "go",
				IndexedAt: observed,
			}),
		)
	}
	for _, rel := range gen.deleted {
		fileTombstone := model.FactEnvelope("file", h.scope.ScopeID, gen.id, observed, "file:"+h.repo.ID+":"+rel,
			map[string]any{
				"graph_id": h.repo.ID + ":" + rel, "graph_kind": "file", "repo_id": h.repo.ID,
				"relative_path": rel, "is_dependency": false,
			}, repoPath+"/"+rel)
		fileTombstone.IsTombstone = true
		contentTombstone := model.FactEnvelope("content", h.scope.ScopeID, gen.id, observed, "content:"+h.repo.ID+":"+rel,
			map[string]any{"content_path": rel, "content_digest": "", "repo_id": h.repo.ID}, repoPath+"/"+rel)
		contentTombstone.IsTombstone = true
		envelopes = append(envelopes, fileTombstone, contentTombstone)
	}
	stream := make(chan facts.Envelope, len(envelopes))
	for _, envelope := range envelopes {
		stream <- envelope
	}
	close(stream)
	if err := h.store.CommitScopeGeneration(ctx, h.scope, generation, stream); err != nil {
		h.t.Errorf("commit %s: %v", gen.id, err)
		return err
	}
	return nil
}

// unionGraphEntityCounts returns per-file Function node counts through the
// Repository-REPO_CONTAINS-File-CONTAINS-Function shape.
func unionGraphEntityCounts(ctx context.Context, h *overlayHarness) map[string]int {
	h.t.Helper()
	records := h.runCypher(ctx, `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e:Function)
RETURN f.relative_path AS rel, count(e) AS c`, map[string]any{"repo_id": h.repo.ID})
	out := make(map[string]int, len(records))
	for _, record := range records {
		rel, _ := record.Get("rel")
		var count int
		if c, ok := record.Get("c"); ok {
			_, _ = fmt.Sscan(fmt.Sprint(c), &count)
		}
		out[fmt.Sprint(rel)] = count
	}
	return out
}

// unionContentFiles returns the content store's file set for the proof repo.
func unionContentFiles(ctx context.Context, h *overlayHarness) []string {
	h.t.Helper()
	rows, err := h.sqlDB.QueryContext(ctx, `SELECT relative_path FROM content_files WHERE repo_id = $1 ORDER BY relative_path`, h.repo.ID)
	if err != nil {
		h.t.Fatalf("read content_files: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			h.t.Fatalf("scan content row: %v", err)
		}
		out = append(out, rel)
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("read content_files: %v", err)
	}
	return out
}

// unionCounterTotal sums eshu_dp_projector_delta_union_total for outcome.
func unionCounterTotal(ctx context.Context, t *testing.T, reader *sdkmetric.ManualReader, outcome string) int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, recorded := range scopeMetrics.Metrics {
			if recorded.Name != "eshu_dp_projector_delta_union_total" {
				continue
			}
			sum, ok := recorded.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == "outcome" && attr.Value.AsString() == outcome {
						total += point.Value
					}
				}
			}
		}
	}
	return total
}

func unionAssertEntityCounts(t *testing.T, label string, got map[string]int, want map[string]int) {
	t.Helper()
	for rel, count := range want {
		if got[rel] != count {
			t.Fatalf("%s: graph entities for %q = %d, want %d (all: %v)", label, rel, got[rel], count, got)
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			t.Fatalf("%s: unexpected graph entities for %q (all: %v)", label, rel, got)
		}
	}
}

// TestDeltaUnionProjectsFullTreeFromEmptyLive projects the delta B onto a
// wiped graph: the union must restore the full tree B {keep.go, x.go,
// other.go} with keep.go's new entity, not only B's own files.
func TestDeltaUnionProjectsFullTreeFromEmptyLive(t *testing.T) {
	dsn := overlayProofEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	h := newOverlayHarness(ctx, t, dsn)
	reader := h.withDriftReader(t)
	svc, _ := h.buildService(ctx)
	h.run(ctx, svc)

	if err := commitUnionGen(ctx, h, unionGenA, unionEntitiesA); err != nil {
		t.FailNow()
	}
	h.waitGeneration(ctx, unionGenA.id, "active")
	h.assertFiles(ctx, "baseline full A", []string{"keep.go", "other.go", "w.go"})
	unionAssertEntityCounts(t, "baseline full A", unionGraphEntityCounts(ctx, h),
		map[string]int{"keep.go": 1, "w.go": 1, "other.go": 1})

	h.runCypher(ctx, "MATCH (n) DETACH DELETE n", nil)

	if err := commitUnionGen(ctx, h, unionGenB, unionEntitiesB); err != nil {
		t.FailNow()
	}
	h.waitGeneration(ctx, unionGenB.id, "active")
	h.logState(ctx, "after delta B projected from empty graph")
	h.assertFiles(ctx, "delta B from empty graph", []string{"keep.go", "other.go", "x.go"})
	unionAssertEntityCounts(t, "delta B from empty graph", unionGraphEntityCounts(ctx, h),
		map[string]int{"keep.go": 1, "x.go": 1, "other.go": 1})
	if got, want := unionContentFiles(ctx, h), []string{"keep.go", "other.go", "x.go"}; !equalStrings(got, want) {
		t.Fatalf("content_files = %v, want %v (query surface must agree with the graph)", got, want)
	}
	for rel, gen := range h.graphFiles(ctx) {
		if gen != unionGenB.id {
			t.Fatalf("graph file %q generation = %q, want the delta generation", rel, gen)
		}
	}
	if got := unionCounterTotal(ctx, t, reader, "union_base_found"); got < 1 {
		t.Fatalf("union_base_found count = %d, want at least 1", got)
	}
}

// TestDeltaUnionBaseMissingRefusesWithoutPartialLive drains the base full
// generation's facts (the retention-prune shape), then projects the delta:
// the union refuses with the base-missing class, the graph keeps the full
// tree it had (never partial), and a reindex watermark routes the repair.
func TestDeltaUnionBaseMissingRefusesWithoutPartialLive(t *testing.T) {
	dsn := overlayProofEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	h := newOverlayHarness(ctx, t, dsn)
	svc, _ := h.buildService(ctx)
	h.run(ctx, svc)

	if err := commitUnionGen(ctx, h, unionGenA, unionEntitiesA); err != nil {
		t.FailNow()
	}
	h.waitGeneration(ctx, unionGenA.id, "active")
	h.assertFiles(ctx, "baseline full A", []string{"keep.go", "other.go", "w.go"})

	if _, err := h.sqlDB.ExecContext(ctx, `DELETE FROM fact_records WHERE scope_id = $1 AND generation_id = $2`,
		h.scope.ScopeID, unionGenA.id); err != nil {
		t.Fatalf("drain base facts: %v", err)
	}

	if err := commitUnionGen(ctx, h, unionGenB, unionEntitiesB); err != nil {
		t.FailNow()
	}
	h.waitGeneration(ctx, unionGenB.id, "superseded")
	h.logState(ctx, "after refused delta B")
	if class := h.workFailureClass(ctx, unionGenB.id); class != projector.DeltaBaselineBaseMissingClass {
		t.Fatalf("B failure_class = %q, want %q (refused before any write)", class, projector.DeltaBaselineBaseMissingClass)
	}
	if h.writeStarted(ctx, unionGenB.id) {
		t.Fatal("refused delta B started writing: the graph may hold a partial overlay")
	}
	h.assertFiles(ctx, "graph after refusal (must be unchanged)", []string{"keep.go", "other.go", "w.go"})
	var watermark string
	if err := h.sqlDB.QueryRowContext(ctx, `SELECT COALESCE(requested_at::text, '') FROM repository_reindex_requests WHERE scope_id = $1`,
		h.scope.ScopeID).Scan(&watermark); err != nil {
		t.Fatalf("read reindex watermark: %v (refusal must route to the forced-full path)", err)
	}
	if strings.TrimSpace(watermark) == "" {
		t.Fatal("no reindex watermark for the refused scope")
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	gotSorted := append([]string(nil), got...)
	wantSorted := append([]string(nil), want...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	for i := range gotSorted {
		if gotSorted[i] != wantSorted[i] {
			return false
		}
	}
	return true
}

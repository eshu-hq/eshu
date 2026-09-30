// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package reducer_test

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/query/repository"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// #7446 live graph truth for the path-less Repository stub. The
// repo_dependency and submodule_pin writers keep MERGE-by-id on the target
// Repository (arbiter ruling on #7446): after the path-conflict retirement
// removed an id, a later write naming it re-creates a path-less stub. The
// stub must be counted (eshu_dp_canonical_repository_stubs_created_total and
// the `canonical repository stub created` log), must never put two
// Repository nodes at one path, and must be reapable: once its owners
// retract their edges, the Repository orphan sweep deletes it.

const (
	stubRepoDependencySource = "resolver/cross-repo"
	stubSubmodulePinSource   = "reducer/submodule_pin"
	stubsMetric              = "eshu_dp_canonical_repository_stubs_created_total"
)

// stubProof carries what the stub leg asserts against.
type stubProof struct {
	oldID, newID, path string
	logs               func() []map[string]any
	instruments        *telemetry.Instruments
	reader             *sdkmetric.ManualReader
}

// edgeWriter returns the real shared-edge writer over the live executor,
// with the test's instruments and the (captured) default logger.
func (l *repoRetryLive) edgeWriter(instruments *telemetry.Instruments) *edgewriter.EdgeWriter {
	w := edgewriter.NewEdgeWriter(l.exec, 0)
	w.Instruments = instruments
	w.Logger = slog.Default()
	return w
}

// writeStubEdges runs the real repo_dependency and submodule_pin writes from
// the other fixture repository to target.
func (l *repoRetryLive) writeStubEdges(ctx context.Context, t *testing.T, w *edgewriter.EdgeWriter, target string) {
	t.Helper()
	otherID := l.repoID(retryFixtureOther.name)
	dependency := []reducer.SharedProjectionIntentRow{{
		IntentID: "intent-7446-dep-" + target, RepositoryID: otherID, GenerationID: "reducer-gen",
		Payload: map[string]any{
			"repo_id": otherID, "target_repo_id": target, "relationship_type": "DEPENDS_ON",
			"generation_id": "reducer-gen", "confidence": 0.9, "evidence_type": "test", "resolved_id": "resolved-7446",
			"evidence_count": 1, "resolution_source": "test", "rationale": "#7446", "source_tool": "test",
		},
	}}
	if _, err := w.WriteEdges(ctx, reducer.DomainRepoDependency, dependency, stubRepoDependencySource); err != nil {
		t.Fatalf("repo_dependency WriteEdges to %s: %v", target, err)
	}
	pin := []reducer.SharedProjectionIntentRow{{
		IntentID: "intent-7446-pin-" + target, RepositoryID: otherID,
		Payload: map[string]any{
			"parent_repo_id": otherID, "resolved_repo_id": target, "submodule_path": "vendor/payments",
			"generation_id": "reducer-gen",
		},
	}}
	if _, err := w.WriteEdges(ctx, reducer.DomainSubmodulePinEdges, pin, stubSubmodulePinSource); err != nil {
		t.Fatalf("submodule_pin WriteEdges to %s: %v", target, err)
	}
}

func (l *repoRetryLive) stubCounters(t *testing.T, reader *sdkmetric.ManualReader) (int64, int64) {
	t.Helper()
	return counterValue(t, reader, stubsMetric, "writer", telemetry.RepositoryStubWriterRepoDependency),
		counterValue(t, reader, stubsMetric, "writer", telemetry.RepositoryStubWriterSubmodulePin)
}

// assertStubRecreation drives the real writers at the retired id and asserts
// the stub's shape, its signal, and the query read over it. It leaves the
// stub in place, so the harness cleanup must remove it (#7445).
func (l *repoRetryLive) assertStubRecreation(ctx context.Context, t *testing.T, p stubProof) {
	t.Helper()
	w := l.edgeWriter(p.instruments)
	l.writeStubEdges(ctx, t, w, p.oldID)

	// repo_dependency runs first and creates the stub; submodule_pin then
	// MERGEs the existing node and creates nothing.
	if dep, pin := l.stubCounters(t, p.reader); dep != 1 || pin != 0 {
		t.Fatalf("%s repo_dependency=%d submodule_pin=%d, want 1 and 0", stubsMetric, dep, pin)
	}
	var created []map[string]any
	for _, entry := range p.logs() {
		if entry["msg"] == "canonical repository stub created" {
			created = append(created, entry)
		}
	}
	if len(created) != 1 {
		t.Fatalf("`canonical repository stub created` lines = %d, want 1: %v", len(created), created)
	}
	for key, want := range map[string]any{
		"level": "INFO", "writer": "repo_dependency", "target_repo_id": p.oldID,
		"source_repo_id": l.repoID(retryFixtureOther.name), "generation_id": "reducer-gen",
		"nodes_created": float64(1), "attributed": true,
	} {
		if created[0][key] != want {
			t.Fatalf("stub log %s = %#v, want %#v; entry = %v", key, created[0][key], want, created[0])
		}
	}

	rows, err := l.exec.readRows(ctx, `MATCH (r:Repository {id: $old})
OPTIONAL MATCH (r)<-[rel]-()
RETURN r.path AS path, r.evidence_source AS evidence_source, collect(type(rel)) AS incoming`, map[string]any{"old": p.oldID})
	if err != nil {
		t.Fatalf("read stub: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Repository nodes under the retired id = %d, want 1: %v", len(rows), rows)
	}
	stub := rows[0]
	if stub["path"] != nil {
		t.Fatalf("stub path = %#v, want null", stub["path"])
	}
	if stub["evidence_source"] != stubRepoDependencySource {
		t.Fatalf("stub evidence_source = %#v, want %q", stub["evidence_source"], stubRepoDependencySource)
	}
	var incoming []string
	for _, v := range stub["incoming"].([]any) {
		incoming = append(incoming, v.(string))
	}
	sort.Strings(incoming)
	if strings.Join(incoming, ",") != "DEPENDS_ON,PINS_SUBMODULE" {
		t.Fatalf("stub incoming edge types = %v, want [DEPENDS_ON PINS_SUBMODULE]", incoming)
	}
	pathParams := map[string]any{"path": p.path, "new": p.newID}
	if got := l.count(ctx, t, `MATCH (r:Repository {path: $path}) RETURN count(r) AS count`, pathParams); got != 1 {
		t.Fatalf("Repository nodes at the path = %d, want 1", got)
	}
	if got := l.count(ctx, t, `MATCH (r:Repository {path: $path}) WHERE r.id = $new RETURN count(r) AS count`, pathParams); got != 1 {
		t.Fatalf("new-id Repository at the path = %d, want 1", got)
	}

	// Query truth: the repository dependency read returns the target by the
	// id the graph holds (the stub), and nothing at the re-keyed path.
	deps, degraded := repository.QueryRepoDependencies(ctx, liveEdgeReader{exec: l.exec},
		map[string]any{"repo_id": l.repoID(retryFixtureOther.name)})
	if degraded {
		t.Fatal("QueryRepoDependencies degraded")
	}
	if len(deps) != 1 || deps[0]["type"] != "DEPENDS_ON" || deps[0]["target_id"] != p.oldID {
		t.Fatalf("QueryRepoDependencies = %v, want one DEPENDS_ON to %s", deps, p.oldID)
	}

	// Negative: a repo_dependency to a live projected id creates no node.
	l.writeStubEdges(ctx, t, w, p.newID)
	if dep, pin := l.stubCounters(t, p.reader); dep != 1 || pin != 0 {
		t.Fatalf("%s after a write to a live target: repo_dependency=%d submodule_pin=%d, want still 1 and 0", stubsMetric, dep, pin)
	}
	if got := l.count(ctx, t, `MATCH (r:Repository {path: $path}) RETURN count(r) AS count`, pathParams); got != 1 {
		t.Fatalf("Repository nodes at the path after a live-target write = %d, want 1", got)
	}
}

// RunSingle completes querycontract.GraphQuery for the query-truth read.
func (r liveEdgeReader) RunSingle(ctx context.Context, query string, params map[string]any) (map[string]any, error) {
	rows, err := r.exec.readRows(ctx, query, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// TestLiveRepositoryStubReapedAfterOwnerRetracts is the #7446 lifecycle
// contract: the stub is truthful while an owner asserts an edge into it, and
// reapable once they stop. Both owners retract by evidence_source, then the
// Repository orphan sweep marks and, past its TTL, deletes the stub, and
// leaves the projected new-id Repository alone.
func TestLiveRepositoryStubReapedAfterOwnerRetracts(t *testing.T) {
	live := openRepoRetryLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	writer := live.writerShapes()["atomic_group"]
	newID, oldID := live.repoID(retryFixtureRepo.name), live.repoID(retryFixtureRepo.name+"-rekeyed")
	otherID := live.repoID(retryFixtureOther.name)

	live.write(ctx, t, writer, live.materialization(retryFixtureOther, "gen-1", true))
	live.write(ctx, t, writer, live.materializationAs(retryFixtureRepo, oldID, "gen-1", true))
	live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-2", false))
	live.writeStubEdges(ctx, t, live.edgeWriter(nil), oldID)
	params := map[string]any{"old": oldID, "new": newID}
	if got := live.count(ctx, t, `MATCH (r:Repository {id: $old}) WHERE r.path IS NULL RETURN count(r) AS count`, params); got != 1 {
		t.Fatalf("positive control: path-less stub under the retired id = %d, want 1", got)
	}

	for _, stmt := range []cypher.Statement{
		cypher.BuildRetractRepoDependencyEdges([]string{otherID}, stubRepoDependencySource),
		cypher.BuildRetractSubmodulePinEdges([]string{otherID}, stubSubmodulePinSource),
	} {
		if err := live.exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("owner retract %q: %v", stmt.Cypher, err)
		}
	}
	if got := live.count(ctx, t, `MATCH (:Repository {id: $old})-[rel]-() RETURN count(rel) AS count`, params); got != 0 {
		t.Fatalf("edges on the stub after both owner retracts = %d, want 0", got)
	}

	sweep := cypher.NewOrphanSweepStore(live.exec, liveEdgeReader{exec: live.exec})
	now := time.Now().UTC()
	policy := cypher.OrphanSweepPolicy{
		OrphanTTL: time.Minute, BatchLimit: 1000, CountLimit: 1000,
		Labels: []string{string(cypher.OrphanSweepLabelRepository)},
	}
	for _, at := range []time.Time{now, now.Add(2 * time.Minute)} {
		policy.Now = at
		if _, err := sweep.SweepOrphanNodes(ctx, policy); err != nil {
			t.Fatalf("Repository orphan sweep at %s: %v", at, err)
		}
	}
	if got := live.count(ctx, t, `MATCH (r:Repository {id: $old}) RETURN count(r) AS count`, params); got != 0 {
		t.Fatalf("stub after owner retracts and the orphan sweep = %d, want 0 (reaped)", got)
	}
	if got := live.count(ctx, t, `MATCH (r:Repository {id: $new}) WHERE r.path IS NOT NULL RETURN count(r) AS count`, params); got != 1 {
		t.Fatalf("projected new-id Repository after the sweep = %d, want 1 (untouched)", got)
	}
}

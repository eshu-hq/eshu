// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package reducer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// #7324 live graph truth for the one Repository retirement contract A keeps:
// canonicalNodeRepositoryPathCleanupCypher DETACH DELETEs a DIFFERENT-id
// Repository still holding the projected path (repository_path is UNIQUE),
// and so drops every edge on it, including incoming edges written by other
// scopes' reducers that nothing re-arms. The retirement must say what it
// deleted: a `canonical repository retired` log and an
// eshu_dp_canonical_repository_retirements_total increment carrying the
// backend's own relationships-deleted count.

// retiredIncomingSeeds writes one incoming edge per reducer family the #7324
// ruling lists into the Repository that is about to be retired ($old). The
// source nodes are prefix-scoped so cleanup removes them.
var retiredIncomingSeeds = []string{
	`MATCH (r:Repository {id: $old}), (o:Repository {id: $other}) MERGE (o)-[x:DEPENDS_ON]->(r) SET x.evidence_source = 'test/7324'`,
	`MATCH (r:Repository {id: $old}), (o:Repository {id: $other}) MERGE (o)-[x:DEPLOYS_FROM]->(r) SET x.evidence_source = 'test/7324'`,
	`MATCH (r:Repository {id: $old}), (o:Repository {id: $other}) MERGE (o)-[x:DISCOVERS_CONFIG_IN]->(r) SET x.evidence_source = 'test/7324'`,
	`MATCH (r:Repository {id: $old}), (o:Repository {id: $other}) MERGE (o)-[x:PROVISIONS_DEPENDENCY_FOR]->(r) SET x.evidence_source = 'test/7324'`,
	`MATCH (r:Repository {id: $old}), (o:Repository {id: $other}) MERGE (o)-[x:USES_MODULE]->(r) SET x.evidence_source = 'test/7324'`,
	`MATCH (r:Repository {id: $old}), (o:Repository {id: $other}) MERGE (o)-[x:READS_CONFIG_FROM]->(r) SET x.evidence_source = 'test/7324'`,
	`MATCH (r:Repository {id: $old}) MERGE (e:EvidenceArtifact {id: $prefix + '-retire-evidence'}) MERGE (e)-[x:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r) SET x.evidence_source = 'test/7324'`,
	`MATCH (r:Repository {id: $old}) MERGE (i:WorkloadInstance {id: $prefix + '-retire-instance'}) MERGE (i)-[x:DEPLOYMENT_SOURCE]->(r) SET x.evidence_source = 'test/7324'`,
	`MATCH (r:Repository {id: $old}), (o:Repository {id: $other}) MERGE (o)-[x:CORRELATES_DEPLOYABLE_UNIT]->(r) SET x.evidence_source = 'test/7324'`,
	`MATCH (r:Repository {id: $old}), (o:Repository {id: $other}) MERGE (o)-[x:PINS_SUBMODULE {path: 'vendor/payments'}]->(r) SET x.evidence_source = 'test/7324'`,
	`MATCH (r:Repository {id: $old}) MERGE (c:ContainerImage {uid: $prefix + '-retire-image'}) MERGE (c)-[x:BUILT_FROM]->(r) SET x.evidence_source = 'test/7324'`,
}

// materializationAs is materialization with every repository id rewritten
// to repoID, keeping fixture f's path: the re-keyed repository shape that
// makes the path-conflict retirement match.
func (l *repoRetryLive) materializationAs(f repoFixture, repoID, gen string, first bool) canonical.CanonicalMaterialization {
	mat := l.materialization(f, gen, first)
	mat.RepoID = repoID
	mat.ScopeID = "git-repository-scope:" + repoID
	mat.Repository.RepoID = repoID
	for i := range mat.Directories {
		mat.Directories[i].RepoID = repoID
	}
	for i := range mat.Files {
		mat.Files[i].RepoID = repoID
	}
	return mat
}

// captureJSONLogs routes the default slog logger to a JSON buffer at INFO
// for the test's lifetime and returns a reader of the decoded entries.
func captureJSONLogs(t *testing.T) func() []map[string]any {
	t.Helper()
	var mu sync.Mutex
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(lockedWriter{mu: &mu, buf: &buf}, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		var entries []map[string]any
		for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
			var entry map[string]any
			if json.Unmarshal(line, &entry) == nil {
				entries = append(entries, entry)
			}
		}
		return entries
	}
}

type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// counterValue sums the int64 counter name over points carrying key=value.
func counterValue(t *testing.T, reader *sdkmetric.ManualReader, name, key, value string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", name, m.Data)
			}
			for _, point := range sum.DataPoints {
				if got, ok := point.Attributes.Value(attribute.Key(key)); ok && got.AsString() == value {
					total += point.Value
				}
			}
		}
	}
	return total
}

// TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges is the
// #7324 retirement proof: a re-keyed repository (new id, same path) projected
// on the retry shape (FirstGeneration=false, DeltaProjection=false) retires
// the old-id Repository, and the writer reports the relationships the backend
// deleted with it, under both production executor shapes.
//
// relationships_deleted is asserted equal to the old node's full degree:
// the eleven seeded incoming families plus its own REPO_CONTAINS and
// depth-0 CONTAINS edges (measured 11 + 9 = 20), because the backend counts
// both directions and the writer does not attribute direction.
func TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges(t *testing.T) {
	live := openRepoRetryLive(t)
	for _, shape := range []string{"atomic_group", "phase_group"} {
		t.Run(shape, func(t *testing.T) {
			live.cleanup(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments: %v", err)
			}
			writer := live.instrumentedWriterShapes(instruments)[shape]
			newID, oldID := live.repoID(retryFixtureRepo.name), live.repoID(retryFixtureRepo.name+"-rekeyed")
			path := live.repoPath(retryFixtureRepo.name)

			live.write(ctx, t, writer, live.materialization(retryFixtureOther, "gen-1", true))
			live.write(ctx, t, writer, live.materializationAs(retryFixtureRepo, oldID, "gen-1", true))
			params := map[string]any{"old": oldID, "other": live.repoID(retryFixtureOther.name), "prefix": live.prefix}
			for _, seed := range retiredIncomingSeeds {
				live.run(ctx, t, seed, params)
			}
			incoming := live.count(ctx, t, `MATCH (:Repository {id: $old})<-[rel]-() RETURN count(rel) AS count`, params)
			if incoming != int64(len(retiredIncomingSeeds)) {
				t.Fatalf("positive control: incoming edges on the old-id Repository = %d, want %d", incoming, len(retiredIncomingSeeds))
			}
			// The retired node also carries its own projector edges, and the
			// backend's relationships-deleted count covers both directions:
			// pin the exact total so an undercount cannot pass.
			projectorOutgoing := live.count(ctx, t, `MATCH (:Repository {id: $old})-[rel:REPO_CONTAINS|CONTAINS]->() RETURN count(rel) AS count`, params)
			wantProjector := int64(len(retryFixtureRepo.files) + retryFixtureRepo.depthZeroDirectories())
			if projectorOutgoing != wantProjector {
				t.Fatalf("positive control: projector edges on the old-id Repository = %d, want %d", projectorOutgoing, wantProjector)
			}
			degree := live.count(ctx, t, `MATCH (:Repository {id: $old})-[rel]-() RETURN count(rel) AS count`, params)
			if degree != incoming+projectorOutgoing {
				t.Fatalf("positive control: old-id Repository degree = %d, want %d incoming + %d projector", degree, incoming, projectorOutgoing)
			}
			t.Logf("old-id Repository before retirement: incoming=%d projector outgoing=%d total degree=%d", incoming, projectorOutgoing, degree)

			logs := captureJSONLogs(t)
			live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-2", false))

			if got := live.count(ctx, t, `MATCH (r:Repository {id: $old}) RETURN count(r) AS count`, params); got != 0 {
				t.Fatalf("old-id Repository nodes after the path-conflict projection = %d, want 0 (retired)", got)
			}
			pathParams := map[string]any{"path": path, "new": newID}
			if got := live.count(ctx, t, `MATCH (r:Repository {path: $path}) WHERE r.id = $new RETURN count(r) AS count`, pathParams); got != 1 {
				t.Fatalf("new-id Repository at the path = %d, want 1", got)
			}
			// Contract A's documented gap, recorded rather than repaired: the
			// incoming families lived on the retired node and are gone.
			if got := live.count(ctx, t, `MATCH (:Repository {id: $new})<-[rel]-() WHERE rel.evidence_source = 'test/7324' RETURN count(rel) AS count`, pathParams); got != 0 {
				t.Fatalf("seeded incoming edges on the new-id Repository = %d, want 0 (they were on the retired node)", got)
			}

			var retired []map[string]any
			for _, entry := range logs() {
				if entry["msg"] == "canonical repository retired" {
					retired = append(retired, entry)
				}
			}
			if len(retired) != 1 {
				t.Fatalf("`canonical repository retired` log lines = %d, want 1 (the retirement is silent)", len(retired))
			}
			entry := retired[0]
			t.Logf("retirement log: %v", entry)
			for key, want := range map[string]any{
				"level": "WARN", "repo_id": newID, "path": path, "generation_id": "gen-2",
				"scope_id": "git-repository-scope:" + newID, "deletes_counted": true,
				"nodes_deleted": float64(1), "relationships_deleted": float64(degree),
			} {
				if entry[key] != want {
					t.Fatalf("retirement log %s = %#v, want %#v; entry = %v", key, entry[key], want, entry)
				}
			}
			const metricName = "eshu_dp_canonical_repository_retirements_total"
			if got := counterValue(t, reader, metricName, "outcome", "dropped_relationships"); got != 1 {
				t.Fatalf("%s{outcome=dropped_relationships} = %d, want 1", metricName, got)
			}
			if got := counterValue(t, reader, metricName, "outcome", "clean"); got != 0 {
				t.Fatalf("%s{outcome=clean} = %d, want 0", metricName, got)
			}

			// A steady-state retry of the same generation matches nothing:
			// no log line and no counter increment.
			live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-2", false))
			if got := counterValue(t, reader, metricName, "outcome", "dropped_relationships") +
				counterValue(t, reader, metricName, "outcome", "clean"); got != 1 {
				t.Fatalf("%s after a steady-state retry = %d, want still 1", metricName, got)
			}

			live.observeStubRecreation(ctx, t, oldID)
		})
	}
}

// observeStubRecreation records, without asserting it correct (#7324 arbiter
// "unverified"), what the stub-MERGE writers do to the retired id: the
// repo_dependency upsert and the submodule_pin edge both MERGE a Repository
// by id, so a later run naming the retired id can re-create it path-less.
func (l *repoRetryLive) observeStubRecreation(ctx context.Context, t *testing.T, oldID string) {
	t.Helper()
	otherID := l.repoID(retryFixtureOther.name)
	l.run(ctx, t, cypher.CanonicalRepoDependencyUpsertCypher, map[string]any{
		"repo_id": otherID, "target_repo_id": oldID,
		"evidence_source": "resolver/cross-repo", "generation_id": "reducer-gen", "confidence": 0.9,
		"evidence_type": "test", "resolved_id": "resolved-7324", "evidence_count": 1,
		"evidence_kinds": []string{"test"}, "resolution_source": "test", "rationale": "#7324", "source_tool": "test",
	})
	l.run(ctx, t, cypher.BatchCanonicalSubmodulePinEdgeCypher, map[string]any{"rows": []map[string]any{{
		"parent_repo_id": otherID, "resolved_repo_id": oldID, "submodule_path": "vendor/payments",
		"pinned_sha": nil, "generation_id": "reducer-gen", "evidence_source": "reducer/submodule_pin",
	}}})
	rows, err := l.exec.readRows(ctx, `MATCH (r:Repository {id: $old})
OPTIONAL MATCH (r)<-[rel]-()
RETURN r.path AS path, r.evidence_source AS evidence_source, collect(type(rel)) AS incoming`, map[string]any{"old": oldID})
	if err != nil {
		t.Fatalf("observe stub recreation: %v", err)
	}
	t.Logf("OBSERVED stub recreation after retirement: %d Repository node(s) under the retired id %s: %v", len(rows), oldID, rows)
}

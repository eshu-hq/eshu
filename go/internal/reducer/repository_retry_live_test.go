// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package reducer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/entity"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// #7285 live graph truth. A projector retry of a generation whose reducer
// follow-ups already succeeded (projector/service.go forces
// PreviousGenerationExists on attempt >= 2; the liveness re-drive reopens a
// succeeded row after reducer work drained) re-ran repository_cleanup, whose
// lookup=id statement DETACH DELETEd the Repository node and every reducer
// edge on it. The reducers are never re-armed (ON CONFLICT DO NOTHING), so the
// edges stayed lost: the empty-shell workloads on ops-qa.

var (
	retryFixtureRepo  = repoFixture{name: "payments", files: retryFixtureFiles, remoteURL: "https://example.invalid/payments.git"}
	retryFixtureOther = repoFixture{name: "ledger", files: []string{"README.md", "src/main.go"}}
)

// reducerEdgeSeeds writes one edge per reducer family the ruling's blast
// radius table lists, including the cross-scope INCOMING families written by
// another repository's scope. DEFINES, repository-side EXPOSES_ENDPOINT and
// DEPLOYMENT_SOURCE are written by the real workload materializer below; the
// rest use the family's relationship type and direction with test provenance.
var reducerEdgeSeeds = []string{
	`MATCH (r:Repository {id: $repo}), (o:Repository {id: $other}) MERGE (r)-[x:DEPENDS_ON]->(o) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}), (o:Repository {id: $other}) MERGE (o)-[x:DEPENDS_ON]->(r) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}), (o:Repository {id: $other}) MERGE (o)-[x:DEPLOYS_FROM]->(r) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}), (o:Repository {id: $other}) MERGE (r)-[x:DISCOVERS_CONFIG_IN]->(o) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}), (o:Repository {id: $other}) MERGE (o)-[x:PROVISIONS_DEPENDENCY_FOR]->(r) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}), (o:Repository {id: $other}) MERGE (r)-[x:USES_MODULE]->(o) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}), (o:Repository {id: $other}) MERGE (o)-[x:READS_CONFIG_FROM]->(r) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}), (o:Repository {id: $other}) MERGE (r)-[x:CORRELATES_DEPLOYABLE_UNIT]->(o) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}), (o:Repository {id: $other}) MERGE (o)-[x:CORRELATES_DEPLOYABLE_UNIT]->(r) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}), (o:Repository {id: $other}) MERGE (o)-[x:PINS_SUBMODULE]->(r) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}) MERGE (e:EvidenceArtifact {id: $prefix + '-evidence'}) MERGE (r)-[x:HAS_DEPLOYMENT_EVIDENCE]->(e) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}) MERGE (e:EvidenceArtifact {id: $prefix + '-evidence'}) MERGE (e)-[x:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}) MERGE (p:Platform {id: $prefix + '-platform'}) MERGE (r)-[x:PROVISIONS_PLATFORM]->(p) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}) MERGE (c:CodeownerTeam {id: $prefix + '-team'}) MERGE (r)-[x:DECLARES_CODEOWNER]->(c) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}) MERGE (p:Package {uid: $prefix + '-package'}) MERGE (r)-[x:PUBLISHES]->(p) SET x.evidence_source = 'test/7285'`,
	`MATCH (r:Repository {id: $repo}) MERGE (i:ContainerImage {uid: $prefix + '-image'}) MERGE (i)-[x:BUILT_FROM]->(r) SET x.evidence_source = 'test/7285'`,
}

// wantReducerEdgesOnRepo is the positive control: the per-type counts every
// retry must preserve on the Repository.
var wantReducerEdgesOnRepo = map[string]int64{
	"DEFINES": 1, "EXPOSES_ENDPOINT": 1, "DEPLOYMENT_SOURCE": 1, "DEPENDS_ON": 2,
	"DEPLOYS_FROM": 1, "DISCOVERS_CONFIG_IN": 1, "PROVISIONS_DEPENDENCY_FOR": 1,
	"USES_MODULE": 1, "READS_CONFIG_FROM": 1, "CORRELATES_DEPLOYABLE_UNIT": 2,
	"PINS_SUBMODULE": 1, "HAS_DEPLOYMENT_EVIDENCE": 1, "EVIDENCES_REPOSITORY_RELATIONSHIP": 1,
	"PROVISIONS_PLATFORM": 1, "DECLARES_CODEOWNER": 1, "PUBLISHES": 1, "BUILT_FROM": 1,
}

func (l *repoRetryLive) workloadID(repo string) string {
	return "workload:" + l.prefix + "-" + repo
}

// materializeRetryWorkloads runs the real WorkloadMaterializer: repo defines
// a workload that exposes an endpoint, and the OTHER repository's instance
// names repo as its deployment source (a cross-scope incoming edge).
func (l *repoRetryLive) materializeRetryWorkloads(ctx context.Context, t *testing.T) {
	t.Helper()
	if err := l.materializeRetryWorkloadsErr(ctx); err != nil {
		t.Fatal(err)
	}
}

// materializeRetryWorkloadsErr is materializeRetryWorkloads for goroutines.
func (l *repoRetryLive) materializeRetryWorkloadsErr(ctx context.Context) error {
	repoID, otherID := l.repoID(retryFixtureRepo.name), l.repoID(retryFixtureOther.name)
	own := &reducer.ProjectionResult{
		WorkloadRows: []reducer.WorkloadRow{{
			RepoID: repoID, WorkloadID: l.workloadID("payments"), WorkloadName: "payments",
			WorkloadKind: "service", Classification: "service", Confidence: 0.95,
		}},
		EndpointRows: []reducer.APIEndpointRow{{
			EndpointID: "endpoint:" + l.prefix + "-pay", RepoID: repoID,
			WorkloadID: l.workloadID("payments"), WorkloadName: "payments", Path: "/v1/pay",
		}},
	}
	other := &reducer.ProjectionResult{
		WorkloadRows: []reducer.WorkloadRow{{
			RepoID: otherID, WorkloadID: l.workloadID("ledger"), WorkloadName: "ledger",
			WorkloadKind: "service", Classification: "service", Confidence: 0.95,
		}},
		InstanceRows: []reducer.InstanceRow{{
			WorkloadID: l.workloadID("ledger"), InstanceID: "workload-instance:" + l.prefix + "-ledger:prod",
			WorkloadName: "ledger", WorkloadKind: "service", Classification: "service",
			Environment: "prod", RepoID: otherID, Confidence: 0.95,
		}},
		DeploymentSourceRows: []reducer.DeploymentSourceRow{{
			InstanceID: "workload-instance:" + l.prefix + "-ledger:prod", DeploymentRepoID: repoID, Confidence: 0.9,
		}},
	}
	for _, projection := range []*reducer.ProjectionResult{own, other} {
		if _, err := l.materializer().Materialize(ctx, projection); err != nil {
			return fmt.Errorf("materialize workloads: %w", err)
		}
	}
	return nil
}

func (l *repoRetryLive) seedReducerEdges(ctx context.Context, t *testing.T) {
	t.Helper()
	if err := l.seedReducerEdgesErr(ctx); err != nil {
		t.Fatal(err)
	}
}

// seedReducerEdgesErr is seedReducerEdges for goroutines.
func (l *repoRetryLive) seedReducerEdgesErr(ctx context.Context) error {
	params := map[string]any{
		"repo": l.repoID(retryFixtureRepo.name), "other": l.repoID(retryFixtureOther.name), "prefix": l.prefix,
	}
	for _, seed := range reducerEdgeSeeds {
		// Managed transaction: the driver retries a transient deadlock with a
		// concurrent projector write, as a production writer's retry would.
		if err := l.exec.ExecuteGroup(ctx, []cypher.Statement{{Cypher: seed, Parameters: params}}); err != nil {
			return fmt.Errorf("seed %q: %w", seed, err)
		}
	}
	return nil
}

// TestLiveRepositoryRetryKeepsReducerEdges is Test A of #7285 and matrix
// rows 1 and 4 (a reducer completes, then projector attempt N+1 of the same
// generation; the liveness re-drive writes the same shape). Every reducer
// edge family must survive, on the same Repository node, and the projector
// edges must be rebuilt without duplicates, under both production executor
// shapes.
func TestLiveRepositoryRetryKeepsReducerEdges(t *testing.T) {
	live := openRepoRetryLive(t)
	for shape, writer := range live.writerShapes() {
		t.Run(shape, func(t *testing.T) {
			live.cleanup(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			repoID := live.repoID(retryFixtureRepo.name)
			pinParams := map[string]any{"repo_id": repoID, "other": live.repoID(retryFixtureOther.name)}

			// P4 (#7324): the submodule_pin writer MERGEs the pinned
			// Repository as a path-less stub before its first projection;
			// the projector's MERGE + SET must adopt that node, not replace it.
			live.run(ctx, t, cypher.BatchCanonicalSubmodulePinEdgeCypher, map[string]any{"rows": []map[string]any{{
				"parent_repo_id": pinParams["other"], "resolved_repo_id": repoID, "submodule_path": "vendor/payments",
				"pinned_sha": nil, "generation_id": "reducer-gen", "evidence_source": "reducer/submodule_pin",
			}}})
			stubElement := live.repositoryElementID(ctx, t, repoID)

			live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-1", true))
			live.write(ctx, t, writer, live.materialization(retryFixtureOther, "gen-1", true))
			if got := live.repositoryElementID(ctx, t, repoID); got != stubElement {
				t.Fatalf("first projection replaced the submodule_pin stub Repository %s with %s", stubElement, got)
			}
			live.assertSubmodulePinSurvives(ctx, t, pinParams, "after stub adoption")
			live.materializeRetryWorkloads(ctx, t)
			live.seedReducerEdges(ctx, t)

			before := live.relationshipTypeCounts(ctx, t, repoID)
			if !reflect.DeepEqual(before, wantReducerEdgesOnRepo) {
				t.Fatalf("positive control: reducer edges before retry = %v, want %v", before, wantReducerEdgesOnRepo)
			}
			elementBefore := live.repositoryElementID(ctx, t, repoID)

			// Attempt 2 of the SAME generation: the retry shape.
			live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-1", false))

			after := live.relationshipTypeCounts(ctx, t, repoID)
			if !reflect.DeepEqual(after, wantReducerEdgesOnRepo) {
				t.Fatalf("reducer edges after projector retry = %v, want %v (the retry destroyed reducer-owned edges)", after, wantReducerEdgesOnRepo)
			}
			if got := live.repositoryElementID(ctx, t, repoID); got != elementBefore {
				t.Fatalf("Repository element id changed %s -> %s: the retry deleted and recreated the node", elementBefore, got)
			}
			live.assertProjectorEdges(ctx, t, retryFixtureRepo, "gen-1")
			live.assertQueryLayerDefiningRepo(ctx, t, repoID)

			// A new full generation that drops one file: reducer edges stay,
			// and no stale projector-owned edge survives (arbiter shim step 4).
			smaller := retryFixtureRepo
			smaller.files = retryFixtureFiles[:len(retryFixtureFiles)-1]
			live.write(ctx, t, writer, live.materialization(smaller, "gen-2", false))
			if got := live.relationshipTypeCounts(ctx, t, repoID); !reflect.DeepEqual(got, wantReducerEdgesOnRepo) {
				t.Fatalf("reducer edges after generation 2 = %v, want %v", got, wantReducerEdgesOnRepo)
			}
			live.assertProjectorEdges(ctx, t, smaller, "gen-2")

			// P3 (#7324): a delta generation skips repository_cleanup and
			// touches only its changed files, so every reducer edge and the
			// node itself survive it too.
			delta := live.materialization(smaller, "gen-3", false)
			delta.DeltaProjection = true
			delta.DeltaFilePaths = []string{delta.RepoPath + "/README.md"}
			delta.Files = delta.Files[:1]
			delta.Directories = nil
			live.write(ctx, t, writer, delta)
			if got := live.relationshipTypeCounts(ctx, t, repoID); !reflect.DeepEqual(got, wantReducerEdgesOnRepo) {
				t.Fatalf("reducer edges after delta generation 3 = %v, want %v", got, wantReducerEdgesOnRepo)
			}
			if got := live.repositoryElementID(ctx, t, repoID); got != elementBefore {
				t.Fatalf("Repository element id changed %s -> %s across the delta generation", elementBefore, got)
			}
			if got := live.count(ctx, t, `MATCH (:Repository {id: $repo_id})-[rel:REPO_CONTAINS]->(:File) RETURN count(rel) AS count`,
				map[string]any{"repo_id": repoID}); got != int64(len(smaller.files)) {
				t.Fatalf("REPO_CONTAINS after delta generation 3 = %d, want %d", got, len(smaller.files))
			}
			live.assertSubmodulePinSurvives(ctx, t, pinParams, "after delta generation 3")
		})
	}
}

// assertSubmodulePinSurvives checks the path-keyed PINS_SUBMODULE edge the
// submodule_pin writer created on the stub is still on the Repository.
func (l *repoRetryLive) assertSubmodulePinSurvives(ctx context.Context, t *testing.T, params map[string]any, when string) {
	t.Helper()
	if got := l.count(ctx, t, `MATCH (:Repository {id: $other})-[pin:PINS_SUBMODULE {path: 'vendor/payments'}]->(:Repository {id: $repo_id})
RETURN count(pin) AS count`, params); got != 1 {
		t.Fatalf("PINS_SUBMODULE {path: vendor/payments} %s = %d, want 1", when, got)
	}
}

// assertProjectorEdges checks REPO_CONTAINS and depth-0 CONTAINS equal the
// fixture's files and depth-0 directories, all stamped with gen, and that the
// id is still unique.
func (l *repoRetryLive) assertProjectorEdges(ctx context.Context, t *testing.T, f repoFixture, gen string) {
	t.Helper()
	params := map[string]any{"repo_id": l.repoID(f.name), "gen": gen}
	checks := []struct {
		label string
		query string
		want  int64
	}{
		{"Repository nodes with the id", `MATCH (r:Repository {id: $repo_id}) RETURN count(r) AS count`, 1},
		{"REPO_CONTAINS", `MATCH (:Repository {id: $repo_id})-[rel:REPO_CONTAINS]->(:File) RETURN count(rel) AS count`, int64(len(f.files))},
		{"depth-0 CONTAINS", `MATCH (:Repository {id: $repo_id})-[rel:CONTAINS]->(:Directory) RETURN count(rel) AS count`, int64(f.depthZeroDirectories())},
		{"stale projector edges", `MATCH (:Repository {id: $repo_id})-[rel]->()
WHERE rel.evidence_source = 'projector/canonical' AND rel.generation_id <> $gen
RETURN count(rel) AS count`, 0},
	}
	for _, check := range checks {
		if got := l.count(ctx, t, check.query, params); got != check.want {
			t.Fatalf("%s = %d, want %d", check.label, got, check.want)
		}
	}
}

// assertQueryLayerDefiningRepo is API truth (arbiter ruling, RED shape item
// 5): the production get_workload_context HTTP handler
// (query/entity Handler.GetWorkloadContext, behind the MCP tool of that name)
// reads the live graph and must answer with the workload's defining
// repository. Its repo_id comes from the workload's incoming DEFINES edge, so
// an empty-shell workload answers repo_id "".
func (l *repoRetryLive) assertQueryLayerDefiningRepo(ctx context.Context, t *testing.T, repoID string) {
	t.Helper()
	registerContextOverviewCapability.Do(func() {
		// Production registers it from root package query's capability
		// matrix, which this test binary does not link; use the same
		// family declaration (query/entity main_test.go does the same).
		querycontract.RegisterCapabilities(querycontract.CapabilityRegistration{
			Capability: repository.ContextOverviewCapability, Support: repository.ContextOverviewSupport(),
		})
	})
	handler := &entity.Handler{Neo4j: liveGraphQuery{exec: l.exec}, Profile: querycontract.ProfileLocalAuthoritative}
	workloadID := l.workloadID("payments")
	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/"+workloadID+"/context", nil).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType) // the {data, truth} shape MCP callers get
	req.SetPathValue("workload_id", workloadID)
	rec := httptest.NewRecorder()
	handler.GetWorkloadContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get_workload_context status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			ID     string `json:"id"`
			RepoID string `json:"repo_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode get_workload_context body %s: %v", rec.Body.String(), err)
	}
	if body.Data.ID != workloadID || body.Data.RepoID != repoID {
		t.Fatalf("get_workload_context = id %q repo_id %q, want %q defined by %q (empty-shell workload); body = %s",
			body.Data.ID, body.Data.RepoID, workloadID, repoID, rec.Body.String())
	}
}

// registerContextOverviewCapability registers the capability
// get_workload_context gates on, once per test binary.
var registerContextOverviewCapability sync.Once

// liveGraphQuery is the query layer's GraphQuery port over the live driver.
type liveGraphQuery struct{ exec provenanceReplayExecutor }

func (q liveGraphQuery) Run(ctx context.Context, cypherText string, params map[string]any) ([]map[string]any, error) {
	return q.exec.readRows(ctx, cypherText, params)
}

func (q liveGraphQuery) RunSingle(ctx context.Context, cypherText string, params map[string]any) (map[string]any, error) {
	rows, err := q.exec.readRows(ctx, cypherText, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// TestLiveRepositoryRetryRefreshesProjectorProperties is the live half of
// #7285 Test C. The node is now MERGEd in place instead of recreated, so its
// property map must still equal exactly what a fresh projection writes: a
// reducer stub created first (ON CREATE SET evidence_source, generation_id)
// is overwritten, and a transition (renamed, remote removed) leaves no stale
// value. The static half, TestRepositoryPropertyWritersStayInsideTheProjectorUpsert
// in storage/cypher, pins that no other writer sets a Repository property the
// upsert does not own.
func TestLiveRepositoryRetryRefreshesProjectorProperties(t *testing.T) {
	live := openRepoRetryLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	writer := live.writerShapes()["atomic_group"]
	repoID := live.repoID(retryFixtureRepo.name)
	live.write(ctx, t, writer, live.materialization(retryFixtureOther, "gen-1", true))
	live.run(ctx, t, cypher.CanonicalRepoDependencyUpsertCypher, map[string]any{
		"repo_id": repoID, "target_repo_id": live.repoID(retryFixtureOther.name),
		"evidence_source": "resolver/cross-repo", "generation_id": "reducer-gen", "confidence": 0.9,
		"evidence_type": "test", "resolved_id": "resolved-7285", "evidence_count": 1,
		"evidence_kinds": []string{"test"}, "resolution_source": "test", "rationale": "#7285", "source_tool": "test",
	})
	live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-1", true))

	renamed := live.materialization(retryFixtureRepo, "gen-2", false)
	renamed.Repository.Name = "payments-renamed"
	renamed.Repository.RemoteURL = ""
	renamed.Repository.HasRemote = false
	live.write(ctx, t, writer, renamed)

	want := map[string]any{
		"id": repoID, "name": "payments-renamed", "path": renamed.Repository.Path,
		"local_path": renamed.Repository.LocalPath, "remote_url": "", "repo_slug": renamed.Repository.RepoSlug,
		"has_remote": false, "scope_id": renamed.ScopeID, "generation_id": "gen-2",
		"evidence_source": "projector/canonical",
	}
	if got := live.repositoryProperties(ctx, t, repoID); !reflect.DeepEqual(got, want) {
		t.Fatalf("Repository properties after re-projection = %v, want exactly the fresh-projection map %v", got, want)
	}
	if got := live.relationshipTypeCounts(ctx, t, repoID)["DEPENDS_ON"]; got != 1 {
		t.Fatalf("reducer DEPENDS_ON on the stub-created Repository = %d, want 1", got)
	}
}

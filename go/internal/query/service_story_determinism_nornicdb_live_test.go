// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const serviceStoryDeterminismLiveEnv = "ESHU_SERVICE_STORY_DETERMINISM_NORNICDB_LIVE"

// TestServiceStoryTruncationSelectionIsDeterministicLiveNornicDB settles the
// #5724 open question: the #5644 fix collects the bounded row set, sorts it in
// Go, and truncates to the collection limit. That is only correct if the
// backend's `ORDER BY <total-order> LIMIT $sentinel` returns a DETERMINISTIC
// candidate subset that already contains the true lexicographic top-N. The
// unit tests in service_story_determinism_test.go only shuffle a FIXED,
// already-selected row set in memory, so they cannot observe whether the
// backend's SELECTION (which N rows survive the LIMIT) is stable and correct
// when the distinct candidate count exceeds the sentinel.
//
// This env-gated proof seeds an isolated NornicDB with MORE distinct rows than
// the sentinel and asserts two properties per call, repeated many times:
//
//  1. Stability: repeated identical calls return the same survivor set.
//  2. Correctness: that survivor set equals the lexicographic top-N computed
//     independently in the test from ALL seeded rows (not merely from the
//     subset the backend chose to return), so `ORDER BY ... LIMIT` selects the
//     correct candidate subset rather than only ordering delivery of an
//     arbitrary one.
//
// Both production plan shapes the fix depends on are proven through their real
// production functions at their real sentinels: the plain
// `MATCH ... ORDER BY ... LIMIT` runtime-instance read via
// fetchWorkloadRuntimeTopology (sentinel 51), and the aggregating
// `WITH ... collect() ... ORDER BY ... LIMIT` attached-platform read via
// fetchWorkloadPlatformResult (sentinel 2501).
//
// Run against an isolated NornicDB:
//
//	ESHU_SERVICE_STORY_DETERMINISM_NORNICDB_LIVE=1 \
//	ESHU_NEO4J_URI=bolt://localhost:37687 \
//	go test ./internal/query -run TestServiceStoryTruncationSelectionIsDeterministicLiveNornicDB -count=1 -v
func TestServiceStoryTruncationSelectionIsDeterministicLiveNornicDB(t *testing.T) {
	if strings.TrimSpace(os.Getenv(serviceStoryDeterminismLiveEnv)) == "" {
		t.Skip("set " + serviceStoryDeterminismLiveEnv + "=1 to run the live NornicDB selection-determinism proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open NornicDB driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	reader := NewNeo4jReader(driver, "nornic")
	requireIsolatedDeterminismGraph(ctx, t, reader)

	t.Run("runtime_instance_plain_order_by_limit", func(t *testing.T) {
		// 120 distinct instances against the 51-row sentinel, one platform each.
		seed := newLiveDeterminismSeed(t, ctx, driver, "runtime", contextStoryItemLimit*2+20, 1)
		defer seed.cleanup()
		seed.write()
		seed.assertRuntimeInstanceSelectionDeterministic(ctx, t, reader)
	})

	t.Run("attached_platform_aggregating_order_by_limit", func(t *testing.T) {
		// contextStoryItemLimit instances x (contextStoryItemLimit+1) platforms
		// = 2,550 distinct RUNS_ON edges against the 2,501-row sentinel. This is
		// the real production ceiling: fetchWorkloadPlatformResult restricts
		// i.id IN $instance_ids to the already-truncated topology, so at most
		// contextStoryItemLimit distinct instances can ever appear.
		seed := newLiveDeterminismSeed(t, ctx, driver, "platform", contextStoryItemLimit, contextStoryItemLimit+1)
		defer seed.cleanup()
		seed.write()
		seed.assertAttachedPlatformSelectionDeterministic(ctx, t, reader)
	})
}

// requireIsolatedDeterminismGraph refuses to run against a populated graph. The
// reads under test are unscoped and the seeds below write real Repository,
// Workload, WorkloadInstance, and Platform nodes, so a shared graph would both
// pollute the reads and expose retained evidence to this test's cleanup.
// Repository is included because a partially-failed cleanup can leave one
// behind, and an unchecked label would let that orphan go unnoticed forever.
func requireIsolatedDeterminismGraph(ctx context.Context, t *testing.T, reader GraphQuery) {
	t.Helper()
	for _, label := range []string{"Repository", "Workload", "WorkloadInstance", "Platform"} {
		countRow, err := reader.RunSingle(ctx, fmt.Sprintf("MATCH (n:%s) RETURN count(n) AS count", label), nil)
		if err != nil {
			t.Fatalf("count existing %s nodes: %v", label, err)
		}
		if got := IntVal(countRow, "count"); got != 0 {
			t.Fatalf("live proof requires an isolated graph with zero %s nodes, got %d", label, got)
		}
	}
}

// liveDeterminismSeed owns an isolated, uniquely-prefixed subgraph and its
// cleanup.
type liveDeterminismSeed struct {
	t                    *testing.T
	ctx                  context.Context
	driver               neo4jdriver.DriverWithContext
	prefix               string
	repoID               string
	workloadID           string
	instanceCount        int
	platformsPerInstance int
	// instanceIDs holds every seeded WorkloadInstance id in insertion order,
	// which is deliberately the REVERSE of lexicographic order so a backend
	// that returned scan order instead of true sort order would produce a
	// demonstrably wrong survivor set.
	instanceIDs []string
	instanceEnv map[string]string
	// platformRows records every seeded (instance_id, platform_name,
	// platform_id) triple so the expected top-N can be computed independently
	// of anything the backend returns.
	platformRows []attachedPlatformRow
}

type attachedPlatformRow struct {
	instanceID   string
	platformName string
	platformID   string
}

func newLiveDeterminismSeed(
	t *testing.T,
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	kind string,
	instanceCount int,
	platformsPerInstance int,
) *liveDeterminismSeed {
	t.Helper()
	prefix := fmt.Sprintf("svc-story-determinism-live-%s-%d-", kind, time.Now().UnixNano())
	return &liveDeterminismSeed{
		t:                    t,
		ctx:                  ctx,
		driver:               driver,
		prefix:               prefix,
		repoID:               prefix + "repository:orders",
		workloadID:           prefix + "workload:orders-api",
		instanceCount:        instanceCount,
		platformsPerInstance: platformsPerInstance,
		instanceEnv:          map[string]string{},
	}
}

// run executes a seed/cleanup write, failing the test on error.
func (s *liveDeterminismSeed) run(cypher string, params map[string]any) {
	s.t.Helper()
	if err := s.exec(cypher, params); err != nil {
		s.t.Fatalf("live NornicDB write: %v", err)
	}
}

func (s *liveDeterminismSeed) exec(cypher string, params map[string]any) error {
	session := s.driver.NewSession(s.ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: "nornic",
	})
	defer func() { _ = session.Close(s.ctx) }()
	result, runErr := session.Run(s.ctx, cypher, params)
	if runErr != nil {
		return runErr
	}
	_, consumeErr := result.Consume(s.ctx)
	return consumeErr
}

// write seeds one Repository-DEFINES->Workload plus instanceCount distinct
// WorkloadInstance-INSTANCE_OF->Workload rows, each with platformsPerInstance
// RUNS_ON platforms.
func (s *liveDeterminismSeed) write() {
	s.t.Helper()
	environments := []string{"dev", "prod", "staging"}
	rows := make([]map[string]any, 0, s.instanceCount*s.platformsPerInstance)
	// Insert in reverse lexicographic index order to decouple scan order from
	// the sort order the fix depends on.
	for i := s.instanceCount - 1; i >= 0; i-- {
		instanceID := fmt.Sprintf("%sworkload-instance:orders:inst-%03d", s.prefix, i)
		env := environments[i%len(environments)]
		s.instanceIDs = append(s.instanceIDs, instanceID)
		s.instanceEnv[instanceID] = env
		for p := s.platformsPerInstance - 1; p >= 0; p-- {
			platformID := fmt.Sprintf("%splatform:eks-%03d-%03d", s.prefix, i, p)
			platformName := fmt.Sprintf("eks-%03d-%03d", i, p)
			s.platformRows = append(s.platformRows, attachedPlatformRow{
				instanceID: instanceID, platformName: platformName, platformID: platformID,
			})
			rows = append(rows, map[string]any{
				"instance_id":   instanceID,
				"environment":   env,
				"platform_id":   platformID,
				"platform_name": platformName,
				"platform_kind": "argocd_applicationset",
			})
		}
	}

	s.run(`CREATE (repo:Repository {id: $repo_id, name: 'orders'})
	       CREATE (w:Workload {id: $workload_id, name: 'orders-api', repo_id: $repo_id})
	       CREATE (repo)-[:DEFINES {confidence: 0.9, source_fact_id: $defines_fact}]->(w)`,
		map[string]any{
			"repo_id":      s.repoID,
			"workload_id":  s.workloadID,
			"defines_fact": s.prefix + "fact-defines",
		})

	for _, instanceID := range s.instanceIDs {
		s.run(`MATCH (w:Workload {id: $workload_id})
		       CREATE (i:WorkloadInstance {id: $instance_id, workload_id: $workload_id,
		               environment: $environment, materialization_confidence: 0.9})
		       CREATE (i)-[:INSTANCE_OF {confidence: 0.9, source_fact_id: $instance_id}]->(w)`,
			map[string]any{
				"workload_id": s.workloadID,
				"instance_id": instanceID,
				"environment": s.instanceEnv[instanceID],
			})
	}

	for start := 0; start < len(rows); start += 200 {
		end := min(start+200, len(rows))
		s.run(`
UNWIND $rows AS row
MATCH (i:WorkloadInstance {id: row.instance_id})
CREATE (p:Platform {id: row.platform_id, name: row.platform_name, kind: row.platform_kind})
CREATE (i)-[:RUNS_ON {confidence: 0.9, source_fact_id: row.platform_id}]->(p)`,
			map[string]any{"rows": rows[start:end]})
	}
}

// cleanup is best-effort: it reports failures without aborting, so a transient
// error partway through cannot skip the remaining deletes and strand nodes that
// the next run's isolation guard would then trip over.
func (s *liveDeterminismSeed) cleanup() {
	statements := []struct {
		cypher string
		params map[string]any
	}{
		{
			`MATCH (i:WorkloadInstance)-[:RUNS_ON]->(p:Platform) WHERE i.workload_id = $workload_id DETACH DELETE p`,
			map[string]any{"workload_id": s.workloadID},
		},
		{
			`MATCH (i:WorkloadInstance) WHERE i.workload_id = $workload_id DETACH DELETE i`,
			map[string]any{"workload_id": s.workloadID},
		},
		{`MATCH (w:Workload {id: $workload_id}) DETACH DELETE w`, map[string]any{"workload_id": s.workloadID}},
		{`MATCH (repo:Repository {id: $repo_id}) DETACH DELETE repo`, map[string]any{"repo_id": s.repoID}},
	}
	for _, statement := range statements {
		if err := s.exec(statement.cypher, statement.params); err != nil {
			s.t.Errorf("live NornicDB cleanup failed (graph may retain %q nodes): %v", s.prefix, err)
		}
	}
}

// expectedRuntimeTop returns the lexicographic top-contextStoryItemLimit
// instance ids by the (environment, instance_id) total order the fix declares,
// computed from every seeded instance independently of the backend.
func (s *liveDeterminismSeed) expectedRuntimeTop() []string {
	ids := append([]string(nil), s.instanceIDs...)
	sort.Slice(ids, func(i, j int) bool {
		if a, b := s.instanceEnv[ids[i]], s.instanceEnv[ids[j]]; a != b {
			return a < b
		}
		return ids[i] < ids[j]
	})
	if len(ids) > contextStoryItemLimit {
		ids = ids[:contextStoryItemLimit]
	}
	return ids
}

// expectedAttachedPlatformTop returns the lexicographic top-
// workloadPlatformEdgeLimit (instance_id, platform_name, platform_id) keys,
// computed from every seeded RUNS_ON edge independently of the backend.
func (s *liveDeterminismSeed) expectedAttachedPlatformTop() []string {
	keys := make([]string, 0, len(s.platformRows))
	for _, row := range s.platformRows {
		keys = append(keys, row.instanceID+"\x00"+row.platformName+"\x00"+row.platformID)
	}
	sort.Strings(keys)
	if len(keys) > workloadPlatformEdgeLimit {
		keys = keys[:workloadPlatformEdgeLimit]
	}
	return keys
}

func (s *liveDeterminismSeed) assertRuntimeInstanceSelectionDeterministic(ctx context.Context, t *testing.T, reader GraphQuery) {
	t.Helper()
	want := s.expectedRuntimeTop()
	const repeats = 25
	var first []string
	for call := 0; call < repeats; call++ {
		result, err := fetchWorkloadRuntimeTopology(
			ctx, reader, "w.id = $workload_id", map[string]any{"workload_id": s.workloadID}, s.repoID,
		)
		if err != nil {
			t.Fatalf("fetchWorkloadRuntimeTopology() call %d error = %v", call, err)
		}
		got := instanceIDs(result.instances)
		if len(got) != contextStoryItemLimit {
			t.Fatalf("call %d survivor count = %d, want %d", call, len(got), contextStoryItemLimit)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("call %d survivor set != independent lexicographic top-%d\n got  = %v\n want = %v\nbackend ORDER BY ... LIMIT did not select the correct candidate subset",
				call, contextStoryItemLimit, got, want)
		}
		if first == nil {
			first = got
			continue
		}
		if strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("call %d survivor set differs from call 0 (non-deterministic backend selection)", call)
		}
	}
}

// assertAttachedPlatformSelectionDeterministic drives the real
// fetchWorkloadPlatformResult at its real 2,501-row sentinel over 2,550 seeded
// edges, so the aggregating plan shape is proven at production cardinality
// through production query text rather than by extrapolation from a scratch
// query.
func (s *liveDeterminismSeed) assertAttachedPlatformSelectionDeterministic(ctx context.Context, t *testing.T, reader GraphQuery) {
	t.Helper()
	handler := &EntityHandler{Neo4j: reader}
	instances := make([]map[string]any, 0, len(s.instanceIDs))
	for _, instanceID := range s.instanceIDs {
		instances = append(instances, map[string]any{"instance_id": instanceID})
	}
	want := s.expectedAttachedPlatformTop()

	const repeats = 5
	var first []string
	for call := 0; call < repeats; call++ {
		result, err := handler.fetchWorkloadPlatformResult(ctx, s.repoID, s.workloadID, instances)
		if err != nil {
			t.Fatalf("fetchWorkloadPlatformResult() call %d error = %v", call, err)
		}
		got := make([]string, 0, len(result.rows))
		for _, row := range result.rows {
			got = append(got, StringVal(row, "instance_id")+"\x00"+StringVal(row, "platform_name")+"\x00"+StringVal(row, "platform_id"))
		}
		if len(got) != workloadPlatformEdgeLimit {
			t.Fatalf("call %d survivor count = %d, want %d", call, len(got), workloadPlatformEdgeLimit)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("call %d attached-platform survivor set != independent lexicographic top-%d; aggregating ORDER BY ... LIMIT did not select the correct candidate subset",
				call, workloadPlatformEdgeLimit)
		}
		if first == nil {
			first = got
			continue
		}
		if strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("call %d attached-platform survivor set differs from call 0 (non-deterministic backend selection)", call)
		}
	}
}

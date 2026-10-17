// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package writer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// The #8283 verified-success probe against a real backend. The live-backend
// CI job runs this file on NornicDB and on Neo4j (ESHU_NEO4J_URI,
// ESHU_LIVE_GRAPH_BACKEND, ESHU_LIVE_GRAPH_DATABASE, auth disabled). Locally:
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:<port> ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test -tags live_nornicdb_answer_truth ./internal/storage/cypher/edge/writer \
//	  -run '^TestLiveVerifyDeployableUnitEdges' -count=1 -v

func openLiveVerifyRunner(t *testing.T) *boltRetractTestRunner {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required for the live verification proof")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if database == "" {
		database = "nornic"
		if strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")) == "neo4j" {
			database = "neo4j"
		}
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open bolt driver %q: %v", uri, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		t.Fatalf("verify bolt connectivity %q: %v", uri, err)
	}
	runner := &boltRetractTestRunner{driver: driver, databaseName: database}
	t.Cleanup(func() { runner.close(context.Background()) })
	return runner
}

// liveVerifyRepos returns run-unique source and deployment Repository ids and
// registers their cleanup.
func liveVerifyRepos(t *testing.T, runner *boltRetractTestRunner, label string) (string, string) {
	t.Helper()
	prefix := fmt.Sprintf("repository:8283-verify-%s-%d", label, time.Now().UnixNano())
	source, target := prefix+"-app", prefix+"-deploy"
	t.Cleanup(func() {
		for _, id := range []string{source, target} {
			if err := boltWriteStatement(context.Background(), runner,
				`MATCH (r:Repository {id: $id}) DETACH DELETE r`, map[string]any{"id": id}); err != nil {
				t.Errorf("cleanup live repository %s: %v", id, err)
			}
		}
	})
	return source, target
}

func seedLiveRepository(t *testing.T, runner *boltRetractTestRunner, id string) {
	t.Helper()
	if err := boltWriteStatement(context.Background(), runner,
		`MERGE (:Repository {id: $id})`, map[string]any{"id": id}); err != nil {
		t.Fatalf("seed live repository %s: %v", id, err)
	}
}

func liveDeployableUnitRow(source, target string) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:     "intent-8283-verify-live",
		RepositoryID: source,
		GenerationID: "gen-8283",
		Payload: map[string]any{
			"repo_id":             source,
			"deployment_repo_id":  target,
			"deployable_unit_key": "du-8283",
			"correlation_key":     "corr-8283",
			"confidence":          0.9,
			"reason":              "live verification proof",
		},
	}
}

func TestLiveVerifyDeployableUnitEdgesSourcePresentTargetAbsent(t *testing.T) {
	runner := openLiveVerifyRunner(t)
	source, target := liveVerifyRepos(t, runner, "target-absent")
	seedLiveRepository(t, runner, source)

	w := NewEdgeWriter(&boltProbeTestExecutor{runner: runner}, 0)
	err := w.VerifyDeployableUnitEdges(context.Background(),
		[]reducer.SharedProjectionIntentRow{liveDeployableUnitRow(source, target)}, deployableUnitTestEvidenceSource)
	if got := verifyClassOf(err); got != reducer.SharedEdgeTargetNotReadyFailureClass {
		t.Fatalf("verify with the target absent = %v (class %q), want %q", err, got, reducer.SharedEdgeTargetNotReadyFailureClass)
	}
	var sampled interface{ SampleTarget() (string, string) }
	if !errors.As(err, &sampled) {
		t.Fatalf("verify error %v carries no SampleTarget", err)
	}
	if repo, _ := sampled.SampleTarget(); repo != target {
		t.Fatalf("sample repo = %q, want the absent target %q", repo, target)
	}
}

func TestLiveVerifyDeployableUnitEdgesBothPresentEdgeAbsent(t *testing.T) {
	runner := openLiveVerifyRunner(t)
	source, target := liveVerifyRepos(t, runner, "edge-absent")
	seedLiveRepository(t, runner, source)
	seedLiveRepository(t, runner, target)

	w := NewEdgeWriter(&boltProbeTestExecutor{runner: runner}, 0)
	err := w.VerifyDeployableUnitEdges(context.Background(),
		[]reducer.SharedProjectionIntentRow{liveDeployableUnitRow(source, target)}, deployableUnitTestEvidenceSource)
	if got := verifyClassOf(err); got != reducer.DeployableUnitEdgeWriteUnverifiedFailureClass {
		t.Fatalf("verify with both nodes and no edge = %v (class %q), want %q", err, got, reducer.DeployableUnitEdgeWriteUnverifiedFailureClass)
	}
}

// The edge is written by the real WriteEdges path, then verified present. An
// edge another evidence source owns does not count as this handler's edge.
func TestLiveVerifyDeployableUnitEdgesEdgePresent(t *testing.T) {
	runner := openLiveVerifyRunner(t)
	source, target := liveVerifyRepos(t, runner, "edge-present")
	seedLiveRepository(t, runner, source)
	seedLiveRepository(t, runner, target)
	ctx := context.Background()

	w := NewEdgeWriter(&boltProbeTestExecutor{runner: runner}, 0)
	rows := []reducer.SharedProjectionIntentRow{liveDeployableUnitRow(source, target)}
	if _, err := w.WriteEdges(ctx, reducer.DomainDeployableUnitEdges, rows, deployableUnitTestEvidenceSource); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if err := w.VerifyDeployableUnitEdges(ctx, rows, deployableUnitTestEvidenceSource); err != nil {
		t.Fatalf("verify after a real write = %v, want nil", err)
	}
	err := w.VerifyDeployableUnitEdges(ctx, rows, "reducer/some-other-source")
	if got := verifyClassOf(err); got != reducer.DeployableUnitEdgeWriteUnverifiedFailureClass {
		t.Fatalf("verify under another evidence source = %v (class %q), want %q", err, got, reducer.DeployableUnitEdgeWriteUnverifiedFailureClass)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live parity proof for the #6794 incoming-anchor rewrite. The repository
// context reads that walk edges INTO the bound repository were right-anchored,
// (source:Repository)-[rel]->(r:Repository {id: $repo_id}). The tests run the
// production functions and the pre-change statements against the same seed,
// and check both against rows known by construction.
//
// Run against an isolated container (see nornicdb_answer_truth_live_test.go):
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27687 go test ./internal/query/repository \
//	  -tags live_nornicdb_answer_truth -run TestLiveNornicDBRepositoryIncomingAnchor -count=1 -v
package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const incomingAnchorRelTypes = "DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON|CORRELATES_DEPLOYABLE_UNIT"

// Pre-change statements, kept verbatim as the parity oracle.
const (
	incomingAnchorOldOverview = `
		MATCH (source:Repository)-[rel:` + incomingAnchorRelTypes + `]->(r:Repository {id: $repo_id})
		RETURN 'incoming' AS direction, type(rel) AS type, source.name AS source_name, source.id AS source_id,
		       r.name AS target_name, r.id AS target_id, rel.evidence_type AS evidence_type
		ORDER BY type, source_name`
	incomingAnchorOldDeployableUnit = `
		MATCH (source:Repository)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(r:Repository {id: $repo_id})
		RETURN 'incoming' AS direction, type(rel) AS type, source.name AS source_name, source.id AS source_id,
		       r.name AS target_name, r.id AS target_id, rel.evidence_type AS evidence_type
		ORDER BY source_name`
	incomingAnchorOldConsumers = `
		MATCH (consumer:Repository)-[rel:` + incomingAnchorRelTypes + `]->(r:Repository {id: $repo_id})
		RETURN consumer.name AS consumer_name, consumer.id AS consumer_id
		ORDER BY consumer_name`
)

// incomingAnchorSeed, by construction:
//   - hub has incoming DEPENDS_ON and RUNS_ON from c1, USES_MODULE from c2,
//     CORRELATES_DEPLOYABLE_UNIT from c3, and one outgoing DEPLOYS_FROM to d1;
//   - c1 -> d1 is noise that touches neither hub nor leaf;
//   - leaf has no edges.
var incomingAnchorSeed = []string{
	`CREATE (:Repository {id: 'incoming-anchor:hub', name: 'ia-hub'})`,
	`CREATE (:Repository {id: 'incoming-anchor:leaf', name: 'ia-leaf'})`,
	`CREATE (:Repository {id: 'incoming-anchor:c1', name: 'ia-c1'})`,
	`CREATE (:Repository {id: 'incoming-anchor:c2', name: 'ia-c2'})`,
	`CREATE (:Repository {id: 'incoming-anchor:c3', name: 'ia-c3'})`,
	`CREATE (:Repository {id: 'incoming-anchor:d1', name: 'ia-d1'})`,
	repoAnswerTruthEdge("Repository", "incoming-anchor:c1", "DEPENDS_ON", "Repository", "incoming-anchor:hub"),
	repoAnswerTruthEdge("Repository", "incoming-anchor:c1", "RUNS_ON", "Repository", "incoming-anchor:hub"),
	repoAnswerTruthEdge("Repository", "incoming-anchor:c2", "USES_MODULE", "Repository", "incoming-anchor:hub"),
	repoAnswerTruthEdge("Repository", "incoming-anchor:c3", "CORRELATES_DEPLOYABLE_UNIT", "Repository", "incoming-anchor:hub"),
	repoAnswerTruthEdge("Repository", "incoming-anchor:hub", "DEPLOYS_FROM", "Repository", "incoming-anchor:d1"),
	repoAnswerTruthEdge("Repository", "incoming-anchor:c1", "DEPENDS_ON", "Repository", "incoming-anchor:d1"),
}

const incomingAnchorCleanup = `MATCH (n) WHERE n.id STARTS WITH 'incoming-anchor:' DETACH DELETE n`

func TestLiveNornicDBRepositoryIncomingAnchor(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}

	reader := repoLiveReader{driver: driver}
	reader.write(ctx, t, incomingAnchorCleanup)
	for _, stmt := range incomingAnchorSeed {
		reader.write(ctx, t, stmt)
	}
	defer reader.write(context.Background(), t, incomingAnchorCleanup)

	for _, tc := range []struct {
		repo         string
		overviewIn   string
		deployableIn string
		consumers    string
	}{
		{
			repo:         "incoming-anchor:hub",
			overviewIn:   "CORRELATES_DEPLOYABLE_UNIT:ia-c3,DEPENDS_ON:ia-c1,RUNS_ON:ia-c1,USES_MODULE:ia-c2",
			deployableIn: "CORRELATES_DEPLOYABLE_UNIT:ia-c3",
			consumers:    "ia-c1,ia-c1,ia-c2,ia-c3",
		},
		{repo: "incoming-anchor:leaf"},
	} {
		t.Run(tc.repo, func(t *testing.T) {
			params := map[string]any{"repo_id": tc.repo}

			overview := incomingRows(queryRepoRelationshipOverview(ctx, reader, params))
			oldOverview := queryRepoRelationshipOverviewDirection(ctx, reader, params, incomingAnchorOldOverview)
			assertIncomingRows(t, "overview", overview, incomingRows(oldOverview), tc.overviewIn)

			deployable := incomingRows(queryRepoDeployableUnitRelationshipOverview(ctx, reader, params))
			oldDeployable := queryRepoRelationshipOverviewDirection(ctx, reader, params, incomingAnchorOldDeployableUnit)
			assertIncomingRows(t, "deployable unit", deployable, incomingRows(oldDeployable), tc.deployableIn)

			consumers := joinField(queryRepoConsumers(ctx, reader, params), "name")
			oldConsumerRows, err := reader.Run(ctx, incomingAnchorOldConsumers, params)
			if err != nil {
				t.Fatalf("old consumers read: %v", err)
			}
			if old := joinField(oldConsumerRows, "consumer_name"); consumers != old || consumers != tc.consumers {
				t.Fatalf("consumers = %q, pre-change = %q, want %q", consumers, old, tc.consumers)
			}
		})
	}
}

// incomingRows renders the incoming rows of a relationship overview in order.
func incomingRows(rows []map[string]any) []string {
	out := []string{}
	for _, row := range rows {
		if row["direction"] == "incoming" {
			out = append(out, fmt.Sprintf("%v:%v", row["type"], row["source_name"]))
		}
	}
	return out
}

func assertIncomingRows(t *testing.T, name string, got, old []string, want string) {
	t.Helper()
	if g, o := strings.Join(got, ","), strings.Join(old, ","); g != o || g != want {
		t.Fatalf("%s incoming = %q, pre-change = %q, want %q", name, g, o, want)
	}
}

func joinField(rows []map[string]any, field string) string {
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, fmt.Sprint(row[field]))
	}
	return strings.Join(values, ",")
}

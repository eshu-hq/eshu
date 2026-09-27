// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// #7306 audit. #7304 fixed a workload_materialization retract whose keep-list
// came from the entity-filtered projection while its repository set was
// scope-wide, so a foreign-keyed intent deleted what a sibling wrote. These
// tests pin why deployable_unit_correlation does not share that defect: its
// retract set and its write set are both the repositories the intent's keys
// match, matched on the same repository-fact identities, and each matched
// repository gets a whole retract-then-rewrite of its own truth. An intent
// either leaves a repository alone or rewrites all of it, so no order of
// intents changes the final graph.

const (
	duScopeRepoID    = "repository:r_orders"
	duScopeRepoName  = "orders"
	duScopeID        = "git-repository-scope:" + duScopeRepoID
	duDeployRepoID   = "repository:r_deployments"
	duStaleTargetID  = "repository:r_retired_deployments"
	duCollectorKey   = "repo:" + duScopeRepoName
	duScopeIntentGen = "generation-1"
)

// statefulDeployableUnitGraph holds CORRELATES_DEPLOYABLE_UNIT edges with the
// production statements' semantics: RetractEdges deletes every edge whose
// source repository a row names (RetractDeployableUnitCorrelationEdgesCypher,
// ids collected like the edge writer's collectRepoIDs), and WriteEdges MERGEs
// one edge per (repo_id, deployment_repo_id).
type statefulDeployableUnitGraph struct {
	edges      map[string]struct{}
	statements int
}

func newStatefulDeployableUnitGraph(seed ...string) *statefulDeployableUnitGraph {
	g := &statefulDeployableUnitGraph{edges: map[string]struct{}{}}
	for _, edge := range seed {
		g.edges[edge] = struct{}{}
	}
	return g
}

func (g *statefulDeployableUnitGraph) RetractEdges(
	_ context.Context, domain string, rows []SharedProjectionIntentRow, evidenceSource string,
) error {
	if domain != DomainDeployableUnitEdges || evidenceSource != deployableUnitCorrelationEvidenceSource {
		return fmt.Errorf("unexpected retract %s/%s", domain, evidenceSource)
	}
	g.statements++
	for _, row := range rows {
		repoID := row.RepositoryID
		if repoID == "" {
			repoID = anyToString(row.Payload["repo_id"])
		}
		for edge := range g.edges {
			if strings.HasPrefix(edge, repoID+"->") {
				delete(g.edges, edge)
			}
		}
	}
	return nil
}

func (g *statefulDeployableUnitGraph) WriteEdges(
	_ context.Context, domain string, rows []SharedProjectionIntentRow, evidenceSource string,
) (SharedProjectionWriteReport, error) {
	if domain != DomainDeployableUnitEdges || evidenceSource != deployableUnitCorrelationEvidenceSource {
		return SharedProjectionWriteReport{}, fmt.Errorf("unexpected write %s/%s", domain, evidenceSource)
	}
	g.statements++
	for _, row := range rows {
		edge := anyToString(row.Payload["repo_id"]) + "->" + anyToString(row.Payload["deployment_repo_id"])
		g.edges[edge] = struct{}{}
	}
	return SharedProjectionWriteReport{}, nil
}

func (g *statefulDeployableUnitGraph) sortedEdges() []string {
	edges := make([]string, 0, len(g.edges))
	for edge := range g.edges {
		edges = append(edges, edge)
	}
	sort.Strings(edges)
	return edges
}

// duScopeFacts is the scope generation's repository fact plus, when deployable,
// the Dockerfile that makes the repository a deployable-unit candidate.
func duScopeFacts(deployable bool) []facts.Envelope {
	var files []map[string]any
	if deployable {
		files = append(files, map[string]any{
			"repo_id": duScopeRepoID, "language": "dockerfile", "relative_path": "Dockerfile",
			"parsed_file_data": map[string]any{"dockerfile_stages": []any{map[string]any{"name": "runtime"}}},
		})
	}
	return deployableUnitCorrelationEnvelopes(duScopeRepoID, duScopeRepoName, files)
}

// runDUScopeIntent runs one intent for the scope generation through the real
// Handle path (fact load, candidate extraction, resolved deployment sources,
// entity-key filter, evaluation, retract, write) against graph.
func runDUScopeIntent(graph *statefulDeployableUnitGraph, deployable bool, keys ...string) error {
	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{envelopes: duScopeFacts(deployable)},
		ResolvedLoader: &stubDeployableUnitResolvedLoader{resolved: []relationships.ResolvedRelationship{{
			SourceRepoID: duDeployRepoID, TargetRepoID: duScopeRepoID,
			RelationshipType: relationships.RelDeploysFrom, Confidence: 0.94,
			Details: map[string]any{"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDAppSource)}},
		}}},
		PhasePublisher: &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:     graph,
	}
	_, err := handler.Handle(context.Background(), Intent{
		IntentID: fmt.Sprintf("intent-%v", keys), ScopeID: duScopeID, GenerationID: duScopeIntentGen,
		SourceSystem: "git", Domain: DomainDeployableUnitCorrelation, Cause: "facts committed",
		EntityKeys: keys,
	})
	return err
}

// duScopeKeyTable is #7304's key table for one single-repository git scope.
// matches is whether the key selects the scope's repository.
var duScopeKeyTable = []struct {
	name    string
	key     string
	matches bool
}{
	// The git collector's key (collector/repo/git/followup_facts.go).
	{name: "matching collector repo:<name>", key: duCollectorKey, matches: true},
	{name: "matching repo:<graph id>", key: "repo:" + duScopeRepoID, matches: true},
	// repo_dependency's PROVISIONS_DEPENDENCY_FOR target key shape.
	{name: "foreign repo:<other graph id>", key: "repo:repository:r_billing", matches: false},
	// deployment_mapping's fallback shape. Its last segment is the scope
	// repository's own id suffix, so the alias selects the repository.
	{name: "repo:<scope id>", key: "repo:" + duScopeID, matches: true},
}

// TestDeployableUnitRetractScopeFinalGraphIsIntentOrderIndependent runs the
// collector's matching intent and each key-table intent in both orders, from a
// graph holding the correct edge plus a stale one. Both orders must end at the
// same graph: the correct edge kept, the stale edge retracted.
func TestDeployableUnitRetractScopeFinalGraphIsIntentOrderIndependent(t *testing.T) {
	t.Parallel()

	want := []string{duScopeRepoID + "->" + duDeployRepoID}
	for _, row := range duScopeKeyTable {
		for _, order := range []string{"matching then keyed", "keyed then matching"} {
			t.Run(row.name+"/"+order, func(t *testing.T) {
				t.Parallel()
				graph := newStatefulDeployableUnitGraph(
					duScopeRepoID+"->"+duDeployRepoID, duScopeRepoID+"->"+duStaleTargetID,
				)
				keys := []string{duCollectorKey, row.key}
				if order == "keyed then matching" {
					keys[0], keys[1] = keys[1], keys[0]
				}
				for _, key := range keys {
					if err := runDUScopeIntent(graph, true, key); err != nil {
						t.Fatalf("Handle(%q) error = %v", key, err)
					}
				}
				if got := graph.sortedEdges(); !reflect.DeepEqual(got, want) {
					t.Fatalf("final edges = %v, want %v", got, want)
				}
			})
		}
	}
}

// TestDeployableUnitRetractScopeKeyedIntentAloneTouchesOnlyMatchedRepository
// runs each key-table intent alone, once while the repository still deploys
// and once after its deployment evidence disappeared. A matching intent
// rewrites the repository's whole truth (stale edge gone; after disappearance
// every edge gone). A non-matching intent issues no graph statement at all, so
// it cannot delete what another intent wrote.
func TestDeployableUnitRetractScopeKeyedIntentAloneTouchesOnlyMatchedRepository(t *testing.T) {
	t.Parallel()

	seed := []string{duScopeRepoID + "->" + duDeployRepoID, duScopeRepoID + "->" + duStaleTargetID}
	for _, row := range duScopeKeyTable {
		for _, deployable := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/deployable=%v", row.name, deployable), func(t *testing.T) {
				t.Parallel()
				graph := newStatefulDeployableUnitGraph(seed...)
				if err := runDUScopeIntent(graph, deployable, row.key); err != nil {
					t.Fatalf("Handle(%q) error = %v", row.key, err)
				}
				want := seed
				switch {
				case row.matches && deployable:
					want = []string{duScopeRepoID + "->" + duDeployRepoID}
				case row.matches:
					want = []string{}
				}
				if got := graph.sortedEdges(); !reflect.DeepEqual(got, want) {
					t.Fatalf("final edges = %v, want %v", got, want)
				}
				if !row.matches && graph.statements != 0 {
					t.Fatalf("non-matching intent issued %d graph statements, want 0", graph.statements)
				}
			})
		}
	}
}

// TestDeployableUnitRetractScopeNoKeysFailsBeforeAnyGraphStatement pins the
// table's "no keys" row: the intent is rejected before facts are read, so it
// can neither retract nor write.
func TestDeployableUnitRetractScopeNoKeysFailsBeforeAnyGraphStatement(t *testing.T) {
	t.Parallel()

	graph := newStatefulDeployableUnitGraph(duScopeRepoID + "->" + duDeployRepoID)
	if err := runDUScopeIntent(graph, false); err == nil {
		t.Fatal("Handle() with no entity keys succeeded, want an error")
	}
	if graph.statements != 0 {
		t.Fatalf("keyless intent issued %d graph statements, want 0", graph.statements)
	}
	if got, want := graph.sortedEdges(), []string{duScopeRepoID + "->" + duDeployRepoID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("final edges = %v, want %v", got, want)
	}
}

// TestDeployableUnitRetractMatcherAgreesWithCandidateFilter pins the invariant
// the order independence rests on: the zero-result retract's repository match
// (deployableUnitIntentMatchesRepository) and the candidate filter
// (filterDeployableUnitCandidates) select exactly the same repositories for
// every key shape. If they drifted, a zero-result intent could retract a
// repository whose candidate another intent's filter admitted and wrote.
func TestDeployableUnitRetractMatcherAgreesWithCandidateFilter(t *testing.T) {
	t.Parallel()

	repos := []struct{ id, name string }{
		{id: duScopeRepoID, name: duScopeRepoName},
		{id: "repository:r_Mixed_Case", name: "Mixed-Case"},
		{id: "repo-plain", name: "plain"},
	}
	keys := []string{
		duCollectorKey, "repo:" + duScopeRepoID, "repo:repository:r_billing", "repo:" + duScopeID,
		"workload:orders", "ORDERS", "repo:mixed-case", "repository:r_mixed_case", "repo:repo-plain",
		"repo:plain", "platform:orders", "repo:", " repo:orders ",
	}
	for _, key := range keys {
		entityKeys, err := deployableUnitCorrelationEntityKeys(Intent{IntentID: "i", EntityKeys: []string{key}})
		if err != nil {
			t.Fatalf("entity keys %q: %v", key, err)
		}
		for _, repo := range repos {
			retracts := deployableUnitIntentMatchesRepository(entityKeys, repo.id, repo.name)
			filtered := len(filterDeployableUnitCandidates(
				[]WorkloadCandidate{{RepoID: repo.id, RepoName: repo.name}}, entityKeys,
			)) > 0
			if retracts != filtered {
				t.Errorf("key %q repo %s/%s: retract match = %v, candidate filter = %v; they must agree",
					key, repo.id, repo.name, retracts, filtered)
			}
		}
	}
}

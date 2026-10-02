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
	duCollectorKey   = "repo:" + duScopeRepoID
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

// duScopeKeyTable is #7304's key table for one single-repository git scope,
// rekeyed for #7384's id-only selection. matches is whether the key selects
// the scope's repository.
var duScopeKeyTable = []struct {
	name    string
	key     string
	matches bool
}{
	// The git collector's key (collector/repo/git/followup_facts.go): repo:<id>.
	{name: "matching collector repo:<id>", key: duCollectorKey, matches: true},
	{name: "matching bare <graph id>", key: duScopeRepoID, matches: true},
	// repo_dependency's PROVISIONS_DEPENDENCY_FOR target key shape.
	{name: "foreign repo:<other graph id>", key: "repo:repository:r_billing", matches: false},
	// deployment_mapping's fallback shape. Its last segment is the scope
	// repository's own id suffix, so the alias selects the repository.
	{name: "repo:<scope id>", key: "repo:" + duScopeID, matches: true},
	// The legacy collector repo:<name> key no longer selects: names are not
	// unique across a run and can end in a colon the alias normalizer cannot
	// match, so name-keyed keys never select (#7384).
	{name: "legacy collector repo:<name> selects nothing", key: "repo:" + duScopeRepoName, matches: false},
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
		{id: "repository:r_dep1", name: "group:artifact"},
		{id: "repository:r_dep2", name: "pkg:"},
	}
	keys := []string{
		duCollectorKey, "repo:" + duScopeRepoID, "repo:repository:r_billing", "repo:" + duScopeID,
		"workload:orders", "ORDERS", "repo:mixed-case", "repository:r_mixed_case", "repo:repo-plain",
		"repo:plain", "platform:orders", "repo:", " repo:orders ",
		"repo:group:artifact", "repo:pkg:",
	}
	for _, key := range keys {
		entityKeys, err := deployableUnitCorrelationEntityKeys(Intent{IntentID: "i", EntityKeys: []string{key}})
		if err != nil {
			t.Fatalf("entity keys %q: %v", key, err)
		}
		for _, repo := range repos {
			retracts := deployableUnitIntentMatchesRepository(entityKeys, repo.id)
			filtered, _ := filterDeployableUnitCandidates(
				[]WorkloadCandidate{{RepoID: repo.id, RepoName: repo.name}}, entityKeys,
			)
			filteredOK := len(filtered) > 0
			if retracts != filteredOK {
				t.Errorf("key %q repo %s/%s: retract match = %v, candidate filter = %v; they must agree",
					key, repo.id, repo.name, retracts, filteredOK)
			}
		}
	}
}

// TestDeployableUnitCollectorKeySelectsDisplayNames pins how the collector's
// key for a repository ID selects that repository (#7384). The git collector
// builds repo:<repository ID>, and selection compares IDs only: names are not
// unique across a run and can end in a colon the alias normalizer cannot
// match, so name-keyed keys never select. A trailing-colon name with the
// correct ID still selects via the ID half; a matching name with a wrong ID
// selects nothing.
func TestDeployableUnitCollectorKeySelectsDisplayNames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		entityKey string
		repoID    string
		repoName  string
		matches   bool
	}{
		{name: "id key selects", entityKey: "repo:repository:r_display", repoID: "repository:r_display", repoName: "pkg-display", matches: true},
		{name: "mixed case id key selects", entityKey: "repo:Repository:R_DISPLAY", repoID: "repository:r_display", repoName: "pkg-display", matches: true},
		{name: "trailing colon name with correct id selects via id", entityKey: "repo:repository:r_display", repoID: "repository:r_display", repoName: "pkg:", matches: true},
		{name: "matching name with wrong id selects nothing", entityKey: "repo:pkg-display", repoID: "repository:r_other", repoName: "pkg-display", matches: false},
		{name: "basename key selects nothing", entityKey: "repo:checkout-dir", repoID: "repository:r_display", repoName: "pkg-display", matches: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entityKeys, err := deployableUnitCorrelationEntityKeys(Intent{
				IntentID: "i", EntityKeys: []string{tc.entityKey},
			})
			if err != nil {
				t.Fatalf("entity keys: %v", err)
			}
			repo := WorkloadCandidate{RepoID: tc.repoID, RepoName: tc.repoName}
			filtered, _ := filterDeployableUnitCandidates([]WorkloadCandidate{repo}, entityKeys)
			retracts := deployableUnitIntentMatchesRepository(entityKeys, repo.RepoID)
			if (len(filtered) > 0) != tc.matches || retracts != tc.matches {
				t.Fatalf("key %s: candidate filter = %v, retract match = %v, want both %v",
					tc.entityKey, len(filtered) > 0, retracts, tc.matches)
			}
		})
	}
}

// TestFilterDeployableUnitCandidatesReportsSelectionReason pins the #7384
// closed reason taxonomy: every admitted/selected shape maps to exactly one
// reason, so a zero selection is distinguishable from a mismatch without
// reading the keys.
func TestFilterDeployableUnitCandidatesReportsSelectionReason(t *testing.T) {
	t.Parallel()

	repo := WorkloadCandidate{RepoID: "repository:r_orders", RepoName: "orders"}
	keys := func(ks ...string) map[string]struct{} {
		if len(ks) == 0 {
			return map[string]struct{}{}
		}
		set, err := deployableUnitCorrelationEntityKeys(Intent{IntentID: "i", EntityKeys: ks})
		if err != nil {
			t.Fatalf("entity keys: %v", err)
		}
		return set
	}

	for _, tc := range []struct {
		name     string
		cands    []WorkloadCandidate
		keys     map[string]struct{}
		admitted int
		selected int
		reason   CandidateSelectionReason
	}{
		{name: "no keys", cands: []WorkloadCandidate{repo}, keys: map[string]struct{}{}, admitted: 1, selected: 0, reason: SelectionNoKeys},
		{name: "no admitted", cands: nil, keys: keys("repo:repository:r_orders"), admitted: 0, selected: 0, reason: SelectionNoAdmittedCandidates},
		{name: "key match", cands: []WorkloadCandidate{repo}, keys: keys("repo:repository:r_orders"), admitted: 1, selected: 1, reason: SelectionKeyMatch},
		{name: "alias match", cands: []WorkloadCandidate{repo}, keys: keys("repo:r_orders"), admitted: 1, selected: 1, reason: SelectionKeyMatch},
		{name: "no key match", cands: []WorkloadCandidate{repo}, keys: keys("repo:repository:r_other"), admitted: 1, selected: 0, reason: SelectionNoKeyMatch},
		{name: "name key is a mismatch", cands: []WorkloadCandidate{repo}, keys: keys("repo:orders", "orders"), admitted: 1, selected: 0, reason: SelectionNoKeyMatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filtered, report := filterDeployableUnitCandidates(tc.cands, tc.keys)
			if len(filtered) != tc.selected {
				t.Fatalf("selected = %d, want %d", len(filtered), tc.selected)
			}
			if report.Admitted != tc.admitted || report.Selected != tc.selected || report.Reason != tc.reason {
				t.Fatalf("report = %+v, want admitted=%d selected=%d reason=%q",
					report, tc.admitted, tc.selected, tc.reason)
			}
		})
	}
}

// TestRefineCandidateSelectionReasonForeignKey pins the legitimate-zero half
// of #7384 Q4: keys that reference repositories outside the scope refine to
// foreign_key_expected, while an in-scope miss stays no_key_match and every
// other reason passes through unchanged.
func TestRefineCandidateSelectionReasonForeignKey(t *testing.T) {
	t.Parallel()

	scope := []string{"repository:r_orders"}
	for _, tc := range []struct {
		name   string
		report CandidateSelectionReport
		keys   []string
		scopes []string
		want   CandidateSelectionReason
	}{
		{
			name:   "foreign keys refine",
			report: CandidateSelectionReport{Admitted: 1, Selected: 0, Reason: SelectionNoKeyMatch},
			keys:   []string{"repo:repository:r_other"},
			scopes: scope,
			want:   SelectionForeignKeyExpected,
		},
		{
			name:   "in-scope miss stays mismatch",
			report: CandidateSelectionReport{Admitted: 1, Selected: 0, Reason: SelectionNoKeyMatch},
			keys:   []string{"repo:repository:r_orders"},
			scopes: []string{"repository:r_orders", "repository:r_something_else"},
			want:   SelectionNoKeyMatch,
		},
		{
			name:   "key match passes through",
			report: CandidateSelectionReport{Admitted: 1, Selected: 1, Reason: SelectionKeyMatch},
			keys:   []string{"repo:repository:r_other"},
			scopes: scope,
			want:   SelectionKeyMatch,
		},
		{
			name:   "legitimate empty passes through",
			report: CandidateSelectionReport{Admitted: 0, Selected: 0, Reason: SelectionNoAdmittedCandidates},
			keys:   []string{"repo:repository:r_other"},
			scopes: scope,
			want:   SelectionNoAdmittedCandidates,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := refineCandidateSelectionReason(tc.report, tc.keys, tc.scopes); got != tc.want {
				t.Fatalf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}

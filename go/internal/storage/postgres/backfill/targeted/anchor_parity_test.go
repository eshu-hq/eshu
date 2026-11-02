// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// wantOwedEdges names one edge per way evidence reaches an owed repository. The
// parity comparison would pass on an empty corpus, so this list is what makes it
// mean something: every route has to produce its edge on the fleet side.
var wantOwedEdges = []string{
	parityRepoBilling + "->" + parityRepoPayments,     // alias token
	parityRepoLedger + "->" + parityRepoPayments,      // full repo id
	parityRepoPayments + "->" + parityRepoOrders,      // r_ token, owed source
	parityRepoGateway + "->" + parityRepoPayments,     // GCP cloud scope
	parityRepoFluxSource + "->" + parityRepoRenamed,   // Flux RemoteURL equality
	parityRepoPayments + "->" + parityRepoExtConfig,   // ArgoCD external config
	parityRepoSelfConfig + "->" + parityRepoInventory, // ArgoCD control config
	parityRepoRouteTerraform + "->" + parityRepoPayments,
	parityRepoRouteTerragrunt + "->" + parityRepoPayments,
	parityRepoRouteHelmChart + "->" + parityRepoPayments,
	parityRepoRouteHelmValues + "->" + parityRepoPayments,
	parityRepoRouteKustomize + "->" + parityRepoPayments,
	parityRepoRouteDockerfile + "->" + parityRepoPayments,
	parityRepoRouteJenkins + "->" + parityRepoPayments,
	parityRepoRouteArgoCDApp + "->" + parityRepoPayments,
	parityRepoRouteFluxAlias + "->" + parityRepoPayments,
	parityRepoRouteRawKind + "->" + parityRepoPayments,
	parityRepoEncodedSource + "->" + parityRepoEncodedDash,   // percent-encoded Flux URL
	parityRepoEncodedSource + "->" + parityRepoEncodedAccent, // percent-encoded Flux URL
	parityRepoBilling + "->" + parityRepoLongOwner,           // slug with a long owner
}

// touchingOwed projects evidence onto the rows that touch an owed repository, in
// a stable form that keeps everything an edge's identity depends on.
func touchingOwed(evidence []relationships.EvidenceFact, owed map[string]struct{}) []string {
	rows := make([]string, 0, len(evidence))
	for _, fact := range evidence {
		_, sourceOwed := owed[fact.SourceRepoID]
		_, targetOwed := owed[fact.TargetRepoID]
		if fact.SourceRepoID == "" || fact.TargetRepoID == "" || (!sourceOwed && !targetOwed) {
			continue
		}
		rows = append(rows, strings.Join([]string{
			string(fact.EvidenceKind), string(fact.RelationshipType),
			fact.SourceRepoID, fact.TargetRepoID, fact.TargetEntityID,
			detailString(fact, "path"), detailString(fact, "matched_value"),
		}, "|"))
	}
	sort.Strings(rows)
	return rows
}

func detailString(fact relationships.EvidenceFact, key string) string {
	value, _ := fact.Details[key].(string)
	return value
}

func edgePairs(rows []string) map[string]struct{} {
	pairs := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		parts := strings.Split(row, "|")
		pairs[parts[2]+"->"+parts[3]] = struct{}{}
	}
	return pairs
}

// fleetEvidence is the reference answer: discovery over every row in the corpus.
func fleetEvidence(t *testing.T, catalog []relationships.CatalogEntry, owed map[string]struct{}) []string {
	t.Helper()
	envelopes := parityEnvelopes(t, parityCorpus())
	return touchingOwed(relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(envelopes, catalog)), owed)
}

// TestAnchorParityCorpusCoversEveryRoute keeps the comparisons honest. Without
// it, a corpus that produced no ArgoCD or Flux evidence on the fleet side would
// pass every parity test below.
func TestAnchorParityCorpusCoversEveryRoute(t *testing.T) {
	t.Parallel()

	rows := fleetEvidence(t, parityCatalog(), parityOwed())
	pairs := edgePairs(rows)
	for _, want := range wantOwedEdges {
		if _, ok := pairs[want]; !ok {
			t.Errorf("fleet side has no edge %s, so the corpus does not exercise that route", want)
		}
	}
	if t.Failed() {
		t.Logf("fleet evidence: %v", rows)
	}
}

// TestAnchorScopedLoadKeepsOwedEvidence runs the production two-phase load
// against the corpus and compares discovery over the narrowed rows with discovery
// over every row, for the same owed repositories. It is the accuracy gate for
// the anchor predicate: fewer rows must not mean fewer edges.
func TestAnchorScopedLoadKeepsOwedEvidence(t *testing.T) {
	t.Parallel()

	catalog := parityCatalog()
	owed := parityOwed()
	fleet := fleetEvidence(t, catalog, owed)

	queryer := &parityQueryer{t: t, corpus: parityCorpus()}
	narrowed, err := backfill.LoadAnchorScopedRelationshipFacts(
		context.Background(), queryer, backfill.RepositoryScopedCatalog(catalog, owed), catalog)
	if err != nil {
		t.Fatalf("LoadAnchorScopedRelationshipFacts: %v", err)
	}

	got := touchingOwed(relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(narrowed, catalog)), owed)
	if !reflect.DeepEqual(got, fleet) {
		t.Errorf("narrowed evidence differs from fleet evidence\nnarrowed (%d): %v\nfleet (%d):    %v\nmissing: %v",
			len(got), got, len(fleet), fleet, missingRows(fleet, got))
	}
	if noise := countFacts(narrowed, "noise-content-"); noise != 0 {
		t.Errorf("%d noise content rows were selected; each carries a repository: repo_id, so the predicate is not narrowing", noise)
	}
	wantArgoCDFileRowsNarrowed(t, narrowed)
	t.Logf("corpus=%d anchor_rows=%d config_rows=%d narrowed=%d fleet_edges=%d narrowed_edges=%d noise_content=%d noise_file=%d",
		len(queryer.corpus), queryer.anchorRows, queryer.configRows, len(narrowed), len(fleet), len(got),
		countFacts(narrowed, "noise-content-"), countFacts(narrowed, "noise-file-"))
}

// wantArgoCDFileRowsNarrowed is the content half of the predicate: a file row
// loads for what its ArgoCD buckets hold, never for carrying the keys.
func wantArgoCDFileRowsNarrowed(t *testing.T, narrowed []facts.Envelope) {
	t.Helper()
	if noise := countFacts(narrowed, "noise-file-"); noise != 0 {
		t.Errorf("%d noise file rows were selected; their ArgoCD keys are null", noise)
	}
	for _, envelope := range narrowed {
		if strings.HasPrefix(envelope.FactID, "negative-") {
			t.Errorf("%s was selected, but its ArgoCD keys hold nothing a reader can use", envelope.FactID)
		}
	}
	for _, factID := range []string{"marker-application-file", "marker-applicationset-file", "config-file-only"} {
		if countFacts(narrowed, factID) != 1 {
			t.Errorf("%s was not selected; it reaches the load only through its ArgoCD content", factID)
		}
	}
}

// TestInboundClosureKeepsOwedSources runs the real closure step. Each partition's
// group is discovered on its own there, so an ArgoCD edge survives only if the
// group's config files arrive through the second phase, not by sitting in a
// fleet-wide load next to the ApplicationSet.
func TestInboundClosureKeepsOwedSources(t *testing.T) {
	t.Parallel()

	catalog := parityCatalog()
	owed := parityOwed()
	fleet := fleetEvidence(t, catalog, owed)

	wantSources := make(map[string]struct{})
	for pair := range edgePairs(fleet) {
		wantSources[strings.SplitN(pair, "->", 2)[0]] = struct{}{}
	}

	owedRepos := make(map[string]backfill.RepositoryGenerationIdentity, len(owed))
	for repoID := range owed {
		scopeID := "git:" + repoID
		owedRepos[repoID] = backfill.RepositoryGenerationIdentity{
			RepoID: repoID, ScopeID: scopeID, GenerationID: parityGeneration(scopeID),
		}
	}
	store := Store{Backfill: backfill.Store{DB: &parityQueryer{t: t, corpus: parityCorpus()}}}
	load := make(map[backfill.ScopeGenerationPartition]struct{})
	sourceRepoIDs := make(map[string]struct{})
	if err := store.addInboundTargetedMaintenanceSources(
		context.Background(), catalog, owedRepos, load, sourceRepoIDs,
	); err != nil {
		t.Fatalf("addInboundTargetedMaintenanceSources: %v", err)
	}

	if !reflect.DeepEqual(sourceRepoIDs, wantSources) {
		t.Errorf("closure sources = %v, want %v", sortedKeys(sourceRepoIDs), sortedKeys(wantSources))
	}
	quiet := backfill.ScopeGenerationPartition{ScopeID: "git:" + parityRepoQuiet, GenerationID: parityGeneration("git:" + parityRepoQuiet)}
	if _, loaded := load[quiet]; loaded {
		t.Errorf("quiet partition %v was marked for loading, but none of its evidence touches an owed repository", quiet)
	}
}

// TestPerCommitScopedLoadKeepsNewRepositoryEvidence is the second caller of the
// load. A per-commit pass matches against a scoped catalog, so the control
// repository has to come back from the config-repo resolution as well as its
// files, or the control-config edge has no catalog entry to resolve through.
func TestPerCommitScopedLoadKeepsNewRepositoryEvidence(t *testing.T) {
	t.Parallel()

	catalog := parityCatalog()
	newRepoIDs := map[string]struct{}{parityRepoInventory: {}}
	fleet := fleetEvidence(t, catalog, newRepoIDs)
	if len(fleet) == 0 {
		t.Fatal("fleet side has no evidence for the new repository")
	}

	queryer := &parityQueryer{t: t, corpus: parityCorpus()}
	narrowed, err := backfill.LoadAnchorScopedRelationshipFacts(
		context.Background(), queryer, backfill.RepositoryScopedCatalog(catalog, newRepoIDs), catalog)
	if err != nil {
		t.Fatalf("LoadAnchorScopedRelationshipFacts: %v", err)
	}
	scoped := backfill.BackfillScopedCatalog(catalog, narrowed, newRepoIDs)
	got := touchingOwed(relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(narrowed, scoped)), newRepoIDs)
	if !reflect.DeepEqual(got, fleet) {
		t.Errorf("per-commit evidence differs from fleet evidence\nnarrowed: %v\nfleet:    %v", got, fleet)
	}
	wantArgoCDFileRowsNarrowed(t, narrowed)
}

func missingRows(want, got []string) []string {
	have := make(map[string]struct{}, len(got))
	for _, row := range got {
		have[row] = struct{}{}
	}
	var missing []string
	for _, row := range want {
		if _, ok := have[row]; !ok {
			missing = append(missing, row)
		}
	}
	return missing
}

func countFacts(envelopes []facts.Envelope, prefix string) int {
	count := 0
	for _, envelope := range envelopes {
		if strings.HasPrefix(envelope.FactID, prefix) {
			count++
		}
	}
	return count
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

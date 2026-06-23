package relationships

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// argoCDExternalConfigCorpus builds the corpus that exercises the codex P2
// under-select: a control-plane ApplicationSet whose git generator targets an
// EXTERNAL config repo, the external config file whose param synthesizes the
// deploy repoURL, and the catalog entries. The deploy edge is only discoverable
// when the external config file is present in the envelopes DiscoverEvidence
// sees, because argocdEvaluatedTemplateSources renders the deploy repoURL from
// the config file's params.
func argoCDExternalConfigCorpus() (
	appSet facts.Envelope,
	externalConfig facts.Envelope,
	catalog []CatalogEntry,
) {
	appSet = facts.Envelope{
		FactID:  "fact-appset",
		ScopeID: "repo:control-plane",
		Payload: map[string]any{
			"repo_id":       "repo:control-plane",
			"artifact_type": "argocd",
			"relative_path": "appset.yaml",
			"content": "kind: ApplicationSet\n" +
				"spec:\n" +
				"  generators:\n" +
				"    - git:\n" +
				"        repoURL: gitops-config\n" +
				"        files:\n" +
				"          - path: apps/*/config.yaml\n" +
				"  template:\n" +
				"    spec:\n" +
				"      source:\n" +
				"        repoURL: '{{.team}}-{{.service}}'\n",
		},
	}
	// The deploy repoURL "payments-api" is SYNTHESIZED from team + service. It
	// appears verbatim in NEITHER the ApplicationSet payload NOR this config file,
	// so neither the payments-api alias anchor nor the ArgoCD markers select this
	// file. This is the genuine under-select the two-phase load must repair.
	externalConfig = facts.Envelope{
		FactID:  "fact-config",
		ScopeID: "repo:gitops-config",
		Payload: map[string]any{
			"repo_id":       "repo:gitops-config",
			"relative_path": "apps/payments/config.yaml",
			"content":       "team: payments\nservice: api\n",
		},
	}
	catalog = []CatalogEntry{
		{RepoID: "repo:control-plane", Aliases: []string{"control-plane"}},
		{RepoID: "repo:gitops-config", Aliases: []string{"gitops-config"}},
		{RepoID: "repo:payments", Aliases: []string{"payments-api"}},
	}
	return appSet, externalConfig, catalog
}

// TestArgoCDExternalConfigEdgeNeedsConfigFile pins the premise of the two-phase
// fix: with the external config file present DiscoverEvidence finds the deploy
// edge, and without it the edge is lost. If this ever stops holding the
// under-select risk is gone and the second load phase can be revisited.
func TestArgoCDExternalConfigEdgeNeedsConfigFile(t *testing.T) {
	t.Parallel()

	appSet, externalConfig, catalog := argoCDExternalConfigCorpus()

	withConfig := DedupeEvidenceFacts(DiscoverEvidence([]facts.Envelope{appSet, externalConfig}, catalog))
	withoutConfig := DedupeEvidenceFacts(DiscoverEvidence([]facts.Envelope{appSet}, catalog))

	if !hasDeploySourceEdge(withConfig, "repo:payments") {
		t.Fatalf("expected deploy edge to repo:payments with config file, got %v", canonicalEvidence(withConfig))
	}
	if hasDeploySourceEdge(withoutConfig, "repo:payments") {
		t.Fatalf("expected NO deploy edge to repo:payments without config file, got %v", canonicalEvidence(withoutConfig))
	}
}

// TestTwoPhaseScopedLoadIncludesExternalArgoCDConfig is the central correctness
// gate for the codex P2 fix: the two-phase scoped fact load (marker-selected
// ApplicationSet facts + ResolveArgoCDGeneratorConfigRepos-driven external config
// files) discovers the same evidence as the full corpus, while a single-phase
// load that stops at the marker-selected ApplicationSet drops the deploy edge.
func TestTwoPhaseScopedLoadIncludesExternalArgoCDConfig(t *testing.T) {
	t.Parallel()

	appSet, externalConfig, catalog := argoCDExternalConfigCorpus()
	fullCorpus := []facts.Envelope{appSet, externalConfig}

	// The new deploy repo is repo:payments; scope the catalog to it like the
	// per-commit backfill does.
	scopedCatalog := []CatalogEntry{{RepoID: "repo:payments", Aliases: []string{"payments-api"}}}

	// Phase one: the marker anchors select the ApplicationSet but NOT the external
	// config file (its payload contains neither the payments-api alias nor an
	// ArgoCD marker).
	markerAnchors := argoCDOverSelectMarkersSim
	var phaseOne []facts.Envelope
	for _, envelope := range fullCorpus {
		if payloadMatchesAnchorsSim(t, envelope, append(CatalogPayloadAnchors(scopedCatalog), markerAnchors...)) {
			phaseOne = append(phaseOne, envelope)
		}
	}
	if containsFactID(phaseOne, "fact-config") {
		t.Fatal("precondition failed: external config file should NOT be selected by phase-one anchors")
	}
	if !containsFactID(phaseOne, "fact-appset") {
		t.Fatal("precondition failed: ApplicationSet should be selected by the ArgoCD marker")
	}

	// Phase two: resolve the external config repos the loaded ApplicationSets
	// target (against the full catalog) and add their generator-path config files.
	configRefs := ResolveArgoCDGeneratorConfigRepos(phaseOne, catalog)
	configRepoIDs := make(map[string]struct{}, len(configRefs))
	for _, ref := range configRefs {
		configRepoIDs[ref.ConfigRepoID] = struct{}{}
	}
	var phaseTwo []facts.Envelope
	for _, envelope := range fullCorpus {
		if _, ok := configRepoIDs[sourceRepositoryIDFromEnvelope(envelope)]; ok {
			phaseTwo = append(phaseTwo, envelope)
		}
	}
	scopedLoad := mergeEnvelopesByFactID(phaseOne, phaseTwo)

	// The scoped catalog must also include the ArgoCD config repos so the deploy
	// edge can resolve the intermediate config repoURL, mirroring
	// backfillScopedCatalog. Without the config repo entry the ApplicationSet
	// discovery loop is skipped and the deploy edge is never emitted.
	scopedCatalogWithConfig := append([]CatalogEntry(nil), scopedCatalog...)
	for repoID := range configRepoIDs {
		for _, entry := range catalog {
			if entry.RepoID == repoID {
				scopedCatalogWithConfig = append(scopedCatalogWithConfig, entry)
			}
		}
	}

	// Even with the config repo in the catalog, a single-phase load (ApplicationSet
	// without the external config FILE) still drops the edge — the config file's
	// params are required to synthesize the deploy repoURL. This isolates the loss
	// to the missing config file, which phase two repairs.
	singlePhase := DedupeEvidenceFacts(DiscoverEvidence(phaseOne, scopedCatalogWithConfig))
	if hasDeploySourceEdge(singlePhase, "repo:payments") {
		t.Fatal("single-phase load unexpectedly found the deploy edge; corpus does not exercise the under-select")
	}

	fullEvidence := DedupeEvidenceFacts(DiscoverEvidence(fullCorpus, scopedCatalogWithConfig))
	scopedEvidence := DedupeEvidenceFacts(DiscoverEvidence(scopedLoad, scopedCatalogWithConfig))

	if !hasDeploySourceEdge(scopedEvidence, "repo:payments") {
		t.Fatalf("two-phase scoped load missing the deploy edge, got %v", canonicalEvidence(scopedEvidence))
	}
	if !reflect.DeepEqual(canonicalEvidence(fullEvidence), canonicalEvidence(scopedEvidence)) {
		t.Fatalf("two-phase scoped evidence != full evidence\nfull:   %v\nscoped: %v",
			canonicalEvidence(fullEvidence), canonicalEvidence(scopedEvidence))
	}
}

func hasDeploySourceEdge(evidence []EvidenceFact, sourceRepoID string) bool {
	for _, fact := range evidence {
		if fact.SourceRepoID == sourceRepoID &&
			fact.EvidenceKind == EvidenceKindArgoCDApplicationSetDeploySource {
			return true
		}
	}
	return false
}

func containsFactID(envelopes []facts.Envelope, factID string) bool {
	for _, envelope := range envelopes {
		if envelope.FactID == factID {
			return true
		}
	}
	return false
}

// mergeEnvelopesByFactID mirrors the postgres mergeRelationshipFacts dedupe so
// the pure-Go equality test reproduces the two-phase merge.
func mergeEnvelopesByFactID(primary, secondary []facts.Envelope) []facts.Envelope {
	seen := make(map[string]struct{}, len(primary))
	for _, envelope := range primary {
		if envelope.FactID != "" {
			seen[envelope.FactID] = struct{}{}
		}
	}
	merged := append([]facts.Envelope(nil), primary...)
	for _, envelope := range secondary {
		if envelope.FactID != "" {
			if _, ok := seen[envelope.FactID]; ok {
				continue
			}
			seen[envelope.FactID] = struct{}{}
		}
		merged = append(merged, envelope)
	}
	return merged
}

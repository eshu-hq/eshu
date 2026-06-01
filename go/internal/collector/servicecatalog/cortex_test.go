package servicecatalog

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func cortexContext() FixtureContext {
	return FixtureContext{
		ScopeID:             "git://github.com/eshu-hq/checkout-api",
		GenerationID:        "gen-1",
		CollectorInstanceID: "fixture-cortex",
		FencingToken:        9,
		ObservedAt:          time.Date(2026, 5, 31, 3, 30, 0, 0, time.UTC),
		SourceURI:           "cortex.yaml",
	}
}

func TestCortexManifestEnvelopesEmitsTypedContract(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	envelopes, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	// Four parseable service documents -> four entity facts.
	assertKindCount(t, byKind, facts.ServiceCatalogEntityFactKind, 4)
	// Every entity declares an owner -> four ownership facts.
	assertKindCount(t, byKind, facts.ServiceCatalogOwnershipFactKind, 4)
	// checkout-api (exact), ledger (derived), search (name-only) declare a
	// repository link; notifications declares none.
	assertKindCount(t, byKind, facts.ServiceCatalogRepositoryLinkFactKind, 3)
	// checkout-api dependsOn ledger + payments-db.
	assertKindCount(t, byKind, facts.ServiceCatalogDependencyFactKind, 2)
	// checkout-api runbook link is a clean operational link; the PagerDuty link
	// carries a token query string and is redacted to a warning instead.
	assertKindCount(t, byKind, facts.ServiceCatalogOperationalLinkFactKind, 1)
	assertWarningReason(t, byKind, "operational_link_redacted")

	entity := findEntity(t, envelopes, "service:cortex/checkout-api")
	assertPayload(t, entity.Payload, "provider", string(ProviderCortex))
	assertPayload(t, entity.Payload, "entity_ref", "service:cortex/checkout-api")
	assertPayload(t, entity.Payload, "entity_type", "service")
	assertPayload(t, entity.Payload, "display_name", "Checkout API")
	assertPayload(t, entity.Payload, "tier", "tier-1")

	// Hard producer constraint: never mint canonical identity from catalog text.
	assertBlank(t, entity.Payload, "service_id")
	assertBlank(t, entity.Payload, "workload_id")

	link := findRepositoryLink(t, envelopes, "service:cortex/checkout-api")
	assertPayload(t, link.Payload, "repository_url", "https://github.com/eshu-hq/checkout-api")
	assertBlank(t, link.Payload, "repository_id")
	assertBlank(t, link.Payload, "service_id")
	assertBlank(t, link.Payload, "workload_id")

	owner := findOwnership(t, envelopes, "service:cortex/checkout-api")
	assertPayload(t, owner.Payload, "owner_ref", "team-payments")

	// Search declares an unknown/self-hosted git provider, which cannot be
	// expanded into a URL. It must surface as a name-only locator, never a
	// fabricated URL, so the reducer can reject it.
	searchLink := findRepositoryLink(t, envelopes, "service:cortex/search")
	assertPayload(t, searchLink.Payload, "repository_name", "search")
	assertBlank(t, searchLink.Payload, "repository_url")

	// Every emitted service-catalog envelope must carry the shared schema
	// version, or the projector silently rejects the reducer intent.
	for _, envelope := range envelopes {
		if _, ok := facts.ServiceCatalogSchemaVersion(envelope.FactKind); !ok {
			t.Fatalf("emitted unexpected fact kind %q", envelope.FactKind)
		}
		if envelope.SchemaVersion != facts.ServiceCatalogSchemaVersionV1 {
			t.Fatalf("fact %q schema_version = %q, want %q", envelope.FactKind, envelope.SchemaVersion, facts.ServiceCatalogSchemaVersionV1)
		}
		if envelope.CollectorKind != CollectorKind {
			t.Fatalf("fact %q collector_kind = %q, want %q", envelope.FactKind, envelope.CollectorKind, CollectorKind)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceObserved {
			t.Fatalf("fact %q source_confidence = %q, want observed", envelope.FactKind, envelope.SourceConfidence)
		}
		if envelope.ScopeID != cortexContext().ScopeID || envelope.GenerationID != cortexContext().GenerationID {
			t.Fatalf("fact %q scope/generation = %q/%q, want fixture boundary", envelope.FactKind, envelope.ScopeID, envelope.GenerationID)
		}
	}
}

func TestCortexGitProviderHostDerivation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		provider string
		project  string
		repo     string
		wantURL  string
		wantName string
	}{
		{"github", "github", "", "eshu-hq/checkout-api", "https://github.com/eshu-hq/checkout-api", ""},
		{"gitlab", "gitlab", "", "group/sub/project", "https://gitlab.com/group/sub/project", ""},
		{"bitbucket", "bitbucket", "", "team/repo", "https://bitbucket.org/team/repo", ""},
		{"azure", "azure", "myproject", "myrepo", "https://dev.azure.com/myproject/_git/myrepo", ""},
		{"unknown_provider", "self-hosted", "", "team/repo", "", "team/repo"},
		{"slug_without_path", "github", "", "barename", "", "barename"},
		{"azure_missing_project", "azure", "", "myrepo", "", "myrepo"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			git := cortexGit{Providers: map[string]cortexGitProvider{
				tc.provider: {Project: tc.project, Repository: tc.repo},
			}}
			gotURL, gotName := git.repositoryLocator()
			if gotURL != tc.wantURL {
				t.Fatalf("repositoryLocator() url = %q, want %q", gotURL, tc.wantURL)
			}
			if gotName != tc.wantName {
				t.Fatalf("repositoryLocator() name = %q, want %q", gotName, tc.wantName)
			}
		})
	}
}

func TestCortexGitMultiProviderIsDeterministic(t *testing.T) {
	t.Parallel()

	// A descriptor declaring more than one provider must resolve deterministically
	// (sorted provider order), or stable fact ids would drift across emissions.
	git := cortexGit{Providers: map[string]cortexGitProvider{
		"gitlab": {Repository: "group/project"},
		"github": {Repository: "eshu-hq/checkout-api"},
	}}
	wantURL := "https://github.com/eshu-hq/checkout-api"
	for i := 0; i < 50; i++ {
		gotURL, gotName := git.repositoryLocator()
		if gotURL != wantURL || gotName != "" {
			t.Fatalf("multi-provider repositoryLocator() = (%q, %q), want (%q, \"\")", gotURL, gotName, wantURL)
		}
	}
}

// TestCortexManifestReducerRoundTrip is the payload-key-fidelity contract test:
// emitted envelopes flow through the real reducer index and must reach the
// intended outcomes. It imports the reducer in test code only.
func TestCortexManifestReducerRoundTrip(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	catalog, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}

	// Synthetic active repository facts the reducer correlates against. Only
	// checkout-api and ledger have a real active match.
	repos := []facts.Envelope{
		activeRepositoryFact("repo-checkout", "https://github.com/eshu-hq/checkout-api", false),
		activeRepositoryFact("repo-ledger", "https://github.com/eshu-hq/ledger.git", false),
	}

	decisions := reducer.BuildServiceCatalogCorrelationDecisions(append(catalog, repos...))
	byEntity := decisionsByEntity(decisions)

	assertOutcome(t, byEntity, "service:cortex/checkout-api", reducer.ServiceCatalogCorrelationExact)
	assertOutcome(t, byEntity, "service:cortex/ledger", reducer.ServiceCatalogCorrelationDerived)
	assertOutcome(t, byEntity, "service:cortex/notifications", reducer.ServiceCatalogCorrelationUnresolved)
	assertOutcome(t, byEntity, "service:cortex/search", reducer.ServiceCatalogCorrelationRejected)

	// Non-over-admission: provenance-only entities never carry canonical ids.
	for _, ref := range []string{"service:cortex/notifications", "service:cortex/search"} {
		decision := byEntity[ref]
		if decision.ServiceID != "" || decision.WorkloadID != "" || decision.RepositoryID != "" {
			t.Fatalf("entity %q over-admitted: service=%q workload=%q repo=%q", ref, decision.ServiceID, decision.WorkloadID, decision.RepositoryID)
		}
		if !decision.ProvenanceOnly {
			t.Fatalf("entity %q must remain provenance-only", ref)
		}
	}

	// Owners are recorded as provenance even when unresolved.
	if got := byEntity["service:cortex/notifications"].OwnerRef; got != "team-platform" {
		t.Fatalf("notifications owner_ref = %q, want team-platform", got)
	}

	// Dependency facts are carried but must not change the entity outcome.
	if byEntity["service:cortex/checkout-api"].Outcome != reducer.ServiceCatalogCorrelationExact {
		t.Fatalf("dependency facts must not alter checkout-api outcome")
	}
}

func TestCortexManifestStaleAndAmbiguous(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	catalog, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}

	// ledger matches only a tombstoned repo -> stale. checkout-api matches two
	// active repos with the same remote -> ambiguous.
	repos := []facts.Envelope{
		activeRepositoryFact("repo-checkout-a", "https://github.com/eshu-hq/checkout-api", false),
		activeRepositoryFact("repo-checkout-b", "https://github.com/eshu-hq/checkout-api", false),
		activeRepositoryFact("repo-ledger", "https://github.com/eshu-hq/ledger.git", true),
	}

	decisions := reducer.BuildServiceCatalogCorrelationDecisions(append(catalog, repos...))
	byEntity := decisionsByEntity(decisions)

	assertOutcome(t, byEntity, "service:cortex/checkout-api", reducer.ServiceCatalogCorrelationAmbiguous)
	assertOutcome(t, byEntity, "service:cortex/ledger", reducer.ServiceCatalogCorrelationStale)
}

func TestCortexManifestPartialDocumentsWarnNotDrop(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_partial.yaml")
	envelopes, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	// Unsupported descriptor: entity still emitted plus a warning.
	assertWarningReason(t, byKind, "unsupported_descriptor_version")
	if findEntityOK(envelopes, "service:cortex/future-shape") == nil {
		t.Fatalf("unsupported-version entity must still be emitted, not dropped")
	}
	// Missing tag: no entity, but an invalid_ref warning.
	assertWarningReason(t, byKind, "invalid_ref")
	// Token-bearing dashboard link redacted with a warning.
	assertWarningReason(t, byKind, "operational_link_redacted")
}

func TestCortexManifestDuplicateEntityWarns(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_duplicate.yaml")
	envelopes, err := CortexManifestEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	// First-wins: exactly one billing entity, plus a duplicate_entity warning.
	assertKindCount(t, byKind, facts.ServiceCatalogEntityFactKind, 1)
	assertWarningReason(t, byKind, "duplicate_entity")

	entity := findEntity(t, envelopes, "service:cortex/billing")
	assertPayload(t, entity.Payload, "display_name", "Billing (first)")
}

func TestCortexManifestEmptyInputIsClean(t *testing.T) {
	t.Parallel()

	envelopes, err := CortexManifestEnvelopes([]byte("\n# nothing here\n"), cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() empty error = %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("empty manifest envelopes = %d, want 0", len(envelopes))
	}
}

func TestCortexManifestIsIdempotent(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	ctx := cortexContext()
	first, err := CortexManifestEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("first emit error = %v", err)
	}
	second, err := CortexManifestEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("second emit error = %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("re-emit count drift: %d vs %d", len(first), len(second))
	}
	firstIDs := factIDSet(first)
	for _, envelope := range second {
		if !firstIDs[envelope.FactID] {
			t.Fatalf("re-emit produced unstable fact id %q for kind %q", envelope.FactID, envelope.FactKind)
		}
	}
}

func TestCortexManifestRequiresContext(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_catalog.yaml")
	if _, err := CortexManifestEnvelopes(raw, FixtureContext{GenerationID: "gen-1", CollectorInstanceID: "x"}); err == nil {
		t.Fatalf("blank scope_id must error")
	}
	if _, err := CortexManifestEnvelopes(raw, FixtureContext{ScopeID: "s", CollectorInstanceID: "x"}); err == nil {
		t.Fatalf("blank generation_id must error")
	}
	if _, err := CortexManifestEnvelopes(raw, FixtureContext{ScopeID: "s", GenerationID: "g"}); err == nil {
		t.Fatalf("blank collector_instance_id must error")
	}
}

// TestCortexScorecardEnvelopesEmitsCarriedFacts proves the scorecard descriptor
// produces scorecard_definition (per rule) and scorecard_result (per entity)
// facts that pass the schema-version gate, anchor on provider+entity_ref, and
// never carry canonical ids.
func TestCortexScorecardEnvelopesEmitsCarriedFacts(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/cortex_scorecard.yaml")
	envelopes, err := CortexScorecardEnvelopes(raw, cortexContext())
	if err != nil {
		t.Fatalf("CortexScorecardEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	// Two rules -> two definition facts; two declared results -> two result facts.
	assertKindCount(t, byKind, facts.ServiceCatalogScorecardDefinitionFactKind, 2)
	assertKindCount(t, byKind, facts.ServiceCatalogScorecardResultFactKind, 2)

	def := findScorecardDefinition(t, envelopes, "has-runbook")
	assertPayload(t, def.Payload, "provider", string(ProviderCortex))
	assertPayload(t, def.Payload, "scorecard_tag", "production-readiness")
	assertPayload(t, def.Payload, "rule_identifier", "has-runbook")
	assertPayload(t, def.Payload, "level", "Bronze")

	result := findScorecardResult(t, envelopes, "service:cortex/checkout-api")
	assertPayload(t, result.Payload, "provider", string(ProviderCortex))
	assertPayload(t, result.Payload, "entity_ref", "service:cortex/checkout-api")
	assertPayload(t, result.Payload, "scorecard_tag", "production-readiness")
	assertPayload(t, result.Payload, "level", "Gold")
	// Never mint canonical identity from a scorecard result.
	assertBlank(t, result.Payload, "service_id")
	assertBlank(t, result.Payload, "workload_id")

	for _, envelope := range envelopes {
		if _, ok := facts.ServiceCatalogSchemaVersion(envelope.FactKind); !ok {
			t.Fatalf("emitted unexpected fact kind %q", envelope.FactKind)
		}
		if envelope.SchemaVersion != facts.ServiceCatalogSchemaVersionV1 {
			t.Fatalf("fact %q schema_version = %q, want %q", envelope.FactKind, envelope.SchemaVersion, facts.ServiceCatalogSchemaVersionV1)
		}
	}
}

// TestCortexScorecardResultsDoNotChangeCorrelation proves scorecard facts are
// carried-only: feeding them alongside entity facts must not alter any entity's
// reducer outcome, because the reducer index does not consume scorecards yet.
func TestCortexScorecardResultsDoNotChangeCorrelation(t *testing.T) {
	t.Parallel()

	catalog, err := CortexManifestEnvelopes(readFixture(t, "testdata/cortex_catalog.yaml"), cortexContext())
	if err != nil {
		t.Fatalf("CortexManifestEnvelopes() error = %v", err)
	}
	scorecards, err := CortexScorecardEnvelopes(readFixture(t, "testdata/cortex_scorecard.yaml"), cortexContext())
	if err != nil {
		t.Fatalf("CortexScorecardEnvelopes() error = %v", err)
	}
	repos := []facts.Envelope{
		activeRepositoryFact("repo-checkout", "https://github.com/eshu-hq/checkout-api", false),
	}

	withoutScorecards := decisionsByEntity(reducer.BuildServiceCatalogCorrelationDecisions(append(catalog, repos...)))
	combined := append(append([]facts.Envelope{}, catalog...), scorecards...)
	withScorecards := decisionsByEntity(reducer.BuildServiceCatalogCorrelationDecisions(append(combined, repos...)))

	if len(withoutScorecards) != len(withScorecards) {
		t.Fatalf("scorecard facts changed decision count: %d vs %d", len(withoutScorecards), len(withScorecards))
	}
	for ref, before := range withoutScorecards {
		after, ok := withScorecards[ref]
		if !ok {
			t.Fatalf("entity %q lost its decision when scorecards were added", ref)
		}
		if before.Outcome != after.Outcome {
			t.Fatalf("entity %q outcome drifted with scorecards: %q -> %q", ref, before.Outcome, after.Outcome)
		}
	}
}

func TestCortexScorecardEmptyInputIsClean(t *testing.T) {
	t.Parallel()

	envelopes, err := CortexScorecardEnvelopes([]byte("\n# nothing\n"), cortexContext())
	if err != nil {
		t.Fatalf("CortexScorecardEnvelopes() empty error = %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("empty scorecard envelopes = %d, want 0", len(envelopes))
	}
}

// --- cortex-specific helpers ---

func findScorecardDefinition(t *testing.T, envelopes []facts.Envelope, ruleIdentifier string) facts.Envelope {
	t.Helper()
	for i := range envelopes {
		if envelopes[i].FactKind == facts.ServiceCatalogScorecardDefinitionFactKind && envelopes[i].Payload["rule_identifier"] == ruleIdentifier {
			return envelopes[i]
		}
	}
	t.Fatalf("scorecard definition for rule %q not found", ruleIdentifier)
	return facts.Envelope{}
}

func findScorecardResult(t *testing.T, envelopes []facts.Envelope, entityRef string) facts.Envelope {
	t.Helper()
	for i := range envelopes {
		if envelopes[i].FactKind == facts.ServiceCatalogScorecardResultFactKind && envelopes[i].Payload["entity_ref"] == entityRef {
			return envelopes[i]
		}
	}
	t.Fatalf("scorecard result for %q not found", entityRef)
	return facts.Envelope{}
}

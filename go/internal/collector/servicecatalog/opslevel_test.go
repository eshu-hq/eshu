// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func opslevelContext() FixtureContext {
	return FixtureContext{
		ScopeID:             "git://github.com/eshu-hq/checkout-api",
		GenerationID:        "gen-1",
		CollectorInstanceID: "fixture-opslevel",
		FencingToken:        9,
		ObservedAt:          time.Date(2026, 6, 1, 3, 30, 0, 0, time.UTC),
		SourceURI:           "opslevel.yml",
	}
}

func TestOpsLevelManifestEnvelopesEmitsTypedContract(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/opslevel_components.yml")
	envelopes, err := OpsLevelManifestEnvelopes(raw, opslevelContext())
	if err != nil {
		t.Fatalf("OpsLevelManifestEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	// Four parseable component documents -> four entity facts.
	assertKindCount(t, byKind, facts.ServiceCatalogEntityFactKind, 4)
	// Every component declares an owner -> four ownership facts.
	assertKindCount(t, byKind, facts.ServiceCatalogOwnershipFactKind, 4)
	// checkout-api (derivable URL), ledger (derivable URL), search (name-only,
	// unknown provider) declare a repository link; notifications declares none.
	assertKindCount(t, byKind, facts.ServiceCatalogRepositoryLinkFactKind, 3)
	// checkout-api dependsOn ledger + payments_db.
	assertKindCount(t, byKind, facts.ServiceCatalogDependencyFactKind, 2)
	// checkout-api runbook tool is a clean operational link; the PagerDuty tool
	// carries a token query string and is redacted to a warning instead.
	assertKindCount(t, byKind, facts.ServiceCatalogOperationalLinkFactKind, 1)
	assertWarningReason(t, byKind, "operational_link_redacted")

	entity := findEntity(t, envelopes, "component:opslevel/checkout-api")
	assertPayload(t, entity.Payload, "provider", string(ProviderOpsLevel))
	assertPayload(t, entity.Payload, "entity_ref", "component:opslevel/checkout-api")
	assertPayload(t, entity.Payload, "entity_type", "service")
	assertPayload(t, entity.Payload, "display_name", "Checkout API")
	assertPayload(t, entity.Payload, "lifecycle", "generally_available")
	assertPayload(t, entity.Payload, "tier", "tier_1")

	// Hard producer constraint: never mint canonical identity from catalog text.
	assertBlank(t, entity.Payload, "service_id")
	assertBlank(t, entity.Payload, "workload_id")

	// A known provider + slug expands to a derivable repository_url; the producer
	// never fabricates a repository_id, which would force a false exact match.
	link := findRepositoryLink(t, envelopes, "component:opslevel/checkout-api")
	assertPayload(t, link.Payload, "repository_url", "https://github.com/eshu-hq/checkout-api")
	assertBlank(t, link.Payload, "repository_id")
	assertBlank(t, link.Payload, "service_id")
	assertBlank(t, link.Payload, "workload_id")

	// An unknown provider yields a name-only slug locator, never a URL.
	searchLink := findRepositoryLink(t, envelopes, "component:opslevel/search")
	assertPayload(t, searchLink.Payload, "repository_name", "search")
	assertBlank(t, searchLink.Payload, "repository_url")
	assertBlank(t, searchLink.Payload, "repository_id")

	owner := findOwnership(t, envelopes, "component:opslevel/checkout-api")
	assertPayload(t, owner.Payload, "owner_ref", "order_management_team")

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
		if envelope.ScopeID != opslevelContext().ScopeID || envelope.GenerationID != opslevelContext().GenerationID {
			t.Fatalf("fact %q scope/generation = %q/%q, want fixture boundary", envelope.FactKind, envelope.ScopeID, envelope.GenerationID)
		}
	}
}

// TestOpsLevelManifestReducerRoundTrip is the payload-key-fidelity contract
// test: emitted envelopes flow through the real reducer index and must reach the
// intended outcomes. It imports the reducer in test code only.
func TestOpsLevelManifestReducerRoundTrip(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/opslevel_components.yml")
	catalog, err := OpsLevelManifestEnvelopes(raw, opslevelContext())
	if err != nil {
		t.Fatalf("OpsLevelManifestEnvelopes() error = %v", err)
	}

	// Synthetic active repository facts the reducer correlates against. Only
	// checkout-api and ledger have a real active match.
	repos := []facts.Envelope{
		activeRepositoryFact("repo-checkout", "https://github.com/eshu-hq/checkout-api", false),
		activeRepositoryFact("repo-ledger", "https://github.com/eshu-hq/ledger.git", false),
	}

	decisions := reducer.BuildServiceCatalogCorrelationDecisions(append(catalog, repos...))
	byEntity := decisionsByEntity(decisions)

	assertOutcome(t, byEntity, "component:opslevel/checkout-api", reducer.ServiceCatalogCorrelationExact)
	assertOutcome(t, byEntity, "component:opslevel/ledger", reducer.ServiceCatalogCorrelationDerived)
	assertOutcome(t, byEntity, "component:opslevel/notifications", reducer.ServiceCatalogCorrelationUnresolved)
	assertOutcome(t, byEntity, "component:opslevel/search", reducer.ServiceCatalogCorrelationRejected)

	// Non-over-admission: provenance-only entities never carry canonical ids.
	for _, ref := range []string{"component:opslevel/notifications", "component:opslevel/search"} {
		decision := byEntity[ref]
		if decision.ServiceID != "" || decision.WorkloadID != "" || decision.RepositoryID != "" {
			t.Fatalf("entity %q over-admitted: service=%q workload=%q repo=%q", ref, decision.ServiceID, decision.WorkloadID, decision.RepositoryID)
		}
		if !decision.ProvenanceOnly {
			t.Fatalf("entity %q must remain provenance-only", ref)
		}
	}

	// Owners are recorded as provenance even when unresolved.
	if got := byEntity["component:opslevel/notifications"].OwnerRef; got != "team_platform" {
		t.Fatalf("notifications owner_ref = %q, want team_platform", got)
	}

	// Dependency facts are carried but must not change the entity outcome.
	if byEntity["component:opslevel/checkout-api"].Outcome != reducer.ServiceCatalogCorrelationExact {
		t.Fatalf("dependency facts must not alter checkout-api outcome")
	}
}

func TestOpsLevelManifestStaleAndAmbiguous(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/opslevel_components.yml")
	catalog, err := OpsLevelManifestEnvelopes(raw, opslevelContext())
	if err != nil {
		t.Fatalf("OpsLevelManifestEnvelopes() error = %v", err)
	}

	// ledger matches only a tombstoned repo -> stale. checkout-api matches two
	// active repos with the same remote -> ambiguous.
	repos := []facts.Envelope{
		activeRepositoryFact("repo-checkout-a", "https://github.com/eshu-hq/checkout-api", false),
		activeRepositoryFact("repo-checkout-b", "https://github.com/eshu-hq/checkout-api", false),
		activeRepositoryFact("repo-ledger", "https://github.com/eshu-hq/ledger", true),
	}

	decisions := reducer.BuildServiceCatalogCorrelationDecisions(append(catalog, repos...))
	byEntity := decisionsByEntity(decisions)

	assertOutcome(t, byEntity, "component:opslevel/checkout-api", reducer.ServiceCatalogCorrelationAmbiguous)
	assertOutcome(t, byEntity, "component:opslevel/ledger", reducer.ServiceCatalogCorrelationStale)
}

func TestOpsLevelManifestPartialDocumentsWarnNotDrop(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/opslevel_partial.yml")
	envelopes, err := OpsLevelManifestEnvelopes(raw, opslevelContext())
	if err != nil {
		t.Fatalf("OpsLevelManifestEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	// Unsupported descriptor version: entity still emitted plus a warning.
	assertWarningReason(t, byKind, "unsupported_descriptor_version")
	if findEntityOK(envelopes, "component:opslevel/future-shape") == nil {
		t.Fatalf("unsupported-version entity must still be emitted, not dropped")
	}
	// Missing name: no entity, but an invalid_ref warning.
	assertWarningReason(t, byKind, "invalid_ref")
	// Token-bearing dashboard tool redacted with a warning.
	assertWarningReason(t, byKind, "operational_link_redacted")
}

func TestOpsLevelManifestDuplicateEntityWarns(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/opslevel_duplicate.yml")
	envelopes, err := OpsLevelManifestEnvelopes(raw, opslevelContext())
	if err != nil {
		t.Fatalf("OpsLevelManifestEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	// First-wins: exactly one billing entity, plus a duplicate_entity warning.
	assertKindCount(t, byKind, facts.ServiceCatalogEntityFactKind, 1)
	assertWarningReason(t, byKind, "duplicate_entity")

	entity := findEntity(t, envelopes, "component:opslevel/billing")
	assertPayload(t, entity.Payload, "display_name", "Billing (first)")
}

func TestOpsLevelManifestEmptyInputIsClean(t *testing.T) {
	t.Parallel()

	envelopes, err := OpsLevelManifestEnvelopes([]byte("\n# nothing here\n"), opslevelContext())
	if err != nil {
		t.Fatalf("OpsLevelManifestEnvelopes() empty error = %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("empty manifest envelopes = %d, want 0", len(envelopes))
	}
}

func TestOpsLevelManifestIsIdempotent(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/opslevel_components.yml")
	ctx := opslevelContext()
	first, err := OpsLevelManifestEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("first emit error = %v", err)
	}
	second, err := OpsLevelManifestEnvelopes(raw, ctx)
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

func TestOpsLevelEntityRefRejectsEmptySlugAnchor(t *testing.T) {
	t.Parallel()

	// An anchor that is non-blank but consists entirely of punctuation slugifies
	// to "". entityRef must return "" rather than emit "component:opslevel/",
	// which would collide across every such entity and break reducer correlation.
	for _, anchor := range []string{"---", "!!!", "  ///  ", "***"} {
		component := opslevelComponent{Name: anchor}
		if ref := component.entityRef(); ref != "" {
			t.Fatalf("entityRef(name=%q) = %q, want empty (slug is empty)", anchor, ref)
		}
		// Same guard must hold when the empty-slug anchor arrives via an alias.
		aliased := opslevelComponent{Aliases: []string{anchor}, Name: "fallback-name"}
		if ref := aliased.entityRef(); ref != "component:opslevel/fallback-name" {
			t.Fatalf("entityRef(alias=%q) = %q, want fallback to name", anchor, ref)
		}
	}
}

func TestOpsLevelManifestEmptySlugAnchorWarnsNotAdmits(t *testing.T) {
	t.Parallel()

	// A component whose only identifier slugifies to empty must yield an
	// invalid_ref warning and zero entity facts: non-over-admission of a junk ref.
	raw := []byte("version: 1\ncomponent:\n  name: \"---\"\n  owner: order_team\n")
	envelopes, err := OpsLevelManifestEnvelopes(raw, opslevelContext())
	if err != nil {
		t.Fatalf("OpsLevelManifestEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)
	assertKindCount(t, byKind, facts.ServiceCatalogEntityFactKind, 0)
	assertWarningReason(t, byKind, "invalid_ref")
	// No envelope may carry a junk "component:opslevel/" ref with an empty slug.
	for _, envelope := range envelopes {
		if ref, ok := envelope.Payload["entity_ref"].(string); ok {
			if ref == opslevelComponentKind+":"+ProviderOpsLevelNamespace+"/" {
				t.Fatalf("emitted junk empty-slug ref %q", ref)
			}
		}
	}
}

func TestOpsLevelManifestRequiresContext(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/opslevel_components.yml")
	if _, err := OpsLevelManifestEnvelopes(raw, FixtureContext{GenerationID: "gen-1", CollectorInstanceID: "x"}); err == nil {
		t.Fatalf("blank scope_id must error")
	}
	if _, err := OpsLevelManifestEnvelopes(raw, FixtureContext{ScopeID: "s", CollectorInstanceID: "x"}); err == nil {
		t.Fatalf("blank generation_id must error")
	}
	if _, err := OpsLevelManifestEnvelopes(raw, FixtureContext{ScopeID: "s", GenerationID: "g"}); err == nil {
		t.Fatalf("blank collector_instance_id must error")
	}
}

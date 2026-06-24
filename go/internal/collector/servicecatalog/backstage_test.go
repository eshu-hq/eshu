// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func backstageContext() FixtureContext {
	return FixtureContext{
		ScopeID:             "git://github.com/eshu-hq/checkout-api",
		GenerationID:        "gen-1",
		CollectorInstanceID: "fixture-backstage",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 31, 3, 30, 0, 0, time.UTC),
		SourceURI:           "catalog-info.yaml",
	}
}

func TestBackstageManifestEnvelopesEmitsTypedContract(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/backstage_catalog_info.yaml")
	envelopes, err := BackstageManifestEnvelopes(raw, backstageContext())
	if err != nil {
		t.Fatalf("BackstageManifestEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	// Four parseable Component documents -> four entity facts.
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

	entity := findEntity(t, envelopes, "component:default/checkout-api")
	assertPayload(t, entity.Payload, "provider", string(ProviderBackstage))
	assertPayload(t, entity.Payload, "entity_ref", "component:default/checkout-api")
	assertPayload(t, entity.Payload, "entity_type", "service")
	assertPayload(t, entity.Payload, "display_name", "Checkout API")
	assertPayload(t, entity.Payload, "lifecycle", "production")
	assertPayload(t, entity.Payload, "tier", "tier-1")

	// Hard producer constraint: never mint canonical identity from catalog text.
	assertBlank(t, entity.Payload, "service_id")
	assertBlank(t, entity.Payload, "workload_id")

	link := findRepositoryLink(t, envelopes, "component:default/checkout-api")
	assertPayload(t, link.Payload, "repository_url", "https://github.com/eshu-hq/checkout-api")
	assertBlank(t, link.Payload, "repository_id")
	assertBlank(t, link.Payload, "service_id")
	assertBlank(t, link.Payload, "workload_id")

	owner := findOwnership(t, envelopes, "component:default/checkout-api")
	assertPayload(t, owner.Payload, "owner_ref", "team-payments")

	// A kind:namespace/name owner must be preserved verbatim as provenance; the
	// reducer keeps the full reference, and collapsing it to a bare name would
	// merge distinct owners (group:default/x vs user:default/x).
	ledgerOwner := findOwnership(t, envelopes, "component:default/ledger")
	assertPayload(t, ledgerOwner.Payload, "owner_ref", "group:default/team-finance")

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
		if envelope.ScopeID != backstageContext().ScopeID || envelope.GenerationID != backstageContext().GenerationID {
			t.Fatalf("fact %q scope/generation = %q/%q, want fixture boundary", envelope.FactKind, envelope.ScopeID, envelope.GenerationID)
		}
	}
}

// TestBackstageManifestReducerRoundTrip is the payload-key-fidelity contract
// test: emitted envelopes flow through the real reducer index and must reach
// the intended outcomes. It imports the reducer in test code only.
func TestBackstageManifestReducerRoundTrip(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/backstage_catalog_info.yaml")
	catalog, err := BackstageManifestEnvelopes(raw, backstageContext())
	if err != nil {
		t.Fatalf("BackstageManifestEnvelopes() error = %v", err)
	}

	// Synthetic active repository facts the reducer correlates against. Only
	// checkout-api and ledger have a real active match.
	repos := []facts.Envelope{
		activeRepositoryFact("repo-checkout", "https://github.com/eshu-hq/checkout-api", false),
		activeRepositoryFact("repo-ledger", "https://github.com/eshu-hq/ledger", false),
	}

	decisions := reducer.BuildServiceCatalogCorrelationDecisions(append(catalog, repos...))
	byEntity := decisionsByEntity(decisions)

	assertOutcome(t, byEntity, "component:default/checkout-api", reducer.ServiceCatalogCorrelationExact)
	assertOutcome(t, byEntity, "component:default/ledger", reducer.ServiceCatalogCorrelationDerived)
	assertOutcome(t, byEntity, "component:default/notifications", reducer.ServiceCatalogCorrelationUnresolved)
	assertOutcome(t, byEntity, "component:default/search", reducer.ServiceCatalogCorrelationRejected)

	// Non-over-admission: provenance-only entities never carry canonical ids.
	for _, ref := range []string{"component:default/notifications", "component:default/search"} {
		decision := byEntity[ref]
		if decision.ServiceID != "" || decision.WorkloadID != "" || decision.RepositoryID != "" {
			t.Fatalf("entity %q over-admitted: service=%q workload=%q repo=%q", ref, decision.ServiceID, decision.WorkloadID, decision.RepositoryID)
		}
		if !decision.ProvenanceOnly {
			t.Fatalf("entity %q must remain provenance-only", ref)
		}
	}

	// Owners are recorded as provenance even when unresolved.
	if got := byEntity["component:default/notifications"].OwnerRef; got != "team-platform" {
		t.Fatalf("notifications owner_ref = %q, want team-platform", got)
	}

	// Dependency facts are carried but must not change the entity outcome.
	if byEntity["component:default/checkout-api"].Outcome != reducer.ServiceCatalogCorrelationExact {
		t.Fatalf("dependency facts must not alter checkout-api outcome")
	}
}

func TestBackstageManifestStaleAndAmbiguous(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/backstage_catalog_info.yaml")
	catalog, err := BackstageManifestEnvelopes(raw, backstageContext())
	if err != nil {
		t.Fatalf("BackstageManifestEnvelopes() error = %v", err)
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

	assertOutcome(t, byEntity, "component:default/checkout-api", reducer.ServiceCatalogCorrelationAmbiguous)
	assertOutcome(t, byEntity, "component:default/ledger", reducer.ServiceCatalogCorrelationStale)
}

func TestBackstageManifestPartialDocumentsWarnNotDrop(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/backstage_partial.yaml")
	envelopes, err := BackstageManifestEnvelopes(raw, backstageContext())
	if err != nil {
		t.Fatalf("BackstageManifestEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	// Unsupported descriptor: entity still emitted plus a warning.
	assertWarningReason(t, byKind, "unsupported_descriptor_version")
	if findEntityOK(envelopes, "component:default/future-shape") == nil {
		t.Fatalf("unsupported-version entity must still be emitted, not dropped")
	}
	// Missing name: no entity, but an invalid_ref warning.
	assertWarningReason(t, byKind, "invalid_ref")
	// Token-bearing dashboard link redacted with a warning.
	assertWarningReason(t, byKind, "operational_link_redacted")
}

func TestBackstageManifestDuplicateEntityWarns(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/backstage_duplicate.yaml")
	envelopes, err := BackstageManifestEnvelopes(raw, backstageContext())
	if err != nil {
		t.Fatalf("BackstageManifestEnvelopes() error = %v", err)
	}
	byKind := envelopesByKind(envelopes)

	// First-wins: exactly one billing entity, plus a duplicate_entity warning.
	assertKindCount(t, byKind, facts.ServiceCatalogEntityFactKind, 1)
	assertWarningReason(t, byKind, "duplicate_entity")

	entity := findEntity(t, envelopes, "component:default/billing")
	assertPayload(t, entity.Payload, "display_name", "Billing (first)")
}

func TestBackstageManifestEmptyInputIsClean(t *testing.T) {
	t.Parallel()

	envelopes, err := BackstageManifestEnvelopes([]byte("\n# nothing here\n"), backstageContext())
	if err != nil {
		t.Fatalf("BackstageManifestEnvelopes() empty error = %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("empty manifest envelopes = %d, want 0", len(envelopes))
	}
}

func TestBackstageManifestIsIdempotent(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/backstage_catalog_info.yaml")
	ctx := backstageContext()
	first, err := BackstageManifestEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("first emit error = %v", err)
	}
	second, err := BackstageManifestEnvelopes(raw, ctx)
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

func TestBackstageManifestRequiresContext(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/backstage_catalog_info.yaml")
	if _, err := BackstageManifestEnvelopes(raw, FixtureContext{GenerationID: "gen-1", CollectorInstanceID: "x"}); err == nil {
		t.Fatalf("blank scope_id must error")
	}
	if _, err := BackstageManifestEnvelopes(raw, FixtureContext{ScopeID: "s", CollectorInstanceID: "x"}); err == nil {
		t.Fatalf("blank generation_id must error")
	}
	if _, err := BackstageManifestEnvelopes(raw, FixtureContext{ScopeID: "s", GenerationID: "g"}); err == nil {
		t.Fatalf("blank collector_instance_id must error")
	}
}

// --- helpers ---

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

func envelopesByKind(envelopes []facts.Envelope) map[string][]facts.Envelope {
	byKind := make(map[string][]facts.Envelope)
	for _, envelope := range envelopes {
		byKind[envelope.FactKind] = append(byKind[envelope.FactKind], envelope)
	}
	return byKind
}

func assertKindCount(t *testing.T, byKind map[string][]facts.Envelope, kind string, want int) {
	t.Helper()
	if got := len(byKind[kind]); got != want {
		t.Fatalf("kind %q count = %d, want %d", kind, got, want)
	}
}

func assertWarningReason(t *testing.T, byKind map[string][]facts.Envelope, reason string) {
	t.Helper()
	for _, warning := range byKind[facts.ServiceCatalogWarningFactKind] {
		if warning.Payload["reason"] == reason {
			return
		}
	}
	t.Fatalf("missing warning reason %q", reason)
}

func assertPayload(t *testing.T, payload map[string]any, key, want string) {
	t.Helper()
	if got, _ := payload[key].(string); got != want {
		t.Fatalf("payload[%q] = %#v, want %q", key, payload[key], want)
	}
}

func assertBlank(t *testing.T, payload map[string]any, key string) {
	t.Helper()
	if got, ok := payload[key]; ok && got != "" {
		t.Fatalf("payload[%q] = %#v, want blank or absent", key, got)
	}
}

func findEntity(t *testing.T, envelopes []facts.Envelope, entityRef string) facts.Envelope {
	t.Helper()
	envelope := findEntityOK(envelopes, entityRef)
	if envelope == nil {
		t.Fatalf("entity %q not found", entityRef)
	}
	return *envelope
}

func findEntityOK(envelopes []facts.Envelope, entityRef string) *facts.Envelope {
	for i := range envelopes {
		if envelopes[i].FactKind == facts.ServiceCatalogEntityFactKind && envelopes[i].Payload["entity_ref"] == entityRef {
			return &envelopes[i]
		}
	}
	return nil
}

func findRepositoryLink(t *testing.T, envelopes []facts.Envelope, entityRef string) facts.Envelope {
	t.Helper()
	for i := range envelopes {
		if envelopes[i].FactKind == facts.ServiceCatalogRepositoryLinkFactKind && envelopes[i].Payload["entity_ref"] == entityRef {
			return envelopes[i]
		}
	}
	t.Fatalf("repository link for %q not found", entityRef)
	return facts.Envelope{}
}

func findOwnership(t *testing.T, envelopes []facts.Envelope, entityRef string) facts.Envelope {
	t.Helper()
	for i := range envelopes {
		if envelopes[i].FactKind == facts.ServiceCatalogOwnershipFactKind && envelopes[i].Payload["entity_ref"] == entityRef {
			return envelopes[i]
		}
	}
	t.Fatalf("ownership for %q not found", entityRef)
	return facts.Envelope{}
}

func decisionsByEntity(decisions []reducer.ServiceCatalogCorrelationDecision) map[string]reducer.ServiceCatalogCorrelationDecision {
	byEntity := make(map[string]reducer.ServiceCatalogCorrelationDecision, len(decisions))
	for _, decision := range decisions {
		byEntity[decision.EntityRef] = decision
	}
	return byEntity
}

func assertOutcome(t *testing.T, byEntity map[string]reducer.ServiceCatalogCorrelationDecision, entityRef string, want reducer.ServiceCatalogCorrelationOutcome) {
	t.Helper()
	decision, ok := byEntity[entityRef]
	if !ok {
		t.Fatalf("no decision for %q", entityRef)
	}
	if decision.Outcome != want {
		t.Fatalf("entity %q outcome = %q, want %q (reason: %s)", entityRef, decision.Outcome, want, decision.Reason)
	}
}

// activeRepositoryFact builds a minimal repository fact the reducer index reads
// via serviceCatalogRepositoryFromFact (graph_id/repo_id, remote_url, tombstone).
func activeRepositoryFact(repoID, remoteURL string, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactID:      "repo-fact-" + repoID,
		FactKind:    "repository",
		IsTombstone: tombstone,
		Payload: map[string]any{
			"graph_id":   repoID,
			"name":       repoID,
			"remote_url": remoteURL,
		},
	}
}

func factIDSet(envelopes []facts.Envelope) map[string]bool {
	set := make(map[string]bool, len(envelopes))
	for _, envelope := range envelopes {
		set[envelope.FactID] = true
	}
	return set
}

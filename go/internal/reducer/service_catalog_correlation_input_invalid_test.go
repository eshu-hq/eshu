// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestServiceCatalogCorrelationQuarantinesEntityMissingEntityRef is the
// flagship regression test for Wave 4f S3 of Contract System v1 (issue
// #4755): the service_catalog family's typed-decode migration. It proves the
// accuracy guarantee the migration exists to protect: a
// "service_catalog.entity" fact missing its required entity_ref key
// dead-letters as a per-fact input_invalid quarantine via
// partitionDecodeFailures, NOT a silent empty-string catalog identity.
//
// Before the migration this was impossible to observe: the correlation index
// read entity_ref with payloadString, which returns "" for the absent key,
// and the malformed fact was silently dropped by the
// `if entity.entityRef != ""` guard with no operator-visible signal — no
// quarantinedFact was ever recorded. After the migration, the index decodes
// each "service_catalog.entity" fact's outer envelope through
// factschema.DecodeServiceCatalogEntity (decodeServiceCatalogEntity); the
// malformed fact yields a classified *factDecodeError that
// partitionDecodeFailures routes to an explicit quarantinedFact naming the
// missing field, while a valid sibling entity fact in the same batch still
// produces its correlation decision (per-fact isolation).
func TestServiceCatalogCorrelationQuarantinesEntityMissingEntityRef(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-entity-missing-entity-ref",
		FactKind: facts.ServiceCatalogEntityFactKind,
		Payload: map[string]any{
			// "entity_ref" intentionally absent.
			"provider":     "backstage",
			"entity_type":  "component",
			"display_name": "Orphan",
		},
	}
	valid := serviceCatalogEntityFact("valid-entity", "component:default/checkout", "Checkout")
	repositoryLink := serviceCatalogRepositoryLinkFact("repo-link-checkout", "component:default/checkout", "https://github.com/acme/checkout.git")
	repository := repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false)

	loader := &stubServiceCatalogCorrelationFactLoader{
		scopeFacts:  []facts.Envelope{malformed, valid, repositoryLink},
		activeRepos: []facts.Envelope{repository},
	}
	writer := &recordingServiceCatalogCorrelationWriter{}
	handler := ServiceCatalogCorrelationHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-service-catalog-quarantine",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		Domain:       DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
		Cause:        "service catalog facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	// Per-fact isolation: the valid sibling entity still produces its exact
	// correlation decision despite the malformed entity sharing the batch.
	decisions := serviceCatalogDecisionsByEntity(writer.write.Decisions)
	valid1 := decisions["component:default/checkout"]
	if valid1.Outcome != ServiceCatalogCorrelationExact {
		t.Fatalf("valid sibling outcome = %q, want %q; a malformed entity in the same batch must not block a valid sibling from correlating", valid1.Outcome, ServiceCatalogCorrelationExact)
	}
	if valid1.RepositoryID != "repo-checkout" {
		t.Fatalf("valid sibling RepositoryID = %q, want repo-checkout", valid1.RepositoryID)
	}

	// No decision anywhere carries an empty-string EntityRef (the
	// pre-migration failure mode this test guards against): the malformed
	// fact must be excluded entirely, never surfaced as an unresolved
	// "" -keyed correlation.
	if _, ok := decisions[""]; ok {
		t.Fatalf("decisions contain an empty-string EntityRef key: %#v; the malformed fact must be quarantined, never surfaced under an empty catalog identity", decisions)
	}

	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle() status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
}

// TestPartitionServiceCatalogFactsQuarantinesMalformedEntity exercises the
// partition helper directly (the unit-level counterpart to the handler-level
// test above), asserting the exact quarantine record shape: field name and
// classification.
func TestPartitionServiceCatalogFactsQuarantinesMalformedEntity(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-entity-missing-entity-ref",
		FactKind: facts.ServiceCatalogEntityFactKind,
		Payload: map[string]any{
			"provider": "backstage",
		},
	}
	valid := serviceCatalogEntityFact("valid-entity", "component:default/checkout", "Checkout")

	_, quarantined, fatal := buildServiceCatalogCorrelationIndexWithQuarantine([]facts.Envelope{malformed, valid})
	if fatal != nil {
		t.Fatalf("fatal = %v, want nil; a missing required field is a quarantinable input_invalid, not a fatal error", fatal)
	}

	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-entity_ref entity fact must be quarantined via partitionDecodeFailures: %#v", len(quarantined), quarantined)
	}
	if quarantined[0].field != "entity_ref" {
		t.Fatalf("quarantined[0].field = %q, want %q", quarantined[0].field, "entity_ref")
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined[0].classification = %q, want %q", quarantined[0].classification, "input_invalid")
	}
	if quarantined[0].factID != "malformed-entity-missing-entity-ref" {
		t.Fatalf("quarantined[0].factID = %q, want %q", quarantined[0].factID, "malformed-entity-missing-entity-ref")
	}
}

// TestPartitionServiceCatalogFactsQuarantinesMalformedOwnershipAndLink extends
// the same proof to the ownership and repository_link kinds: each is decoded
// through its own contracts seam, and a missing entity_ref on either
// dead-letters independently while a valid sibling of the SAME kind still
// projects.
func TestPartitionServiceCatalogFactsQuarantinesMalformedOwnershipAndLink(t *testing.T) {
	t.Parallel()

	malformedOwnership := facts.Envelope{
		FactID:   "malformed-ownership",
		FactKind: facts.ServiceCatalogOwnershipFactKind,
		Payload: map[string]any{
			// "entity_ref" intentionally absent.
			"owner_ref": "group:default/payments",
		},
	}
	malformedLink := facts.Envelope{
		FactID:   "malformed-link",
		FactKind: facts.ServiceCatalogRepositoryLinkFactKind,
		Payload: map[string]any{
			// "entity_ref" intentionally absent.
			"repository_id": "repo-checkout",
		},
	}
	validOwnership := serviceCatalogOwnershipFact("valid-ownership", "component:default/checkout", "group:default/payments")
	validLink := serviceCatalogRepositoryLinkFact("valid-link", "component:default/checkout", "https://github.com/acme/checkout.git")

	index, quarantined, fatal := buildServiceCatalogCorrelationIndexWithQuarantine([]facts.Envelope{
		malformedOwnership, malformedLink, validOwnership, validLink,
	})
	if fatal != nil {
		t.Fatalf("fatal = %v, want nil", fatal)
	}

	if len(quarantined) != 2 {
		t.Fatalf("len(quarantined) = %d, want 2: %#v", len(quarantined), quarantined)
	}
	for _, q := range quarantined {
		if q.field != "entity_ref" {
			t.Fatalf("quarantined field = %q, want entity_ref: %#v", q.field, q)
		}
	}

	key := serviceCatalogEntityKey{provider: "backstage", entityRef: "component:default/checkout"}
	if _, ok := index.ownership[key]; !ok {
		t.Fatal("valid ownership fact was not indexed despite a malformed ownership fact sharing the batch")
	}
	if links := index.repoLinks[key]; len(links) != 1 {
		t.Fatalf("indexed repo links = %d, want 1 (only the valid link)", len(links))
	}
}

// TestBuildServiceCatalogCorrelationIndexDoesNotIndexPresentButEmptyEntityRef
// locks the absent-vs-present-empty distinction the typed decode must preserve
// exactly (Contract System v1 Wave 4f S3, issue #4755). Before the migration
// the index guarded every kind with `if entityRef != ""`, so BOTH an absent and
// a present-but-empty entity_ref were dropped without a signal. The typed decode
// splits those: an ABSENT entity_ref now dead-letters as input_invalid (the
// accuracy win), but a present-but-EMPTY entity_ref is a valid decode that must
// still be dropped, NOT keyed under an empty-string identity — otherwise an
// empty-ref entity, ownership, or repository_link would create a spurious
// ""-keyed correlation decision that never existed before. This test proves the
// present-but-empty facts are neither indexed nor quarantined, exactly matching
// the pre-migration drop.
func TestBuildServiceCatalogCorrelationIndexDoesNotIndexPresentButEmptyEntityRef(t *testing.T) {
	t.Parallel()

	emptyRefEntity := facts.Envelope{
		FactID:   "empty-ref-entity",
		FactKind: facts.ServiceCatalogEntityFactKind,
		Payload: map[string]any{
			"provider":   "backstage",
			"entity_ref": "", // present but empty: a valid decode, but no usable identity
		},
	}
	emptyRefOwnership := facts.Envelope{
		FactID:   "empty-ref-ownership",
		FactKind: facts.ServiceCatalogOwnershipFactKind,
		Payload: map[string]any{
			"provider":   "backstage",
			"entity_ref": "",
			"owner_ref":  "group:default/payments",
		},
	}
	emptyRefLink := facts.Envelope{
		FactID:   "empty-ref-link",
		FactKind: facts.ServiceCatalogRepositoryLinkFactKind,
		Payload: map[string]any{
			"provider":      "backstage",
			"entity_ref":    "",
			"repository_id": "repo-checkout",
		},
	}

	index, quarantined, fatal := buildServiceCatalogCorrelationIndexWithQuarantine([]facts.Envelope{
		emptyRefEntity, emptyRefOwnership, emptyRefLink,
	})
	if fatal != nil {
		t.Fatalf("fatal = %v, want nil", fatal)
	}

	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a present-but-empty entity_ref is a valid decode, not an input_invalid dead-letter: %#v", len(quarantined), quarantined)
	}
	if len(index.entities) != 0 {
		t.Fatalf("index.entities = %d, want 0; a present-but-empty entity_ref must not be keyed under an empty-string identity: %#v", len(index.entities), index.entities)
	}
	if len(index.ownership) != 0 {
		t.Fatalf("index.ownership = %d, want 0; an empty-entity_ref ownership must not be indexed", len(index.ownership))
	}
	if len(index.repoLinks) != 0 {
		t.Fatalf("index.repoLinks = %d, want 0; an empty-entity_ref link must not be indexed", len(index.repoLinks))
	}
	if _, ok := index.entities[serviceCatalogEntityKey{provider: "backstage", entityRef: ""}]; ok {
		t.Fatal("index keyed an entity under an empty entity_ref; the pre-migration drop behavior was not preserved")
	}
}

// TestBuildServiceCatalogCorrelationIndexPropagatesUnsupportedMajorAsFatal is
// the regression for the codex review finding on #4757: a service_catalog fact
// carrying an unsupported schema major (for example "2.0.0") is version skew,
// NOT a per-fact input_invalid. service_catalog is registered and
// schema-version-admitted, so unlike the unregistered codegraph file/repository
// kinds this is a reachable class. partitionDecodeFailures returns it as the
// fatal third result, and buildServiceCatalogCorrelationIndexWithQuarantine must
// return it as fatalErr (never swallow it into a quarantine) so the handler
// fails the whole work item for retry once the reducer supports the new major,
// rather than publishing version-skewed service-catalog truth with the fact
// silently omitted.
func TestBuildServiceCatalogCorrelationIndexPropagatesUnsupportedMajorAsFatal(t *testing.T) {
	t.Parallel()

	// A valid sibling that WOULD project, to prove the whole build aborts on
	// the fatal fact rather than partially projecting around it.
	valid := serviceCatalogEntityFact("valid-entity", "component:default/checkout", "Checkout")
	unsupportedMajor := facts.Envelope{
		FactID:        "entity-unsupported-major",
		FactKind:      facts.ServiceCatalogEntityFactKind,
		SchemaVersion: "2.0.0", // a real major-2 version — NOT the version-less "0.0.0" sentinel
		Payload: map[string]any{
			"provider":   "backstage",
			"entity_ref": "component:default/future",
		},
	}

	index, quarantined, fatal := buildServiceCatalogCorrelationIndexWithQuarantine(
		[]facts.Envelope{valid, unsupportedMajor},
	)
	if fatal == nil {
		t.Fatalf("fatal = nil, want non-nil; an unsupported schema major (2.0.0) on a registered service_catalog fact must fail the whole intent, not be quarantined")
	}
	if !errors.Is(fatal, factschema.ErrUnsupportedSchemaMajor) {
		t.Fatalf("fatal = %v, want errors.Is factschema.ErrUnsupportedSchemaMajor", fatal)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want none; an unsupported major is fatal, never a per-fact quarantine", quarantined)
	}
	if len(index.entities) != 0 {
		t.Fatalf("index.entities = %d, want 0; on a fatal error the partial index must be discarded, not partially built", len(index.entities))
	}
}

// TestServiceCatalogCorrelationHandleFailsIntentOnUnsupportedMajor is the
// handler-level counterpart: Handle must return a non-nil error (failing the
// work item) when a service_catalog fact carries an unsupported schema major,
// so the durable queue triages it for retry rather than committing incomplete
// correlations.
func TestServiceCatalogCorrelationHandleFailsIntentOnUnsupportedMajor(t *testing.T) {
	t.Parallel()

	loader := &stubServiceCatalogCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			serviceCatalogEntityFact("valid-entity", "component:default/checkout", "Checkout"),
			{
				FactID:        "entity-unsupported-major",
				FactKind:      facts.ServiceCatalogEntityFactKind,
				SchemaVersion: "2.0.0",
				Payload: map[string]any{
					"provider":   "backstage",
					"entity_ref": "component:default/future",
				},
			},
		},
	}
	writer := &recordingServiceCatalogCorrelationWriter{}
	handler := ServiceCatalogCorrelationHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-unsupported-major",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		Domain:       DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
		Cause:        "service catalog facts observed",
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil; an unsupported schema major must fail the intent")
	}
	if !errors.Is(err, factschema.ErrUnsupportedSchemaMajor) {
		t.Fatalf("Handle() error = %v, want errors.Is factschema.ErrUnsupportedSchemaMajor", err)
	}
	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0; no correlations may be written when a fatal decode error aborts the intent", writer.calls)
	}
}

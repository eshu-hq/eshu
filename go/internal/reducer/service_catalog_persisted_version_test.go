// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDecodeServiceCatalogAcceptsPersistedVersionlessSchemaVersion is the S3
// counterpart to S1's codegraph corpus-gate regression test (PR #4753 /
// issue #4749): it locks in that a service_catalog fact loaded from Postgres
// carrying the persisted version-less sentinel ("0.0.0",
// go/internal/storage/postgres/facts.go emptyToDefault) still decodes as the
// latest major, not dead-letters.
//
// service_catalog.* carries a REAL SchemaVersion ("1.0.0") end to end today —
// the collector always stamps it and facts.ValidateSchemaVersion admits only
// that value — so this scenario should never occur on the real ingestion
// path. This test exists as a defensive parity check: factschemaEnvelope's
// "0.0.0" normalization (added for the version-less codegraph family) applies
// unconditionally to every fact kind that decodes through it, service_catalog
// included, so a version-less service_catalog fact — however it might arrive
// (a future schema-version regression, a hand-built test fixture, a replay of
// an old queue row) — must still decode rather than silently dead-letter the
// whole correlation batch.
func TestDecodeServiceCatalogAcceptsPersistedVersionlessSchemaVersion(t *testing.T) {
	t.Parallel()

	persistedEntity := facts.Envelope{
		FactID:        "persisted-version-entity",
		FactKind:      facts.ServiceCatalogEntityFactKind,
		SchemaVersion: "0.0.0", // what the Postgres load path returns for a version-less fact
		Payload: map[string]any{
			"provider":   "backstage",
			"entity_ref": "component:default/checkout",
		},
	}
	entity, err := decodeServiceCatalogEntity(persistedEntity)
	if err != nil {
		t.Fatalf("decodeServiceCatalogEntity(SchemaVersion=0.0.0) error = %v, want nil; the persisted version-less sentinel must decode as the latest major, not dead-letter", err)
	}
	if entity.EntityRef != "component:default/checkout" {
		t.Fatalf("decodeServiceCatalogEntity EntityRef = %q, want component:default/checkout", entity.EntityRef)
	}

	persistedOwnership := facts.Envelope{
		FactID:        "persisted-version-ownership",
		FactKind:      facts.ServiceCatalogOwnershipFactKind,
		SchemaVersion: "0.0.0",
		Payload: map[string]any{
			"entity_ref": "component:default/checkout",
			"owner_ref":  "group:default/payments",
		},
	}
	if ownership, err := decodeServiceCatalogOwnership(persistedOwnership); err != nil {
		t.Fatalf("decodeServiceCatalogOwnership(SchemaVersion=0.0.0) error = %v, want nil", err)
	} else if ownership.EntityRef != "component:default/checkout" {
		t.Fatalf("decodeServiceCatalogOwnership EntityRef = %q, want component:default/checkout", ownership.EntityRef)
	}

	persistedLink := facts.Envelope{
		FactID:        "persisted-version-link",
		FactKind:      facts.ServiceCatalogRepositoryLinkFactKind,
		SchemaVersion: "0.0.0",
		Payload: map[string]any{
			"entity_ref":     "component:default/checkout",
			"repository_id":  "repo-checkout",
			"normalized_url": "https://github.com/acme/checkout.git",
		},
	}
	if link, err := decodeServiceCatalogRepositoryLink(persistedLink); err != nil {
		t.Fatalf("decodeServiceCatalogRepositoryLink(SchemaVersion=0.0.0) error = %v, want nil", err)
	} else if link.EntityRef != "component:default/checkout" {
		t.Fatalf("decodeServiceCatalogRepositoryLink EntityRef = %q, want component:default/checkout", link.EntityRef)
	}
}

// TestBuildServiceCatalogCorrelationIndexAcceptsPersistedVersionlessRepository
// proves the correlation index itself — not just the standalone decode
// wrapper — still resolves a repo-local descriptor scope when the "repository"
// fact carries the persisted version-less sentinel, exercising the reused
// decodeCodegraphRepository seam (Wave 4f S1) through this family's own
// index-build path (issue #4755).
func TestBuildServiceCatalogCorrelationIndexAcceptsPersistedVersionlessRepository(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		serviceCatalogEntityFactWithScope(
			"entity-local",
			"git-repository-scope:repo-checkout",
			"component:default/checkout",
			"Checkout",
		),
		{
			FactID:        "repo-checkout",
			FactKind:      factKindRepository,
			SchemaVersion: "0.0.0", // the persisted version-less sentinel a real loaded fact carries
			Payload: map[string]any{
				"repo_id": "repo-checkout",
				"name":    "checkout",
			},
		},
	}

	index, quarantined, fatal := buildServiceCatalogCorrelationIndexWithQuarantine(envelopes)
	if fatal != nil {
		t.Fatalf("fatal = %v, want nil; a persisted-version-sentinel repository fact must decode, not fatally fail", fatal)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want none; a persisted-version-sentinel repository fact must decode, not dead-letter", quarantined)
	}
	if len(index.repositories) != 1 || index.repositories[0].repositoryID != "repo-checkout" {
		t.Fatalf("index.repositories = %#v, want one repo-checkout entry", index.repositories)
	}
}

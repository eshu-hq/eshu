// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// catalogEntity is the provider-agnostic normalized form one manifest document
// resolves to. Provider parsers (Backstage, and later OpsLevel/Cortex) build
// this shape, and the shared builders below turn it into fact envelopes.
type catalogEntity struct {
	provider      Provider
	entityRef     string
	entityType    string
	displayName   string
	lifecycle     string
	tier          string
	ownerRef      string
	repositoryURL string
	// repositoryName is a name-only locator with no resolvable URL. The reducer
	// rejects it because a bare name cannot prove repository ownership.
	repositoryName string
	dependencies   []string
}

type operationalLink struct {
	linkType string
	title    string
	url      string
}

// entityEnvelope emits one service_catalog.entity fact. service_id and
// workload_id are deliberately absent: the collector observes a YAML file and
// has no canonical service or workload identity to assert.
func entityEnvelope(ctx FixtureContext, entity catalogEntity) facts.Envelope {
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(entity.provider),
		"entity_ref":            entity.entityRef,
		"entity_type":           entity.entityType,
		"display_name":          entity.displayName,
		"lifecycle":             entity.lifecycle,
		"tier":                  entity.tier,
	}
	stableKey := facts.StableID(facts.ServiceCatalogEntityFactKind, map[string]any{
		"entity_ref": entity.entityRef,
		"provider":   string(entity.provider),
	})
	return newEnvelope(ctx, facts.ServiceCatalogEntityFactKind, stableKey, entity.entityRef, payload)
}

// ownershipEnvelope emits one service_catalog.ownership fact recording the
// declared owner as provenance. It never creates canonical service identity.
func ownershipEnvelope(ctx FixtureContext, entity catalogEntity) facts.Envelope {
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(entity.provider),
		"entity_ref":            entity.entityRef,
		"owner_ref":             entity.ownerRef,
	}
	stableKey := facts.StableID(facts.ServiceCatalogOwnershipFactKind, map[string]any{
		"entity_ref": entity.entityRef,
		"owner_ref":  entity.ownerRef,
		"provider":   string(entity.provider),
	})
	return newEnvelope(ctx, facts.ServiceCatalogOwnershipFactKind, stableKey, entity.entityRef, payload)
}

// repositoryLinkEnvelope emits one service_catalog.repository_link fact. The
// producer emits only the manifest's declared URL/name; it never fabricates a
// repository_id, which would force a false exact correlation. The declared URL
// is emitted verbatim into repository_url and the reducer applies its own
// git-URL canonicalization, which preserves the exact-vs-derived distinction.
func repositoryLinkEnvelope(ctx FixtureContext, entity catalogEntity) facts.Envelope {
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(entity.provider),
		"entity_ref":            entity.entityRef,
	}
	if entity.repositoryURL != "" {
		payload["repository_url"] = entity.repositoryURL
	}
	if entity.repositoryName != "" {
		payload["repository_name"] = entity.repositoryName
	}
	stableKey := facts.StableID(facts.ServiceCatalogRepositoryLinkFactKind, map[string]any{
		"entity_ref":      entity.entityRef,
		"provider":        string(entity.provider),
		"repository_name": entity.repositoryName,
		"repository_url":  entity.repositoryURL,
	})
	return newEnvelope(ctx, facts.ServiceCatalogRepositoryLinkFactKind, stableKey, entity.entityRef, payload)
}

// dependencyEnvelope emits one service_catalog.dependency fact. The reducer
// index does not consume dependency facts yet, so these are carried for
// read-surface completeness and forward compatibility only.
func dependencyEnvelope(ctx FixtureContext, entity catalogEntity, target string) facts.Envelope {
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(entity.provider),
		"entity_ref":            entity.entityRef,
		"depends_on_ref":        target,
	}
	stableKey := facts.StableID(facts.ServiceCatalogDependencyFactKind, map[string]any{
		"depends_on_ref": target,
		"entity_ref":     entity.entityRef,
		"provider":       string(entity.provider),
	})
	return newEnvelope(ctx, facts.ServiceCatalogDependencyFactKind, stableKey, entity.entityRef+"->"+target, payload)
}

// operationalLinkEnvelope emits one service_catalog.operational_link fact for a
// link that already passed the URL safety check.
func operationalLinkEnvelope(ctx FixtureContext, entity catalogEntity, link operationalLink) facts.Envelope {
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(entity.provider),
		"entity_ref":            entity.entityRef,
		"link_type":             link.linkType,
		"title":                 link.title,
		"url":                   link.url,
	}
	stableKey := facts.StableID(facts.ServiceCatalogOperationalLinkFactKind, map[string]any{
		"entity_ref": entity.entityRef,
		"provider":   string(entity.provider),
		"url":        link.url,
	})
	return newEnvelope(ctx, facts.ServiceCatalogOperationalLinkFactKind, stableKey, entity.entityRef, payload)
}

// warningEnvelope emits one service_catalog.warning fact so degraded manifest
// shapes are observable instead of silently dropped.
func warningEnvelope(ctx FixtureContext, provider Provider, entityRef, reason, message string) facts.Envelope {
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(provider),
		"reason":                trim(reason),
		"message":               redactSensitiveText(trim(message)),
		"partial_generation":    true,
	}
	if entityRef != "" {
		payload["entity_ref"] = entityRef
	}
	stableKey := facts.StableID(facts.ServiceCatalogWarningFactKind, map[string]any{
		"entity_ref": entityRef,
		"provider":   string(provider),
		"reason":     trim(reason),
	})
	return newEnvelope(ctx, facts.ServiceCatalogWarningFactKind, stableKey, entityRef+":"+trim(reason), payload)
}

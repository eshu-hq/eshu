// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BackstageManifestEnvelopes normalizes one offline Backstage catalog manifest
// (catalog-info.yaml) into observed-confidence service-catalog facts.
//
// The manifest may contain multiple `---`-separated documents. Each document is
// parsed independently: a malformed or degraded document emits a
// service_catalog.warning fact and the loop continues, so one bad document never
// aborts the whole file. The producer never fabricates repository, service, or
// workload identity; it only emits what the manifest declares.
func BackstageManifestEnvelopes(raw []byte, ctx FixtureContext) ([]facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}

	envelopes := make([]facts.Envelope, 0)
	seenEntities := make(map[string]bool)
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	docIndex := 0
	for {
		var entity backstageEntity
		err := decoder.Decode(&entity)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A single unparseable document degrades to a warning; the rest of
			// the stream is still processed.
			envelopes = append(envelopes, warningEnvelope(ctx, ProviderBackstage, "",
				"invalid_document", fmt.Sprintf("backstage document %d failed to parse: %v", docIndex, err)))
			docIndex++
			continue
		}
		docIndex++
		envelopes = append(envelopes, backstageDocumentEnvelopes(ctx, entity, seenEntities)...)
	}
	return deduplicateEnvelopes(envelopes), nil
}

// backstageDocumentEnvelopes turns one parsed Backstage document into facts.
func backstageDocumentEnvelopes(ctx FixtureContext, entity backstageEntity, seenEntities map[string]bool) []facts.Envelope {
	ref := entity.entityRef()
	if ref == "" {
		// No name means no anchor; record the gap instead of dropping silently.
		return []facts.Envelope{warningEnvelope(ctx, ProviderBackstage, "",
			"invalid_ref", "backstage document omitted metadata.name; cannot anchor an entity reference")}
	}
	if seenEntities[ref] {
		// First-wins: keep the deterministic first entity, warn on the second.
		return []facts.Envelope{warningEnvelope(ctx, ProviderBackstage, ref,
			"duplicate_entity", "backstage manifest declared entity "+ref+" more than once; keeping the first")}
	}
	seenEntities[ref] = true

	normalized := catalogEntity{
		provider:       ProviderBackstage,
		entityRef:      ref,
		entityType:     trim(entity.Spec.Type),
		displayName:    trim(entity.Metadata.Title),
		lifecycle:      trim(entity.Spec.Lifecycle),
		tier:           entity.tier(),
		ownerRef:       entity.ownerRef(),
		repositoryURL:  entity.repositoryURL(),
		repositoryName: entity.repositoryName(),
		dependencies:   entity.dependencies(),
	}

	var out []facts.Envelope
	if !supportedBackstageAPIVersions[trim(entity.APIVersion)] {
		// Unsupported version is still minimally parseable: emit the entity and
		// a warning rather than a silent drop.
		out = append(out, warningEnvelope(ctx, ProviderBackstage, ref,
			"unsupported_descriptor_version", "backstage apiVersion "+trim(entity.APIVersion)+" is not fully supported"))
	}

	out = append(out, entityEnvelope(ctx, normalized))
	if normalized.ownerRef != "" {
		out = append(out, ownershipEnvelope(ctx, normalized))
	}
	if normalized.repositoryURL != "" || normalized.repositoryName != "" {
		out = append(out, repositoryLinkEnvelope(ctx, normalized))
	}
	for _, dep := range normalized.dependencies {
		out = append(out, dependencyEnvelope(ctx, normalized, dep))
	}
	out = append(out, backstageOperationalLinkEnvelopes(ctx, normalized, entity)...)
	return out
}

// backstageOperationalLinkEnvelopes emits operational-link facts for safe links
// and a redaction warning for any link carrying credentials or a query string.
func backstageOperationalLinkEnvelopes(ctx FixtureContext, normalized catalogEntity, entity backstageEntity) []facts.Envelope {
	var out []facts.Envelope
	for _, link := range entity.Metadata.Links {
		rawURL := trim(link.URL)
		if rawURL == "" {
			continue
		}
		if !isSafeURL(rawURL) {
			out = append(out, warningEnvelope(ctx, ProviderBackstage, normalized.entityRef,
				"operational_link_redacted", "operational link for "+normalized.entityRef+" omitted because it carried credentials or a query string"))
			continue
		}
		out = append(out, operationalLinkEnvelope(ctx, normalized, operationalLink{
			linkType: trim(link.Type),
			title:    trim(link.Title),
			url:      rawURL,
		}))
	}
	return out
}

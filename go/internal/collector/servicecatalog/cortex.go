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

// CortexManifestEnvelopes normalizes one offline Cortex entity descriptor
// (cortex.yaml) into observed-confidence service-catalog facts.
//
// The descriptor may contain multiple `---`-separated OpenAPI documents, each
// declaring one Cortex entity via `info.x-cortex-*` extensions. Each document is
// parsed independently: a malformed or degraded document emits a
// service_catalog.warning fact and the loop continues, so one bad document never
// aborts the whole file. The producer never fabricates repository, service, or
// workload identity; Cortex references repositories by a known git provider plus
// a name slug, which the producer expands into a derivable URL only for known
// public providers and otherwise emits as a name-only locator the reducer
// rejects.
func CortexManifestEnvelopes(raw []byte, ctx FixtureContext) ([]facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}

	envelopes := make([]facts.Envelope, 0)
	seenEntities := make(map[string]bool)
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	docIndex := 0
	for {
		var descriptor cortexDescriptor
		err := decoder.Decode(&descriptor)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A single unparseable document degrades to a warning; the rest of
			// the stream is still processed.
			envelopes = append(envelopes, warningEnvelope(ctx, ProviderCortex, "",
				"invalid_document", fmt.Sprintf("cortex document %d failed to parse: %v", docIndex, err)))
			docIndex++
			continue
		}
		docIndex++
		envelopes = append(envelopes, cortexDocumentEnvelopes(ctx, descriptor, seenEntities)...)
	}
	return deduplicateEnvelopes(envelopes), nil
}

// cortexDocumentEnvelopes turns one parsed Cortex descriptor into facts.
func cortexDocumentEnvelopes(ctx FixtureContext, descriptor cortexDescriptor, seenEntities map[string]bool) []facts.Envelope {
	ref := descriptor.Info.entityRef()
	if ref == "" {
		// No x-cortex-tag means no anchor; record the gap instead of dropping.
		return []facts.Envelope{warningEnvelope(ctx, ProviderCortex, "",
			"invalid_ref", "cortex document omitted info.x-cortex-tag; cannot anchor an entity reference")}
	}
	if seenEntities[ref] {
		// First-wins: keep the deterministic first entity, warn on the second.
		return []facts.Envelope{warningEnvelope(ctx, ProviderCortex, ref,
			"duplicate_entity", "cortex manifest declared entity "+ref+" more than once; keeping the first")}
	}
	seenEntities[ref] = true

	repoURL, repoName := descriptor.Info.Git.repositoryLocator()
	normalized := catalogEntity{
		provider:       ProviderCortex,
		entityRef:      ref,
		entityType:     descriptor.Info.entityType(),
		displayName:    trim(descriptor.Info.Title),
		tier:           descriptor.Info.tier(),
		ownerRef:       descriptor.Info.ownerRef(),
		repositoryURL:  repoURL,
		repositoryName: repoName,
		dependencies:   descriptor.Info.dependencies(),
	}

	var out []facts.Envelope
	if !supportedCortexOpenAPIVersions[descriptor.version()] {
		// Unsupported version is still minimally parseable: emit the entity and a
		// warning rather than a silent drop.
		out = append(out, warningEnvelope(ctx, ProviderCortex, ref,
			"unsupported_descriptor_version", "cortex openapi version "+descriptor.version()+" is not fully supported"))
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
	out = append(out, cortexOperationalLinkEnvelopes(ctx, normalized, descriptor)...)
	return out
}

// cortexOperationalLinkEnvelopes emits operational-link facts for safe Cortex
// links and a redaction warning for any link carrying credentials or a query
// string.
func cortexOperationalLinkEnvelopes(ctx FixtureContext, normalized catalogEntity, descriptor cortexDescriptor) []facts.Envelope {
	var out []facts.Envelope
	for _, link := range descriptor.Info.Links {
		rawURL := trim(link.URL)
		if rawURL == "" {
			continue
		}
		if !isSafeURL(rawURL) {
			out = append(out, warningEnvelope(ctx, ProviderCortex, normalized.entityRef,
				"operational_link_redacted", "operational link for "+normalized.entityRef+" omitted because it carried credentials or a query string"))
			continue
		}
		out = append(out, operationalLinkEnvelope(ctx, normalized, operationalLink{
			linkType: trim(link.Type),
			title:    trim(link.Name),
			url:      rawURL,
		}))
	}
	return out
}

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

// OpsLevelManifestEnvelopes normalizes one offline OpsLevel manifest
// (opslevel.yml) into observed-confidence service-catalog facts.
//
// The manifest may contain multiple `---`-separated documents, each declaring
// one component (or the deprecated service block). Each document is parsed
// independently: a malformed or degraded document emits a
// service_catalog.warning fact and the loop continues, so one bad document never
// aborts the whole file. The producer never fabricates repository, service, or
// workload identity; OpsLevel references repositories by provider plus a name
// slug, which the producer expands into a derivable URL only for known public
// providers and otherwise emits as a name-only locator the reducer rejects.
func OpsLevelManifestEnvelopes(raw []byte, ctx FixtureContext) ([]facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}

	envelopes := make([]facts.Envelope, 0)
	seenEntities := make(map[string]bool)
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	docIndex := 0
	for {
		var doc opslevelDocument
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A single unparseable document degrades to a warning; the rest of
			// the stream is still processed.
			envelopes = append(envelopes, warningEnvelope(ctx, ProviderOpsLevel, "",
				"invalid_document", fmt.Sprintf("opslevel document %d failed to parse: %v", docIndex, err)))
			docIndex++
			continue
		}
		docIndex++
		envelopes = append(envelopes, opslevelDocumentEnvelopes(ctx, doc, seenEntities)...)
	}
	return deduplicateEnvelopes(envelopes), nil
}

// opslevelDocumentEnvelopes turns one parsed OpsLevel document into facts.
func opslevelDocumentEnvelopes(ctx FixtureContext, doc opslevelDocument, seenEntities map[string]bool) []facts.Envelope {
	component := doc.component()
	if component == nil {
		// A version-only or empty document carries no entity; record the gap.
		return []facts.Envelope{warningEnvelope(ctx, ProviderOpsLevel, "",
			"invalid_ref", "opslevel document declared no component or service block")}
	}

	ref := component.entityRef()
	if ref == "" {
		// No name or alias means no anchor; record the gap instead of dropping.
		return []facts.Envelope{warningEnvelope(ctx, ProviderOpsLevel, "",
			"invalid_ref", "opslevel component omitted name and aliases; cannot anchor an entity reference")}
	}
	if seenEntities[ref] {
		// First-wins: keep the deterministic first entity, warn on the second.
		return []facts.Envelope{warningEnvelope(ctx, ProviderOpsLevel, ref,
			"duplicate_entity", "opslevel manifest declared entity "+ref+" more than once; keeping the first")}
	}
	seenEntities[ref] = true

	repoURL, repoName := component.repositoryLocator()
	normalized := catalogEntity{
		provider:       ProviderOpsLevel,
		entityRef:      ref,
		entityType:     component.entityType(),
		displayName:    trim(component.Name),
		lifecycle:      trim(component.Lifecycle),
		tier:           trim(component.Tier),
		ownerRef:       component.ownerRef(),
		repositoryURL:  repoURL,
		repositoryName: repoName,
		dependencies:   component.dependencies(),
	}

	var out []facts.Envelope
	if !supportedOpsLevelVersions[doc.version()] {
		// Unsupported version is still minimally parseable: emit the entity and a
		// warning rather than a silent drop.
		out = append(out, warningEnvelope(ctx, ProviderOpsLevel, ref,
			"unsupported_descriptor_version", "opslevel version "+doc.version()+" is not fully supported"))
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
	out = append(out, opslevelOperationalLinkEnvelopes(ctx, normalized, component)...)
	return out
}

// opslevelOperationalLinkEnvelopes emits operational-link facts for safe OpsLevel
// tools and a redaction warning for any tool URL carrying credentials or a query
// string.
func opslevelOperationalLinkEnvelopes(ctx FixtureContext, normalized catalogEntity, component *opslevelComponent) []facts.Envelope {
	var out []facts.Envelope
	for _, tool := range component.Tools {
		rawURL := trim(tool.URL)
		if rawURL == "" {
			continue
		}
		if !isSafeURL(rawURL) {
			out = append(out, warningEnvelope(ctx, ProviderOpsLevel, normalized.entityRef,
				"operational_link_redacted", "operational link for "+normalized.entityRef+" omitted because it carried credentials or a query string"))
			continue
		}
		title := trim(tool.Name)
		if title == "" {
			title = trim(tool.DisplayName)
		}
		out = append(out, operationalLinkEnvelope(ctx, normalized, operationalLink{
			linkType: trim(tool.Category),
			title:    title,
			url:      rawURL,
		}))
	}
	return out
}

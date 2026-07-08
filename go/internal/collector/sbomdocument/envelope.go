// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomdocument

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

func newEnvelope(ctx FixtureContext, factKind, stableKey, sourceRecordID string, payload map[string]any) facts.Envelope {
	schemaVersion, _ := facts.SBOMAttestationSchemaVersion(factKind)
	return facts.Envelope{
		FactID:           sbomDocumentFactID(factKind, stableKey, ctx.ScopeID, ctx.GenerationID),
		ScopeID:          ctx.ScopeID,
		GenerationID:     ctx.GenerationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    CollectorKind,
		FencingToken:     ctx.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       normalizedObservedAt(ctx.ObservedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        ctx.ScopeID,
			GenerationID:   ctx.GenerationID,
			FactKey:        stableKey,
			SourceURI:      strings.TrimSpace(ctx.SourceURI),
			SourceRecordID: sourceRecordID,
		},
	}
}

func sbomDocumentFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("SBOMDocumentFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func normalizedObservedAt(observedAt time.Time) time.Time {
	if observedAt.IsZero() {
		return time.Now().UTC()
	}
	return observedAt.UTC()
}

func validateContext(ctx FixtureContext) error {
	if strings.TrimSpace(ctx.ScopeID) == "" {
		return fmt.Errorf("sbom document fixture scope_id must not be blank")
	}
	if strings.TrimSpace(ctx.GenerationID) == "" {
		return fmt.Errorf("sbom document fixture generation_id must not be blank")
	}
	if strings.TrimSpace(ctx.CollectorInstanceID) == "" {
		return fmt.Errorf("sbom document fixture collector_instance_id must not be blank")
	}
	return nil
}

func documentDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func documentID(format Format, sourceRecordID, documentDigest string) string {
	return facts.StableID("SBOMDocument", map[string]any{
		"document_digest":  documentDigest,
		"format":           string(format),
		"source_record_id": strings.TrimSpace(sourceRecordID),
	})
}

// canonicalPackageIDFromPURL derives the canonical package identity a component
// shares with vulnerability and package-registry facts. It returns "" when the
// purl is blank or cannot be normalized into a canonical identity, leaving the
// component to correlate by version-stripped purl instead of failing the
// document.
func canonicalPackageIDFromPURL(purl string) string {
	packageID, err := packageidentity.PackageIDFromPURL(purl)
	if err != nil {
		return ""
	}
	return packageID
}

func componentID(documentID, purl, name, version, identifier string) string {
	return facts.StableID("SBOMComponent", map[string]any{
		"document_id": documentID,
		"identifier":  strings.TrimSpace(identifier),
		"name":        strings.TrimSpace(name),
		"purl":        strings.TrimSpace(purl),
		"version":     strings.TrimSpace(version),
	})
}

func warningFact(ctx FixtureContext, documentID string, key string, reason WarningReason, summary string) facts.Envelope {
	payload := map[string]any{
		"document_id": documentID,
		"reason":      string(reason),
		"summary":     summary,
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeSBOMWarning(sbomv1.Warning{
			DocumentID: stringPtr(documentID),
			Reason:     stringPtr(string(reason)),
			Summary:    stringPtr(summary),
		})
	})
	stableKey := facts.StableID(facts.SBOMWarningFactKind, map[string]any{
		"document_id": documentID,
		"key":         key,
		"reason":      string(reason),
	})
	return newEnvelope(ctx, facts.SBOMWarningFactKind, stableKey, documentID+":"+key, payload)
}

func dependencyFact(ctx FixtureContext, documentID, from, to, relType, relKind string) facts.Envelope {
	payload := map[string]any{
		"document_id":         documentID,
		"from_component_id":   from,
		"to_component_id":     to,
		"relationship_type":   relType,
		"relationship_origin": relKind,
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeSBOMDependencyRelationship(sbomv1.DependencyRelationship{
			DocumentID:         documentID,
			FromComponentID:    stringPtr(from),
			ToComponentID:      stringPtr(to),
			RelationshipType:   stringPtr(relType),
			RelationshipOrigin: stringPtr(relKind),
		})
	})
	stableKey := facts.StableID(facts.SBOMDependencyRelationshipFactKind, map[string]any{
		"document_id":         documentID,
		"from_component_id":   from,
		"to_component_id":     to,
		"relationship_origin": relKind,
		"relationship_type":   relType,
	})
	return newEnvelope(ctx, facts.SBOMDependencyRelationshipFactKind, stableKey, from+"->"+to, payload)
}

func externalReferenceFact(ctx FixtureContext, documentID, componentID, refType, refURL, refLocator string) facts.Envelope {
	payload := map[string]any{
		"document_id":       documentID,
		"component_id":      componentID,
		"reference_type":    refType,
		"reference_url":     refURL,
		"reference_locator": refLocator,
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeSBOMExternalReference(sbomv1.ExternalReference{
			DocumentID:       documentID,
			ComponentID:      stringPtr(componentID),
			ReferenceType:    stringPtr(refType),
			ReferenceURL:     stringPtr(refURL),
			ReferenceLocator: stringPtr(refLocator),
		})
	})
	stableKey := facts.StableID(facts.SBOMExternalReferenceFactKind, map[string]any{
		"component_id":      componentID,
		"document_id":       documentID,
		"reference_locator": refLocator,
		"reference_type":    refType,
		"reference_url":     refURL,
	})
	recordID := componentID + ":" + refType + ":"
	if refLocator != "" {
		recordID += refLocator
	} else {
		recordID += refURL
	}
	return newEnvelope(ctx, facts.SBOMExternalReferenceFactKind, stableKey, recordID, payload)
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func sortHashEntries(hashes map[string]string) []map[string]string {
	keys := make([]string, 0, len(hashes))
	for k := range hashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]string{"algorithm": k, "value": hashes[k]})
	}
	return out
}

// sortLicenseEntries orders license maps deterministically so two byte-equal
// SBOMs whose producers reorder license arrays still emit identical fact
// bundles.
func sortLicenseEntries(entries []map[string]string) []map[string]string {
	sort.SliceStable(entries, func(i, j int) bool {
		return licenseSortKey(entries[i]) < licenseSortKey(entries[j])
	})
	return entries
}

func licenseSortKey(entry map[string]string) string {
	// Concatenate the projection fields in a fixed order. Each component is
	// suffixed with a NUL so values like "MIT" + "" can never collide with
	// "M" + "IT".
	return entry["id"] + "\x00" + entry["expression"] + "\x00" + entry["name"] + "\x00" + entry["url"]
}

// sortExternalRefEnvelopes orders external reference envelopes by their
// stable fact key so the document's fact bundle is byte-identical across
// producers that reorder externalReferences/externalRefs arrays.
func sortExternalRefEnvelopes(envelopes []facts.Envelope) []facts.Envelope {
	sort.SliceStable(envelopes, func(i, j int) bool {
		if envelopes[i].StableFactKey == envelopes[j].StableFactKey {
			return envelopes[i].FactID < envelopes[j].FactID
		}
		return envelopes[i].StableFactKey < envelopes[j].StableFactKey
	})
	return envelopes
}

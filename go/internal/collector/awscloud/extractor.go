// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This file is the awscloud half of the typed-depth extractor registry that
// issue #4591 asks this package to converge on, mirroring
// go/internal/collector/gcpcloud/extractor.go. The shape is deliberately the
// same so a contributor who has written a GCP extractor can write an AWS one
// without relearning the pattern: one resource type per file, registered from
// that file's init, dispatched through a map rather than a shared switch.
//
// The types are declared here rather than shared with gcpcloud on purpose.
// gcpcloud's ExtractContext is CAI-shaped (full resource name, asset type,
// project id) and its relationship endpoints are CAI full resource names; AWS
// identity is ARN-shaped and its scope is account plus region. Importing one
// collector package into the other to share a struct would couple two provider
// surfaces whose identity models differ, for the sake of three field names.
// Two small declarations that can drift independently are the cheaper trade.
//
// Nothing is registered yet. This lands the registry alone so the first scanner
// migration is a single-file change reviewed against fixture parity, per the
// one-scanner-per-PR acceptance on #4591.

// AttributeExtraction is the bounded, redaction-safe typed-depth output of a
// per-resource-type extractor. It carries:
//
//   - Attributes: a bounded map of type-specific control-plane fields usable for
//     Terraform import/drift, edges, correlation, or monitoring. It never holds
//     secrets, data-plane content, raw policy JSON, IPs, or response bodies.
//   - CorrelationAnchors: cross-source join keys (KMS key ARNs, parent resource
//     ARNs, and similar resource identifiers) for downstream correlation.
//   - Relationships: typed provider relationship observations whose source and
//     target are ARNs; reducers resolve and materialize them.
//
// An extractor that finds no usable typed depth returns the zero value; nil
// maps and slices are valid and mean "nothing to add".
type AttributeExtraction struct {
	Attributes         map[string]any
	CorrelationAnchors []string
	Relationships      []ResourceRelationshipObservation
}

// ResourceRelationshipObservation is one typed provider relationship between
// two AWS resources, named by ARN. It is an observation, not a resolved edge:
// the target may live in another account or region, or may not be indexed at
// all, and the reducer decides what to materialize.
type ResourceRelationshipObservation struct {
	// SourceARN is the ARN of the owning resource.
	SourceARN string
	// SourceResourceType is the resource type of the source.
	SourceResourceType string
	// RelationshipType is the bounded provider relationship type.
	RelationshipType string
	// TargetARN is the ARN of the related resource, preserved verbatim.
	TargetARN string
	// TargetResourceType is the resource type of the target when the provider
	// states it; empty when the ARN alone is what the provider gave us.
	TargetResourceType string
}

// ExtractContext is the bounded input handed to a per-resource-type extractor.
// Data is the raw provider describe/list JSON for one resource; it is never
// persisted, and the extractor must pull only redaction-safe fields from it.
// ARN, ResourceType, AccountID, and Region are the already-normalized resource
// identity the extractor uses to build typed relationship endpoints and anchors.
type ExtractContext struct {
	ARN          string
	ResourceType string
	AccountID    string
	Region       string
	Data         json.RawMessage
}

// ResourceAttributeExtractor extracts bounded typed depth from one provider
// resource payload for a single resource type. Each supported resource type
// registers its own extractor in its own file so parallel type additions never
// collide in a shared parser switch. An extractor must be deterministic and
// side-effect free, and it must never return secrets, data-plane content, or
// raw provider bodies.
type ResourceAttributeExtractor func(ctx ExtractContext) (AttributeExtraction, error)

// resourceExtractors is the per-resource-type extractor registry. It is
// populated only from package init functions (one per resource-type file), so
// it is effectively immutable after program initialization and needs no lock
// for lookup.
var resourceExtractors = map[string]ResourceAttributeExtractor{}

// RegisterResourceExtractor installs the extractor for one AWS resource type.
// It is intended to be called from a package init function in the resource
// type's own file. It panics on a blank resource type, a nil extractor, or a
// duplicate registration so a wiring mistake fails loudly at startup rather
// than silently dropping typed depth or shadowing another type's extractor.
func RegisterResourceExtractor(resourceType string, extractor ResourceAttributeExtractor) {
	trimmed := strings.TrimSpace(resourceType)
	if trimmed == "" {
		panic("awscloud: RegisterResourceExtractor requires a non-blank resource type")
	}
	if extractor == nil {
		panic(fmt.Sprintf("awscloud: RegisterResourceExtractor for %q requires a non-nil extractor", trimmed))
	}
	if _, exists := resourceExtractors[trimmed]; exists {
		panic(fmt.Sprintf("awscloud: duplicate extractor registration for resource type %q", trimmed))
	}
	resourceExtractors[trimmed] = extractor
}

// lookupResourceExtractor returns the registered extractor for a resource type.
func lookupResourceExtractor(resourceType string) (ResourceAttributeExtractor, bool) {
	extractor, ok := resourceExtractors[strings.TrimSpace(resourceType)]
	return extractor, ok
}

// HasResourceExtractor reports whether a typed-depth extractor is registered
// for a resource type. The collector runtime uses it to record
// extraction-outcome telemetry only for resource types that carry typed depth,
// rather than for every observed resource.
func HasResourceExtractor(resourceType string) bool {
	_, ok := lookupResourceExtractor(resourceType)
	return ok
}

// extractResourceAttributes dispatches to the registered extractor for the
// context resource type. It returns handled=false (with no error) when no
// extractor is registered, so the scanner keeps emitting the bounded base
// observation for resource types without typed depth — which is what lets the
// migration proceed one scanner at a time. A registered extractor's error is
// wrapped so the caller can attribute it to the resource type without leaking
// resource data.
func extractResourceAttributes(ctx ExtractContext) (AttributeExtraction, bool, error) {
	extractor, ok := lookupResourceExtractor(ctx.ResourceType)
	if !ok {
		return AttributeExtraction{}, false, nil
	}
	extraction, err := extractor(ctx)
	if err != nil {
		return AttributeExtraction{}, true, fmt.Errorf("extract %s attributes: %w", strings.TrimSpace(ctx.ResourceType), err)
	}
	return extraction, true, nil
}

// unregisterResourceExtractorForTest removes a registration so registry unit
// tests can register sentinel resource types without leaking state across test
// cases. It is test-only and must not be called from production code paths.
func unregisterResourceExtractorForTest(resourceType string) {
	delete(resourceExtractors, strings.TrimSpace(resourceType))
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import "time"

// CollectorKind is the durable collector family name for service-catalog facts.
// It is recorded in fact provenance, source references, and telemetry
// attributes, so it must stay stable once published.
const CollectorKind = "service_catalog"

// Provider identifies the service-catalog product a manifest was authored for.
type Provider string

const (
	// ProviderBackstage identifies Backstage catalog-info.yaml manifests.
	ProviderBackstage Provider = "backstage"
	// ProviderOpsLevel identifies OpsLevel opslevel.yml manifests.
	ProviderOpsLevel Provider = "opslevel"
	// ProviderCortex identifies Cortex cortex.yaml entity descriptors.
	ProviderCortex Provider = "cortex"
)

// ProviderCortexNamespace is the entity-ref namespace segment for Cortex
// entities. Cortex tags are globally unique within a Cortex instance but not
// across providers, so anchoring refs under a per-provider namespace keeps
// Cortex refs distinct from other providers' refs in the shared reducer entity
// key (which is keyed on provider plus entity_ref).
const ProviderCortexNamespace = "cortex"

// FixtureContext carries the collector boundary fields copied into every
// emitted service-catalog fact envelope. It mirrors the cicdrun fixture
// boundary: scope, generation, collector instance, fencing token, observed
// time, and a repo-relative manifest source URI.
type FixtureContext struct {
	// ScopeID is the durable ingestion scope the manifest belongs to.
	ScopeID string
	// GenerationID is the scope generation the facts are emitted under.
	GenerationID string
	// CollectorInstanceID identifies the producing collector instance.
	CollectorInstanceID string
	// FencingToken orders emissions from the same collector instance.
	FencingToken int64
	// ObservedAt is the manifest observation time; zero defaults to now (UTC).
	ObservedAt time.Time
	// SourceURI is the repo-relative manifest path. Token-bearing URLs are
	// stripped before emission.
	SourceURI string
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extractor

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

// ParentScopeKind and its bounded values live in the catalog leaf (#6696,
// slice L) so the root, every extractor file, and the runtime subpackages
// share one definition. The alias keeps the Boundary shape and every
// existing extractor reference compiling unchanged.
type ParentScopeKind = catalog.ParentScopeKind

// Boundary carries the durable scope-generation and claim identity shared by all
// facts emitted for one GCP collector claim.
//
// ParentScopeKind, ParentScopeID, and the asset/content family identify the
// bounded shard; ScopeID is the stable Eshu scope key; GenerationID and
// FencingToken fence the bounded scan so duplicate or stale deliveries converge
// or are rejected rather than corrupting current facts.
type Boundary struct {
	// CollectorInstanceID is the configured runtime instance that owns target
	// policy and credential environment.
	CollectorInstanceID string
	// ParentScopeKind is the bounded CAI parent scope kind for the shard.
	ParentScopeKind ParentScopeKind
	// ParentScopeID is the provider parent identifier (organization number,
	// folder number, or project id). It is source evidence, not a telemetry label.
	ParentScopeID string
	// AssetTypeFamily is the bounded asset family for the shard, for example
	// "compute" or "storage". It is derived from the CAI asset type service
	// segment and is safe as a telemetry label.
	AssetTypeFamily string
	// ContentFamily is the bounded CAI content family for the shard, for example
	// "resource" or "iam_policy".
	ContentFamily string
	// LocationBucket is the bounded location bucket for the shard, for example a
	// region or "global". It is safe as a telemetry label.
	LocationBucket string
	// ScopeID is the stable Eshu scope key for the parent scope and shard.
	ScopeID string
	// GenerationID is the collector- or coordinator-assigned id for one bounded
	// scan.
	GenerationID string
	// FencingToken fences the claim so stale generations cannot replace current
	// facts. It must be positive.
	FencingToken int64
	// ReadTime is the Cloud Asset Inventory snapshot or response read time.
	ReadTime time.Time
	// ObservedAt is the Eshu observation time. When zero the envelope builder
	// stamps the current UTC time.
	ObservedAt time.Time
}

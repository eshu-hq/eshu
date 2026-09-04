// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package crossplanesatisfiedby builds the
// crossplane_satisfied_by_materialization reducer intent from one immutable
// scope generation. The trigger fires on the earliest content_entity fact
// whose entity_kind (falling back to entity_type) is K8sResource or
// CrossplaneXRD — the two candidate types
// crossplane.ExtractCrossplaneSatisfiedByEdgeRows classifies (issue #5347). A
// Crossplane Claim candidate is never parser-labeled: it is an ordinary
// K8sResource row, so the trigger reads the entity type directly rather than
// firing on any content_entity presence. Only envelope-level fields and the
// two payload keys are read; the reducer's
// DomainCrossplaneSatisfiedByMaterialization handler owns the cross-scope
// join against active CrossplaneXRD facts and the SATISFIED_BY graph write.
// The intent's source-system label is the shared two-tier
// projectorintent.SourceSystem fallback (trimmed SourceRef.SourceSystem,
// else trimmed CollectorKind) — the pre-extraction local helper had the
// identical body, so this is a behavior-preserving substitution, not a
// change. Root projector assembly owns lookup construction and lifetime,
// invocation order, queue writes, retries, and telemetry.
package crossplanesatisfiedby

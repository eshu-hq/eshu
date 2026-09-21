// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package decode is the projector's single seam between an untyped fact
// payload and the typed factschema values the canonical extractors build rows
// from.
//
// It carries four things: the typed per-family decoders
// ([CodegraphFile], [OCIImageManifest], [PackageRegistryPackage],
// [TerraformStateResource] and their siblings), the untyped payload accessors
// those decoders and their callers share ([PayloadString], [PayloadInt] and
// the rest), fact-kind normalization and selection ([NormalizeFactKind],
// [FilterFileFacts]), and the quarantine path that classifies a decode
// failure.
//
// The quarantine contract is the reason the seam is one package: a fact that
// fails to decode because a required payload field is absent is
// input_invalid and [PartitionFailures] returns it as a [QuarantinedFact] for
// the caller to dead-letter, while any other decode error is returned
// fatally. Neither is ever silently skipped — a swallowed decode failure is
// exactly the wrong-graph-truth failure the repository's accuracy rule
// forbids.
//
// The package is a leaf: it imports no other projector package, so the
// canonical extractors, the stages and the runtime can all depend on it.
package decode

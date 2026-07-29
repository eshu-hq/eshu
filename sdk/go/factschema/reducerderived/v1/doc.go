// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 contains schema-version-1 payload structs for governed
// reducer-derived facts. These are durable read-model facts written by reducer
// domains after source evidence has already been admitted.
//
// SupplyChainImpactFinding carries one closed-vocabulary field.
// EnvironmentEvidence is a map keyed by the same environment names the
// finding's Environments slice lists, whose values are exactly "deploy_event"
// (a provider deployment event was observed at the deploying run's commit) or
// "declared" (the CI-declared workflow job gate alone). Producers must not
// invent a third value: consumers branch on the two, and an unrecognized value
// reads as the weaker "declared" state rather than failing. The field is
// additive-optional (a pointerless map with omitempty, absent from the
// generated schema's required set), so a payload written before it existed
// still decodes and a finding with no corroboration is byte-identical to what
// it was before. Environments is unchanged and remains the field readers
// filter on; EnvironmentEvidence is a sibling, not a replacement.
//
// CIDeclaredArtifactDigest and CIDeclaredImageRef (issue #5469) carry the
// matched cicd_run_correlation deployment's OWN declared artifact identity --
// its own artifact_digest and image_ref, never SubjectDigest/ImageRef borrowed
// from the finding itself. The reducer bakes these only when that deployment
// matched through a strong identity branch (digest or image-ref equality,
// never the weak repository+environment+operational-anchor branch), so their
// presence signals a genuine, independently-declared artifact identity a
// query-time resolver can compare against the finding's own claim. Both are
// *string with omitempty, additive-optional like EnvironmentEvidence: a
// payload written before #5469 carries neither key and decodes with both
// fields nil.
package v1

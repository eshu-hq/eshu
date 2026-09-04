// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package containerimage owns the container-image identity and provenance
// family: resolving an image reference to a canonical identity, and projecting
// the BUILT_FROM and DERIVED_FROM provenance edges that hang off it.
//
// It covers reference parsing (ParseContainerImageRef, DigestFromImageRef,
// NormalizeContainerRepositoryKey), the identity decision records the handler
// and writer exchange (ContainerImageIdentityDecision, ContainerImageIdentityWrite,
// ContainerImageIdentityWriteResult), the graph existence lookup, and the
// derived-from and built-from edge projection.
//
// The package never imports the parent reducer package. Everything it needs
// from the reducer's shared vocabulary comes from leaf packages instead:
// contract for the domain, intent, and result types; factload for fact
// loading; factdecode for quarantine handling; factwrite for the batched
// fact-row writer; payloadcore for payload accessors and identity helpers;
// schemadecode for the sdk/go/factschema decode seam (AWS/Azure/GCP image
// references, OCI registry and CI/CD envelopes, SBOM attestation predicates);
// cicdrun for the CI-run correlation key/pointer helpers the build-provenance
// join reads; packagesourcecore for repository-hint matching; and sbomattest
// for the attestation attachment decisions the SLSA provenance join reads.
// The reducer root keeps a compatibility surface
// (container_image_identity_compat.go) that aliases this family's exported
// symbols back for its own remaining callers, so the import direction stays
// one-way: root depends on containerimage, never the reverse.
//
// GraphQueryRunner and activeRepositoryFactLoader are declared here rather
// than imported. The reducer root owns identical types used by several
// families that have not moved yet, and importing the root to reach them
// would invert the one-way dependency above. Go interfaces are structural, so
// a local declaration with the same method set is satisfied by the same
// concrete implementations root wires in, without duplicating any logic. The
// codetaint package resolves the same problem the same way.
//
// Telemetry: the identity handler increments
// eshu_dp_container_image_identity_decisions_total (labeled by domain and
// outcome) for every identity decision and
// eshu_dp_container_image_identity_retirements_total (labeled by domain and
// outcome) for every retirement action. Both edge projectors increment the
// shared eshu_dp_provenance_edges_total counter (labeled by domain and
// outcome) on a successful writer call; that counter is registered in
// internal/telemetry and shared with the other producers of canonical
// PUBLISHES, BUILT_FROM, and DERIVED_FROM rows, so an operator filters it by
// domain rather than expecting a family-specific instrument. Facts rejected
// for a malformed payload feed the shared
// eshu_dp_reducer_input_invalid_facts_total counter through factdecode
// instead of a family-specific one.
package containerimage

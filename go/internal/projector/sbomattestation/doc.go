// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sbomattestation builds the SBOM-attestation-attachment reducer
// intent from one immutable scope generation: when the generation carries at
// least one subject-anchor fact — an sbom.document, an attestation.statement,
// or an OCI referrer whose payload names both subject and referrer digests —
// it asks the reducer to attach that supply-chain evidence to canonical image
// subjects once. The anchor is the earliest candidate fact in original input
// order across the three kinds — there is no per-kind priority — so the
// reducer claim is stable across reprojections of the same generation.
// Component-only SBOM evidence never triggers: components, dependency edges,
// external references, and warnings only enrich the reducer decision once a
// document-scoped intent exists. Only the fact kind is read; no payload is
// decoded, and schema-version admission stays with root projection, which
// rejects an unsupported SBOM-attestation schema version before any builder
// runs. The source-system label is the shared two-tier
// projectorintent.SourceSystem fallback (SourceRef, then CollectorKind); the
// root file's private helper was body-identical to it, so the child carries no
// helper of its own. The reducer's DomainSBOMAttestationAttachment handler
// owns subject-digest admission and every attachment decision and write. Root
// projector assembly owns lookup construction and lifetime, invocation order,
// queue writes, retries, and telemetry.
package sbomattestation

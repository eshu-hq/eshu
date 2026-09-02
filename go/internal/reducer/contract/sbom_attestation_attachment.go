// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// SBOMAttestationAttachmentFactKind names the durable fact kind the
// sbom_attestation attachment writer publishes under. It is exported so
// families below the reducer root (e.g. sbomattest) and the reducer root's
// supply_chain_impact family can both name it without either importing the
// reducer root package, which would violate the strictly downward
// package-import direction (root -> family -> shared-core -> contract).
const SBOMAttestationAttachmentFactKind = "reducer_sbom_attestation_attachment"

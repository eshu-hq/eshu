// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evidence holds the evidence-citation contract and the evidence
// boundary disclosures that query responses carry.
//
// EvidenceCitationHandle and EvidenceCitationHandleKey identify one citation a
// visualization or answer packet points at; EvidenceCitationResponse,
// EvidenceCitation, EvidenceCitationCoverage and EvidenceCitationProvenance
// are the packet a handle resolves into. EvidenceBoundariesFor and
// AttachEvidenceBoundaries add the disclosures a response owes when part of
// its truth came from a narrower source, such as PostgresOnlyBoundary.
//
// The package imports nothing from its parent querycontract. The parent
// imports it: its visualization and answer packets carry
// EvidenceCitationHandle, which is why this leaf lands before theirs.
package evidence

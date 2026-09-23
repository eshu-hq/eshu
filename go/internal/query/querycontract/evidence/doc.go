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
// The package imports nothing from its parent querycontract, and the parent
// does not import it. The sibling leaves querycontract/answer and
// querycontract/visualization import it because answer packets and
// visualization nodes carry EvidenceCitationHandle; that is why this leaf
// landed before theirs.
package evidence

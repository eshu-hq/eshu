// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evidence owns the evidence handler family: the drilldown,
// citation, admission decision, live bundle, and deployable-unit packet
// routes served through Handler.Mount.
//
// EvidenceCitationHandle is a type alias for the canonical
// querycontract/evidence handle wire shape, not a redefinition: every field
// stays exported and unchanged, so handles built through this package and
// handles built through querycontract/evidence are the same value.
//
// The deployable-unit packet route composes through the
// DeployableUnitPacketResponder seam, which root package query implements
// from the lane-B packet envelope; the handler passes only leaf values and
// the live request. When the investigation/packet leaf lands, delete the
// seam and call the leaf directly.
//
// The relationship read model (ContentReader.RelationshipEvidenceByResolvedID)
// and the citation file batch (ContentReader.EvidenceCitationFiles) stay in
// the root query package with the content/read family; this package asserts
// them through the RelationshipEvidenceReadModelStore and
// EvidenceCitationFileStore ports. Capability rows live in capabilities.go
// and are shared with root's contract matrix from those constructors; the
// packet envelope (BuildDeployableUnitPacket and the refusal composer)
// stays in root until the packet leaf lands.
//
// This package must never import the root query package.
package evidence

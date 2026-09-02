// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/sbomattest"
)

// This file is the transitional compatibility surface for the sbom_attestation
// attachment family that moved to [sbomattest] (issue #6061). Reducer-root call
// sites and the external packages that name these types keep their current
// spelling; each entry is deleted once its last caller has moved into a family
// subpackage.

// SBOMAttachmentStatus names the reducer decision for one SBOM or attestation
// document attachment. See [sbomattest.SBOMAttachmentStatus].
type SBOMAttachmentStatus = sbomattest.SBOMAttachmentStatus

const (
	// SBOMAttachmentAttachedVerified forwards to
	// [sbomattest.SBOMAttachmentAttachedVerified].
	SBOMAttachmentAttachedVerified = sbomattest.SBOMAttachmentAttachedVerified
	// SBOMAttachmentAttachedUnverified forwards to
	// [sbomattest.SBOMAttachmentAttachedUnverified].
	SBOMAttachmentAttachedUnverified = sbomattest.SBOMAttachmentAttachedUnverified
	// SBOMAttachmentAttachedParseOnly forwards to
	// [sbomattest.SBOMAttachmentAttachedParseOnly].
	SBOMAttachmentAttachedParseOnly = sbomattest.SBOMAttachmentAttachedParseOnly
	// SBOMAttachmentSubjectMismatch forwards to
	// [sbomattest.SBOMAttachmentSubjectMismatch].
	SBOMAttachmentSubjectMismatch = sbomattest.SBOMAttachmentSubjectMismatch
	// SBOMAttachmentAmbiguousSubject forwards to
	// [sbomattest.SBOMAttachmentAmbiguousSubject].
	SBOMAttachmentAmbiguousSubject = sbomattest.SBOMAttachmentAmbiguousSubject
	// SBOMAttachmentUnknownSubject forwards to
	// [sbomattest.SBOMAttachmentUnknownSubject].
	SBOMAttachmentUnknownSubject = sbomattest.SBOMAttachmentUnknownSubject
	// SBOMAttachmentUnparseable forwards to
	// [sbomattest.SBOMAttachmentUnparseable].
	SBOMAttachmentUnparseable = sbomattest.SBOMAttachmentUnparseable
)

// SBOMAttestationAttachmentDecision records one reducer attachment decision.
// See [sbomattest.SBOMAttestationAttachmentDecision].
type SBOMAttestationAttachmentDecision = sbomattest.SBOMAttestationAttachmentDecision

// SBOMAttestationAttachmentWrite carries decisions for durable publication.
// See [sbomattest.SBOMAttestationAttachmentWrite].
type SBOMAttestationAttachmentWrite = sbomattest.SBOMAttestationAttachmentWrite

// SBOMAttestationAttachmentWriteResult summarizes durable publication. See
// [sbomattest.SBOMAttestationAttachmentWriteResult].
type SBOMAttestationAttachmentWriteResult = sbomattest.SBOMAttestationAttachmentWriteResult

// SBOMAttestationAttachmentWriter persists reducer-owned attachment facts.
// See [sbomattest.SBOMAttestationAttachmentWriter].
type SBOMAttestationAttachmentWriter = sbomattest.SBOMAttestationAttachmentWriter

// SBOMAttestationAttachmentHandler attaches SBOM and attestation documents to
// image digests only when subject evidence is explicit. See
// [sbomattest.SBOMAttestationAttachmentHandler].
type SBOMAttestationAttachmentHandler = sbomattest.SBOMAttestationAttachmentHandler

// PostgresSBOMAttestationAttachmentWriter stores reducer-owned SBOM and
// attestation attachment decisions in the shared fact store. See
// [sbomattest.PostgresSBOMAttestationAttachmentWriter].
type PostgresSBOMAttestationAttachmentWriter = sbomattest.PostgresSBOMAttestationAttachmentWriter

// BuildSBOMAttestationAttachmentDecisions forwards to
// [sbomattest.BuildSBOMAttestationAttachmentDecisions].
func BuildSBOMAttestationAttachmentDecisions(envelopes []facts.Envelope) []SBOMAttestationAttachmentDecision {
	return sbomattest.BuildSBOMAttestationAttachmentDecisions(envelopes)
}

// MaxSBOMAttachmentComponentEvidenceRows forwards to
// [sbomattest.MaxSBOMAttachmentComponentEvidenceRows].
const MaxSBOMAttachmentComponentEvidenceRows = sbomattest.MaxSBOMAttachmentComponentEvidenceRows

// ComponentEvidence is the exported field-for-field mirror of the internal
// sbomAttachmentComponentEvidence tuple. See [sbomattest.ComponentEvidence].
type ComponentEvidence = sbomattest.ComponentEvidence

// ComponentEvidenceLess forwards to [sbomattest.ComponentEvidenceLess].
func ComponentEvidenceLess(a, b ComponentEvidence) bool {
	return sbomattest.ComponentEvidenceLess(a, b)
}

// ComponentEvidenceTupleEqual forwards to
// [sbomattest.ComponentEvidenceTupleEqual].
func ComponentEvidenceTupleEqual(a, b ComponentEvidence) bool {
	return sbomattest.ComponentEvidenceTupleEqual(a, b)
}

// normalizedVerificationStatus forwards to
// [sbomattest.NormalizedVerificationStatus].
func normalizedVerificationStatus(raw string) string {
	return sbomattest.NormalizedVerificationStatus(raw)
}

// payloadStrings forwards to [sbomattest.PayloadStrings].
func payloadStrings(payload map[string]any, scalarKey string, sliceKey string) []string {
	return sbomattest.PayloadStrings(payload, scalarKey, sliceKey)
}

// sbomAttestationAttachmentFactKind lives in intent.go, aliased directly from
// [reducercontract.SBOMAttestationAttachmentFactKind] rather than forwarded
// through sbomattest -- see that file's alias block, mirroring
// containerImageIdentityFactKind's identical shape for the same reason
// (#6431).

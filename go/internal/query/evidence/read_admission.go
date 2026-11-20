// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// EvidenceReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the evidence reads (the relationship
// evidence drilldown, the admission-decision list, the live evidence
// bundle, the deployable-unit packet, and the citation packet). Canonical
// reads persist through explicit core-owned admission contracts:
// eshu-api keeps the HTTP shape and calls this versioned port, which the
// core-owned content reader, decision store, citation store, status
// reader, and graph reader satisfy jointly, composed by the consumer. A
// breaking change to the read shape fails the frozen method-set test until
// that list is deliberately updated; bump this version alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const EvidenceReadAdmissionContractVersion = "v0"

// EvidenceReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the six shared handles. It
// declares exactly the seven reads the evidence routes admit through —
// the relationship drilldown (RelationshipEvidenceByResolvedID,
// mirroring RelationshipEvidenceReadModelStore), the decision reads
// (ListAdmissionDecisions, ListAdmissionDecisionEvidence, mirroring
// AdmissionDecisionReadStore as exercised through the
// admission-decisions and deployable-unit packet legs), the citation
// reads (EvidenceCitationFiles, mirroring EvidenceCitationFileStore,
// plus the GetFileContent fallback, mirroring ContentStore as exercised
// through the citations leg), and the bundle reads
// (ReadStatusSnapshotFiltered, mirroring status.Reader, plus RunSingle
// for the repository count, mirroring GraphQuery, as exercised through
// the bundle leg) — so the boundary is held honest by the
// compile-time mirror assertion below plus the frozen method-set
// contract test. It will back the eshu-api extraction handoff for
// these reads. The seam is read-only on purpose: every declared
// method is a bounded read and the port carries no mutation surface,
// so a dest consumer implementing only this contract can serve the
// evidence reads but cannot acquire a write it did not bargain for
// through it.
//
// W-wave consumer: W5 evidence (all 5 oracle Evidence rows).
//
// Narrow by construction, recorded not silently assumed: the methods
// re-declare the store signatures in this evidence-scoped shape
// instead of embedding the stores. This pin therefore grants nothing
// to the other families that read through shared ports — every other
// store consumer keeps its own per-route relay reason until deliberate
// pins cover them.
//
// The deployable-unit packet composes through the packet responder
// seam (an HTTP-writing seam, not a read): the packet leg admits its
// decision read through ListAdmissionDecisions above, and the
// responder stays handler behavior, not port shape — there is no
// method set to freeze for it.
type EvidenceReadAdmission interface {
	RelationshipEvidenceByResolvedID(context.Context, string) (RelationshipEvidenceReadModel, error)
	ListAdmissionDecisions(context.Context, AdmissionDecisionReadFilter) ([]AdmissionDecisionReadRow, error)
	ListAdmissionDecisionEvidence(context.Context, string, int) ([]packet.AdmissionDecisionEvidenceRow, error)
	EvidenceCitationFiles(context.Context, []EvidenceCitationFileLookup) (map[EvidenceCitationFileKey]querycontract.FileContent, error)
	GetFileContent(context.Context, string, string) (*querycontract.FileContent, error)
	ReadStatusSnapshotFiltered(context.Context, time.Time, status.SnapshotSelection) (status.RawSnapshot, error)
	RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error)
}

// The mirror assertion keeps the evidence-scoped re-declaration
// honest, and it works in one direction only: removal, rename, or
// re-signing of a mirrored method on any staying handler-facing port
// fails compilation here. A new method on those ports does not widen
// this pin — adoption takes deliberate re-declaration plus a
// frozen-list update. It pins no shared port — the assertion names
// the read surface only as the shapes this pin mirrors, and no other
// family may cite this pin as coverage.
var _ EvidenceReadAdmission = interface {
	RelationshipEvidenceReadModelStore
	AdmissionDecisionReadStore
	EvidenceCitationFileStore
	querycontract.ContentStore
	status.Reader
	GraphQuery
}(nil)

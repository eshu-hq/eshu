// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/evidence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// compat_evidence.go is the root alias shim for the evidence handler family,
// which moved to internal/query/evidence (#6642). cmd/api and cmd/mcp-server
// build the handler as query.EvidenceHandler, the APIRouter field names it,
// and the staying evidence tests drive it, so the aliases below keep those
// call sites compiling unchanged until the #6642 alias sweep repoints them.
//
// === Handler aliases ===
//
// This section preserves the root package query surface cmd/api,
// cmd/mcp-server, and the staying tests still use for the evidence handler.
// The implementation moved to internal/query/evidence; these aliases forward
// unchanged.

// EvidenceHandler exposes drilldown endpoints for compact evidence
// pointers. See evidence.Handler.
type EvidenceHandler = evidence.Handler

// RelationshipEvidenceReadModel is the shared Postgres relationship evidence
// row. See evidence.RelationshipEvidenceReadModel.
type relationshipEvidenceReadModel = evidence.RelationshipEvidenceReadModel

// Admission decision read-model ports and values. See the evidence package.
type (
	AdmissionDecisionReadStore         = evidence.AdmissionDecisionReadStore
	AdmissionDecisionReadFilter        = evidence.AdmissionDecisionReadFilter
	AdmissionDecisionReadRow           = evidence.AdmissionDecisionReadRow
	AdmissionDecisionEvidenceRow       = evidence.AdmissionDecisionEvidenceRow
	AdmissionDecisionResult            = evidence.AdmissionDecisionResult
	AdmissionDecisionSourceHandle      = evidence.AdmissionDecisionSourceHandle
	AdmissionDecisionCanonicalWrite    = evidence.AdmissionDecisionCanonicalWrite
	AdmissionDecisionNextAction        = evidence.AdmissionDecisionNextAction
	RecommendedNextCall                = evidence.RecommendedNextCall
	PostgresAdmissionDecisionReadStore = evidence.PostgresAdmissionDecisionReadStore
)

// Content/read port aliases. Root's ContentReader implements the evidence
// family's narrow ports; the interfaces and their lookup/key types moved to
// the leaf with the handlers that assert them, so these aliases keep the
// staying content/read implementation and its tests spelling the pre-move
// names. See the evidence package.
type (
	relationshipEvidenceReadModelStore = evidence.RelationshipEvidenceReadModelStore
	evidenceCitationFileStore          = evidence.EvidenceCitationFileStore
	evidenceCitationFileLookup         = evidence.EvidenceCitationFileLookup
	evidenceCitationFileKey            = evidence.EvidenceCitationFileKey
)

// evidenceCitationMaxLimit bounds the citation packet handles per request.
// Staying callers: the lane-B packet builder's per-layer citation cap. See
// evidence.EvidenceCitationMaxLimit.
const evidenceCitationMaxLimit = evidence.EvidenceCitationMaxLimit

// NewPostgresAdmissionDecisionReadStore creates the Postgres-backed admission
// decision read model. See evidence.NewPostgresAdmissionDecisionReadStore.
func NewPostgresAdmissionDecisionReadStore(database db.ExecQueryer) PostgresAdmissionDecisionReadStore {
	return evidence.NewPostgresAdmissionDecisionReadStore(database)
}

// NewPostgresAdmissionDecisionReadStoreWithReadStore reads admission decisions
// through a guarded query-only port. See
// evidence.NewPostgresAdmissionDecisionReadStoreWithReadStore.
func NewPostgresAdmissionDecisionReadStoreWithReadStore(database db.Queryer) PostgresAdmissionDecisionReadStore {
	return evidence.NewPostgresAdmissionDecisionReadStoreWithReadStore(database)
}

// Unexported forwards for staying root files. Each names its staying
// callers; evidence-internal callers use the exported evidence names
// directly.

// admissionDecisionCapability keys admission decision rows in the root
// contract capability matrix. Staying callers: the staying admission and
// packet tests. See evidence.AdmissionDecisionCapability.
const admissionDecisionCapability = evidence.AdmissionDecisionCapability

// admissionDecisionResult projects one persisted admission decision row into
// the HTTP/MCP read shape. Staying callers: the staying deployable-unit
// packet test, which builds its expected packet through it. See
// evidence.AdmissionDecisionResultFromRow.
func admissionDecisionResult(row AdmissionDecisionReadRow) AdmissionDecisionResult {
	return evidence.AdmissionDecisionResultFromRow(row)
}

// relationshipEvidenceRowWithinAccess is the relationship-evidence access
// gate: the predicate lives in querycontract (relationship_evidence_authz.go,
// the #6642 evidence-leaf pre-slice) so the evidence leaf shares the single
// source of truth without importing root. Staying callers: the staying
// relationship-evidence scoped test. See
// querycontract.RelationshipEvidenceRowWithinAccess.
func relationshipEvidenceRowWithinAccess(row map[string]any, access querycontract.RepositoryAccessFilter) bool {
	return querycontract.RelationshipEvidenceRowWithinAccess(row, access)
}

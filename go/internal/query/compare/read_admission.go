// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compare

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// CompareReadAdmissionContractVersion versions the #9669 Gap 04
// admission-contract shape for the Compare POST read (POST
// /api/v0/compare/environments). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned graph
// and content stores satisfy. A breaking change to the read shape
// fails the frozen method-set test until that list is deliberately
// updated; bump this version alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers
// can pin the shape.
const CompareReadAdmissionContractVersion = "v0"

// CompareReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the two shared handles. It
// declares exactly the four reads POST
// /api/v0/compare/environments admits through — the workload and
// environment-snapshot graph reads (Run, RunSingle, mirroring
// querycontract.GraphQuery) and the service-evidence content reads
// (ListRepoFiles, GetFileContent, mirroring service.EvidenceReader
// as exercised through service.LoadServiceQueryEvidence) — so the
// boundary is held honest by the compile-time mirror assertion below
// plus the frozen method-set contract test. It will back the eshu-api
// extraction handoff for this read. The seam is read-only on
// purpose: every declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve the comparison but cannot acquire a write
// it did not bargain for through it.
//
// D-wave consumer: Compare gap-04 deliberate later pin (D6
// follow-up, per the arbiter D-wave ruling minting rule and the
// row-7 Path B ruling gap-04 disposition — a blanket or shared-port
// pin for Compare is forbidden permanently). The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin
// is invalid. Arbiter review of this pin precedes satellite
// termination of the Compare POST leg.
//
// Narrow by construction, recorded not silently assumed: the four
// methods re-declare the shared-port signatures in this
// Compare-scoped shape instead of embedding or aliasing
// querycontract.GraphQuery or service.EvidenceReader. This pin
// therefore grants nothing to the other families that read through
// those shared ports — the Neo4j-count legs on the index-status
// routes, the hosted-readiness leftovers, and every other GraphQuery
// or EvidenceReader consumer keep their own per-route relay reasons
// until deliberate pins cover them.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - The grant check (RepositoryAccessFilterFromContext plus the
//     repo grant comparison) and the fixed failure messages are
//     handler behavior, not port shape: there is no method set to
//     freeze, the same standing as QueryProfile in this handler and
//     Clock/FinalizeTimeout in the mutation pins.
//   - No suppression leg exists on this route; the suppressions
//     mutation row stays the Decision 2 tier-C survivor via
//     SuppressionAdmission, never a read termination here. The
//     Decision 2 suppression-state-filtering parity obligation stays
//     on the SupplyChain finding/reconciliation legs this pin does
//     not touch.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type CompareReadAdmission interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error)
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error)
	GetFileContent(ctx context.Context, repoID, relativePath string) (*querycontract.FileContent, error)
}

// The mirror assertion keeps the Compare-scoped re-declaration
// honest: any one-sided signature drift on either shared port fails
// compilation here. It pins no shared port — the assertion names the
// combination only as the shape this pin mirrors, and no other
// family may cite this pin as coverage.
var _ CompareReadAdmission = interface {
	querycontract.GraphQuery
	service.EvidenceReader
}(nil)

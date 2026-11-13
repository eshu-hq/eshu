// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
)

// CodeDeadCodeReadAdmissionContractVersion versions the #9669
// admission-contract shape for the dead-code content-fallback reads: the
// graph run plus the thirteen content reads the dead-code investigation,
// scan, and cross-repo consumer-evidence legs reach. Canonical reads
// persist through explicit core-owned admission contracts: eshu-api keeps
// the HTTP shape and calls this versioned port, which the core-owned
// graph reader and Postgres content store satisfy. A breaking change to
// the read shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest consumers
// can pin the shape.
const CodeDeadCodeReadAdmissionContractVersion = "v0"

// CodeDeadCodeReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of direct GraphQuery/ContentReader
// handles. It re-declares exactly the fourteen dead-code reads the
// dead-code legs admit through — the graph run behind the SQL-routine
// EXECUTES probes, the candidate/incoming/downgrade reads behind the
// investigation and scan, the entity hydration reads, the coverage and
// index-freshness reads, and the three cross-repo consumer-evidence
// reads — so the boundary is held honest by the compile-time witness
// assertion below plus the frozen method-set contract test; it will back
// the eshu-api extraction handoff for this family. The seam is read-only
// on purpose: every declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve dead-code reads but cannot acquire a write it
// did not bargain for through it.
//
// Satellite consumer: the dead-code content-fallback lane, per the
// arbiter SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#124. The
// satellite records this pin in a follow-up; a satellite PR consuming
// an unminted pin is invalid. Arbiter review of this pin precedes
// satellite termination of the dead-code legs.
//
// In scope (each leg admits its reads through exactly this):
//
//   - The SQL-routine reachability probe admits its graph lane through
//     Run.
//   - The dead-code investigation and scan admit their candidate,
//     incoming-edge, reachability, and downgrade reads through
//     DeadCodeCandidateRows, DeadCodeIncomingEntityIDs,
//     CodeReachabilityIncomingEntityIDs, and DowngradedCodeRootKinds.
//   - Candidate hydration admits its entity reads through
//     GetEntityContents and GetEntityContent.
//   - The coverage block admits its cheap freshness reads through
//     RepositoryContextCoverage and RepositoryFilesLastIndexedAt, and
//     its full aggregate through RepositoryCoverage.
//   - The cross-repo legs admit their consumer evidence, coverage, and
//     root-path reads through CrossRepoDeadCodeConsumerEvidence,
//     CrossRepoDeadCodeConsumerCoverage, and
//     CrossRepoDeadCodeConsumerRootPaths, as well as the boundary-evidence
//     read through RepositoryRelationshipReadModel.
//
// RunSingle is OUT by construction: the dead-code lane never issues a
// single-row graph read.
//
// Narrow by construction, recorded not silently assumed: the fourteen
// methods re-declare the GraphQuery/ContentReader signatures in this
// dead-code-scoped shape instead of embedding the shared ports or
// aliasing the stores, so no other family can cite this pin — a blanket
// or shared-port pin is forbidden, and the widened shape needs arbiter
// review before satellite consumption. The CodeGraph, CodeSearch, and
// IaC pins are not cited as coverage here: each family stands on its
// own re-declaration, and this pin adopts only the reads named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The single-row graph read (RunSingle) stays unpinned here for the
//     reason above.
//   - The search legs (SearchEntityContent, SearchCodeCandidates, the
//     AnyRepo and exact-name twins) and the file-content point reads
//     keep the CodeSearch, Content search, and D13 point pins; there
//     is no search or file method in the frozen set.
//   - The list legs (ListRepoEntities and the by-type/by-path/by-ID
//     twins, ListRepoFiles) stay unpinned here for the same reason.
//   - The grant checks, the repository selector, the fixed failure
//     messages, the rerank, and QueryProfile are handler behavior, not
//     port shape: there is no method set to freeze.
//   - No ledger, stored-summary, or snapshot surface exists on this
//     family; the seal test holds that exclusion the same way.
//
// There is deliberately no WithReadStore constructor on this pin: the
// dead-code seam spans two backends (the concurrent-safe graph reader
// plus the guarded Postgres content reader) and minting one constructor
// would pick a wiring the satellite never runs. The satellite keeps
// building both backends the way cmd/api wiring does today; this pin
// freezes only the read shape the handlers may call through.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type CodeDeadCodeReadAdmission interface {
	DeadCodeCandidateRows(ctx context.Context, query codeshaping.DeadCodeCandidateQuery) ([]map[string]any, error)
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	GetEntityContents(ctx context.Context, entityIDs []string) (map[string]*EntityContent, error)
	GetEntityContent(ctx context.Context, entityID string) (*EntityContent, error)
	DowngradedCodeRootKinds(ctx context.Context, repoID string, entityIDs []string) (map[string]map[string]struct{}, error)
	DeadCodeIncomingEntityIDs(ctx context.Context, repoID string, entityIDs []string) (map[string]code.DeadCodeIncomingEdge, error)
	CodeReachabilityIncomingEntityIDs(ctx context.Context, repoID string, entityIDs []string, allowedRepositoryIDs []string) (map[string]code.DeadCodeIncomingEdge, error)
	RepositoryContextCoverage(ctx context.Context, repoID string) (RepositoryContentCoverage, error)
	RepositoryFilesLastIndexedAt(ctx context.Context, repoID string) (time.Time, error)
	RepositoryCoverage(ctx context.Context, repoID string) (RepositoryContentCoverage, error)
	RepositoryRelationshipReadModel(ctx context.Context, repoID string) (RepositoryRelationshipReadModel, error)
	CrossRepoDeadCodeConsumerEvidence(ctx context.Context, producerRepoID string, entityIDs []string, reads code.CrossRepoDeadCodeConsumerReads) (map[string][]deadcode.CrossRepoDeadCodeEvidence, code.CrossRepoDeadCodeHiddenConsumers, error)
	CrossRepoDeadCodeConsumerCoverage(ctx context.Context, request code.CrossRepoDeadCodeCoverageRequest) (code.CrossRepoDeadCodeCoverage, error)
	CrossRepoDeadCodeConsumerRootPaths(ctx context.Context, rootEntityIDs []string, consumerRepoIDs []string) (map[string]string, error)
}

// codeDeadCodeReadAdmissionWitness is the unexported mirror that keeps
// the dead-code-scoped re-declaration honest: the graph half mirrors
// GraphQuery and the content half mirrors *ContentReader, so a
// one-sided signature drift on any of the fourteen pinned reads fails
// compilation here. The assertion below proves the (GraphQuery +
// *ContentReader) pair the handlers run against today satisfies the
// pin; it pins no wider store surface, and no other family may cite
// this pin as coverage.
type codeDeadCodeReadAdmissionWitness struct {
	querycontract.GraphQuery
	*ContentReader
}

var _ CodeDeadCodeReadAdmission = codeDeadCodeReadAdmissionWitness{}

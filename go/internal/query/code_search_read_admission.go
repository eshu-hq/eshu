// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
)

// CodeSearchReadAdmissionContractVersion versions the #9669 W4
// admission-contract shape for the Code search reads: the graph run
// plus the eight content reads the code/search and symbols/search
// routes reach. Canonical reads persist through explicit core-owned
// admission contracts: eshu-api keeps the HTTP shape and calls this
// versioned port, which the core-owned graph reader and Postgres
// content store satisfy. A breaking change to the read shape fails the
// frozen method-set test until that list is deliberately updated; bump
// this version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const CodeSearchReadAdmissionContractVersion = "v0"

// CodeSearchReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of direct GraphQuery/ContentStore
// handles. It re-declares exactly the nine Code search reads the two
// code search routes admit through — the graph run plus the global
// entity-name index, the symbol-aware read, the candidate/name/content
// reads in their scoped, grant-loop, and corpus-wide shapes, and the
// by-ID hydration behind graph enrichment — so the boundary is held
// honest by the compile-time witness assertion below plus the frozen
// method-set contract test; it will back the eshu-api extraction
// handoff for this family. The seam is read-only on purpose: every
// declared method is a bounded read and the port carries no mutation
// surface, so a dest consumer implementing only this contract can serve
// code search reads but cannot acquire a write it did not bargain for
// through it.
//
// D-wave consumer: W4 search family, per the arbiter W4 ruling on #9669
// (APPROVED-WITH-CONDITIONS). The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the code
// search legs.
//
// In scope (each route admits its reads through exactly this):
//
//   - POST /api/v0/code/search admits its graph lane through Run, its
//     global entity-name index through SearchEntityNames, its
//     repository-scoped and grant-loop content fallback through
//     SearchCodeCandidates, SearchEntitiesByName, and
//     SearchEntityContent, its corpus-wide fallback through
//     SearchEntitiesByNameAnyRepo and SearchEntityContentAnyRepo, and
//     its graph-enrichment hydration through ListRepoEntitiesByIDs.
//   - POST /api/v0/code/symbols/search admits its symbol-aware read
//     through SearchSymbols, its plain-name fallback through
//     SearchEntitiesByName, and its graph lane plus enrichment through
//     Run and ListRepoEntitiesByIDs.
//
// RunSingle is OUT by construction: the code search family never issues
// a single-row graph read (complexity and relationship single-row reads
// belong to other families' pins).
//
// Narrow by construction, recorded not silently assumed: the nine
// methods re-declare the GraphQuery/ContentReader signatures in this
// Code-scoped shape instead of embedding the shared ports or aliasing
// the stores, so no other family can cite this pin — a blanket or
// shared-port pin is forbidden, and the widened shape needs arbiter
// review before satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The single-row graph read (RunSingle) stays unpinned here for the
//     reason above.
//   - The file-content search legs (SearchFileContent, the AnyRepo and
//     exact-case twins), the paged file/entity seams (SearchFiles,
//     SearchEntities and the unscoped walks), and the
//     Dockerfile-install and PHP-config entity pages belong to the
//     Content search pin, not this one.
//   - The point reads (GetFileContent, GetEntityContent and the twins)
//     keep the D13 point pin; the exact-name twins, the
//     component-reference and language/type searches, and every other
//     List leg stay unpinned here.
//   - The grant checks (codeGrantAccessFilter, codeContentGrantScope),
//     the repository selector, the fixed failure messages, the hybrid
//     rerank, and QueryProfile are handler behavior, not port shape:
//     there is no method set to freeze.
//   - The semantic overlay (entitysemantics summaries) is handler
//     shaping; SemanticSearch v0 is a record-only cross-reference and
//     nothing here touches it.
//   - No ledger, stored-summary, or snapshot surface exists on this
//     family; the seal test holds that exclusion the same way.
//
// There is deliberately no WithReadStore constructor on this pin: the
// code search seam spans two backends (the concurrent-safe graph reader
// plus the guarded Postgres content reader) and minting one constructor
// would pick a wiring the satellite never runs. The satellite keeps
// building both backends the way cmd/api wiring does today; this pin
// freezes only the read shape the handlers may call through.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type CodeSearchReadAdmission interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	SearchEntityNames(ctx context.Context, search entity.EntityNameSearch) ([]EntityContent, error)
	SearchSymbols(ctx context.Context, req codequery.SymbolSearchRequest) ([]EntityContent, error)
	SearchCodeCandidates(ctx context.Context, repoID, pattern, language string, limit int, exact bool) (nameMatches, sourceMatches []EntityContent, err error)
	SearchEntitiesByName(ctx context.Context, repoID, entityType, name string, limit int) ([]EntityContent, error)
	SearchEntityContent(ctx context.Context, repoID, pattern string, limit int) ([]EntityContent, error)
	SearchEntitiesByNameAnyRepo(ctx context.Context, entityType, name string, limit int) ([]EntityContent, error)
	SearchEntityContentAnyRepo(ctx context.Context, pattern string, limit int) ([]EntityContent, error)
	ListRepoEntitiesByIDs(ctx context.Context, repoID string, entityIDs []string, limit int) ([]EntityContent, error)
}

// codeSearchReadAdmissionWitness is the unexported mirror that keeps the
// Code-scoped re-declaration honest: the graph half mirrors GraphQuery
// and the content half mirrors *ContentReader, so a one-sided signature
// drift on any of the nine pinned reads fails compilation here. The
// assertion below proves the (GraphQuery + *ContentReader) pair the
// handlers run against today satisfies the pin; it pins no wider store
// surface, and no other family may cite this pin as coverage.
type codeSearchReadAdmissionWitness struct {
	querycontract.GraphQuery
	*ContentReader
}

var _ CodeSearchReadAdmission = codeSearchReadAdmissionWitness{}

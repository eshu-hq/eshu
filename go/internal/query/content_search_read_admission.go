// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ContentSearchReadAdmissionContractVersion versions the #9669 W4
// admission-contract shape for the Content search reads: the paged
// file/entity seams, the scoped and corpus-wide substring reads, the
// bounded unscoped walks, and the Dockerfile-install and PHP-config
// entity pages behind the four content search routes. Canonical reads
// persist through explicit core-owned admission contracts: eshu-api
// keeps the HTTP shape and calls this versioned port, which the
// core-owned Postgres content store satisfies. A breaking change to the
// read shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest consumers
// can pin the shape.
const ContentSearchReadAdmissionContractVersion = "v0"

// ContentSearchReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of a direct ContentStore
// handle. It re-declares exactly the ten ContentReader search reads the
// four content search routes admit through — the paged file/entity
// seams, the scoped and corpus-wide substring reads, the bounded
// unscoped walks, and the Dockerfile-install and PHP-config entity
// pages — so the boundary is held honest by the compile-time mirror
// assertion below plus the frozen method-set contract test; it will back
// the eshu-api extraction handoff for this family. The seam is read-only
// on purpose: every declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve content search reads but cannot acquire a
// write it did not bargain for through it.
//
// D-wave consumer: W4 search family, per the arbiter W4 ruling on #9669
// (APPROVED-WITH-CONDITIONS). The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the
// content search legs.
//
// In scope (each route admits its reads through exactly this):
//
//   - POST /api/v0/content/files/search admits its scoped file reads
//     through SearchFiles (the paged seam), SearchFileContent (the
//     single-repository fallback), and SearchFileContentAnyRepo (the
//     corpus-wide fallback), and its unscoped bounded walk through
//     SearchFilesUnscoped.
//   - POST /api/v0/content/entities/search admits its scoped entity
//     reads through SearchEntities (the paged seam),
//     SearchEntityContent (the single-repository fallback), and
//     SearchEntityContentAnyRepo (the corpus-wide fallback), and its
//     unscoped bounded walk through SearchEntitiesUnscoped.
//   - POST /api/v0/content/dockerfile-installs/search admits its
//     install-entity page through SearchDockerfileInstallEntities.
//   - POST /api/v0/content/php-config/search admits its PHP evidence
//     page through SearchPHPConfigEntities.
//
// Narrow by construction, recorded not silently assumed: the ten
// methods re-declare the ContentReader signatures in this
// Content-scoped shape instead of embedding the ContentStore port or
// aliasing the store, so no other family can cite this pin — a blanket
// or shared-port pin is forbidden, and the widened shape needs arbiter
// review before satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The point reads (GetFileContent, GetFileLines, GetEntityContent
//     and the scoped/entity-list twins) keep the D13 point pin; no
//     point read is re-declared here.
//   - The name-search twins (SearchEntitiesByName,
//     SearchEntitiesByNameAnyRepo), the exact-name twins, the
//     entity-name index (SearchEntityNames), the symbol-aware read
//     (SearchSymbols), the candidate pages (SearchCodeCandidates), and
//     the by-ID hydration (ListRepoEntitiesByIDs) belong to the Code
//     search pin, not this one.
//   - The exact-case and reference AnyRepo twins
//     (SearchFileContentAnyRepoExactCase, SearchFileReferenceAnyRepo),
//     the language/type, component-reference, and in-repository
//     searches, and every List leg stay unpinned here.
//   - The grant checks (RepositoryAccessFilterFromContext plus the
//     repo-grant comparison), the selector resolution, the fixed failure
//     messages, and QueryProfile are handler behavior, not port shape:
//     there is no method set to freeze.
//   - The semantic/hybrid rerank (HybridRanker) is handler behavior
//     gated on the embedder being enabled; SemanticSearch v0 is a
//     record-only cross-reference and nothing here touches it.
//   - No ledger, stored-summary, or snapshot surface exists on this
//     family; the seal test holds that exclusion the same way.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type ContentSearchReadAdmission interface {
	SearchFiles(ctx context.Context, repoID string, repoIDs []string, pattern string, limit, offset int) ([]FileContent, error)
	SearchEntities(ctx context.Context, repoID string, repoIDs []string, pattern string, limit, offset int) ([]EntityContent, error)
	SearchFileContent(ctx context.Context, repoID, pattern string, limit int) ([]FileContent, error)
	SearchFileContentAnyRepo(ctx context.Context, pattern string, limit int) ([]FileContent, error)
	SearchEntityContent(ctx context.Context, repoID, pattern string, limit int) ([]EntityContent, error)
	SearchEntityContentAnyRepo(ctx context.Context, pattern string, limit int) ([]EntityContent, error)
	SearchFilesUnscoped(ctx context.Context, pattern string, limit, offset int, cursorRepoID, cursorPath string, budgetMS int) (querycontract.FileSearchPage, error)
	SearchEntitiesUnscoped(ctx context.Context, pattern string, limit, offset int, cursor querycontract.SearchCursor) (querycontract.EntitySearchPage, error)
	SearchDockerfileInstallEntities(ctx context.Context, name string, repoIDs []string, afterRepoID, afterPath string, afterLine int, afterID string, limit int) ([]querycontract.EntityContent, error)
	SearchPHPConfigEntities(ctx context.Context, repoIDs []string, name string, after *querycontract.PHPConfigCursor, limit int) ([]querycontract.EntityContent, error)
}

// The mirror assertion keeps the Content-scoped re-declaration honest:
// any one-sided signature drift on the ten ContentReader search methods
// fails compilation here. It pins no wider store surface — the
// assertion names *ContentReader only as the shape this pin mirrors,
// and no other family may cite this pin as coverage.
var _ ContentSearchReadAdmission = (*ContentReader)(nil)

// NewContentSearchReadAdmissionWithReadStore builds the Content-scoped
// admission satisfier over a guarded read-only query port, so the
// satellite consumes the pin without a direct table or queue handle:
// pass the request-scoped db.ReadStore the same way the API wiring
// does, plus the unscoped-search work budget, logger, and instruments
// the wiring resolves once at startup (unscoped.BudgetFromEnv for the
// budget). It serves exactly the ten pinned search reads over nothing
// wider than the guarded ReadStore the satellite holds.
//
// The constructor delegates to the existing
// NewContentReaderWithReadStore deliberately instead of minting
// parallel wiring, and applies WithInstruments plus WithUnscopedSearch
// with the production budget the same way cmd/api wiring does: the
// transport proof (fail-closed over a guarded ReadStore plus the full
// wire-object byte comparison against *ContentReader in the
// wire-shapes test) covers the satellite path end to end, and a second
// construction path — or a constructor that drops the budget or the
// instruments — would be a second place for the guarded handle, the
// bounded walk, or operator metrics to be misconfigured. A constructor
// that stops applying WithUnscopedSearch fails the wire-shapes budget
// test, and one that stops applying WithInstruments fails the
// instruments assertion in the same test.
func NewContentSearchReadAdmissionWithReadStore(readStore db.ReadStore, budget time.Duration, logger *slog.Logger, instruments *telemetry.Instruments) ContentSearchReadAdmission {
	return NewContentReaderWithReadStore(readStore).WithInstruments(instruments).WithUnscopedSearch(budget, logger)
}

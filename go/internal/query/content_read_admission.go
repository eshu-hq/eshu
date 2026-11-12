// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ContentPointReadAdmissionContractVersion versions the #9669 D13
// admission-contract shape for the Content point reads: the file-content
// read, the file line-range read, and the entity-content read behind the
// three content point routes. Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and calls
// this versioned port, which the core-owned Postgres content store
// satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list
// is the enforcement) so dest consumers can pin the shape.
const ContentPointReadAdmissionContractVersion = "v0"

// ContentPointReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of a direct ContentStore handle. It
// re-declares exactly the three ContentStore reads the content point
// routes admit through — the file-content read, the file line-range
// read, and the entity-content read — so the boundary is held honest by
// the compile-time mirror assertion below plus the frozen method-set
// contract test; it will back the eshu-api extraction handoff for this
// family. The seam is read-only on purpose: every declared method is a
// bounded read and the port carries no mutation surface, so a dest
// consumer implementing only this contract can serve content point
// reads but cannot acquire a write it did not bargain for through it.
//
// D-wave consumer: D13 Content-first Wave-2, per the arbiter Wave-2
// ruling on #9669 (APPROVED-WITH-CONDITIONS). The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin
// is invalid. Arbiter review of this pin precedes satellite
// termination of the content point legs.
//
// In scope (each route admits its reads through exactly this):
//
//   - POST /api/v0/content/files/read admits its file read through
//     GetFileContent, the bounded single-file content lookup.
//   - POST /api/v0/content/files/lines admits its line-range read
//     through GetFileLines, the bounded single-file slice over the
//     same lookup (clamping and empty-range edge semantics included).
//   - POST /api/v0/content/entities/read admits its entity read
//     through GetEntityContent, the bounded single-entity lookup by
//     entity_id.
//
// Narrow by construction, recorded not silently assumed: the three
// methods re-declare the ContentStore signatures in this
// Content-scoped shape instead of embedding the ContentStore port or
// aliasing the store, so no other family can cite this pin — a blanket
// or shared-port pin is forbidden, and the widened shape needs arbiter
// review before satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The grant checks (RepositoryAccessFilterFromContext plus the
//     repo-grant comparison), the selector resolution
//     (resolveRepositorySelectorForAccess), the fixed failure messages
//     ("content file read failed", "content entity read failed"), and
//     QueryProfile are handler behavior, not port shape: there is no
//     method set to freeze, the same standing as Clock and
//     FinalizeTimeout in the mutation pins.
//   - The search legs (SearchFileContent, SearchEntityContent, and the
//     AnyRepo/exact-name/candidate twins) stay unpinned here: there is
//     no search method in the frozen set, so a dest consumer cannot
//     acquire search reads through it.
//   - The list legs (ListRepoFiles, ListRepoEntities and the by-type /
//     by-path / by-ID twins) stay unpinned here for the same reason.
//   - The scoped-entity leg (GetEntityContentInRepositories) stays
//     unpinned here: the pin mirrors only the three point reads, and
//     the grant-scoped single-entity shape keeps its own standing.
//   - No ledger, stored-summary, or snapshot surface exists on this
//     family; the seal test holds that exclusion the same way.
//
// Evidence (not enforcement — the frozen set plus the mirror are the
// enforcement): the shared handler double behind the route tests is
// testutil/content.FakePortContentStore, blob
// 5c836cce6245ac9c470a837c986fcde96e40946d
// (go/internal/query/testutil/content/port_store.go, last touched by
// 030c434afda78a7c0d5ec6dbb97a877237b4837c).
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type ContentPointReadAdmission interface {
	GetFileContent(context.Context, string, string) (*querycontract.FileContent, error)
	GetFileLines(context.Context, string, string, int, int) (*querycontract.FileContent, error)
	GetEntityContent(context.Context, string) (*querycontract.EntityContent, error)
}

// The mirror assertion keeps the Content-scoped re-declaration
// honest: any one-sided signature drift on the three ContentStore
// methods fails compilation here. It pins no wider store surface —
// the assertion names *ContentReader only as the shape this pin
// mirrors, and no other family may cite this pin as coverage.
var _ ContentPointReadAdmission = (*ContentReader)(nil)

// NewContentPointReadAdmissionWithReadStore builds the Content-scoped
// admission satisfier over a guarded read-only query port, so the
// satellite consumes the pin without a direct table or queue handle:
// pass the request-scoped db.ReadStore the same way the API wiring
// does. It serves exactly the three pinned point reads over nothing
// wider than the guarded ReadStore the satellite holds.
//
// The constructor delegates to the existing
// NewContentReaderWithReadStore deliberately instead of minting
// parallel wiring: the transport proof (fail-closed over a guarded
// ReadStore plus the full wire-object byte comparison against
// *ContentReader in the wire-shapes test) covers the satellite path
// end to end, and a second construction path would be a second place
// for the guarded handle to be dropped.
func NewContentPointReadAdmissionWithReadStore(readStore db.ReadStore) ContentPointReadAdmission {
	return NewContentReaderWithReadStore(readStore)
}

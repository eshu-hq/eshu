// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
)

// CodeLanguageQueryReadAdmissionContractVersion versions the #9669
// admission-contract shape for the W6c language-query read (POST
// /api/v0/code/language-query). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned graph reader and
// Postgres content store satisfy. A breaking change to the read shape
// fails the frozen method-set test until that list is deliberately
// updated; bump this version alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers can
// pin the shape.
const CodeLanguageQueryReadAdmissionContractVersion = "v0"

// CodeLanguageQueryReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of direct GraphQuery and
// ContentStore handles plus the two optional-interface upgrades the
// enrichment type-asserts. It re-declares exactly the four reads the W6c
// language-query route admits through — the bounded graph traversal
// (Run), the unscoped content-entity lookup
// (SearchEntitiesByLanguageAndType), the grant-bound upgrade
// (SearchEntitiesByLanguageAndTypeForAccess), and the id-anchored
// metadata upgrade (ListEntityMetadataByIDsForAccess) — so the boundary
// is held honest by the compile-time mirror assertion below plus the
// frozen method-set contract test; it will back the eshu-api extraction
// handoff for this family. The seam is read-only on purpose: every
// declared method is a bounded read and the port carries no mutation
// surface, so a dest consumer implementing only this contract can serve
// language-query reads but cannot acquire a write it did not bargain
// for through it.
//
// W-wave consumer: W6c language-query ONLY (per the arbiter supplemental
// ruling on eshu-hq/eshu-api#98, §2E). This pin lives in package
// language, not codequery: language.Handler mounts the route and
// codequery cannot mount it without an import cycle. Do not move the
// handler. The satellite records this pin in a follow-up; a satellite PR
// consuming an unminted pin is invalid. Arbiter review of this pin
// precedes satellite termination of the language-query leg.
//
// In scope: the one W6c language-query read. The route admits its reads
// through exactly this pin:
//
//   - POST /api/v0/code/language-query (Run for the graph-backed,
//     graph-first-content, and directory branches; the three content
//     reads for the content-only branch and the two enrichments)
//
// All four methods are IN because production satisfies all four: the
// mirror below compiles against the staying handler-facing ports, the
// production content reader implements the three content reads
// (proven at mint time), and the graph reader serves Run. Had production
// lacked any of the four, that method would have stayed out and the
// satellite would have kept the handler's existing fallback (per-repo
// loop or page read) — recorded in the seal, not silently dropped.
//
// The languageQueryMaxLimit coupling to
// taxonomy.MaxLanguageEntityIDLookup (both 200) is handler behavior: the
// route clamps the page before the id set reaches the anchored lookup,
// and the reader refuses a larger set rather than truncating it. The
// satellite must preserve both.
//
// Narrow by construction, recorded not silently assumed: the four
// methods re-declare the staying-port signatures in this
// language-scoped shape instead of embedding any source port as the
// exported surface or aliasing any port, so no other family can cite
// this pin — a blanket or shared-port pin is forbidden, and the widened
// shape needs arbiter review before satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - RunSingle and every other graph method: this pin adopts Run only.
//   - Every other ContentStore method (name, exact-name, component,
//     file, catalog reads): a new shared-port method is adopted only by
//     deliberate re-declaration plus a frozen-list update.
//   - The grant checks, the entity-type-family dispatch, the per-repo
//     fallback loop, the page-read fallback, the fixed failure messages,
//     and QueryProfile are handler behavior, not port shape: there is no
//     method set to freeze.
//   - No constructor, builder, selector, or ReadStore surface: the
//     satellite satisfies this port with its own wiring; this pin carries
//     the read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type CodeLanguageQueryReadAdmission interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
	SearchEntitiesByLanguageAndType(context.Context, string, string, string, string, int) ([]querycontract.EntityContent, error)
	SearchEntitiesByLanguageAndTypeForAccess(context.Context, taxonomy.LanguageEntitySearch) ([]querycontract.EntityContent, error)
	ListEntityMetadataByIDsForAccess(context.Context, taxonomy.LanguageEntityIDLookup) ([]querycontract.EntityContent, error)
}

// codeLanguageQueryReadAdmissionMirror keeps the language re-declaration
// honest about every one of its staying handler-facing sources in one
// assertion: it embeds the graph port, the content port, and the two
// grant-bound taxonomy search ports, so the promoted method set is their
// union, and the assignment below holds exactly while that union still
// serves this pin. Removal, rename, or re-signing of any of the four
// mirrored methods on any source port fails compilation here. It works in
// one direction only: a new method on any source port does not widen this
// pin — adoption takes deliberate re-declaration plus a frozen-list
// update. It pins no shared port — the assertion names GraphQuery,
// ContentStore, and the taxonomy searchers only as the shapes this pin
// mirrors, and no other family may cite this pin as coverage.
type codeLanguageQueryReadAdmissionMirror struct {
	querycontract.GraphQuery
	querycontract.ContentStore
	taxonomy.LanguageEntityContentSearcher
	taxonomy.LanguageEntityMetadataByIDReader
}

var _ CodeLanguageQueryReadAdmission = (*codeLanguageQueryReadAdmissionMirror)(nil)

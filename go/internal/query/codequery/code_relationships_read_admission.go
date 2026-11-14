// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
)

// CodeRelationshipReadAdmissionContractVersion versions the #9669 G-b
// admission-contract shape for the two relationship reads (POST
// /api/v0/code/relationships and POST
// /api/v0/code/relationships/story). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned graph and
// content stores satisfy. A breaking change to the read shape fails
// the frozen method-set test until that list is deliberately updated;
// bump this version alongside (manually — the bump is convention, the
// frozen list is the enforcement) so dest consumers can pin the shape.
const CodeRelationshipReadAdmissionContractVersion = "v0"

// CodeRelationshipReadAdmission is the versioned admission-contract
// port the API handler will depend on instead of direct GraphQuery and
// ContentStore handles. It re-declares exactly the seventeen reads the
// G-b two routes admit through — the bounded graph traversals (Run,
// RunSingle), the twelve ContentStore reads the resolution,
// repo-identity, enrichment, fallback, and builder-closure paths
// exercise, and the three grant-scoped reads the scoped callers take —
// so the boundary is held honest by the compile-time mirror assertions
// below plus the frozen method-set contract test; it will back the
// eshu-api extraction handoff for this family. The seam is read-only
// on purpose: every declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve relationship reads but cannot acquire a
// write it did not bargain for through it.
//
// Satellite export note: this export exists for the eshu-api
// extraction handoff the same way the G-a, Code-flow, and divergence
// pins do — the dest consumer names this port, so it carries the same
// standing as those pins, not a blanket new export.
//
// W-wave consumer: relationships/story G-b ONLY (per the arbiter
// SUFFICIENT ruling on eshu-hq/eshu-api#122, SPLIT). The satellite
// records this pin in a follow-up; a satellite PR consuming an
// unminted pin is invalid. Arbiter review of this pin precedes
// satellite termination of the G-b legs. G-a graph, divergence, flow,
// and every other family are NOT approved here.
//
// In scope: the G-b two relationship reads. Each of these routes
// admits the G-b reads it exercises through exactly this pin — the
// bounded graph traversal through Run and RunSingle, the entity and
// repository reads below, and the three grant-scoped reads scoped
// callers take through GetEntityContentInRepositories,
// SearchEntitiesByNameInRepositories, and
// SearchEntitiesByLanguageAndTypeForAccess:
//
//   - POST /api/v0/code/relationships
//   - POST /api/v0/code/relationships/story
//
// The grant three are IN, not OUT (per the ruling): two fail closed,
// so a pin satisfier missing them turns granted scoped reads into
// silent not-founds, and the third changes scoped language-story
// answers by fallback. That is an accuracy regression a pin must not
// license. Grant *checks* stay handler behavior OUT (G-a precedent);
// grant *reads* are store shape and belong in the frozen set.
//
// BuildContentRelationships is NOT a pin method. It is core logic;
// the pin freezes the four reads its narrowed port is now
// compiler-confined to (querycontract.ContentRelationshipReader:
// SearchEntitiesByName, SearchEntitiesByReferencingComponent,
// ListRepoFiles, ListRepoEntitiesByType, verbatim), and the assertion
// below proves those four signatures match this pin's. The assertion
// names ContentRelationshipReader only as the shape this pin covers
// for the builder leg, and no other family may cite this pin as
// coverage.
//
// Narrow by construction, recorded not silently assumed: the
// seventeen methods re-declare the staying-port signatures in this
// relationships-scoped shape instead of embedding GraphQuery,
// ContentStore, or ContentRelationshipReader, or aliasing any port,
// so no other family can cite this pin — a blanket or shared-port pin
// is forbidden, and the widened shape needs arbiter review before
// satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - BuildContentRelationships and every other content-relationship
//     builder surface: core logic, not port shape; the builder leg is
//     covered through the four narrowed reads above.
//   - Every other ContentStore method: GetFileContent, GetFileLines,
//     SearchFileContent, SearchFileContentAnyRepo,
//     SearchFileContentAnyRepoExactCase, SearchEntityContent,
//     SearchEntityContentAnyRepo, SearchCodeCandidates,
//     NonBlankRepoFilePaths, GetFileContents, ListRepoEntities,
//     ListRepoEntitiesByTypes, ListRepoEntitiesByPaths,
//     ListRepoEntitiesByKeys, ListRepoK8sSelectCandidates,
//     ListFrameworkRoutes, RepositoryCoverage,
//     CountRepositoriesByLanguage, ListRepositoriesByLanguage,
//     ReadRepositoriesByLanguage, RepositoryLanguageInventory,
//     MatchRepositoriesByIDs, ResolveRepository. A new shared-port
//     method is adopted only by deliberate re-declaration plus a
//     frozen-list update.
//   - The sibling families and their own contracts: G-a graph pin,
//     divergence pin, code-flow pin, content point/search pins,
//     complexity/quality, bundles, cypher/visualize,
//     structure/inventory, language-query, and graph/entities.
//   - The grant checks (codeGrantAccessFilter plus the scoped
//     repository selector), the unsupported-language short-circuit,
//     the fixed failure messages, and QueryProfile are handler
//     behavior, not port shape: there is no method set to freeze.
//   - No constructor, selector, or ReadStore surface: the satellite
//     satisfies this port with its own wiring; this pin carries the
//     read shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type CodeRelationshipReadAdmission interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
	GetEntityContent(context.Context, string) (*querycontract.EntityContent, error)
	SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)
	SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error)
	SearchEntitiesByExactName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)
	SearchEntitiesByExactNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error)
	SearchEntitiesByLanguageAndType(context.Context, string, string, string, string, int) ([]querycontract.EntityContent, error)
	SearchEntitiesReferencingComponent(context.Context, string, string, int) ([]querycontract.EntityContent, error)
	ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error)
	ListRepoEntitiesByType(context.Context, string, string, int) ([]querycontract.EntityContent, error)
	ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error)
	ListRepositories(context.Context) ([]querycontract.RepositoryCatalogEntry, error)
	MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)
	GetEntityContentInRepositories(context.Context, string, []string) (*querycontract.EntityContent, error)
	SearchEntitiesByNameInRepositories(context.Context, []string, string, string, int) ([]querycontract.EntityContent, error)
	SearchEntitiesByLanguageAndTypeForAccess(context.Context, taxonomy.LanguageEntitySearch) ([]querycontract.EntityContent, error)
}

// codeRelationshipReadAdmissionMirror keeps the relationships
// re-declaration honest about every one of its staying handler-facing
// sources in one assertion: it embeds the GraphQuery port, the
// ContentStore port, and the three grant-scoped read ports (the two
// relationship-grant ports plus the taxonomy language searcher), so
// the promoted method set is their union, and the assignment below
// holds exactly while that union still serves this pin. Removal,
// rename, or re-signing of any of the seventeen mirrored methods on
// any source port fails compilation here. It works in one direction
// only: a new method on any source port does not widen this pin —
// adoption takes deliberate re-declaration plus a frozen-list update.
// It pins no shared port — the assertion names GraphQuery,
// ContentStore, and the grant ports only as the shapes this pin
// mirrors, and no other family may cite this pin as coverage.
type codeRelationshipReadAdmissionMirror struct {
	querycontract.GraphQuery
	querycontract.ContentStore
	grantScopedEntityContentStore
	grantScopedEntityNameSearcher
	taxonomy.LanguageEntityContentSearcher
}

var _ CodeRelationshipReadAdmission = (*codeRelationshipReadAdmissionMirror)(nil)

// The ContentRelationshipReader four-shape assertion keeps the
// builder leg honest: the pin must keep serving every read the
// narrowed builder port is compiler-confined to. It works in one
// direction only — the pin carries thirteen more methods the builder
// never calls — so a builder-closure change still needs its own
// census, but a re-signing of any of the four shared reads fails
// compilation here on both sides at once.
var _ querycontract.ContentRelationshipReader = CodeRelationshipReadAdmission(nil)

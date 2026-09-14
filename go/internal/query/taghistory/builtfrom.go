// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taghistory

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// errBuiltFromKeyOverflow and errBuiltFromRowOverflow are the fixed messages a
// bound breach returns. They are deliberately count-free: the handler renders a
// read error into the 500 body verbatim, and both counts describe the raw
// pre-filter window across every tenant (#6564 re-review finding 4). The counts
// are logged at the breach instead.
var (
	errBuiltFromKeyOverflow = errors.New("tag history BUILT_FROM lookup exceeded its key bound")
	errBuiltFromRowOverflow = errors.New("tag history BUILT_FROM lookup exceeded its row bound")
)

// BuiltFromMaxKeys is the most digests one BUILT_FROM lookup can be keyed by:
// a window holds at most MaxLimit rows, each contributing a resolved_digest and
// a previous_digest.
const BuiltFromMaxKeys = 2 * MaxLimit

// BuiltFromMaxRows bounds the BUILT_FROM lookup's RESULT set, which its key
// count does not (#6564 review finding 2). A registered max_results equal to
// the key count was not a bound at all.
//
// The derivation depends on the DISTINCT in BuiltFromCypher and was re-stated
// when that word was added (#6564 re-review finding 2). Without it the
// statement returned one row per EDGE, and BUILT_FROM edge identity is
// {scope_id, evidence_source} (canonicalProvenanceBuiltFromCypher,
// go/internal/storage/cypher/provenance_edge_writer.go), so parallel edges for
// one image<->repository pair are the designed model: a deployment with two
// evidence sources across two scopes carries four edges per pair, so a MEASURED
// limit=200 page of 200 digests, each built from two repositories, returned
// 1600 rows -- over this bound, a 500 for the scoped caller only, on a page an
// unscoped caller reads fine (TestTagHistoryDistinctBoundsBuiltFromFanOut, RED
// with DISTINCT removed: rows=1600 keys=200 max_rows=1200).
// With DISTINCT the row set is one row per distinct (digest, repository) pair,
// so scope and evidence-source multiplicity no longer counts against it and the
// fan-out is genuinely the number of distinct source repositories per digest.
//
// The factor of three therefore now means what it says: up to three distinct
// source repositories per keyed digest. It mirrors the peer entry
// fetchOCIImagesByDigest, which registers 250 keys against 750 results, and
// leaves large headroom over the 1.1x distinct-repository fan-out measured on
// the seed. Overflow fails the read closed rather than truncating: a Cypher
// LIMIT would silently drop BUILT_FROM edges the caller is entitled to and
// could turn a granted row into a withheld one, which is a wrong answer rather
// than a bounded one.
const BuiltFromMaxRows = 3 * BuiltFromMaxKeys

// BuiltFromCypher resolves which Repository nodes each digest on one fetched
// window was built from, so a scoped caller's page can be bound to its
// repository grant (#6564). The observation node carries no source-repository
// key; the only path to a code repository is resolved_digest joined to
// ContainerImage.digest and then the reducer's
// ContainerImage-[:BUILT_FROM]->Repository edge
// (canonicalProvenanceBuiltFromCypher, go/internal/storage/cypher).
//
// The join runs in Go on purpose: a two-MATCH grant join returned zero rows on
// the pinned NornicDB and on upstream v1.3.1 for a seed whose answer was two
// rows, while this single-clause read returned exactly the seeded edges on
// both (docs/internal/evidence/6564-tag-history-grant-binding.md).
//
// Anchor: the container_image_digest index. Keys: the window's distinct
// non-empty resolved and previous digests, at most BuiltFromMaxKeys. Output:
// one row per distinct (digest, repository) pair on those digests, at most
// BuiltFromMaxRows. No LIMIT: truncating could drop a granted edge and silently
// withhold a row the caller is entitled to.
//
// DISTINCT is load-bearing, not cosmetic (#6564 re-review finding 2). BUILT_FROM
// edge identity is {scope_id, evidence_source}, so one image<->repository pair
// carries one edge per scope and evidence source and an undeduplicated RETURN
// fans out by that multiplicity -- enough for a full page in a two-source,
// two-scope deployment to exceed BuiltFromMaxRows and 500 a caller that is
// entitled to every row on it. The only consumer is anyRepositoryGranted, which
// needs set membership, so collapsing parallel edges drops no granted edge.
//
// The shape is the one NornicDB parses correctly: nothing follows the anchoring
// MATCH. RETURN DISTINCT is absorbed into the first projection's source text
// when a trailing OPTIONAL MATCH or a WITH sits between the MATCH and the
// RETURN (docs/public/reference/nornicdb-pitfalls.md), which is why
// BuildDeadCodeIncomingBatchProbeCypher keeps DISTINCT and its scoped sibling
// groups with count(*) instead. Do not add an OPTIONAL MATCH or a WITH here
// without re-measuring against the pin.
const BuiltFromCypher = `
	MATCH (i:ContainerImage)-[:BUILT_FROM]->(repo:Repository)
	WHERE i.digest IN $digests
	RETURN DISTINCT i.digest AS digest, repo.id AS repository_id
`

// GrantCounts tallies what one scoped page's grant filter did.
// WithheldUngranted rows had BUILT_FROM edges, none to a granted repository;
// WithheldUnattributed rows had no BUILT_FROM edge at all, which is the
// coverage cost an operator watches to see how much history a scoped caller
// cannot reach.
type GrantCounts struct {
	Kept                  int
	WithheldUngranted     int
	WithheldUnattributed  int
	PreviousDigestBlanked int
}

// LookupBuiltFromRepositories runs BuiltFromCypher for digests and returns each
// digest's BUILT_FROM repository ids. A digest absent from the map has no
// BUILT_FROM edge.
//
// Both registered bounds are enforced here rather than declared and hoped for
// (#6564 review finding 2): the key count against BuiltFromMaxKeys, and the
// distinct-pair fan-out against BuiltFromMaxRows. Overflow fails the read
// closed; the handler turns that into a 500 and never serves a page a silent
// Cypher LIMIT would have under-attributed.
//
// Neither overflow error carries its counts to the caller (#6564 re-review
// finding 4). Both counts are taken over the RAW pre-filter window, which spans
// every tenant's images on this image_ref, so putting them in an error body
// would disclose cross-tenant volume through the one channel the grant binding
// otherwise closes. The numbers an operator needs go to the log instead, where
// they carry no digest and no repository id.
func LookupBuiltFromRepositories(
	ctx context.Context,
	graph querycontract.GraphQuery,
	digests []string,
) (map[string][]string, error) {
	if len(digests) > BuiltFromMaxKeys {
		slog.ErrorContext(ctx, "tag history BUILT_FROM lookup exceeded its key bound",
			"keys", len(digests), "max_keys", BuiltFromMaxKeys)
		return nil, errBuiltFromKeyOverflow
	}
	rows, err := graph.Run(ctx, BuiltFromCypher, map[string]any{"digests": digests})
	if err != nil {
		return nil, err
	}
	if len(rows) > BuiltFromMaxRows {
		slog.ErrorContext(ctx, "tag history BUILT_FROM lookup exceeded its row bound",
			"rows", len(rows), "keys", len(digests), "max_rows", BuiltFromMaxRows)
		return nil, errBuiltFromRowOverflow
	}
	edges := make(map[string][]string, len(digests))
	for _, row := range rows {
		digest := querycontract.StringVal(row, "digest")
		repositoryID := querycontract.StringVal(row, "repository_id")
		if digest == "" || repositoryID == "" {
			continue
		}
		edges[digest] = append(edges[digest], repositoryID)
	}
	return edges, nil
}

// grantDecision applies the #6564 binding to one observation and tallies what
// it did. A row is visible only when the image at its resolved_digest is
// BUILT_FROM at least one granted repository; a visible row's previous_digest
// is blanked unless that digest's image is also BUILT_FROM a granted
// repository, so a scoped caller never learns another tenant's digest.
//
// Mutated is deliberately NOT rewritten when PreviousDigest is blanked: the row
// stays truthful about the observation the caller can see, and the disclosure
// that Mutated true still implies some prior digest existed is stated in the
// OpenAPI operation, the MCP tool description, and
// docs/public/reference/http-api.md rather than hidden by a lie in the payload.
func grantDecision(
	row Row,
	edges map[string][]string,
	access querycontract.RepositoryAccessFilter,
	counts *GrantCounts,
) (Row, bool) {
	repositoryIDs, attributed := edges[row.ResolvedDigest]
	if !attributed {
		counts.WithheldUnattributed++
		return Row{}, false
	}
	if !anyRepositoryGranted(repositoryIDs, access) {
		counts.WithheldUngranted++
		return Row{}, false
	}
	if row.PreviousDigest != "" && !anyRepositoryGranted(edges[row.PreviousDigest], access) {
		row.PreviousDigest = ""
		counts.PreviousDigestBlanked++
	}
	counts.Kept++
	return row, true
}

// anyRepositoryGranted reports whether at least one repository an image was
// built from is in the caller's grant.
func anyRepositoryGranted(repositoryIDs []string, access querycontract.RepositoryAccessFilter) bool {
	for _, repositoryID := range repositoryIDs {
		if access.AllowsRepositoryID(repositoryID) {
			return true
		}
	}
	return false
}

// grantDigests returns one window's distinct non-empty resolved and previous
// digests, sorted so the lookup's parameters are deterministic.
func grantDigests(window []WindowRow) []string {
	seen := make(map[string]struct{}, 2*len(window))
	for _, entry := range window {
		if entry.Row.ResolvedDigest != "" {
			seen[entry.Row.ResolvedDigest] = struct{}{}
		}
		if entry.Row.PreviousDigest != "" {
			seen[entry.Row.PreviousDigest] = struct{}{}
		}
	}
	digests := make([]string, 0, len(seen))
	for digest := range seen {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	return digests
}

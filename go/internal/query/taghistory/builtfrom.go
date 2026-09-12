// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taghistory

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// BuiltFromMaxKeys is the most digests one BUILT_FROM lookup can be keyed by:
// a window holds at most MaxLimit rows, each contributing a resolved_digest and
// a previous_digest.
const BuiltFromMaxKeys = 2 * MaxLimit

// BuiltFromMaxRows bounds the BUILT_FROM lookup's RESULT set, which its key
// count does not (#6564 review finding 2). BuiltFromCypher returns one row per
// BUILT_FROM edge, so a multi-source image contributes more rows than keys -- a
// 400-key probe returned 440 rows on the seeded corpus. A registered
// max_results equal to the key count was therefore not a bound at all.
//
// The factor of three mirrors the peer entry fetchOCIImagesByDigest, which
// registers 250 keys against 750 results, and leaves large headroom over the
// 1.1x fan-out measured on the seed. Overflow fails the read closed rather than
// truncating: a Cypher LIMIT would silently drop BUILT_FROM edges the caller is
// entitled to and could turn a granted row into a withheld one, which is a
// wrong answer rather than a bounded one.
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
// one row per BUILT_FROM edge on those digests, at most BuiltFromMaxRows. No
// LIMIT: truncating could drop a granted edge and silently withhold a row the
// caller is entitled to.
const BuiltFromCypher = `
	MATCH (i:ContainerImage)-[:BUILT_FROM]->(repo:Repository)
	WHERE i.digest IN $digests
	RETURN i.digest AS digest, repo.id AS repository_id
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
// fan-out against BuiltFromMaxRows, since one row per BUILT_FROM edge means a
// multi-source image returns more rows than keys. Overflow fails the read
// closed; the handler turns that into a 500 and never serves a page a silent
// Cypher LIMIT would have under-attributed.
func LookupBuiltFromRepositories(
	ctx context.Context,
	graph querycontract.GraphQuery,
	digests []string,
) (map[string][]string, error) {
	if len(digests) > BuiltFromMaxKeys {
		return nil, fmt.Errorf(
			"tag history BUILT_FROM lookup was keyed by %d digests, above the %d-key bound",
			len(digests), BuiltFromMaxKeys,
		)
	}
	rows, err := graph.Run(ctx, BuiltFromCypher, map[string]any{"digests": digests})
	if err != nil {
		return nil, err
	}
	if len(rows) > BuiltFromMaxRows {
		return nil, fmt.Errorf(
			"tag history BUILT_FROM lookup returned %d edges for %d digests, above the %d-row bound",
			len(rows), len(digests), BuiltFromMaxRows,
		)
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
func grantDigests(rows []Row) []string {
	seen := make(map[string]struct{}, 2*len(rows))
	for _, row := range rows {
		if row.ResolvedDigest != "" {
			seen[row.ResolvedDigest] = struct{}{}
		}
		if row.PreviousDigest != "" {
			seen[row.PreviousDigest] = struct{}{}
		}
	}
	digests := make([]string, 0, len(seen))
	for digest := range seen {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	return digests
}

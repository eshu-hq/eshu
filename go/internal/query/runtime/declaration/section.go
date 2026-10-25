// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// maxSectionRuntimes bounds the distinct runtimes one repository's context
// section lists. A repository declares a handful (node, python, go, ruby, and
// so on); the cap exists so a malformed graph cannot grow the response.
const maxSectionRuntimes = 50

// sectionCypher aggregates one repository's declarations by runtime. It seeks
// the Repository by its unique id and expands DECLARES_RUNTIME, so the read
// is bounded by that repository's own declarations, never the label.
// count(d.version_major) skips nulls, so it counts the declarations a version
// comparison can place; min() likewise ignores aliases, ranges and unresolved
// specs and is null when the runtime has none resolved.
const sectionCypher = `MATCH (repo:Repository {id: $repo_id})-[:DECLARES_RUNTIME]->(d:RuntimeDeclaration)
RETURN d.runtime AS runtime, count(d) AS declaration_count,
       count(d.version_major) AS resolved_count, min(d.version_major) AS lowest_resolved_major
ORDER BY runtime
LIMIT $limit`

// Section is the runtime_declarations block of the repository context
// response: how many runtime versions the repository declares and, per runtime,
// the lowest resolved major.
type Section struct {
	// Count is the number of declarations across the listed runtimes.
	Count int `json:"count"`
	// Runtimes lists the distinct declared runtimes in name order.
	Runtimes []RuntimeSummary `json:"runtimes"`
	// Truncated is true when the repository declares more runtimes than
	// Runtimes lists.
	Truncated bool `json:"truncated"`
}

// RuntimeSummary summarizes one runtime in a repository.
type RuntimeSummary struct {
	Runtime string `json:"runtime"`
	// DeclarationCount is every declaration of the runtime, aliases and ranges
	// included.
	DeclarationCount int `json:"declaration_count"`
	// ResolvedCount is the declarations with a resolved major (exact or
	// partial). DeclarationCount minus ResolvedCount is what a version
	// comparison cannot place.
	ResolvedCount int `json:"resolved_count"`
	// LowestResolvedMajor is the smallest resolved major, null when no
	// declaration of the runtime resolves to one.
	LowestResolvedMajor *int `json:"lowest_resolved_major"`
}

// LoadSection reads the runtime declaration section for one repository. The
// caller has already resolved and authorized repoID, so this applies no grant
// of its own. A failure is returned for the caller to degrade on with a named
// partial reason rather than render an empty section as authoritative.
func LoadSection(
	ctx context.Context,
	graph querycontract.GraphQuery,
	inst *telemetry.Instruments,
	repoID string,
) (Section, error) {
	started := time.Now()
	rows, err := graph.Run(ctx, sectionCypher, map[string]any{"repo_id": repoID, "limit": maxSectionRuntimes + 1})
	recordRead(ctx, inst, telemetry.RuntimeDeclarationOperationContextSection, started, err)
	if err != nil {
		return Section{Runtimes: []RuntimeSummary{}}, err
	}
	section := Section{Runtimes: make([]RuntimeSummary, 0, len(rows))}
	for _, row := range rows {
		if len(section.Runtimes) == maxSectionRuntimes {
			section.Truncated = true
			break
		}
		summary := RuntimeSummary{
			Runtime:             querycontract.StringVal(row, "runtime"),
			DeclarationCount:    querycontract.IntVal(row, "declaration_count"),
			ResolvedCount:       querycontract.IntVal(row, "resolved_count"),
			LowestResolvedMajor: nullableInt(row, "lowest_resolved_major"),
		}
		section.Count += summary.DeclarationCount
		section.Runtimes = append(section.Runtimes, summary)
	}
	return section, nil
}

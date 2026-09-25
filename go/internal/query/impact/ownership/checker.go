// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Checker decides, for a scoped caller, which nodes of a bounded traversal
// page the caller's grant owns. Route is the closed route pattern its metrics
// are labelled with (e.g. "POST /api/v0/impact/trace-resource-to-code").
// Instruments and Logger may be nil.
type Checker struct {
	Graph       querycontract.GraphQuery
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
	Route       string
}

// Verdict is the result of one Check. Its zero value admits nothing.
type Verdict struct {
	unscoped bool
	access   querycontract.RepositoryAccessFilter
	admitted map[Class]map[string]struct{}
	// unchecked holds the keys CheckedKeyCap left unchecked, by class.
	unchecked map[Class]map[string]struct{}
	capped    bool
}

// AllowAll returns the verdict for an unscoped caller: every node admitted.
func AllowAll() Verdict { return Verdict{unscoped: true} }

// Capped reports whether some statement-checked keys were left unchecked
// because the page exceeded CheckedKeyCap. Those keys are ungranted, so the
// caller must report its result as truncated.
func (v Verdict) Capped() bool { return v.capped }

// Admits reports whether the caller's grant owns node. Deny by default:
// an unrecognized class, an empty key, or an unchecked key is not admitted.
func (v Verdict) Admits(node Node) bool {
	if v.unscoped {
		return true
	}
	switch class := ClassOf(node.Labels); class {
	case ClassRepository:
		return node.ID != "" && v.access.AllowsRepositoryID(node.ID)
	case ClassRepoOwned:
		return node.RepoID != "" && v.access.AllowsRepositoryID(node.RepoID)
	case ClassWorkloadInstance:
		if node.RepoID != "" && v.access.AllowsRepositoryID(node.RepoID) {
			return true
		}
		return v.admittedKey(class, statementKey(class, node))
	case ClassCloudResource, ClassTerraformStateResource:
		return v.admittedKey(class, statementKey(class, node))
	default:
		return false
	}
}

// AdmitsAll reports whether every node is admitted. An empty slice is
// admitted (a caller decides separately whether an empty path means anything).
func (v Verdict) AdmitsAll(nodes []Node) bool {
	for _, node := range nodes {
		if !v.Admits(node) {
			return false
		}
	}
	return true
}

func (v Verdict) admittedKey(class Class, key string) bool {
	if key == "" {
		return false
	}
	_, ok := v.admitted[class][key]
	return ok
}

// GrantIDs returns the caller's deduplicated, sorted grant id union (repository
// ids and scope ids). Check sizes the cap and its capped-page log from it.
func GrantIDs(access querycontract.RepositoryAccessFilter) []string {
	seen := make(map[string]struct{}, len(access.AllowedRepositoryIDs)+len(access.AllowedScopeIDs))
	for _, ids := range [][]string{access.AllowedRepositoryIDs, access.AllowedScopeIDs} {
		for _, id := range ids {
			if id != "" {
				seen[id] = struct{}{}
			}
		}
	}
	for id := range access.Allowed {
		if id != "" {
			seen[id] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Check judges nodes for access. An unscoped caller is admitted without a
// graph call. An empty grant admits nothing and issues no graph call.
// Otherwise Repository and repo-owned nodes are judged in Go, and only the
// deduplicated keys of the statement-checked classes present on the page are
// sent to the graph, one class at a time, in chunks of ChunkSize, capped at
// CheckedKeyCap keys in first-seen order. A class with no keys issues no
// statement.
func (c Checker) Check(ctx context.Context, access querycontract.RepositoryAccessFilter, nodes []Node) (Verdict, error) {
	if !access.Scoped() {
		return AllowAll(), nil
	}
	verdict := Verdict{access: access, admitted: map[Class]map[string]struct{}{}}
	if access.Empty() {
		return verdict, nil
	}
	grantIDs := GrantIDs(access)
	limit := CheckedKeyCap(len(grantIDs))

	pending := map[Class][]string{}
	seen := map[Class]map[string]struct{}{}
	total := 0
	for _, node := range nodes {
		class := ClassOf(node.Labels)
		switch class {
		case ClassWorkloadInstance:
			if node.RepoID != "" && access.AllowsRepositoryID(node.RepoID) {
				continue
			}
		case ClassCloudResource, ClassTerraformStateResource:
		default:
			continue
		}
		key := statementKey(class, node)
		if key == "" {
			continue
		}
		if seen[class] == nil {
			seen[class] = map[string]struct{}{}
		}
		if _, dup := seen[class][key]; dup {
			continue
		}
		seen[class][key] = struct{}{}
		if total >= limit {
			verdict.capped = true
			if verdict.unchecked == nil {
				verdict.unchecked = map[Class]map[string]struct{}{}
			}
			if verdict.unchecked[class] == nil {
				verdict.unchecked[class] = map[string]struct{}{}
			}
			verdict.unchecked[class][key] = struct{}{}
			continue
		}
		total++
		pending[class] = append(pending[class], key)
	}
	if verdict.capped {
		c.logCapped(ctx, len(grantIDs), limit)
	}

	for _, class := range []Class{ClassWorkloadInstance, ClassCloudResource, ClassTerraformStateResource} {
		keys := pending[class]
		if len(keys) == 0 {
			continue
		}
		admitted, unchecked, err := c.runClass(ctx, access, class, keys)
		if err != nil {
			return Verdict{}, err
		}
		verdict.admitted[class] = admitted
		if len(unchecked) > 0 {
			verdict.capped = true
			if verdict.unchecked == nil {
				verdict.unchecked = map[Class]map[string]struct{}{}
			}
			verdict.unchecked[class] = unchecked
		}
	}
	return verdict, nil
}

// runClass runs one class's owner statement over keys in ChunkSize chunks
// and returns the keys whose owner repository the grant allows, plus the keys
// a chunk left undecided because it hit RowLimit (ungranted; the verdict
// reports Capped). A returned key that was not asked about is ignored, so a
// backend that over-returns cannot widen the grant, and a row with an empty
// owner id admits nothing.
func (c Checker) runClass(ctx context.Context, access querycontract.RepositoryAccessFilter, class Class, keys []string) (admitted, unchecked map[string]struct{}, err error) {
	asked := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		asked[key] = struct{}{}
	}
	admitted = map[string]struct{}{}
	cypher := statementFor(class)
	for start := 0; start < len(keys); start += ChunkSize {
		chunk := keys[start:min(start+ChunkSize, len(keys))]
		began := time.Now()
		rows, runErr := c.Graph.Run(ctx, cypher, map[string]any{"uids": chunk, "row_limit": RowLimit})
		c.recordDuration(ctx, class, time.Since(began), runErr)
		if runErr != nil {
			return nil, nil, fmt.Errorf("impact ownership check %s: %w", classMetricLabel(class), runErr)
		}
		for _, row := range rows {
			key := querycontract.StringVal(row, "uid")
			if _, ok := asked[key]; !ok {
				continue
			}
			if owner := querycontract.StringVal(row, "repo_id"); owner != "" && access.AllowsRepositoryID(owner) {
				admitted[key] = struct{}{}
			}
		}
		if len(rows) >= RowLimit {
			for _, key := range chunk {
				if _, ok := admitted[key]; !ok {
					if unchecked == nil {
						unchecked = map[string]struct{}{}
					}
					unchecked[key] = struct{}{}
				}
			}
		}
	}
	return admitted, unchecked, nil
}

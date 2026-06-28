// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedulereplay

import (
	"context"
	"fmt"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/offlinetier"
)

// WorkItem is one unit of recorded projection work delivered through the reducer
// queue. Each item carries the canonical nodes and edges its projection
// contributes. Items deliberately reference shared node keys (a child directory
// item's edge points at its parent item's node), so cross-item delivery order is
// a real conflict-key ordering scenario, not independent inserts.
type WorkItem struct {
	IntentID string
	Nodes    []Node
	Edges    []Edge
}

// Applier applies one work item's contribution to the shared graph. The
// canonical applier (ApplyCanonical) is idempotent and order-independent; tests
// supply a deliberately order-sensitive applier to prove the gate has teeth.
type Applier func(g *Graph, item WorkItem)

// ApplyCanonical idempotently upserts a work item's nodes and edges. Because both
// upserts key on identity, applying the same item twice, or applying items in
// any order, converges on the same graph — the production guarantee the gate
// asserts.
func ApplyCanonical(g *Graph, item WorkItem) {
	for _, n := range item.Nodes {
		g.UpsertNode(n)
	}
	for _, e := range item.Edges {
		g.UpsertEdge(e)
	}
}

// LoadWorkItems reads a committed cassette, materializes every recorded
// generation through the real offline-tier cassette->projector seam, and returns
// the per-entity work items for schedule replay. It fails loudly on a malformed
// cassette rather than returning an empty schedule that would look green.
func LoadWorkItems(cassettePath string) ([]WorkItem, error) {
	src, err := cassette.NewSource(cassettePath)
	if err != nil {
		return nil, fmt.Errorf("open cassette %q: %w", cassettePath, err)
	}
	var items []WorkItem
	for {
		gen, ok, err := src.Next(context.Background())
		if err != nil {
			return nil, fmt.Errorf("read cassette generation: %w", err)
		}
		if !ok {
			break
		}
		mat, err := offlinetier.MaterializationFromGeneration(gen)
		if err != nil {
			return nil, fmt.Errorf("materialize generation: %w", err)
		}
		genItems, err := WorkItemsFromMaterialization(mat)
		if err != nil {
			return nil, err
		}
		items = append(items, genItems...)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("cassette %q yielded no work items", cassettePath)
	}
	return items, nil
}

// WorkItemsFromMaterialization splits a canonical materialization into per-entity
// work items: one for the repository node and one per directory (its node plus
// the CONTAINS edge from its parent). The parent of a depth-0 directory is the
// repository node; deeper directories point at their parent directory node, so
// child items depend on parent items being applied — the #4019 ordering class.
func WorkItemsFromMaterialization(mat projector.CanonicalMaterialization) ([]WorkItem, error) {
	if mat.Repository == nil {
		return nil, fmt.Errorf("materialization for scope %q has no repository row", mat.ScopeID)
	}
	repoNode := Node{
		Label: "Repository",
		ID:    mat.Repository.RepoID,
		Props: map[string]string{
			"name": mat.Repository.Name,
			"path": mat.Repository.Path,
		},
	}
	items := []WorkItem{{IntentID: "repo:" + repoNode.ID, Nodes: []Node{repoNode}}}

	for _, dir := range mat.Directories {
		dirNode := Node{
			Label: "Directory",
			ID:    dir.Path,
			Props: map[string]string{
				"name":    dir.Name,
				"repo_id": dir.RepoID,
				"depth":   strconv.Itoa(dir.Depth),
			},
		}
		// Depth is the authoritative discriminant for "repo is the parent" vs
		// "a directory is the parent": a depth-0 directory hangs off the
		// Repository node, deeper directories off their parent Directory node.
		// (ParentPath equals mat.RepoPath at depth 0, but keying on Depth avoids
		// depending on a repo-root path-string convention.)
		fromKey := repoNode.Key()
		if dir.Depth != 0 {
			fromKey = Node{Label: "Directory", ID: dir.ParentPath}.Key()
		}
		items = append(items, WorkItem{
			IntentID: "dir:" + dir.Path,
			Nodes:    []Node{dirNode},
			Edges:    []Edge{{From: fromKey, Rel: "CONTAINS", To: dirNode.Key()}},
		})
	}
	return items, nil
}

// ScheduleInOrder returns the work items in their natural (repo-first,
// depth-ascending) order.
func ScheduleInOrder(items []WorkItem) []WorkItem {
	return cloneItems(items)
}

// ScheduleReverse returns the work items reversed — the adversarial
// child-before-parent order that breaks an order-sensitive projector.
func ScheduleReverse(items []WorkItem) []WorkItem {
	out := cloneItems(items)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// ScheduleRotated returns the work items rotated left by n positions, an
// interleaved order distinct from both in-order and reverse.
func ScheduleRotated(items []WorkItem, n int) []WorkItem {
	out := cloneItems(items)
	if len(out) == 0 {
		return out
	}
	n %= len(out)
	if n < 0 {
		n += len(out)
	}
	return append(out[n:], out[:n]...)
}

// ScheduleWithDuplicates returns every work item in order, then re-delivers the
// first and last items, modeling duplicate queue delivery of already-applied
// work. A correct idempotent projector converges identically.
func ScheduleWithDuplicates(items []WorkItem) []WorkItem {
	out := cloneItems(items)
	if len(items) == 0 {
		return out
	}
	out = append(out, items[0], items[len(items)-1])
	return out
}

func cloneItems(items []WorkItem) []WorkItem {
	out := make([]WorkItem, len(items))
	copy(out, items)
	return out
}

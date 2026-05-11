package golang

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// goParentLookup amortizes ancestor traversal cost from O(depth) per
// tree-sitter Parent() call to O(1) per lookup after a one-time O(n) build.
//
// Tree-sitter's Node.Parent() does not consult a stored parent pointer; the
// binding re-walks from the root via ts_node_child_with_descendant, which is
// O(depth) per call. Helpers in this package that loop on Parent() per
// identifier compounded that into O(depth^2) per identifier and
// O(n_identifiers * depth^2) per file, saturating CPU on repo-scale inputs
// without committing facts (see #161).
//
// Build the lookup once at the top of Parse(), then pass it to every helper
// that previously walked Parent() in a loop. The lookup is read-only after
// construction and safe to share for the lifetime of the underlying tree.
type goParentLookup struct {
	parents map[uintptr]*tree_sitter.Node
}

// goBuildParentLookup records every child->parent edge in one tree-sitter
// pass, so subsequent ancestor walks consult a Go map rather than re-entering
// cgo via ts_node_parent.
func goBuildParentLookup(root *tree_sitter.Node) *goParentLookup {
	lookup := &goParentLookup{parents: make(map[uintptr]*tree_sitter.Node)}
	if root == nil {
		return lookup
	}
	lookup.indexChildren(root)
	return lookup
}

func (l *goParentLookup) indexChildren(node *tree_sitter.Node) {
	if node == nil {
		return
	}
	count := node.ChildCount()
	for i := range count {
		child := node.Child(i)
		if child == nil {
			continue
		}
		l.parents[child.Id()] = node
		l.indexChildren(child)
	}
}

// Parent returns the recorded parent of node, or nil if node is the tree
// root or the lookup was not built. A nil receiver is treated as an empty
// lookup so callers can safely thread an optional pointer.
func (l *goParentLookup) Parent(node *tree_sitter.Node) *tree_sitter.Node {
	if l == nil || node == nil {
		return nil
	}
	return l.parents[node.Id()]
}

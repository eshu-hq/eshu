package javascript

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// javaScriptParentLookup amortizes ancestor traversal cost from O(depth) per
// tree-sitter Parent() call to O(1) per lookup after a one-time O(n) build.
//
// Tree-sitter's Node.Parent() does not consult a stored parent pointer; the
// binding re-walks from the root via ts_node_child_with_descendant, and every
// call crosses the cgo boundary into the C runtime. The JS/TS dead-code,
// export-surface, and semantic helpers loop on Parent() per declaration node to
// recover ancestor context (is the node under an export_statement, which class
// encloses a method, is a pair inside a CommonJS plugin object). On a
// full-corpus profile of JavaScript/TypeScript parsing that pattern made
// runtime.cgocall (driven by ts_node_parent) roughly half of all parse CPU,
// scaling as O(n_declarations * depth) cgo crossings per file (see #3586).
//
// Build the lookup once at the top of Parse(), then pass it to every helper
// that previously walked Parent() in a loop. The lookup is read-only after
// construction and safe to share for the lifetime of the underlying tree. It is
// not safe to share across trees or concurrent parses; each Parse() owns its
// own lookup, so concurrent parser workers never contend on it.
type javaScriptParentLookup struct {
	parents map[uintptr]*tree_sitter.Node
}

// buildJavaScriptParentLookup records every child->parent edge in one
// tree-sitter pass so subsequent ancestor walks consult a Go map rather than
// re-entering cgo via ts_node_parent. It indexes all children, named and
// unnamed, because Parent() chains traverse unnamed intermediate nodes such as
// export_statement wrappers. The DFS is iterative with an explicit slice stack
// so very deep trees (large generated bundles, deeply chained member
// expressions) cannot blow the goroutine stack.
func buildJavaScriptParentLookup(root *tree_sitter.Node) *javaScriptParentLookup {
	lookup := &javaScriptParentLookup{parents: make(map[uintptr]*tree_sitter.Node)}
	if root == nil {
		return lookup
	}
	stack := []*tree_sitter.Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count := node.ChildCount()
		for i := range count {
			child := node.Child(i)
			if child == nil {
				continue
			}
			lookup.parents[child.Id()] = node
			stack = append(stack, child)
		}
	}
	return lookup
}

// parent returns the recorded parent of node, or nil if node is the tree root
// or the lookup was not built. A nil receiver is treated as an empty lookup so
// callers can safely thread an optional pointer.
func (l *javaScriptParentLookup) parent(node *tree_sitter.Node) *tree_sitter.Node {
	if l == nil || node == nil {
		return nil
	}
	return l.parents[node.Id()]
}

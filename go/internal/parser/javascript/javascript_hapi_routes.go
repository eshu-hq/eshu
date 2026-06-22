package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaScriptHapiRouteObjects returns every route-shaped object node in root: an
// object that declares both a Hapi method and path, and does not itself contain
// a deeper route-shaped object. Isolating routes by AST containment keeps a
// nested config: { handler } block attached to its owning route object and
// prevents a wrapper function body or route array from being read as one route
// (correlation-truth, #2788).
func javaScriptHapiRouteObjects(root *tree_sitter.Node, source []byte) []*tree_sitter.Node {
	if root == nil {
		return nil
	}
	shaped := make([]*tree_sitter.Node, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "object" && javaScriptHapiObjectIsRouteShaped(node, source) {
			shaped = append(shaped, node)
		}
	})
	objects := make([]*tree_sitter.Node, 0, len(shaped))
	for _, candidate := range shaped {
		if javaScriptHapiContainsNestedRoute(candidate, shaped) {
			continue
		}
		objects = append(objects, candidate)
	}
	return objects
}

// javaScriptHapiObjectIsRouteShaped reports whether an object node declares both
// a Hapi method and a Hapi path pair, the minimal signal for a route object.
func javaScriptHapiObjectIsRouteShaped(object *tree_sitter.Node, source []byte) bool {
	method, path, ok := javaScriptHapiObjectMethodPath(object, source)
	return ok && method != "" && path != ""
}

// javaScriptHapiContainsNestedRoute reports whether candidate strictly contains a
// different route-shaped object, i.e. candidate is a wrapper (function body or
// route array) rather than a single route object.
func javaScriptHapiContainsNestedRoute(candidate *tree_sitter.Node, shaped []*tree_sitter.Node) bool {
	start := candidate.StartByte()
	end := candidate.EndByte()
	for _, other := range shaped {
		if other.Id() == candidate.Id() {
			continue
		}
		if other.StartByte() > start && other.EndByte() <= end {
			return true
		}
	}
	return false
}

// javaScriptHapiRouteEntries preserves the observed method/path pairing for Hapi
// route objects, including routes with nested config blocks, and binds the
// route's handler symbol when the object declares an unambiguous bare named
// handler. Each route object is isolated by AST containment, so a handler is
// only ever read from the same object that owns the method/path. A handler from
// a neighbouring route object can never be mis-attached (correlation-truth,
// #2788).
func javaScriptHapiRouteEntries(root *tree_sitter.Node, source []byte) []map[string]string {
	objects := javaScriptHapiRouteObjects(root, source)
	entries := make([]map[string]string, 0, len(objects))
	for _, object := range objects {
		method, path, ok := javaScriptHapiObjectMethodPath(object, source)
		if !ok {
			continue
		}
		entries = append(entries, routeEntry(method, path, javaScriptHapiObjectHandler(object, source)))
	}
	return entries
}

// javaScriptHapiObjectMethodPath extracts the method and path declared directly
// on a route object's pairs. It returns ok=false when either is missing so a
// config object that is not a route is skipped rather than emitted blank. Only
// string-literal values are accepted, and the path must be rooted at "/".
func javaScriptHapiObjectMethodPath(object *tree_sitter.Node, source []byte) (string, string, bool) {
	if object == nil || object.Kind() != "object" {
		return "", "", false
	}
	method := ""
	path := ""
	cursor := object.Walk()
	defer cursor.Close()
	for _, child := range object.NamedChildren(cursor) {
		child := child
		if child.Kind() != "pair" {
			continue
		}
		key := javaScriptHapiPairKey(&child, source)
		value, ok := javaScriptHapiPairStringValue(&child, source)
		if !ok {
			continue
		}
		switch key {
		case "method":
			if method == "" {
				method = strings.TrimSpace(value)
			}
		case "path":
			if path == "" {
				path = strings.TrimSpace(value)
			}
		}
	}
	if method == "" || path == "" || !strings.HasPrefix(path, "/") {
		return "", "", false
	}
	return method, path, true
}

// javaScriptHapiObjectHandler returns the bare named handler for a route object,
// or "" when the handler is inline, absent, or otherwise not a single named
// reference. The handler pair may sit directly on the route object or inside its
// nested config object; both are searched, but a deeper route-shaped object is
// never descended into so a sibling route's handler cannot leak in (#2788).
func javaScriptHapiObjectHandler(object *tree_sitter.Node, source []byte) string {
	if object == nil {
		return ""
	}
	cursor := object.Walk()
	defer cursor.Close()
	for _, child := range object.NamedChildren(cursor) {
		child := child
		if child.Kind() != "pair" {
			continue
		}
		key := javaScriptHapiPairKey(&child, source)
		valueNode := child.ChildByFieldName("value")
		if valueNode == nil {
			continue
		}
		if key == "handler" {
			if valueNode.Kind() == "identifier" {
				return strings.TrimSpace(nodeText(valueNode, source))
			}
			continue
		}
		// Recurse into a nested config object that is not itself a route object so
		// the config: { handler } form binds to its owning route.
		if valueNode.Kind() == "object" && !javaScriptHapiObjectIsRouteShaped(valueNode, source) {
			if handler := javaScriptHapiObjectHandler(valueNode, source); handler != "" {
				return handler
			}
		}
	}
	return ""
}

// javaScriptHapiPairKey returns the property name of a pair node, reading the
// key from an identifier, property_identifier, or string literal.
func javaScriptHapiPairKey(pair *tree_sitter.Node, source []byte) string {
	keyNode := pair.ChildByFieldName("key")
	if keyNode == nil {
		return ""
	}
	switch keyNode.Kind() {
	case "property_identifier", "identifier", "private_property_identifier":
		return strings.TrimSpace(nodeText(keyNode, source))
	case "string":
		return strings.TrimSpace(jsStringLiteralValue(keyNode, source))
	default:
		return strings.TrimSpace(nodeText(keyNode, source))
	}
}

// javaScriptHapiPairStringValue returns the string-literal value of a pair, with
// ok=false when the value is not a plain string literal.
func javaScriptHapiPairStringValue(pair *tree_sitter.Node, source []byte) (string, bool) {
	valueNode := pair.ChildByFieldName("value")
	if valueNode == nil || valueNode.Kind() != "string" {
		return "", false
	}
	return jsStringLiteralValue(valueNode, source), true
}

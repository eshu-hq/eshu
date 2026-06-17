package javascript

import (
	"regexp"
	"strings"
)

// javaScriptHapiHandlerRe binds a Hapi route to its handler only when the
// handler value is a single bare named identifier (e.g. handler: createOrder).
// It requires the value to terminate at a comma, closing brace, newline, or end
// of segment, so an inline function (handler: (req, h) => ... or
// handler: function (...) {...}) never matches and stays unbound (#2788). It
// matches both the top-level handler and the nested config: { handler: ... }
// form because both place the handler key inside the same route object. The
// identifier is intentionally bare (no dotted member access), matching the
// Express slice so a qualified handler.method reference, which downstream
// Function resolution cannot bind exactly, stays unbound rather than guessed.
var javaScriptHapiHandlerRe = regexp.MustCompile(`\bhandler\s*:\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*(?:[,}\n]|$)`)

// javaScriptHapiRouteEntries preserves the observed method/path pairing for Hapi
// route objects, including routes with nested config blocks, and binds the
// route's handler symbol when the object declares an unambiguous bare named
// handler. Each route object is isolated by brace depth before extraction, so a
// handler is only ever read from the same object that owns the method/path. A
// handler from a neighbouring route object can never be mis-attached
// (correlation-truth, #2788).
func javaScriptHapiRouteEntries(source string) []map[string]string {
	objects := javaScriptHapiRouteObjects(source)
	entries := make([]map[string]string, 0, len(objects))
	for _, object := range objects {
		method, path, ok := javaScriptHapiObjectMethodPath(object)
		if !ok {
			continue
		}
		entries = append(entries, routeEntry(method, path, javaScriptHapiObjectHandler(object)))
	}
	return entries
}

// javaScriptHapiObjectMethodPath extracts the method and path declared on a
// single route object. It returns ok=false when either is missing so a config
// object that is not a route is skipped rather than emitted with a blank field.
func javaScriptHapiObjectMethodPath(object string) (string, string, bool) {
	methodMatch := javaScriptHapiMethodRe.FindStringSubmatch(object)
	pathMatch := javaScriptHapiPathRe.FindStringSubmatch(object)
	if len(methodMatch) < 2 || len(pathMatch) < 2 {
		return "", "", false
	}
	method := strings.TrimSpace(methodMatch[1])
	path := strings.TrimSpace(pathMatch[1])
	if method == "" || path == "" || !strings.HasPrefix(path, "/") {
		return "", "", false
	}
	return method, path, true
}

// javaScriptHapiObjectHandler returns the bare named handler for a single route
// object, or "" when the handler is inline, absent, or otherwise not a single
// named reference. Because object is one isolated route object, the handler can
// only belong to that route (#2788).
func javaScriptHapiObjectHandler(object string) string {
	match := javaScriptHapiHandlerRe.FindStringSubmatch(object)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

// braceBlock is the body text of one balanced { ... } block and its nesting
// depth, used to pick the route objects out of wrapper functions and arrays.
type braceBlock struct {
	body  string
	depth int
}

// javaScriptHapiRouteObjects splits Hapi route source into the text of each
// route object. It walks every balanced { ... } block (tracking string and
// template literals so braces inside quotes are ignored), then keeps only the
// route-shaped blocks: those that declare both method and path but do NOT
// themselves contain a deeper route-shaped block. This isolates each route
// object even when it is wrapped in a registerRoutes(server) { ... } function
// or an array, and keeps a nested config: { handler } block attached to its
// owning route object rather than treating the wrapper as a single route
// (correlation-truth, #2788).
func javaScriptHapiRouteObjects(source string) []string {
	blocks := javaScriptBraceBlocks(source)
	objects := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if !javaScriptHapiRouteShaped(block.body) {
			continue
		}
		// A route object is the innermost route-shaped block: it must not contain
		// a deeper block that is itself route-shaped (which would be a sibling
		// route object captured separately). Wrapper functions and route arrays
		// contain multiple route-shaped children and are filtered out here.
		if javaScriptHapiContainsNestedRoute(block, blocks) {
			continue
		}
		objects = append(objects, block.body)
	}
	return objects
}

// javaScriptHapiRouteShaped reports whether a block body declares both a Hapi
// method and a Hapi path, the minimal signal for a route object.
func javaScriptHapiRouteShaped(body string) bool {
	return javaScriptHapiMethodRe.MatchString(body) && javaScriptHapiPathRe.MatchString(body)
}

// javaScriptHapiContainsNestedRoute reports whether block strictly contains a
// different route-shaped block, i.e. block is a wrapper (function body or route
// array) rather than a single route object.
func javaScriptHapiContainsNestedRoute(block braceBlock, blocks []braceBlock) bool {
	for _, other := range blocks {
		if other.depth <= block.depth {
			continue
		}
		if other.body != block.body && strings.Contains(block.body, other.body) && javaScriptHapiRouteShaped(other.body) {
			return true
		}
	}
	return false
}

// javaScriptBraceBlocks returns every balanced { ... } block body in source,
// each tagged with its brace depth. String and template literals are skipped so
// braces inside quotes do not affect balancing.
func javaScriptBraceBlocks(source string) []braceBlock {
	blocks := make([]braceBlock, 0)
	starts := make([]int, 0)
	depth := 0
	var quote byte
	inString := false
	for i := 0; i < len(source); i++ {
		c := source[i]
		if inString {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				inString = false
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			inString = true
			quote = c
		case '{':
			depth++
			starts = append(starts, i+1)
		case '}':
			if len(starts) > 0 {
				start := starts[len(starts)-1]
				starts = starts[:len(starts)-1]
				blocks = append(blocks, braceBlock{body: source[start:i], depth: depth})
				depth--
			}
		}
	}
	return blocks
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// GoImportedInterfaceParamMethods maps lower-case function names, or qualified
// import-path/function keys, to the imported interface methods required by each
// parameter index. An empty method list means the value escapes to an imported
// package interface without a known method set, so exported methods on the
// concrete type are valid runtime hooks.
type GoImportedInterfaceParamMethods map[string]map[int][]string

// GoDirectMethodCallRoots maps qualified lower-case Go method declarations to
// root kinds observed from imported-package selector calls.
type GoDirectMethodCallRoots map[string][]string

// GoPackageSemanticRoots maps absolute Go package directories to package-level
// semantic contracts discovered before per-file parsing.
type GoPackageSemanticRoots map[string]GoPackageSemanticRootOptions

// GoPackageSemanticRootOptions carries Go reachability facts that require a
// repository or package pre-scan before parsing individual files.
type GoPackageSemanticRootOptions struct {
	ImportedInterfaceParamMethods GoImportedInterfaceParamMethods
	DirectMethodCallRoots         GoDirectMethodCallRoots
	ImportPath                    string
}

// Options configures one parser execution.
type Options struct {
	IndexSource                     bool
	VariableScope                   string
	GoImportedInterfaceParamMethods GoImportedInterfaceParamMethods
	GoDirectMethodCallRoots         GoDirectMethodCallRoots
	GoPackageImportPath             string
	// RepositoryID is the stable, generation-independent repository identity used
	// by value-flow FunctionIDs when EmitDataflow is enabled.
	RepositoryID string
	// EmitDataflow opts the parser into emitting per-function control-flow and
	// reaching-definition facts, taint findings, interprocedural findings, and
	// durable summary effects for value-flow-capable languages that support each
	// bucket. Off by default; the payload is byte-identical to before this feature
	// when off.
	EmitDataflow bool
}

// NormalizedVariableScope returns the canonical scope used by language
// adapters that can choose between module-level and full local-variable output.
func (o Options) NormalizedVariableScope() string {
	scope := strings.TrimSpace(strings.ToLower(o.VariableScope))
	if scope == "all" {
		return "all"
	}
	return "module"
}

// BasePayload returns the common parser payload fields and empty buckets shared
// by source-language adapters.
func BasePayload(path string, lang string, isDependency bool) map[string]any {
	return map[string]any{
		"path":           path,
		"lang":           lang,
		"is_dependency":  isDependency,
		"functions":      []map[string]any{},
		"classes":        []map[string]any{},
		"variables":      []map[string]any{},
		"imports":        []map[string]any{},
		"function_calls": []map[string]any{},
	}
}

// sourceCacheEntry holds one path's primed bytes plus the number of
// in-flight ParsePath calls currently relying on them. refs lets two or more
// concurrent calls on the SAME absolute path safely share one primed
// snapshot: the entry is only removed once every primer has cleared it.
type sourceCacheEntry struct {
	body []byte
	refs int
}

// sourceCache holds bytes primed by PrimeSource for the duration of one or
// more concurrent Engine.ParsePath calls on the same absolute path. It lets
// the language parser invoked through parseDefinition and the engine's
// post-parse content-metadata inference share one physical disk read instead
// of each reading the file independently.
//
// Concurrency contract (torn-read safety): sourceCache is process-global and
// keyed by absolute path, so two concurrent ParsePath calls on the same path
// prime and clear the same entry. PrimeSource follows first-writer-wins --
// the first goroutine to prime a path's bytes owns that snapshot for as long
// as any goroutine still holds a reference, and later PrimeSource calls for
// the same path only increment refs rather than replacing the body. ClearSource
// decrements refs and deletes the entry only when the last reference is
// released. This guarantees every concurrent same-path parse observes ONE
// consistent snapshot: within one ParsePath call, the language parser's read
// and the content-metadata read always see the same bytes, and no goroutine
// can delete another in-flight goroutine's entry early. Without this
// contract, a plain last-writer-wins map lets one call's PrimeSource
// overwrite another's bytes, or one call's ClearSource delete an entry a
// sibling call still needs -- producing a payload that mixes two different
// versions of the same nominal file (a torn read) if the file changed
// between the two callers' physical reads. This is strictly safer than the
// pre-single-read-cache code, which could already observe two different file
// versions within one ParsePath call if the file changed mid-parse; the
// cache must not make that hazard worse across concurrent same-path calls.
//
// All access to sourceCacheEntries goes through sourceCacheMu; there is no
// lock-free fast path, since entries are small maps mutated under low
// contention (one entry per concurrently-parsed path, held only for the
// duration of a single file parse).
var (
	sourceCacheMu      sync.Mutex
	sourceCacheEntries = map[string]*sourceCacheEntry{}
)

// readSourceHook, when non-nil, observes every physical disk read performed by
// ReadSource. It exists only so tests can count real os.ReadFile calls without
// changing ReadSource's signature; production code never sets it. Guarded by
// readSourceHookMu so -race sees no data race between a test installing the
// hook and ReadSource invoking it.
var (
	readSourceHookMu sync.Mutex
	readSourceHook   func(path string)
)

// PrimeSource stores pre-read bytes for path so the next ReadSource(path)
// call returns them without touching disk. It follows first-writer-wins
// semantics: if an entry for path already exists (another concurrent
// ParsePath call on the same path primed it first), this call only
// increments that entry's refcount and keeps the existing body -- it never
// overwrites already-cached bytes. This guarantees every concurrent
// same-path caller observes one consistent snapshot instead of a torn mix of
// two callers' reads. Callers MUST pair this with ClearSource once the parse
// that needed the shared read completes, so the entry is released promptly
// and does not leak into an unrelated later call on the same path.
func PrimeSource(path string, body []byte) {
	sourceCacheMu.Lock()
	defer sourceCacheMu.Unlock()

	if entry, ok := sourceCacheEntries[path]; ok {
		entry.refs++
		return
	}
	sourceCacheEntries[path] = &sourceCacheEntry{body: body, refs: 1}
}

// ClearSource releases one reference primed for path. The cache entry is
// deleted only once its refcount reaches zero, so a goroutine finishing its
// ParsePath call cannot delete an entry a sibling goroutine (still parsing
// the same path concurrently) still relies on. Safe to call even when no
// entry was primed.
func ClearSource(path string) {
	sourceCacheMu.Lock()
	defer sourceCacheMu.Unlock()

	entry, ok := sourceCacheEntries[path]
	if !ok {
		return
	}
	entry.refs--
	if entry.refs <= 0 {
		delete(sourceCacheEntries, path)
	}
}

// ReadSource reads one parser input file and wraps the path into read errors.
// When PrimeSource already cached this exact path's bytes, ReadSource returns
// the cached slice instead of issuing a second os.ReadFile, so a single
// ParsePath call reads a file's contents from disk at most once regardless of
// how many internal consumers (the language parser plus content-metadata
// inference) need the bytes. A cache miss never populates the cache -- only
// PrimeSource does that -- so ReadSource callers outside a primed ParsePath
// call always see a real, current disk read.
func ReadSource(path string) ([]byte, error) {
	if cached, ok := cachedSource(path); ok {
		return cached, nil
	}
	readSourceHookMu.Lock()
	hook := readSourceHook
	readSourceHookMu.Unlock()
	if hook != nil {
		hook(path)
	}
	body, err := os.ReadFile(path) // #nosec G304 -- reads an indexed repository source file at a path derived from the scan target
	if err != nil {
		return nil, fmt.Errorf("read source %q: %w", path, err)
	}
	return body, nil
}

// cachedSource returns the primed body for path, if any, under the shared
// cache lock.
func cachedSource(path string) ([]byte, bool) {
	sourceCacheMu.Lock()
	defer sourceCacheMu.Unlock()

	entry, ok := sourceCacheEntries[path]
	if !ok {
		return nil, false
	}
	return entry.body, true
}

// sourceCacheEntryForTest returns the raw cached body for path, if present.
// Test-only: it lets tests assert entry presence/absence directly instead of
// only inferring cache state through ReadSource's cache-or-disk fallback.
func sourceCacheEntryForTest(path string) ([]byte, bool) {
	return cachedSource(path)
}

// SetReadSourceHookForTest installs a hook invoked on every physical
// os.ReadFile performed by ReadSource (cache hits are not observed), and
// returns a restore function that must be deferred to reset the hook.
// Test-only: production code never calls this. Callers must not run this
// test in parallel with any other test that also installs the hook, since
// the hook is process-global.
func SetReadSourceHookForTest(hook func(path string)) func() {
	readSourceHookMu.Lock()
	previous := readSourceHook
	readSourceHook = hook
	readSourceHookMu.Unlock()
	return func() {
		readSourceHookMu.Lock()
		readSourceHook = previous
		readSourceHookMu.Unlock()
	}
}

// WalkNamed visits a node and every named descendant in source order.
func WalkNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if node == nil {
		return
	}

	visit(node)

	cursor := node.Walk()
	defer cursor.Close()
	walkNamedChildren(cursor, visit)
}

// walkNamedChildren streams named direct children through one cursor instead
// of allocating a NamedChildren slice and a new cursor for every visited node.
func walkNamedChildren(cursor *tree_sitter.TreeCursor, visit func(*tree_sitter.Node)) {
	if !cursor.GotoFirstChild() {
		return
	}
	defer cursor.GotoParent()

	for {
		child := cursor.Node()
		if child.IsNamed() {
			visit(child)
			walkNamedChildren(cursor, visit)
		}
		if !cursor.GotoNextSibling() {
			return
		}
	}
}

// NodeText returns the source slice covered by a tree-sitter node.
func NodeText(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return node.Utf8Text(source)
}

// NodeLine returns the 1-based start line for a tree-sitter node.
func NodeLine(node *tree_sitter.Node) int {
	if node == nil {
		return 1
	}
	return int(node.StartPosition().Row) + 1
}

// NodeEndLine returns the 1-based end line for a tree-sitter node.
func NodeEndLine(node *tree_sitter.Node) int {
	if node == nil {
		return 1
	}
	return int(node.EndPosition().Row) + 1
}

// CloneNode returns a stable pointer copy for callers that need to keep a node
// after cursor iteration advances.
func CloneNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cloned := *node
	return &cloned
}

// AppendBucket appends one row to a parser payload bucket.
func AppendBucket(payload map[string]any, key string, item map[string]any) {
	items, _ := payload[key].([]map[string]any)
	payload[key] = append(items, item)
}

// SortNamedBucket sorts a payload bucket by its string name field.
func SortNamedBucket(payload map[string]any, key string) {
	items, _ := payload[key].([]map[string]any)
	SortNamedMaps(items)
	payload[key] = items
}

// SortNamedMaps sorts parser payload rows by their string name field.
func SortNamedMaps(values []map[string]any) {
	slices.SortFunc(values, func(left, right map[string]any) int {
		if delta := IntValue(left["line_number"]) - IntValue(right["line_number"]); delta != 0 {
			return delta
		}
		leftName, _ := left["name"].(string)
		rightName, _ := right["name"].(string)
		return strings.Compare(leftName, rightName)
	})
}

// CollectBucketNames returns cleaned non-empty name values from parser payload
// buckets in caller-provided bucket order.
func CollectBucketNames(payload map[string]any, keys ...string) []string {
	var names []string
	for _, key := range keys {
		items, _ := payload[key].([]map[string]any)
		for _, item := range items {
			name, _ := item["name"].(string)
			if strings.TrimSpace(name) != "" {
				names = append(names, filepath.Clean(name))
			}
		}
	}
	return names
}

// IntValue converts common JSON and parser numeric values to int.
func IntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}

// LastPathSegment returns the final non-empty segment split by separator.
func LastPathSegment(name string, separator string) string {
	parts := strings.Split(strings.TrimSpace(name), separator)
	for i := len(parts) - 1; i >= 0; i-- {
		if segment := strings.TrimSpace(parts[i]); segment != "" {
			return segment
		}
	}
	return strings.TrimSpace(name)
}

// DedupeNonEmptyStrings returns sorted unique non-empty strings.
func DedupeNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

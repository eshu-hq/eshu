// Package python extracts Python parser evidence behind the parent engine's
// Python dispatch methods.
//
// Parse reads .py and .ipynb inputs, runs tree-sitter with a caller-owned parser,
// and returns the payload buckets consumed by source collection and query truth:
// declarations, imports, calls, annotations, framework metadata, and
// dead-code root hints, including cached properties, module dunder hooks, and
// nested dunder protocol hooks with same-scope assignment evidence. PreScan uses
// the same adapter path for import-map discovery. NotebookSource preserves the
// notebook code-cell invariant so notebook parsing cannot index markdown, raw
// cells, or partial JSON.
package python

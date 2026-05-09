// Package python extracts Python parser evidence behind the parent engine's
// Python dispatch methods.
//
// Parse reads .py and .ipynb inputs, runs tree-sitter with a caller-owned parser,
// and returns the payload buckets consumed by source collection and query truth.
// PreScan uses the same adapter path for import-map discovery. NotebookSource
// preserves the notebook code-cell invariant so notebook parsing cannot index
// markdown, raw cells, or partial JSON.
package python

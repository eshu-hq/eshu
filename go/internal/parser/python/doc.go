// Package python extracts Python parser evidence that can stay independent from
// the parent parser dispatch package.
//
// The package currently owns Jupyter notebook source extraction. NotebookSource
// accepts notebook JSON bytes, returns only executable code cells, and leaves
// temporary-file creation and tree-sitter parsing to the parent parser package.
package python

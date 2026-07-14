// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NotebookSource extracts executable Python code from a Jupyter notebook JSON
// document. Non-code cells and blank code cells are ignored, and the remaining
// code cells are joined with a blank line so tree-sitter sees a normal Python
// source stream.
func NotebookSource(source []byte) (string, error) {
	var notebook map[string]any
	if err := json.Unmarshal(source, &notebook); err != nil {
		return "", fmt.Errorf("decode notebook json: %w", err)
	}

	cells, _ := notebook["cells"].([]any)
	if len(cells) == 0 {
		return "", nil
	}

	codeCells := make([]string, 0, len(cells))
	for _, rawCell := range cells {
		cell, ok := rawCell.(map[string]any)
		if !ok {
			continue
		}
		if !strings.EqualFold(fmt.Sprint(cell["cell_type"]), "code") {
			continue
		}
		cellSource := notebookCellSource(cell["source"])
		if strings.TrimSpace(cellSource) == "" {
			continue
		}
		codeCells = append(codeCells, cellSource)
	}
	return strings.Join(codeCells, "\n\n"), nil
}

// notebookPythonSource converts a .ipynb payload into synthesized Python
// source for tree-sitter to parse directly, in memory. It replaces the
// pre-#4874 behavior of writing the converted source to a temp file and
// reading it back before parsing: that disk round trip added write, read, and
// remove syscalls to every notebook parse for no behavioral difference, since
// tree-sitter's Parser.Parse accepts a []byte source directly.
func notebookPythonSource(path string, source []byte) ([]byte, error) {
	code, err := NotebookSource(source)
	if err != nil {
		return nil, fmt.Errorf("convert notebook %q: %w", path, err)
	}
	return []byte(code), nil
}

func notebookCellSource(raw any) string {
	switch typed := raw.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, "")
	case []string:
		return strings.Join(typed, "")
	default:
		return fmt.Sprint(raw)
	}
}

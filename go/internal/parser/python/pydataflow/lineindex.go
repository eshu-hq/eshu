// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pydataflow

import "github.com/eshu-hq/eshu/go/internal/parser/cfg"

// lineIndex maps source lines to CFG statement IDs so tree-sitter nodes can be
// matched to the statements the lowering produced. Resolution is by source line,
// which is exact for idiomatic one-statement-per-line Python; ambiguous lines
// fall back to the first matching statement.
type lineIndex struct {
	defByLine map[int]map[string]int
	useByLine map[int]int
}

// newLineIndex builds the line index from a resolved function CFG.
func newLineIndex(fn cfg.Function) *lineIndex {
	index := &lineIndex{defByLine: map[int]map[string]int{}, useByLine: map[int]int{}}
	for _, block := range fn.Blocks {
		for _, stmt := range block.Stmts {
			for _, def := range stmt.Defs {
				byBinding := index.defByLine[stmt.Line]
				if byBinding == nil {
					byBinding = map[string]int{}
					index.defByLine[stmt.Line] = byBinding
				}
				if _, exists := byBinding[def]; !exists {
					byBinding[def] = stmt.ID
				}
			}
			if len(stmt.Uses) > 0 {
				if _, exists := index.useByLine[stmt.Line]; !exists {
					index.useByLine[stmt.Line] = stmt.ID
				}
			}
		}
	}
	return index
}

// defStmt returns the statement ID that defines a binding on a line.
func (l *lineIndex) defStmt(line int, binding string) (int, bool) {
	byBinding, ok := l.defByLine[line]
	if !ok {
		return 0, false
	}
	stmtID, ok := byBinding[binding]
	return stmtID, ok
}

// useStmt returns the first statement on a line that uses any binding.
func (l *lineIndex) useStmt(line int) (int, bool) {
	stmtID, ok := l.useByLine[line]
	return stmtID, ok
}

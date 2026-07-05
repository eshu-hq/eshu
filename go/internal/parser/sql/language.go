// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// sqlParseSegmentTimeout bounds one tree-sitter parse call so an unforeseen
// pathological segment cannot hang the collector. Tests must rely on the
// maxSQLSegmentBytes size cap for determinism; this timeout is defense in
// depth only, not the primary bound.
//
// tree-sitter's context-cancellation entry point (Parser.ParseCtx) is
// deprecated and crashes on a parser that has never had a cancellation flag
// set (ts_parser_cancellation_flag returns NULL, so the cancel goroutine
// dereferences a nil *uintptr) -- observed directly on this branch. The
// supported replacement is Parser.ParseWithOptions with a ProgressCallback,
// which tree-sitter polls periodically during parsing and which can cancel
// by returning true; that path is used here instead.
const sqlParseSegmentTimeout = 15 * time.Second

// Parse extracts SQL schema objects, relationships, and migration metadata from
// one source file using a tree-sitter SQL grammar.
//
// The file is segmented into statement-sized fragments and each fragment is
// parsed in isolation so a single malformed statement cannot lose its
// neighbours. CREATE PROCEDURE, which the grammar does not parse, is recovered
// by a bounded rewrite to CREATE FUNCTION before parsing. All entity, column,
// routine, trigger, index, and relationship extraction walks the resulting
// abstract syntax tree; no SQL DDL regular expressions are used.
//
// A segment larger than maxSQLSegmentBytes is bounded before it reaches
// tree-sitter: its dollar-quoted bodies are elided, or, if still oversized,
// its tree-sitter parse is skipped entirely. Either bound is recorded in the
// returned payload's "sql_parse_bounded" bucket and logged, so a dropped
// routine body is observable rather than silently missing.
func Parse(
	path string,
	isDependency bool,
	options Options,
	parser *tree_sitter.Parser,
) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"path":              path,
		"sql_tables":        []map[string]any{},
		"sql_columns":       []map[string]any{},
		"sql_views":         []map[string]any{},
		"sql_functions":     []map[string]any{},
		"sql_triggers":      []map[string]any{},
		"sql_indexes":       []map[string]any{},
		"sql_relationships": []map[string]any{},
		"sql_migrations":    []map[string]any{},
		"sql_parse_bounded": []map[string]any{},
		"is_dependency":     isDependency,
		"lang":              "sql",
	}

	extractor := &sqlExtractor{
		payload:   payload,
		source:    source,
		lineIndex: newSQLLineIndex(source),
		options:   options,
		seenEntities: map[string]map[string]struct{}{
			"sql_tables":    {},
			"sql_columns":   {},
			"sql_views":     {},
			"sql_functions": {},
			"sql_triggers":  {},
			"sql_indexes":   {},
		},
		seenRelationships: make(map[string]struct{}),
	}

	for _, segment := range splitSQLStatements(string(source)) {
		extractor.parseSegment(segment, parser, path)
	}

	payload["sql_migrations"] = buildSQLMigrationEntries(path, extractor.lineIndex, payload, extractor.tableMentions)

	for _, bucket := range []string{
		"sql_tables",
		"sql_columns",
		"sql_views",
		"sql_functions",
		"sql_triggers",
		"sql_indexes",
		"sql_relationships",
		"sql_migrations",
	} {
		sortSQLBucket(payload, bucket)
	}
	return payload, nil
}

// parseSegment parses one statement segment and extracts its entities. The
// segment offset is recorded so node positions map back to original source line
// numbers. CREATE PROCEDURE segments are rewritten to CREATE FUNCTION so the
// grammar can parse them, and the recovered routine is flagged as a procedure.
//
// A segment larger than maxSQLSegmentBytes is bounded before it reaches
// tree-sitter (#4422): an opaque dollar-quoted routine body of that size
// parses superlinearly and can hard-crash the process via a tree-sitter
// error-recovery assertion. The interior of every dollar-quoted body in the
// segment is elided first, which preserves the routine signature for
// extraction; if the segment is still oversized afterward (a pathological
// non-dollar-quoted statement), the tree-sitter parse is skipped entirely for
// it. Either bound is recorded in payload["sql_parse_bounded"] and logged so
// the dropped facts are observable, never silent. A parse deadline
// (sqlParseSegmentTimeout) is defense in depth for any segment that reaches
// tree-sitter despite the size bound.
func (x *sqlExtractor) parseSegment(segment sqlSegment, parser *tree_sitter.Parser, path string) {
	segmentText := segment.text
	if len(segmentText) > maxSQLSegmentBytes {
		// Over cap: try eliding dollar-quoted body interiors. Record exactly one
		// action — body_elided only when the elided form is actually parseable
		// (fits the cap), otherwise segment_skipped (no dollar body to elide, or
		// still oversized after elision).
		if bounded, edited := elideOversizedDollarQuotedBodies(segmentText); edited && len(bounded) <= maxSQLSegmentBytes {
			x.recordBoundedSegment(path, segment, "body_elided")
			segmentText = bounded
		} else {
			x.recordBoundedSegment(path, segment, "segment_skipped")
			return
		}
	}

	text, isProcedure, edits := rewriteProcedureSegment(segmentText)

	x.segmentOffset = segment.offset
	x.procedure = isProcedure
	x.originalSegment = segment.text
	x.segmentEdits = edits

	parsed := []byte(text)
	deadline := time.Now().Add(sqlParseSegmentTimeout)
	tree := parser.ParseWithOptions(func(offset int, _ tree_sitter.Point) []byte {
		if offset < len(parsed) {
			return parsed[offset:]
		}
		return nil
	}, nil, &tree_sitter.ParseOptions{
		ProgressCallback: func(tree_sitter.ParseState) bool {
			return time.Now().After(deadline)
		},
	})
	if tree == nil {
		// A cancelled/timed-out ParseWithOptions leaves the parser positioned to
		// resume the aborted parse on its next call; Parse reuses this parser for
		// every segment in the file, so reset it to keep a later segment's parse
		// clean (go-tree-sitter Parser.Reset).
		parser.Reset()
		return
	}
	defer tree.Close()

	root := tree.RootNode()
	visitStatementConstructs(root, parsed, x.dispatchStatement)

	// Accumulate every bounded table mention in this segment for migration
	// metadata, remapping offsets back to the original source.
	for _, mention := range collectMentionsFromNode(root, parsed, true) {
		mention.offset = x.segmentOffset + mention.offset
		x.tableMentions = append(x.tableMentions, mention)
	}
}

// recordBoundedSegment appends a sql_parse_bounded payload row for one
// bounded segment and emits a matching structured log line so a dropped
// routine body or skipped segment is observable rather than silent.
func (x *sqlExtractor) recordBoundedSegment(path string, segment sqlSegment, action string) {
	event := sqlBoundedSegmentEvent{
		path:          path,
		segmentOffset: segment.offset,
		originalBytes: len(segment.text),
		action:        action,
	}
	appendBucket(x.payload, "sql_parse_bounded", event.row())
	slog.Warn("sql parse segment bounded",
		"component", "parser.sql",
		"path", event.path,
		"segment_offset", event.segmentOffset,
		"original_bytes", event.originalBytes,
		"action", event.action,
	)
}

// visitStatementConstructs invokes visit for every statement construct node in
// the tree (create_table, create_view, alter_table, and the rest). It descends
// through wrapper nodes so a construct nested under `statement`, `block`, or an
// ERROR recovery node is still discovered.
func visitStatementConstructs(
	node *tree_sitter.Node,
	source []byte,
	visit func(node *tree_sitter.Node, src []byte),
) {
	for _, child := range namedChildren(node) {
		if _, ok := sqlStatementKinds[child.GrammarName()]; ok {
			visit(child, source)
			continue
		}
		visitStatementConstructs(child, source, visit)
	}
}

// sqlEdit records one substring replacement applied to a segment during the
// procedure rewrite. position is the byte offset in the rewritten buffer where
// the replacement text begins; delta is len(new) - len(old). Edits let routine
// extraction map a rewritten node span back to the original source so the
// indexed snippet is the real CREATE PROCEDURE text.
type sqlEdit struct {
	position int
	delta    int
}

// rewriteProcedureSegment rewrites a leading CREATE [OR REPLACE] PROCEDURE
// header into CREATE FUNCTION ... RETURNS void so the grammar can parse it,
// returning the rewritten text, whether a rewrite occurred, and the ordered
// edits applied (rewritten-buffer position and length delta). Non-procedure
// segments are returned unchanged with no edits. The rewrite is a bounded
// keyword/clause transform, not a data-extraction regex: the routine name,
// arguments, body, and language clause are preserved verbatim for AST
// extraction.
func rewriteProcedureSegment(text string) (string, bool, []sqlEdit) {
	upper := strings.ToUpper(text)
	createIndex := strings.Index(upper, "CREATE")
	if createIndex < 0 {
		return text, false, nil
	}
	procedureIndex := indexOfKeyword(upper, "PROCEDURE", createIndex)
	if procedureIndex < 0 {
		return text, false, nil
	}

	edits := make([]sqlEdit, 0, 2)

	// Replace the PROCEDURE keyword with FUNCTION, preserving surrounding text.
	rewritten := text[:procedureIndex] + "FUNCTION" + text[procedureIndex+len("PROCEDURE"):]
	edits = append(edits, sqlEdit{position: procedureIndex, delta: len("FUNCTION") - len("PROCEDURE")})

	// Insert "RETURNS void" after the argument list close paren that follows
	// the routine name, when the routine does not already declare RETURNS.
	if argsClose := matchingArgumentClose(rewritten, procedureIndex); argsClose >= 0 {
		upperRewritten := strings.ToUpper(rewritten)
		if indexOfKeyword(upperRewritten, "RETURNS", argsClose) < 0 {
			insertion := " RETURNS void"
			rewritten = rewritten[:argsClose+1] + insertion + rewritten[argsClose+1:]
			edits = append(edits, sqlEdit{position: argsClose + 1, delta: len(insertion)})
		}
	}
	return rewritten, true, edits
}

// indexOfKeyword returns the byte index of keyword in upperText at or after
// from, requiring word boundaries so it does not match substrings of
// identifiers. keyword must already be upper-case. Returns -1 when not found.
func indexOfKeyword(upperText string, keyword string, from int) int {
	for offset := from; offset >= 0 && offset < len(upperText); {
		found := strings.Index(upperText[offset:], keyword)
		if found < 0 {
			return -1
		}
		position := offset + found
		if isKeywordBoundary(upperText, position, len(keyword)) {
			return position
		}
		offset = position + len(keyword)
	}
	return -1
}

func isKeywordBoundary(text string, position int, length int) bool {
	if position > 0 {
		prev := text[position-1]
		if isWordByte(prev) {
			return false
		}
	}
	end := position + length
	if end < len(text) {
		next := text[end]
		if isWordByte(next) {
			return false
		}
	}
	return true
}

func isWordByte(b byte) bool {
	return b == '_' ||
		(b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9')
}

// matchingArgumentClose returns the index of the close paren of the routine
// argument list that follows the routine name beginning at headerStart, or -1
// when no balanced argument list is present.
func matchingArgumentClose(text string, headerStart int) int {
	open := strings.IndexByte(text[headerStart:], '(')
	if open < 0 {
		return -1
	}
	open += headerStart
	depth := 0
	for index := open; index < len(text); index++ {
		switch text[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func sortSQLBucket(payload map[string]any, key string) {
	items, _ := payload[key].([]map[string]any)
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		leftLine := shared.IntValue(left["line_number"])
		rightLine := shared.IntValue(right["line_number"])
		if leftLine != rightLine {
			return leftLine < rightLine
		}
		return fmt.Sprint(left["name"]) < fmt.Sprint(right["name"])
	})
	payload[key] = items
}

// appendBucket appends one row to a payload bucket, allocating the slice on
// first use. It mirrors shared.AppendBucket for the SQL payload buckets.
func appendBucket(payload map[string]any, bucket string, item map[string]any) {
	shared.AppendBucket(payload, bucket, item)
}

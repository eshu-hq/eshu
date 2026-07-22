// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"sort"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// sqlSourceTablesCap bounds the number of source tables stamped onto a
// view/function entity's source_tables metadata. Kept consistent with the
// other bounded-collection caps in the parser package (e.g.
// interproc.maxFindingTrailPorts = 64) so payloads and golden-corpus fixtures
// stay stable for pathological fan-out queries.
const sqlSourceTablesCap = 64

// sqlExtractor carries the mutable extraction state shared across the
// per-statement handlers for one SQL file.
type sqlExtractor struct {
	payload           map[string]any
	source            []byte
	lineIndex         sqlLineIndex
	options           Options
	seenEntities      map[string]map[string]struct{}
	seenRelationships map[string]struct{}
	segmentOffset     int
	procedure         bool
	// originalSegment is the un-rewritten text the current segment was parsed
	// from. When a CREATE PROCEDURE rewrite was applied, it differs from the
	// buffer the AST nodes index into, and segmentEdits maps a rewritten node
	// span back into it so IndexSource persists the real procedure source rather
	// than the synthetic CREATE FUNCTION rewrite.
	originalSegment string
	segmentEdits    []sqlEdit
	// tableMentions accumulates bounded table references across all segments,
	// with offsets remapped to the original source. Migration metadata uses
	// these to record tables a migration file touches via DML or references.
	tableMentions []sqlMention
}

// dispatchStatement routes one top-level statement construct node to its
// dedicated extractor based on grammar node kind. src is the buffer the node
// was parsed from, which is one statement segment of the original file.
func (x *sqlExtractor) dispatchStatement(node *tree_sitter.Node, src []byte) {
	switch node.GrammarName() {
	case "create_table":
		x.parseTable(node, src)
	case "create_view":
		x.parseView(node, src, "view")
	case "create_materialized_view":
		x.parseView(node, src, "materialized")
	case "create_function":
		x.parseRoutine(node, src)
	case "create_index":
		x.parseIndex(node, src)
	case "create_trigger":
		x.parseTrigger(node, src)
	case "alter_table":
		x.parseAlterTable(node, src)
	}
}

// lineFor maps a node position in the current segment back to the original
// source line by adding the segment's starting byte offset.
func (x *sqlExtractor) lineFor(node *tree_sitter.Node, _ []byte) int {
	return x.lineIndex.lineForOffset(x.segmentOffset + int(node.StartByte()))
}

// originalLineForOffset maps a byte offset within the current segment back to
// the original source line.
func (x *sqlExtractor) originalLineForOffset(segmentOffset int) int {
	return x.lineIndex.lineForOffset(x.segmentOffset + segmentOffset)
}

func (x *sqlExtractor) parseTable(node *tree_sitter.Node, src []byte) {
	name := objectReferenceName(node, src)
	line := x.lineFor(node, src)
	item := map[string]any{
		"name":            name,
		"line_number":     line,
		"type":            "content_entity",
		"sql_entity_type": "SqlTable",
		"schema":          sqlSchema(name),
		"qualified_name":  name,
	}

	definitions := firstChildByKind(node, "column_definitions")
	var referencedTables []string
	if definitions != nil {
		for _, child := range namedChildren(definitions) {
			switch child.GrammarName() {
			case "column_definition":
				if target := x.appendColumn(name, child, src); target != "" {
					referencedTables = append(referencedTables, target)
				}
			case "constraints":
				referencedTables = append(referencedTables, x.parseTableConstraints(name, child, src)...)
			}
		}
	}
	if targets := boundedSQLTargetNames(referencedTables); len(targets) > 0 {
		// referenced_tables is the durable parser-to-reducer bridge for FK
		// table-to-table REFERENCES_TABLE edges (#5410).
		item["referenced_tables"] = targets
	}
	x.appendEntity("sql_tables", name, item, node, src)
}

func (x *sqlExtractor) appendColumn(tableName string, definition *tree_sitter.Node, src []byte) string {
	columnNode := firstDirectChildByKind(definition, "identifier")
	if columnNode == nil {
		return ""
	}
	columnName := normalizeSQLName(nodeText(columnNode, src))
	if columnName == "" {
		return ""
	}
	dataType := sqlColumnDataType(definition, columnNode, src)
	qualified := tableName + "." + columnName
	line := x.lineFor(definition, src)
	x.appendEntity("sql_columns", qualified, map[string]any{
		"name":            qualified,
		"line_number":     line,
		"type":            "content_entity",
		"sql_entity_type": "SqlColumn",
		"table_name":      tableName,
		"column_name":     columnName,
		"data_type":       dataType,
	}, definition, src)
	x.appendRelationship("HAS_COLUMN", tableName, qualified, line)

	// Inline REFERENCES (foreign key) on the column emits a table relationship.
	if ref := inlineColumnReference(definition); ref != nil {
		target := objectReferenceName(ref, src)
		x.appendRelationship("REFERENCES_TABLE", tableName, target, line)
		return target
	}
	return ""
}

func (x *sqlExtractor) parseTableConstraints(tableName string, constraints *tree_sitter.Node, src []byte) []string {
	var targets []string
	for _, mention := range collectConstraintReferences(constraints, src) {
		x.appendRelationship("REFERENCES_TABLE", tableName, mention.name,
			x.originalLineForOffset(mention.offset))
		targets = append(targets, mention.name)
	}
	return targets
}

func (x *sqlExtractor) parseView(node *tree_sitter.Node, src []byte, kind string) {
	name := objectReferenceName(node, src)
	line := x.lineFor(node, src)
	item := map[string]any{
		"name":            name,
		"line_number":     line,
		"type":            "content_entity",
		"sql_entity_type": "SqlView",
		"schema":          sqlSchema(name),
		"qualified_name":  name,
	}
	if kind != "view" {
		item["view_kind"] = kind
	}

	mentions := collectMentionsFromNode(node, src, true)
	if sourceTables := selectReadTargets(mentions); len(sourceTables) > 0 {
		// source_tables bridges the parser to the reducer's view/function
		// READS_FROM derivation, which keys on entity_metadata.source_tables
		// (#5345). Without this stamp the edge dies between parser and
		// reducer even though the READS_FROM relationship below is emitted.
		item["source_tables"] = sourceTables
	}
	x.appendEntity("sql_views", name, item, node, src)

	for _, mention := range mentions {
		if mention.operation != "select" {
			continue
		}
		x.appendRelationship("READS_FROM", name, mention.name,
			x.originalLineForOffset(mention.offset))
	}
}

func (x *sqlExtractor) parseRoutine(node *tree_sitter.Node, src []byte) {
	name := objectReferenceName(node, src)
	line := x.lineFor(node, src)
	routineKind := "function"
	if x.procedure {
		routineKind = "procedure"
	}
	item := map[string]any{
		"name":              name,
		"line_number":       line,
		"type":              "content_entity",
		"sql_entity_type":   "SqlFunction",
		"schema":            sqlSchema(name),
		"qualified_name":    name,
		"function_language": sqlRoutineLanguage(node, src),
	}
	if routineKind != "function" {
		item["routine_kind"] = routineKind
	}

	body := firstChildByKind(node, "function_body")
	var mentions []sqlMention
	if body != nil {
		mentions = collectMentionsFromNode(body, src, true)
	}
	if sourceTables := selectReadTargets(mentions); len(sourceTables) > 0 {
		// See the matching comment in parseView: this stamp is the bridge the
		// reducer's READS_FROM derivation reads (#5345).
		item["source_tables"] = sourceTables
	}
	if writeTables := routineWriteTargets(mentions); len(writeTables) > 0 {
		// write_tables keeps INSERT/UPDATE/DELETE targets distinct from
		// source_tables so the reducer emits WRITES_TO, never a false read.
		item["write_tables"] = writeTables
	}
	x.appendEntity("sql_functions", name, item, node, src)

	if body == nil {
		return
	}
	for _, mention := range mentions {
		switch mention.operation {
		case "select":
			x.appendRelationship("READS_FROM", name, mention.name,
				x.originalLineForOffset(mention.offset))
		case "insert", "update", "delete":
			x.appendRelationship("WRITES_TO", name, mention.name,
				x.originalLineForOffset(mention.offset))
		}
	}
}

// selectReadTargets returns the deduplicated, sorted, capped set of names
// mentioned as "select" (read) targets. Used to stamp view/function entity
// metadata with source_tables so the reducer can derive READS_FROM edges
// without re-parsing SQL (#5345).
func selectReadTargets(mentions []sqlMention) []string {
	var names []string
	for _, mention := range mentions {
		if mention.operation != "select" {
			continue
		}
		names = append(names, mention.name)
	}
	return boundedSQLTargetNames(names)
}

// routineWriteTargets returns the bounded INSERT/UPDATE/DELETE target set for
// one routine body. SELECT mentions remain exclusively in source_tables.
func routineWriteTargets(mentions []sqlMention) []string {
	var names []string
	for _, mention := range mentions {
		switch mention.operation {
		case "insert", "update", "delete":
			names = append(names, mention.name)
		}
	}
	return boundedSQLTargetNames(names)
}

// boundedSQLTargetNames deduplicates, sorts, and caps entity metadata targets
// so FK, read, and write fan-out stays deterministic and payload-bounded.
func boundedSQLTargetNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, name)
	}
	sort.Strings(unique)
	if len(unique) > sqlSourceTablesCap {
		unique = unique[:sqlSourceTablesCap]
	}
	return unique
}

func (x *sqlExtractor) parseIndex(node *tree_sitter.Node, src []byte) {
	indexName := sqlIndexName(node, src)
	tableName := sqlIndexTable(node, src)
	line := x.lineFor(node, src)
	x.appendEntity("sql_indexes", indexName, map[string]any{
		"name":            indexName,
		"line_number":     line,
		"type":            "content_entity",
		"sql_entity_type": "SqlIndex",
		"table_name":      tableName,
	}, node, src)
	x.appendRelationship("INDEXES", indexName, tableName, line)
}

func (x *sqlExtractor) parseTrigger(node *tree_sitter.Node, src []byte) {
	references := childObjectReferences(node)
	if len(references) < 2 {
		return
	}
	triggerName := objectReferenceName(references[0], src)
	tableName := objectReferenceName(references[1], src)
	functionName := ""
	if len(references) >= 3 {
		functionName = objectReferenceName(references[len(references)-1], src)
	}
	line := x.lineFor(node, src)
	x.appendEntity("sql_triggers", triggerName, map[string]any{
		"name":            triggerName,
		"line_number":     line,
		"type":            "content_entity",
		"sql_entity_type": "SqlTrigger",
		"table_name":      tableName,
		"function_name":   functionName,
	}, node, src)
	x.appendRelationship("TRIGGERS_ON", triggerName, tableName, line)
	if functionName != "" {
		x.appendRelationship("EXECUTES", triggerName, functionName, line)
	}
}

func (x *sqlExtractor) parseAlterTable(node *tree_sitter.Node, src []byte) {
	tableName := objectReferenceName(node, src)
	for _, child := range namedChildren(node) {
		if child.GrammarName() != "add_column" {
			continue
		}
		definition := firstChildByKind(child, "column_definition")
		if definition == nil {
			continue
		}
		columnNode := firstDirectChildByKind(definition, "identifier")
		if columnNode == nil {
			continue
		}
		columnName := normalizeSQLName(nodeText(columnNode, src))
		if columnName == "" {
			continue
		}
		dataType := sqlColumnDataType(definition, columnNode, src)
		qualified := tableName + "." + columnName
		line := x.lineFor(definition, src)
		x.appendEntity("sql_columns", qualified, map[string]any{
			"name":            qualified,
			"line_number":     line,
			"type":            "content_entity",
			"sql_entity_type": "SqlColumn",
			"table_name":      tableName,
			"column_name":     columnName,
			"data_type":       dataType,
		}, definition, src)
		x.appendRelationship("HAS_COLUMN", tableName, qualified, line)
	}
}

func (x *sqlExtractor) appendEntity(
	bucket string,
	name string,
	item map[string]any,
	node *tree_sitter.Node,
	src []byte,
) {
	if strings.TrimSpace(name) == "" {
		return
	}
	if _, ok := x.seenEntities[bucket][name]; ok {
		return
	}
	x.seenEntities[bucket][name] = struct{}{}
	if x.options.IndexSource {
		item["source"] = x.entitySource(node, src)
	}
	appendBucket(x.payload, bucket, item)
}

// entitySource returns the source snippet stored under IndexSource for an
// entity node. When the current segment was rewritten (CREATE PROCEDURE to
// CREATE FUNCTION), the node spans the rewritten buffer, so the span is mapped
// back to the original segment text to persist the real procedure source. When
// the mapped span is out of range the verbatim node text is used as a safe
// fallback.
func (x *sqlExtractor) entitySource(node *tree_sitter.Node, src []byte) string {
	if len(x.segmentEdits) == 0 {
		return nodeText(node, src)
	}
	start := x.mapToOriginalOffset(int(node.StartByte()))
	end := x.mapToOriginalOffset(int(node.EndByte()))
	if start < 0 || end > len(x.originalSegment) || start > end {
		return nodeText(node, src)
	}
	return x.originalSegment[start:end]
}

// mapToOriginalOffset maps a byte offset in the rewritten segment buffer back to
// the corresponding offset in the original segment text by subtracting the
// cumulative length delta of every rewrite edit that begins at or before the
// offset. Procedure-rewrite edits all sit in the header, before the routine
// body, so entity span endpoints fall outside the edited spans and map exactly.
func (x *sqlExtractor) mapToOriginalOffset(offset int) int {
	mapped := offset
	for _, edit := range x.segmentEdits {
		if edit.position <= offset {
			mapped -= edit.delta
		}
	}
	return mapped
}

func (x *sqlExtractor) appendRelationship(
	relationshipType string,
	sourceName string,
	targetName string,
	lineNumber int,
) {
	if strings.TrimSpace(sourceName) == "" || strings.TrimSpace(targetName) == "" {
		return
	}
	key := relationshipType + "|" + sourceName + "|" + targetName
	if _, ok := x.seenRelationships[key]; ok {
		return
	}
	x.seenRelationships[key] = struct{}{}
	appendBucket(x.payload, "sql_relationships", map[string]any{
		"type":        relationshipType,
		"source_name": sourceName,
		"target_name": targetName,
		"line_number": lineNumber,
	})
}

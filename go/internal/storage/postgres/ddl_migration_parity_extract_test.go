// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"regexp"
	"strings"
)

// This file holds the extraction half of the Go-DDL versus numbered-migration
// parity guard (#7792): it turns SQL text into a schema inventory. It reads
// no files, so the seeded cases in ddl_migration_parity_cases_test.go drive
// it with synthetic SQL.

// ddlSource is one SQL text to scan: a numbered migration file or one Go
// string literal. line is the 1-based line where the text starts, and scope
// names the Go declaration that holds the literal (empty for migrations).
type ddlSource struct {
	file  string
	scope string
	line  int
	sql   string
}

// ddlSite names where a DDL statement starts, for failure messages and
// allowlist keys.
type ddlSite struct {
	file  string
	scope string
	line  int
}

// String formats the site as file:line.
func (s ddlSite) String() string { return fmt.Sprintf("%s:%d", s.file, s.line) }

// ddlColumn is one (table, column) pair after identifier folding.
type ddlColumn struct{ table, column string }

// ddlIndex records the table a named index is built on.
type ddlIndex struct {
	table string
	site  ddlSite
}

// ddlObject is a named constraint or trigger (scoped to its table) or a
// function (table empty).
type ddlObject struct{ kind, table, name string }

// ddlTempTable scopes a temporary table to the file that creates it, since a
// temporary table only exists in the session that made it.
type ddlTempTable struct{ file, table string }

// ddlUnresolved is a DDL statement the scan cannot fully read: a fmt verb, a
// name or clause joined on at runtime, or an unnamed index. head is the
// statement text up to the unreadable part, whitespace-collapsed.
type ddlUnresolved struct {
	head string
	site ddlSite
}

// key is the dynamic-DDL allowlist key: file, enclosing Go declaration, and
// statement head. It holds no line number, so unrelated edits keep it stable.
func (u ddlUnresolved) key() string { return u.site.file + "|" + u.site.scope + "|" + u.head }

// ddlInventory is the schema surface a set of DDL sources declares. Each map
// keeps the first site that declared the entry.
type ddlInventory struct {
	tables     map[string]ddlSite
	tempTables map[ddlTempTable]ddlSite
	columns    map[ddlColumn]ddlSite
	indexes    map[string]ddlIndex
	objects    map[ddlObject]ddlSite
	unresolved []ddlUnresolved
}

var (
	// ddlHeadPattern finds every statement start the guard reads.
	ddlHeadPattern = regexp.MustCompile(`(?i)\b(?:create\s+(?:(?:global|local)\s+)?(?:(?:temp|temporary|unlogged)\s+)?table|create\s+(?:unique\s+)?index|alter\s+table|create\s+(?:or\s+replace\s+)?(?:constraint\s+)?trigger|create\s+(?:or\s+replace\s+)?function)\b`)
	// ddlCreateTablePattern is anchored at a CREATE TABLE head. Group 1 marks
	// a temporary or unlogged table, group 2 is the name, and group 3 the
	// body opener: a column list, PARTITION OF, AS, or OF a composite type.
	ddlCreateTablePattern = regexp.MustCompile(`(?is)^create\s+(?:(?:global|local)\s+)?(?:(temp|temporary|unlogged)\s+)?table\s+(?:if\s+not\s+exists\s+)?([^\s(;,]+)(\s*\(|\s+partition\s+of\b|\s+as\b|\s+of\b)?`)
	// ddlCreateIndexPattern is anchored at a CREATE INDEX head; group 1 is
	// the index name and group 2 the table.
	ddlCreateIndexPattern = regexp.MustCompile(`(?is)^create\s+(?:unique\s+)?index\s+(?:concurrently\s+)?(?:if\s+not\s+exists\s+)?([^\s(;,]+)\s+on\s+(?:only\s+)?([^\s(;,]+)`)
	// ddlAlterTablePattern is anchored at an ALTER TABLE head; group 1 is the
	// table and group 2 the action list up to the statement end.
	ddlAlterTablePattern = regexp.MustCompile(`(?is)^alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?([^\s(;,]+)\s+([^;]*)`)
	// ddlCreateTriggerPattern is anchored at a CREATE TRIGGER head; group 1
	// is the trigger name and group 2 the table after ON.
	ddlCreateTriggerPattern = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:constraint\s+)?trigger\s+([^\s(;,]+)\s[^;]*?\bon\s+(?:only\s+)?([^\s(;,]+)`)
	// ddlCreateFunctionPattern is anchored at a CREATE FUNCTION head; group 1
	// is the function name.
	ddlCreateFunctionPattern = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?function\s+([^\s(;,]+?)\s*\(`)
)

// ddlTableConstraintElement matches a CREATE TABLE element that is not a
// column: a named constraint (group "constraint"), an unnamed constraint, or
// a LIKE clause. Each keyword needs the token its grammar requires next.
var ddlTableConstraintElement = regexp.MustCompile(`(?is)^(?:constraint\s+(?P<constraint>` + ddlActionName + `)|` +
	strings.ReplaceAll(ddlUnnamedConstraint, "NAME", ddlActionName) + `|like\s+(?P<like>` + ddlActionName + `))`)

// extractDDLInventory scans every source for CREATE TABLE, ALTER TABLE ...
// ADD [COLUMN] / ADD CONSTRAINT, CREATE INDEX, CREATE TRIGGER, and CREATE
// FUNCTION statements. SQL comments are removed first, so DDL that appears
// only in a comment never counts.
func extractDDLInventory(sources []ddlSource) ddlInventory {
	inv := ddlInventory{
		tables:     map[string]ddlSite{},
		tempTables: map[ddlTempTable]ddlSite{},
		columns:    map[ddlColumn]ddlSite{},
		indexes:    map[string]ddlIndex{},
		objects:    map[ddlObject]ddlSite{},
	}
	for _, src := range sources {
		sql := stripSQLComments(src.sql)
		for _, loc := range ddlHeadPattern.FindAllStringIndex(sql, -1) {
			site := ddlSite{file: src.file, scope: src.scope, line: src.line + strings.Count(sql[:loc[0]], "\n")}
			head := strings.ToLower(sql[loc[0]:loc[1]])
			rest := sql[loc[0]:]
			switch {
			case strings.HasSuffix(head, "trigger"):
				inv.addNamedOn(ddlCreateTriggerPattern, "trigger", rest, site)
			case strings.HasSuffix(head, "function"):
				inv.addCreateFunction(rest, site)
			case strings.HasPrefix(head, "alter"):
				inv.addAlterTable(rest, site)
			case strings.HasSuffix(head, "index"):
				inv.addCreateIndex(rest, site)
			default:
				inv.addCreateTable(rest, site)
			}
		}
	}
	return inv
}

// unresolvedAt records a statement the scan cannot fully read.
func (inv *ddlInventory) unresolvedAt(text string, site ddlSite) {
	inv.unresolved = append(inv.unresolved, ddlUnresolved{head: strings.Join(strings.Fields(text), " "), site: site})
}

// addCreateTable records a CREATE TABLE, its columns, and its named
// constraints. A literal table name followed by no readable body records the
// table and an unresolved entry for the columns the scan cannot see.
func (inv *ddlInventory) addCreateTable(rest string, site ddlSite) {
	m := ddlCreateTablePattern.FindStringSubmatchIndex(rest)
	if m == nil {
		inv.unresolvedAt(ddlFirstLine(rest), site)
		return
	}
	table, ok := normalizeDDLIdent(rest[m[4]:m[5]])
	if !ok {
		inv.unresolvedAt(rest[:m[5]], site)
		return
	}
	if m[2] >= 0 && !strings.EqualFold(rest[m[2]:m[3]], "unlogged") {
		key := ddlTempTable{file: site.file, table: table}
		if _, seen := inv.tempTables[key]; !seen {
			inv.tempTables[key] = site
		}
		return
	}
	if _, seen := inv.tables[table]; !seen {
		inv.tables[table] = site
	}
	if m[6] < 0 {
		inv.unresolvedAt(rest[:m[5]], site) // column list joined on at runtime
		return
	}
	if !strings.HasSuffix(rest[m[6]:m[7]], "(") {
		return // PARTITION OF, AS, or OF: the columns come from elsewhere
	}
	body, ok := balancedParenBody(rest[m[7]-1:])
	if !ok {
		inv.unresolvedAt(rest[:m[5]], site) // column list cut off by a runtime join
		return
	}
	for _, element := range splitTopLevelCommas(body) {
		fields := strings.Fields(element)
		if len(fields) == 0 {
			inv.unresolvedAt(rest[:m[5]], site)
			continue
		}
		names, matched, readable := matchDDLForm(ddlTableConstraintElement, strings.TrimSpace(element))
		switch {
		case matched && !readable:
			inv.unresolvedAt(rest[:m[5]]+" ("+strings.TrimSpace(element), site)
		case matched && names["constraint"] != "":
			inv.addObject("constraint", table, names["constraint"], rest[:m[5]], site)
		case matched:
			// An unnamed constraint or LIKE clause: nothing to compare.
		case len(fields) < 2:
			// A bare keyword or a column with no type: the rest is joined on.
			inv.unresolvedAt(rest[:m[5]]+" ("+element, site)
		default:
			if column, ok := normalizeDDLIdent(fields[0]); ok {
				inv.addColumn(ddlColumn{table: table, column: column}, site)
			} else {
				inv.unresolvedAt(rest[:m[5]]+" ("+fields[0], site)
			}
		}
	}
}

// addAlterTable records each ADD [COLUMN] and ADD CONSTRAINT action of an
// ALTER TABLE. Every action must match a form in ddlAlterForms; an empty
// action list, an empty action, or an unrecognized action means part of the
// statement is joined on at runtime, and is recorded as unresolved.
func (inv *ddlInventory) addAlterTable(rest string, site ddlSite) {
	m := ddlAlterTablePattern.FindStringSubmatchIndex(rest)
	if m == nil {
		inv.unresolvedAt(ddlFirstLine(rest), site)
		return
	}
	head := rest[:m[3]]
	table, ok := normalizeDDLIdent(rest[m[2]:m[3]])
	if !ok || strings.TrimSpace(rest[m[4]:m[5]]) == "" {
		inv.unresolvedAt(head, site)
		return
	}
	for _, action := range splitTopLevelCommas(rest[m[4]:m[5]]) {
		action = strings.TrimSpace(action)
		classified, ok := classifyAlterAction(action)
		switch {
		case !ok:
			inv.unresolvedAt(head+" "+action, site)
		case classified.column != "":
			inv.addColumn(ddlColumn{table: table, column: classified.column}, site)
		case classified.constraint != "":
			inv.addObject("constraint", table, classified.constraint, head, site)
		}
	}
}

// addCreateIndex records a named CREATE INDEX and the table it is on.
func (inv *ddlInventory) addCreateIndex(rest string, site ddlSite) {
	m := ddlCreateIndexPattern.FindStringSubmatchIndex(rest)
	if m == nil {
		inv.unresolvedAt(ddlFirstLine(rest), site)
		return
	}
	name, nameOK := normalizeDDLIdent(rest[m[2]:m[3]])
	table, tableOK := normalizeDDLIdent(rest[m[4]:m[5]])
	if !nameOK || !tableOK {
		inv.unresolvedAt(rest[:m[5]], site)
		return
	}
	if _, seen := inv.indexes[name]; !seen {
		inv.indexes[name] = ddlIndex{table: table, site: site}
	}
}

// addNamedOn records a "<kind> name ... ON table" object such as a trigger.
func (inv *ddlInventory) addNamedOn(pattern *regexp.Regexp, kind, rest string, site ddlSite) {
	m := pattern.FindStringSubmatchIndex(rest)
	if m == nil {
		inv.unresolvedAt(ddlFirstLine(rest), site)
		return
	}
	table, ok := normalizeDDLIdent(rest[m[4]:m[5]])
	if !ok {
		inv.unresolvedAt(rest[:m[5]], site)
		return
	}
	inv.addObject(kind, table, rest[m[2]:m[3]], rest[:m[5]], site)
}

// addCreateFunction records a CREATE [OR REPLACE] FUNCTION by name.
func (inv *ddlInventory) addCreateFunction(rest string, site ddlSite) {
	m := ddlCreateFunctionPattern.FindStringSubmatchIndex(rest)
	if m == nil {
		inv.unresolvedAt(ddlFirstLine(rest), site)
		return
	}
	inv.addObject("function", "", rest[m[2]:m[3]], rest[:m[3]], site)
}

// addObject records a named object, or an unresolved entry under head when
// rawName is not a readable identifier.
func (inv *ddlInventory) addObject(kind, table, rawName, head string, site ddlSite) {
	name, ok := normalizeDDLIdent(rawName)
	if !ok {
		inv.unresolvedAt(head+" "+kind+" "+rawName, site)
		return
	}
	key := ddlObject{kind: kind, table: table, name: name}
	if _, seen := inv.objects[key]; !seen {
		inv.objects[key] = site
	}
}

// addColumn records a column, keeping its first site.
func (inv *ddlInventory) addColumn(col ddlColumn, site ddlSite) {
	if _, seen := inv.columns[col]; !seen {
		inv.columns[col] = site
	}
}

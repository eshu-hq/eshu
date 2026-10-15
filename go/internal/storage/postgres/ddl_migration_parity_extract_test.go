// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// This file holds the pure half of the Go-DDL versus numbered-migration
// parity guard (#7792): it turns SQL text into a schema inventory and
// compares two inventories. It reads no files, so the seeded cases in
// ddl_migration_parity_cases_test.go drive it with synthetic SQL.

// ddlSource is one SQL text to scan: a numbered migration file or one Go
// string literal. line is the 1-based line where the text starts.
type ddlSource struct {
	file string
	line int
	sql  string
}

// ddlSite names where a DDL statement starts, for failure messages.
type ddlSite struct {
	file string
	line int
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

// ddlUnresolved is a DDL statement whose target name the scan cannot read:
// a fmt verb, a name joined at runtime, or an unnamed index. head is the
// statement text up to the name, whitespace-collapsed, used as the
// allowlist key together with the file.
type ddlUnresolved struct {
	head string
	site ddlSite
}

// ddlInventory is the schema surface a set of DDL sources declares. Each map
// keeps the first site that declared the entry.
type ddlInventory struct {
	tables     map[string]ddlSite
	tempTables map[string]ddlSite
	columns    map[ddlColumn]ddlSite
	indexes    map[string]ddlIndex
	unresolved []ddlUnresolved
}

var (
	// ddlHeadPattern finds every statement start the guard cares about.
	ddlHeadPattern = regexp.MustCompile(`(?i)\b(?:create\s+(?:(?:global|local)\s+)?(?:(?:temp|temporary|unlogged)\s+)?table|create\s+(?:unique\s+)?index|alter\s+table)\b`)
	// ddlCreateTablePattern is anchored at a CREATE TABLE head. Group 1 marks
	// a temporary or unlogged table, group 2 is the name, and group 3 is the
	// body opener that separates a statement from prose such as an error
	// message.
	ddlCreateTablePattern = regexp.MustCompile(`(?is)^create\s+(?:(?:global|local)\s+)?(?:(temp|temporary|unlogged)\s+)?table\s+(?:if\s+not\s+exists\s+)?([^\s(;,]+)(\s*\(|\s+partition\s+of\b|\s+as\b)?`)
	// ddlCreateIndexPattern is anchored at a CREATE INDEX head; group 1 is
	// the index name and group 2 the table.
	ddlCreateIndexPattern = regexp.MustCompile(`(?is)^create\s+(?:unique\s+)?index\s+(?:concurrently\s+)?(?:if\s+not\s+exists\s+)?([^\s(;,]+)\s+on\s+(?:only\s+)?([^\s(;,]+)`)
	// ddlAlterTablePattern is anchored at an ALTER TABLE head; group 1 is the
	// table and group 2 the action list up to the statement end.
	ddlAlterTablePattern = regexp.MustCompile(`(?is)^alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?([^\s(;,]+)\s+([^;]*)`)
	// ddlAddColumnPattern matches one ALTER TABLE action that adds a column;
	// the COLUMN keyword is optional in PostgreSQL.
	ddlAddColumnPattern = regexp.MustCompile(`(?is)^add\s+(?:column\s+)?(?:if\s+not\s+exists\s+)?([^\s(,]+)`)
	ddlPlainIdent       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)
)

// ddlNonColumnWords start a table element or ADD action that is a
// constraint, not a column.
var ddlNonColumnWords = map[string]bool{
	"primary": true, "unique": true, "constraint": true, "foreign": true,
	"check": true, "exclude": true, "like": true,
}

// extractDDLInventory scans every source for CREATE TABLE, ALTER TABLE ...
// ADD [COLUMN], and CREATE INDEX statements. SQL comments are removed first,
// so DDL that appears only in a comment never counts.
func extractDDLInventory(sources []ddlSource) ddlInventory {
	inv := ddlInventory{
		tables:     map[string]ddlSite{},
		tempTables: map[string]ddlSite{},
		columns:    map[ddlColumn]ddlSite{},
		indexes:    map[string]ddlIndex{},
	}
	for _, src := range sources {
		sql := stripSQLComments(src.sql)
		for _, loc := range ddlHeadPattern.FindAllStringIndex(sql, -1) {
			site := ddlSite{file: src.file, line: src.line + strings.Count(sql[:loc[0]], "\n")}
			head := strings.ToLower(sql[loc[0]:loc[1]])
			rest := sql[loc[0]:]
			switch {
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

// unresolvedAt records a statement whose target name the scan cannot read.
func (inv *ddlInventory) unresolvedAt(text string, site ddlSite) {
	inv.unresolved = append(inv.unresolved, ddlUnresolved{head: strings.Join(strings.Fields(text), " "), site: site})
}

// addCreateTable records a CREATE TABLE and its column list. Temporary
// tables are session-local, so they are tracked apart from schema tables.
func (inv *ddlInventory) addCreateTable(rest string, site ddlSite) {
	m := ddlCreateTablePattern.FindStringSubmatchIndex(rest)
	if m == nil {
		inv.unresolvedAt(ddlFirstLine(rest), site)
		return
	}
	if m[6] < 0 {
		return // no column list, PARTITION OF, or AS: prose, not a statement
	}
	table, ok := normalizeDDLIdent(rest[m[4]:m[5]])
	if !ok {
		inv.unresolvedAt(rest[:m[5]], site)
		return
	}
	if m[2] >= 0 && !strings.EqualFold(rest[m[2]:m[3]], "unlogged") {
		if _, seen := inv.tempTables[table]; !seen {
			inv.tempTables[table] = site
		}
		return
	}
	if _, seen := inv.tables[table]; !seen {
		inv.tables[table] = site
	}
	if !strings.HasSuffix(rest[m[6]:m[7]], "(") {
		return
	}
	body, ok := balancedParenBody(rest[m[7]-1:])
	if !ok {
		return
	}
	for _, element := range splitTopLevelCommas(body) {
		fields := strings.Fields(element)
		if len(fields) == 0 || ddlNonColumnWords[strings.ToLower(fields[0])] {
			continue
		}
		if column, ok := normalizeDDLIdent(fields[0]); ok {
			inv.addColumn(ddlColumn{table: table, column: column}, site)
		}
	}
}

// addAlterTable records each ADD [COLUMN] action of an ALTER TABLE.
func (inv *ddlInventory) addAlterTable(rest string, site ddlSite) {
	m := ddlAlterTablePattern.FindStringSubmatch(rest)
	if m == nil {
		inv.unresolvedAt(ddlFirstLine(rest), site)
		return
	}
	table, ok := normalizeDDLIdent(m[1])
	if !ok || strings.TrimSpace(m[2]) == "" {
		// A bad name, or an action list joined on at runtime.
		inv.unresolvedAt(rest[:strings.Index(rest, m[1])+len(m[1])], site)
		return
	}
	for _, action := range splitTopLevelCommas(m[2]) {
		add := ddlAddColumnPattern.FindStringSubmatch(strings.TrimSpace(action))
		if add == nil || ddlNonColumnWords[strings.ToLower(add[1])] {
			continue
		}
		column, ok := normalizeDDLIdent(add[1])
		if !ok {
			inv.unresolvedAt(strings.TrimSpace(action), site)
			continue
		}
		inv.addColumn(ddlColumn{table: table, column: column}, site)
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

// addColumn records a column, keeping its first site.
func (inv *ddlInventory) addColumn(col ddlColumn, site ddlSite) {
	if _, seen := inv.columns[col]; !seen {
		inv.columns[col] = site
	}
}

// ddlParityGaps returns one sorted message per violation of the three rules:
// a column or index in Go DDL that no numbered migration declares, and a
// table Go DDL creates that no numbered migration creates. goOnlyTables and
// dynamicDDL are the explicit exemptions; dynamicDDL is keyed by
// "<file>|<statement head>" for statements whose name the scan cannot read.
func ddlParityGaps(goInv, migInv ddlInventory, goOnlyTables, dynamicDDL map[string]string) []string {
	var gaps []string
	goOwned := func(table string) bool {
		_, created := goInv.tables[table]
		_, temp := goInv.tempTables[table]
		return created || temp
	}
	for table, site := range goInv.tables {
		_, inMigrations := migInv.tables[table]
		_, allowed := goOnlyTables[table]
		if !inMigrations && !allowed {
			gaps = append(gaps, fmt.Sprintf("%s: table %s is created by Go DDL but by no numbered migration", site, table))
		}
	}
	for col, site := range goInv.columns {
		switch _, inMigrations := migInv.tables[col.table]; {
		case !inMigrations && goOwned(col.table):
			// Reported, or explicitly allowed, by the table rule.
		case !inMigrations:
			gaps = append(gaps, fmt.Sprintf("%s: column %s.%s is added by Go DDL to a table no numbered migration creates", site, col.table, col.column))
		default:
			if _, ok := migInv.columns[col]; !ok {
				gaps = append(gaps, fmt.Sprintf("%s: column %s.%s exists in Go DDL but in no numbered migration", site, col.table, col.column))
			}
		}
	}
	for name, idx := range goInv.indexes {
		mig, inMigrations := migInv.indexes[name]
		switch _, tableInMigrations := migInv.tables[idx.table]; {
		case !tableInMigrations && goOwned(idx.table):
			// Reported, or explicitly allowed, by the table rule.
		case !inMigrations:
			gaps = append(gaps, fmt.Sprintf("%s: index %s on %s exists in Go DDL but in no numbered migration", idx.site, name, idx.table))
		case mig.table != idx.table:
			gaps = append(gaps, fmt.Sprintf("%s: index %s is on %s in Go DDL but on %s in the numbered migrations", idx.site, name, idx.table, mig.table))
		}
	}
	for _, u := range goInv.unresolved {
		if _, ok := dynamicDDL[u.site.file+"|"+u.head]; ok {
			continue
		}
		gaps = append(gaps, fmt.Sprintf("%s: unresolved DDL target in %q; use a literal name or add %q to the dynamic-DDL allowlist with a reason", u.site, u.head, u.site.file+"|"+u.head))
	}
	sort.Strings(gaps)
	return gaps
}

// normalizeDDLIdent folds a possibly schema-qualified identifier the way
// PostgreSQL does: the schema is dropped, a quoted name keeps its case, and
// an unquoted name is lowercased. It reports false for anything that is not
// a plain or quoted identifier, such as a fmt verb.
func normalizeDDLIdent(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	var parts []string
	inQuote, start := false, 0
	for i := 0; i < len(raw); i++ {
		switch {
		case raw[i] == '"':
			inQuote = !inQuote
		case raw[i] == '.' && !inQuote:
			parts = append(parts, raw[start:i])
			start = i + 1
		}
	}
	parts = append(parts, raw[start:])
	name := ""
	for _, part := range parts {
		if len(part) >= 3 && part[0] == '"' && part[len(part)-1] == '"' {
			name = strings.ReplaceAll(part[1:len(part)-1], `""`, `"`)
			continue
		}
		if !ddlPlainIdent.MatchString(part) {
			return "", false
		}
		name = strings.ToLower(part)
	}
	return name, true
}

// stripSQLComments blanks "--" line comments and "/* */" block comments
// outside single-quoted literals, keeping newlines so line numbers hold.
func stripSQLComments(sql string) string {
	out := []byte(sql)
	inQuote := false
	for i := 0; i < len(out); i++ {
		switch {
		case inQuote:
			if out[i] == '\'' {
				inQuote = false
			}
		case out[i] == '\'':
			inQuote = true
		case out[i] == '-' && i+1 < len(out) && out[i+1] == '-':
			for ; i < len(out) && out[i] != '\n'; i++ {
				out[i] = ' '
			}
		case out[i] == '/' && i+1 < len(out) && out[i+1] == '*':
			end := strings.Index(string(out[i+2:]), "*/")
			stop := len(out)
			if end >= 0 {
				stop = i + 2 + end + 2
			}
			for ; i < stop; i++ {
				if out[i] != '\n' {
					out[i] = ' '
				}
			}
			i--
		}
	}
	return string(out)
}

// balancedParenBody returns the text inside the parenthesis that opens s,
// honouring nested parentheses and single-quoted literals.
func balancedParenBody(s string) (string, bool) {
	depth, inQuote := 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inQuote:
			inQuote = c != '\''
		case c == '\'':
			inQuote = true
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return s[1:i], true
			}
		}
	}
	return "", false
}

// splitTopLevelCommas splits s at commas outside parentheses and
// single-quoted literals.
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth, inQuote, start := 0, false, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inQuote:
			inQuote = c != '\''
		case c == '\'':
			inQuote = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// ddlFirstLine returns s up to its first newline.
func ddlFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

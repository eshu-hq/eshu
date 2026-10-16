// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
)

// Schema objects beyond tables, columns, indexes, constraints, triggers, and
// functions for the Go-DDL parity guard (#7792). A Go-only sequence that a
// writer reads through nextval() breaks every database the schema Job built,
// the same way a Go-only column does, so these kinds are compared too.

// ddlObjectHeads are the ddlHeadPattern alternatives this file handles: a
// CREATE of a sequence, view, materialized view, procedure, type, or domain,
// and an ALTER of an index, sequence, type, domain, or view.
const ddlObjectHeads = `create\s+(?:(?:temp|temporary|unlogged)\s+)?sequence` +
	`|create\s+(?:or\s+replace\s+)?(?:(?:temp|temporary)\s+)?(?:recursive\s+|materialized\s+)?view` +
	`|create\s+(?:or\s+replace\s+)?procedure` +
	`|create\s+(?:type|domain)` +
	`|alter\s+(?:index|sequence|type|domain|view|materialized\s+view)`

var (
	// ddlObjectHeadPattern reports whether a matched statement head is one
	// of ddlObjectHeads.
	ddlObjectHeadPattern = regexp.MustCompile(`(?is)^(?:` + ddlObjectHeads + `)$`)
	// ddlCreateSequencePattern is anchored at a CREATE SEQUENCE head. Group
	// 1 marks a temporary sequence and group 2 is the name.
	ddlCreateSequencePattern = regexp.MustCompile(`(?is)^create\s+(?:(temp|temporary)\s+|unlogged\s+)?sequence\s+(?:if\s+not\s+exists\s+)?([^\s(;,]+)`)
	// ddlCreateViewPattern is anchored at a CREATE VIEW head. Group 1 marks a
	// temporary view, group 2 a materialized view, and group 3 is the name.
	ddlCreateViewPattern = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:(temp|temporary)\s+)?(?:recursive\s+|(materialized)\s+)?view\s+(?:if\s+not\s+exists\s+)?([^\s(;,]+)`)
	// ddlCreateProcedurePattern is anchored at a CREATE PROCEDURE head; group
	// 1 is the name.
	ddlCreateProcedurePattern = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?procedure\s+([^\s(;,]+?)\s*\(`)
	// ddlCreateTypePattern is anchored at a CREATE TYPE or CREATE DOMAIN
	// head; group 1 is the kind and group 2 the name.
	ddlCreateTypePattern = regexp.MustCompile(`(?is)^create\s+(type|domain)\s+([^\s(;,]+)`)
)

// addSchemaObject records a statement whose head matches ddlObjectHeads.
// CREATE SEQUENCE, VIEW, MATERIALIZED VIEW, PROCEDURE, TYPE, and DOMAIN are
// recorded as objects by name, with no table; a temporary sequence or view is
// session-local and is skipped. Every ALTER here is recorded as unresolved:
// the guard never compares what it changes, so it fails unless
// ddlParityDynamicDDL lists it with a reason.
func (inv *ddlInventory) addSchemaObject(head, rest string, site ddlSite) {
	if strings.HasPrefix(head, "alter") {
		inv.unresolvedAt(ddlStatementText(rest), site)
		return
	}
	var kind, name string
	switch {
	case strings.HasSuffix(head, "sequence"):
		m := ddlCreateSequencePattern.FindStringSubmatchIndex(rest)
		if m != nil && m[2] >= 0 {
			return // temporary
		}
		kind, name = "sequence", ddlSubmatch(rest, m, 2)
	case strings.HasSuffix(head, "view"):
		m := ddlCreateViewPattern.FindStringSubmatchIndex(rest)
		if m != nil && m[2] >= 0 {
			return // temporary
		}
		kind = "view"
		if m != nil && m[4] >= 0 {
			kind = "materialized view"
		}
		name = ddlSubmatch(rest, m, 3)
	case strings.HasSuffix(head, "procedure"):
		kind, name = "procedure", ddlSubmatch(rest, ddlCreateProcedurePattern.FindStringSubmatchIndex(rest), 1)
	default:
		m := ddlCreateTypePattern.FindStringSubmatchIndex(rest)
		kind, name = strings.ToLower(ddlSubmatch(rest, m, 1)), ddlSubmatch(rest, m, 2)
	}
	if name == "" {
		inv.unresolvedAt(ddlFirstLine(rest), site)
		return
	}
	inv.addObject(kind, "", name, ddlStatementText(rest), site)
}

// ddlSubmatch returns group n of a FindStringSubmatchIndex result over s, or
// "" when there was no match or the group did not take part.
func ddlSubmatch(s string, m []int, n int) string {
	if m == nil || m[2*n] < 0 {
		return ""
	}
	return s[m[2*n]:m[2*n+1]]
}

// ddlStatementText returns s up to the statement end (ddlStatementEnd) or
// its first newline, whichever comes first: the text an unresolved ALTER is
// keyed by.
func ddlStatementText(s string) string {
	return ddlFirstLine(s[:ddlStatementEnd(s)])
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
)

// The ALTER TABLE action classifier for the Go-DDL parity guard (#7792). It
// is default-deny: an action must match one recognized form below, or the
// statement is recorded as unresolved. That keeps a clause joined on at
// runtime ("ADD " + columnDef) from passing as an action the scan skipped.

// ddlActionName is the identifier shape a form captures; captured names are
// then checked by normalizeDDLIdent, so a fmt verb or a keyword left by a
// runtime join is unreadable.
const ddlActionName = `[^\s(),;]+`

// ddlAlterForm is one recognized ALTER TABLE action form. A "column" group
// is recorded as a column, a "constraint" group as a named constraint, and
// any other named group is only checked for readability. Order matters: the
// first matching form wins.
type ddlAlterForm struct {
	name    string
	pattern *regexp.Regexp
}

func ddlForm(name, pattern string) ddlAlterForm {
	pattern = strings.ReplaceAll(pattern, "NAME", ddlActionName)
	return ddlAlterForm{name: name, pattern: regexp.MustCompile(`(?is)^` + pattern)}
}

// ddlAlterForms lists every ALTER TABLE action the guard accepts. The first
// three are compared against the migrations; the rest are recognized so they
// do not fail the guard, and are not compared. Only names are compared, so
// ADD CONSTRAINT <name> and ADD [COLUMN] <name> are complete once the name
// is readable, even when the definition after it is joined on at runtime.
// Every other form must be complete in the literal.
var ddlAlterForms = []ddlAlterForm{
	ddlForm("add constraint", `add\s+constraint\s+(?P<constraint>NAME)`),
	ddlForm("add unnamed constraint", `add\s+(?:primary\s+key|unique|check|foreign\s+key|exclude)\s*\S`),
	ddlForm("add column", `add\s+(?:column\s+)?(?:if\s+not\s+exists\s+)?(?P<column>NAME)`),
	ddlForm("alter constraint", `alter\s+constraint\s+(?P<name>NAME)\s+\S`),
	ddlForm("alter column", `alter\s+(?:column\s+)?(?P<name>NAME)\s+\S`),
	ddlForm("drop constraint", `drop\s+constraint\s+(?:if\s+exists\s+)?(?P<name>NAME)`),
	ddlForm("drop column", `drop\s+(?:column\s+)?(?:if\s+exists\s+)?(?P<name>NAME)`),
	ddlForm("rename to", `rename\s+to\s+(?P<name>NAME)`),
	ddlForm("rename column or constraint", `rename\s+(?:column\s+|constraint\s+)?(?P<name>NAME)\s+to\s+(?P<to>NAME)`),
	ddlForm("validate constraint", `validate\s+constraint\s+(?P<name>NAME)`),
	ddlForm("set or reset storage parameters", `(?:set|reset)\s*\(.*\)$`),
	ddlForm("set table option", `set\s+(?:logged|unlogged|without\s+cluster|without\s+oids)$`),
	ddlForm("set schema, tablespace, or access method", `set\s+(?:schema|tablespace|access\s+method)\s+(?P<name>NAME)`),
	ddlForm("enable or disable trigger or rule", `(?:enable|disable)\s+(?:(?:always|replica)\s+)?(?:trigger|rule)\s+(?P<name>NAME)`),
	ddlForm("row level security", `(?:enable|disable|force|no\s+force)\s+row\s+level\s+security$`),
	ddlForm("owner to", `owner\s+to\s+(?P<name>NAME)`),
	ddlForm("attach or detach partition", `(?:attach|detach)\s+partition\s+(?P<name>NAME)`),
	ddlForm("cluster on", `cluster\s+on\s+(?P<name>NAME)`),
	ddlForm("replica identity", `replica\s+identity\s+(?:default|full|nothing|using\s+index\s+(?P<name>NAME))$`),
	ddlForm("inherit", `(?:no\s+)?inherit\s+(?P<name>NAME)`),
	ddlForm("of type", `(?:of\s+(?P<name>NAME)|not\s+of)$`),
}

// ddlAlterAction is one classified ALTER TABLE action.
type ddlAlterAction struct {
	form       string // recognized form name; empty when unrecognized
	column     string // folded column name for "add column"
	constraint string // folded constraint name for "add constraint"
}

// classifyAlterAction matches one trimmed ALTER TABLE action against
// ddlAlterForms. It reports false when no form matches or a captured name is
// unreadable, so the caller records the statement as unresolved.
func classifyAlterAction(action string) (ddlAlterAction, bool) {
	for _, form := range ddlAlterForms {
		m := form.pattern.FindStringSubmatch(action)
		if m == nil {
			continue
		}
		out := ddlAlterAction{form: form.name}
		for i, group := range form.pattern.SubexpNames() {
			if group == "" || m[i] == "" {
				continue
			}
			name, ok := normalizeDDLIdent(m[i])
			if !ok {
				return ddlAlterAction{}, false
			}
			switch group {
			case "column":
				out.column = name
			case "constraint":
				out.constraint = name
			}
		}
		return out, true
	}
	return ddlAlterAction{}, false
}

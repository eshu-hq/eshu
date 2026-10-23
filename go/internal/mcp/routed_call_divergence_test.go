// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// divergenceClass is one recorded reason a map that resolveRoute builds a
// route for still fails the advertised input schema. call_tool validates
// against that schema with no tolerance (arbiter ruling on #8757 F2), so these
// maps are rejected on purpose; the class names the handler evidence.
type divergenceClass struct {
	id       string
	name     string
	evidence string
	// divergence is true when a direct tools/call can succeed where call_tool
	// refuses (classes A, B, D, E, F). Class C is refused by the handler
	// too, or 404s.
	divergence bool
	match      func(tool string, schema any, args map[string]any, errs []router.FieldError) bool
}

// validatorDivergenceClasses are checked in order; the first match wins.
var validatorDivergenceClasses = []divergenceClass{
	{
		id: "A", name: "undocumented alias", divergence: true,
		evidence: "mcp/content contentSearchBody sends query when pattern is absent; " +
			"mcp/entity/resolution resolveEntityBody sends the legacy name key",
		match: func(tool string, _ any, args map[string]any, errs []router.FieldError) bool {
			switch tool {
			case "search_entity_content", "search_file_content":
				return onlyFieldErrors(errs, "pattern", "required") && nonBlankString(args["query"])
			case "resolve_entity":
				return onlyFieldErrors(errs, "query", "required") && nonBlankString(args["name"])
			}
			return false
		},
	},
	{
		id: "B", name: "enum case", divergence: true,
		evidence: "query/language Handler.handleLanguageQuery lowercases entity_type before checking it",
		match: func(tool string, schema any, args map[string]any, errs []router.FieldError) bool {
			if tool != "execute_language_query" || !onlyFieldErrors(errs, "entity_type", "enum") {
				return false
			}
			value, _ := args["entity_type"].(string)
			lowered := make(map[string]any, len(args))
			for k, v := range args {
				lowered[k] = v
			}
			lowered["entity_type"] = strings.ToLower(value)
			return len(router.ValidateArguments(schema, lowered)) == 0
		},
	},
	{
		id: "D", name: "off-enum list filter", divergence: true,
		evidence: "the list handlers (cicd Handler.listRunCorrelations, the supply-chain SBOM attachment list, " +
			"the secrets/IAM lists) pass the filter to the store, which matches no rows",
		match: func(tool string, _ any, _ map[string]any, errs []router.FieldError) bool {
			field, ok := listFilterFields[tool]
			if !ok && strings.HasPrefix(tool, "list_secrets_iam_") {
				field, ok = "state", true
			}
			return ok && onlyFieldErrors(errs, field, "enum")
		},
	},
	{
		id: "F", name: "handler-optional required field", divergence: true,
		evidence: "handlerOptionalRequired rows: dispatch defaults the field or the handler serves without it",
		match: func(tool string, _ any, _ map[string]any, errs []router.FieldError) bool {
			return allKeyword(errs, "required") && requiredRowMatches(handlerOptionalRequired, tool, errs)
		},
	},
	{
		id: "C", name: "handler rejects too",
		evidence: "handlerRejectedRequired and handlerRejectedEnums rows: the handler answers 400, or the route 404s",
		match: func(tool string, _ any, _ map[string]any, errs []router.FieldError) bool {
			if allKeyword(errs, "required") {
				return requiredRowMatches(handlerRejectedRequired, tool, errs)
			}
			row, ok := handlerRejectedEnums[tool]
			return ok && onlyFieldErrors(errs, row.field, "enum")
		},
	},
	{
		id: "E", name: "dispatch defaults a wrong type", divergence: true,
		evidence: "defaultedTypeFields rows: dispatch's argument helpers drop a wrong-typed value or use the default",
		match: func(tool string, _ any, _ map[string]any, errs []router.FieldError) bool {
			fields := defaultedTypeFields[tool]
			seen := false
			for _, e := range errs {
				if e.Keyword != "type" {
					continue
				}
				seen = true
				if !slices.Contains(fields, indexFree(e.Path)) {
					return false
				}
			}
			return seen
		},
	},
}

// listFilterFields are list-tool filters whose off-enum value the handler
// passes through as a no-match (class D).
var listFilterFields = map[string]string{
	"list_ci_cd_run_correlations":       "outcome",
	"list_sbom_attestation_attachments": "attachment_status",
}

// requiredRowMatches reports whether errs, all `required`, name exactly the
// missing set of one row for tool.
func requiredRowMatches(rows []requiredFieldRow, tool string, errs []router.FieldError) bool {
	missing := make([]string, 0, len(errs))
	for _, e := range errs {
		missing = append(missing, e.Path)
	}
	sort.Strings(missing)
	for _, row := range rows {
		if row.tool == tool && slices.Equal(row.missing, missing) {
			return true
		}
	}
	return false
}

// indexFree drops array indices from a field path: "ids[2]" becomes "ids".
func indexFree(path string) string {
	if i := strings.IndexByte(path, '['); i >= 0 {
		return path[:i]
	}
	return path
}

// classifyRejection returns the class of a rejected dispatch-accepted map, or
// nil when no recorded class explains it.
func classifyRejection(tool string, schema any, args map[string]any, errs []router.FieldError) *divergenceClass {
	for i := range validatorDivergenceClasses {
		if validatorDivergenceClasses[i].match(tool, schema, args, errs) {
			return &validatorDivergenceClasses[i]
		}
	}
	return nil
}

func onlyFieldErrors(errs []router.FieldError, path, keyword string) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if e.Path != path || e.Keyword != keyword {
			return false
		}
	}
	return true
}

func allKeyword(errs []router.FieldError, keyword string) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if e.Keyword != keyword {
			return false
		}
	}
	return true
}

func nonBlankString(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}

func registrySchema(t *testing.T, name string) any {
	t.Helper()
	for _, tool := range ReadOnlyTools() {
		if tool.Name == name {
			return tool.InputSchema
		}
	}
	t.Fatalf("tool %s is not registered", name)
	return nil
}

// TestDivergenceClassAPinsTheQueryAlias pins class A: dispatch builds a route
// for search_entity_content with query and no pattern, and the validator names
// the missing pattern. When a later change adds query to the schema, this
// fails and class A should shrink.
func TestDivergenceClassAPinsTheQueryAlias(t *testing.T) {
	t.Parallel()

	for tool, args := range map[string]map[string]any{
		"search_entity_content": {"query": "decode", "limit": 10},
		"resolve_entity":        {"name": "exactName", "type": "function"},
	} {
		if _, err := resolveRoute(tool, args); err != nil {
			t.Fatalf("%s: resolveRoute(%v) = %v, want a route", tool, args, err)
		}
		schema := registrySchema(t, tool)
		errs := router.ValidateArguments(schema, args)
		if c := classifyRejection(tool, schema, args, errs); c == nil || c.id != "A" {
			t.Fatalf("%s: errors %v classify as %v, want class A", tool, errs, c)
		}
	}
}

// TestDivergenceClassBPinsTheEnumCase pins class B: dispatch builds a route
// for execute_language_query with a capitalized entity_type, which the handler
// lowercases, and the validator names the case-sensitive enum.
func TestDivergenceClassBPinsTheEnumCase(t *testing.T) {
	t.Parallel()

	args := map[string]any{"entity_type": "Function", "language": "python", "limit": 10, "query": "fetch_remote"}
	if _, err := resolveRoute("execute_language_query", args); err != nil {
		t.Fatalf("resolveRoute(execute_language_query) = %v, want a route", err)
	}
	schema := registrySchema(t, "execute_language_query")
	errs := router.ValidateArguments(schema, args)
	if !onlyFieldErrors(errs, "entity_type", "enum") {
		t.Fatalf("errors = %v, want one entity_type enum error", errs)
	}
	if c := classifyRejection("execute_language_query", schema, args, errs); c == nil || c.id != "B" {
		t.Fatalf("classified as %v, want class B", c)
	}
}

// TestDivergenceClassWitnesses keeps every class live: each witness, taken
// from the runtime capture, must still resolve to a route, fail the schema,
// and land in its class. A witness that starts validating or moves class
// means the class is stale.
func TestDivergenceClassWitnesses(t *testing.T) {
	t.Parallel()

	witnesses := []struct {
		class string
		tool  string
		args  map[string]any
	}{
		{"A", "search_file_content", map[string]any{"query": "decode", "limit": 10}},
		{"B", "execute_language_query", map[string]any{"entity_type": "Class", "language": "go", "limit": 5}},
		{"C", "compare_environments", map[string]any{"repo_id": "repo-1"}},
		{"C", "find_blast_radius", map[string]any{"target": "payments-db", "target_type": "resource"}},
		{"C", "get_workload_context", map[string]any{"workload_id": "wl-1", "evidence_detail": "compact"}},
		{"D", "list_ci_cd_run_correlations", map[string]any{"repository_id": "repo-1", "outcome": "correlated", "limit": 25}},
		{"D", "list_sbom_attestation_attachments", map[string]any{"repository_id": "repo-1", "attachment_status": "verified", "limit": 25}},
		{"F", "analyze_infra_relationships", map[string]any{"target": "tf:aws_s3_bucket.api"}},
		{"F", "analyze_code_relationships", map[string]any{"target": "handler"}},
		{"F", "build_evidence_citation_packet", map[string]any{}},
		{"F", "list_package_registry_packages", map[string]any{"ecosystem": "npm"}},
		{"D", "list_secrets_iam_posture_gaps", map[string]any{"scope_id": "scope-1", "state": "active", "limit": 25}},
		{"E", "list_indexed_repositories", map[string]any{"limit": "25"}},
	}
	seen := map[string]bool{}
	for _, w := range witnesses {
		if _, err := resolveRoute(w.tool, w.args); err != nil {
			t.Errorf("class %s witness %s %v: resolveRoute = %v, want a route", w.class, w.tool, w.args, err)
			continue
		}
		schema := registrySchema(t, w.tool)
		errs := router.ValidateArguments(schema, w.args)
		if len(errs) == 0 {
			t.Errorf("class %s witness %s %v now validates; the class is stale", w.class, w.tool, w.args)
			continue
		}
		c := classifyRejection(w.tool, schema, w.args, errs)
		if c == nil || c.id != w.class {
			t.Errorf("class %s witness %s %v classifies as %v (errors %v)", w.class, w.tool, w.args, c, errs)
			continue
		}
		seen[c.id] = true
	}
	for _, c := range validatorDivergenceClasses {
		if !seen[c.id] {
			t.Errorf("divergence class %s (%s) has no live witness", c.id, c.name)
		}
	}
}

// TestDifferentialRefusesUnlistedRejections is the seeded RED half of the
// class tables: a required-only map and a wrong-type map that no row lists
// must fall in no class, which is exactly what fails
// TestValidatorAcceptsEveryDispatchedArgumentMap.
func TestDifferentialRefusesUnlistedRejections(t *testing.T) {
	t.Parallel()

	for _, seed := range []struct {
		tool string
		args map[string]any
	}{
		// get_file_lines lists only its all-four-missing row.
		{"get_file_lines", map[string]any{"repo_id": "repo-1", "relative_path": "main.go", "end_line": 9}},
		// find_symbol has no defaulted-type row.
		{"find_symbol", map[string]any{"symbol": 42}},
	} {
		if _, err := resolveRoute(seed.tool, seed.args); err != nil {
			t.Fatalf("%s: seed %v builds no route: %v", seed.tool, seed.args, err)
		}
		schema := registrySchema(t, seed.tool)
		errs := router.ValidateArguments(schema, seed.args)
		if len(errs) == 0 {
			t.Fatalf("%s: seed %v validates, want a rejection", seed.tool, seed.args)
		}
		if c := classifyRejection(seed.tool, schema, seed.args, errs); c != nil {
			t.Fatalf("%s: unlisted seed %v classified as %s; the tables must enumerate rows, not match a keyword", seed.tool, seed.args, c.id)
		}
	}
}

// TestDirectCallRefusesEmptyPathSegmentRows holds the class C rows that name a
// path id ("direct dispatch:" evidence) to what they say: resolveRoute refuses
// the empty-argument call with "<field> is required" before any request is
// built, so a direct tools/call is an error (#8772). The mux has no routes: the
// call is refused before dispatch builds a request, so a registered handler
// could never run and a "handler ran" flag could not fail. The error text and
// the absent result carry the test. The router refuses the same call with a
// typed field error, so these are not a divergence.
func TestDirectCallRefusesEmptyPathSegmentRows(t *testing.T) {
	t.Parallel()

	checked := 0
	for _, row := range handlerRejectedRequired {
		if !strings.HasPrefix(row.evidence, "direct dispatch:") {
			continue
		}
		checked++
		mux := http.NewServeMux()
		result, err := dispatchTool(context.Background(), mux, row.tool, map[string]any{}, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err == nil || result != nil {
			t.Errorf("%s: direct dispatch = (%+v, %v), want an error and no result", row.tool, result, err)
			continue
		}
		if want := row.missing[0] + " is required"; !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %q, want it to contain %q", row.tool, err, want)
		}
	}
	if checked != 5 {
		t.Errorf("checked %d direct-dispatch rows, want the 5 path-id tools", checked)
	}
}

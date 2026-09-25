// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// catalogSweepArgsPath is the checked-in per-tool argument table the live
// scoped-token catalog sweep (apps/console/e2e/authMcpE2ECatalogSweep.ts,
// scripts/run-auth-mcp-e2e.sh --module catalog-sweep) replays against a real
// stack. It lives under testdata/ so it adds nothing to this directory's
// dirgate file count.
const catalogSweepArgsPath = "testdata/catalog_sweep_args.json"

// catalogSweepPolicyOutEnv names the environment variable that makes
// TestCatalogSweepPolicy write the derived tool -> route -> class table for the
// sweep runner. Unset, the test still validates the table and writes nothing.
const catalogSweepPolicyOutEnv = "ESHU_CATALOG_SWEEP_POLICY_OUT"

// catalogSweepDisclosurePattern is the regular expression a tool description
// must match when its route refuses a scoped token, so the refusal is
// disclosed instead of silent (#5167 Scope: "the annotation says why, and the
// route's MCP tool description tells a caller holding a scoped token the same
// thing"). The runner applies the same pattern to the live tools/list output.
//
// The pattern is written in the syntax Go and JavaScript share: it carries no
// inline (?i) flag, which is a SyntaxError in JavaScript. Case-insensitivity is
// applied out of band through catalogSweepDisclosureFlags.
const catalogSweepDisclosurePattern = `\b403\b|shared[- ]key|shared ESHU_API_KEY|refused`

// catalogSweepDisclosureFlags is the JavaScript RegExp flag set the runner
// constructs the pattern with; Go applies the same case-insensitivity as (?i).
const catalogSweepDisclosureFlags = "i"

// Route classes the sweep expects a scoped personal token to meet.
const (
	catalogSweepClassAllowlisted     = "allowlisted"
	catalogSweepClassSharedKeyOnly   = "shared_key_only"
	catalogSweepClassPendingFiltered = "pending_row_filtering"
)

// catalogSweepCase is one checked-in call for one tool.
type catalogSweepCase struct {
	Label        string         `json:"label"`
	Arguments    map[string]any `json:"arguments"`
	Accept       []string       `json:"accept,omitempty"`
	AcceptReason string         `json:"acceptReason,omitempty"`
}

// catalogSweepArgsFile is the on-disk shape of the argument table.
type catalogSweepArgsFile struct {
	Tools map[string][]catalogSweepCase `json:"tools"`
}

// catalogSweepPolicyRow is one row of the emitted policy: a call plus the
// route it dispatches to and the class the route policy assigns it.
type catalogSweepPolicyRow struct {
	Tool         string         `json:"tool"`
	Label        string         `json:"label"`
	Method       string         `json:"method"`
	Path         string         `json:"path"`
	Class        string         `json:"class"`
	Arguments    map[string]any `json:"arguments"`
	Accept       []string       `json:"accept"`
	AcceptReason string         `json:"acceptReason,omitempty"`
}

// catalogSweepPolicy is the JSON document handed to the sweep runner.
type catalogSweepPolicy struct {
	DisclosurePattern string                  `json:"disclosurePattern"`
	DisclosureFlags   string                  `json:"disclosureFlags"`
	Rows              []catalogSweepPolicyRow `json:"rows"`
}

// acceptReasonProblem reports why a case's acceptReason cannot justify
// accepting anything other than "ok". A row that tolerates a not-found answer
// stops proving the tool succeeds for its seeded subject, so the reason must
// name the unseeded subject: it has to quote at least one of the case's own
// string arguments (three or more characters), which a boilerplate sentence
// shared across rows cannot do. It returns "" when the reason is acceptable.
func acceptReasonProblem(c catalogSweepCase) string {
	reason := strings.TrimSpace(c.AcceptReason)
	if reason == "" {
		return "has no acceptReason"
	}
	for _, value := range catalogSweepStringArguments(c.Arguments) {
		if len(value) >= 3 && strings.Contains(reason, value) {
			return ""
		}
	}
	return "its acceptReason quotes none of the case's own string arguments, so it does not name the unseeded subject"
}

// catalogSweepStringArguments collects every string value in an argument tree.
func catalogSweepStringArguments(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			out = append(out, catalogSweepStringArguments(item)...)
		}
		return out
	case map[string]any:
		var out []string
		for _, item := range v {
			out = append(out, catalogSweepStringArguments(item)...)
		}
		return out
	default:
		return nil
	}
}

func loadCatalogSweepArgs(t *testing.T) catalogSweepArgsFile {
	t.Helper()
	raw, err := os.ReadFile(catalogSweepArgsPath)
	if err != nil {
		t.Fatalf("read %s: %v", catalogSweepArgsPath, err)
	}
	var file catalogSweepArgsFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode %s: %v", catalogSweepArgsPath, err)
	}
	return file
}

// classifyCatalogSweepRoute derives the class from the same three predicates
// TestEveryMCPReachableRouteIsScopedOrAnnotated gates on, allowlist first, so
// the sweep's expectation cannot drift from the route policy.
func classifyCatalogSweepRoute(req *http.Request) (string, bool) {
	switch {
	case query.ScopedHTTPRouteSupportsTenantFilter(req):
		return catalogSweepClassAllowlisted, true
	case query.IsSharedKeyOnlyRoute(req):
		return catalogSweepClassSharedKeyOnly, true
	case query.IsPendingRowFilteringRoute(req):
		return catalogSweepClassPendingFiltered, true
	default:
		return "", false
	}
}

// TestCatalogSweepPolicy proves the checked-in argument table names exactly the
// tools ReadOnlyTools registers (a new tool without an entry fails here, so it
// cannot be skipped silently by the live sweep), that every case resolves to a
// route the scoped-route policy classifies, that a route refusing scoped tokens
// is disclosed in its tool description, and, when ESHU_CATALOG_SWEEP_POLICY_OUT
// is set, writes the derived tool -> route -> class table for the runner.
func TestCatalogSweepPolicy(t *testing.T) {
	file := loadCatalogSweepArgs(t)
	disclosure := regexp.MustCompile("(?" + catalogSweepDisclosureFlags + ")" + catalogSweepDisclosurePattern)

	registered := map[string]string{}
	for _, tool := range ReadOnlyTools() {
		registered[tool.Name] = tool.Description
	}
	for name := range registered {
		if _, ok := file.Tools[name]; !ok {
			t.Errorf("tool %q is registered but has no entry in %s -- add minimal valid arguments so the live scoped-token sweep covers it", name, catalogSweepArgsPath)
		}
	}
	for name := range file.Tools {
		if _, ok := registered[name]; !ok {
			t.Errorf("%s has an entry for %q, which is not a registered tool -- remove the stale entry", catalogSweepArgsPath, name)
		}
	}

	names := make([]string, 0, len(file.Tools))
	for name := range file.Tools {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]catalogSweepPolicyRow, 0, len(names))
	for _, name := range names {
		cases := file.Tools[name]
		if len(cases) == 0 {
			t.Errorf("tool %q has an empty case list", name)
			continue
		}
		seen := map[string]bool{}
		for _, c := range cases {
			if c.Label == "" || seen[c.Label] {
				t.Errorf("tool %q has a blank or duplicate case label %q", name, c.Label)
			}
			seen[c.Label] = true
			route, err := resolveRoute(name, c.Arguments)
			if err != nil {
				t.Errorf("tool %q case %q does not resolve to a route: %v", name, c.Label, err)
				continue
			}
			class, ok := classifyCatalogSweepRoute(httptest.NewRequest(route.Method, route.Path, nil))
			if !ok {
				t.Errorf("tool %q case %q dispatches to %s %s, which no scoped-route ledger classifies", name, c.Label, route.Method, route.Path)
				continue
			}
			accept := c.Accept
			switch class {
			case catalogSweepClassAllowlisted:
				if len(accept) == 0 {
					accept = []string{"ok"}
				}
				if len(c.Accept) > 0 {
					if problem := acceptReasonProblem(c); problem != "" {
						t.Errorf("tool %q case %q accepts %v but %s", name, c.Label, c.Accept, problem)
					}
				}
			default:
				if len(c.Accept) > 0 {
					t.Errorf("tool %q case %q routes to a %s route, whose expected outcome is a disclosed 403; it must not declare accept", name, c.Label, class)
				}
				if !disclosure.MatchString(registered[name]) {
					t.Errorf("tool %q case %q routes to %s %s (%s) which refuses a scoped token, but the tool description never says so (pattern %s)", name, c.Label, route.Method, route.Path, class, catalogSweepDisclosurePattern)
				}
			}
			rows = append(rows, catalogSweepPolicyRow{
				Tool: name, Label: c.Label, Method: route.Method, Path: route.Path,
				Class: class, Arguments: c.Arguments, Accept: accept, AcceptReason: c.AcceptReason,
			})
		}
	}

	out := os.Getenv(catalogSweepPolicyOutEnv)
	if out == "" || t.Failed() {
		return
	}
	encoded, err := json.MarshalIndent(catalogSweepPolicy{DisclosurePattern: catalogSweepDisclosurePattern, DisclosureFlags: catalogSweepDisclosureFlags, Rows: rows}, "", "  ")
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	if err := os.WriteFile(out, append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
}

// TestCatalogSweepPolicyRejectsUnclassifiedRoute is the negative control for
// classifyCatalogSweepRoute: a route in none of the three ledgers must come
// back unclassified, so the table test above cannot pass on a classifier that
// accepts everything.
func TestCatalogSweepPolicyRejectsUnclassifiedRoute(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/definitely-not-a-real-mcp-route", nil)
	if class, ok := classifyCatalogSweepRoute(req); ok {
		t.Fatalf("classifyCatalogSweepRoute(unknown route) = %q, want unclassified", class)
	}
}

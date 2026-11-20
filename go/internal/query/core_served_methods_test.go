// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/support"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintelhttp"
)

// frozenServedMethodsPath is the mechanical enumeration of the forwarded
// serving surface (Wave-6 D4.1): every Method+Path the satellite at the
// pinned commit can forward to core, plus the two core-owned probe rows.
// Regenerate with scripts/gen-core-served-methods.sh; never hand-edit.
const frozenServedMethodsPath = "testdata/core_served_methods_frozen.txt"

// frozenFloorRows pins the ruling floor inside the enumeration: the 19
// survivor mutations plus the two probe rows must always be present.
// (The 7 riders are catch-all spellings, not registered rows; core
// judges them with 404/405 and the relay carries the bytes back.)
var frozenFloorRows = []string{
	"POST /api/v0/admin/backfill",
	"POST /api/v0/admin/dead-letter",
	"POST /api/v0/admin/reindex",
	"POST /api/v0/admin/skip",
	"POST /api/v0/auth/setup/claim",
	"POST /api/v0/auth/setup/admin",
	"POST /api/v0/auth/setup/mfa",
	"POST /api/v0/admin/replay",
	"POST /api/v0/admin/reopen",
	"POST /api/v0/admin/refinalize",
	"POST /api/v0/admin/recover-generations",
	"PATCH /api/v0/auth/admin/sign-in-policy",
	"GET /api/v0/auth/oidc/login",
	"GET /api/v0/auth/oidc/callback",
	"GET /api/v0/auth/github/login",
	"GET /api/v0/auth/github/callback",
	"GET /api/v0/auth/saml/providers/{provider_id}/metadata",
	"GET /api/v0/auth/saml/providers/{provider_id}/login",
	"POST /api/v0/auth/saml/providers/{provider_id}/acs",
	"GET /health",
	"GET /readyz",
}

func loadFrozenServedMethods(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(frozenServedMethodsPath)
	if err != nil {
		t.Fatalf("read frozen list: %v", err)
	}
	var rows []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rows = append(rows, line)
	}
	if len(rows) == 0 {
		t.Fatal("frozen list is empty")
	}
	return rows
}

func specServedMethods(t *testing.T) map[string]bool {
	t.Helper()
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(query.OpenAPISpec()), &spec); err != nil {
		t.Fatalf("parse OpenAPI spec: %v", err)
	}
	out := make(map[string]bool, len(spec.Paths))
	for path, ops := range spec.Paths {
		for method := range ops {
			out[strings.ToUpper(method)+" "+path] = true
		}
	}
	return out
}

// TestCoreServedMethodsMatchSpec pins the enumeration against the assembled
// OpenAPI spec: frozen and spec agree exactly, minus the /readyz probe row
// (a backend-gated probe, deliberately outside the API spec like the
// satellite's own readyz). Adding a route without updating the frozen list,
// or editing the list without the matching spec fragment, fails here.
func TestCoreServedMethodsMatchSpec(t *testing.T) {
	t.Parallel()
	frozen := loadFrozenServedMethods(t)
	spec := specServedMethods(t)

	frozenSet := make(map[string]bool, len(frozen))
	for _, row := range frozen {
		if frozenSet[row] {
			t.Errorf("frozen list has duplicate row %q", row)
		}
		frozenSet[row] = true
	}
	for _, want := range frozenFloorRows {
		if !frozenSet[want] {
			t.Errorf("frozen list dropped floor row %q", want)
		}
	}

	var missing, extra []string
	for row := range frozenSet {
		if row == "GET /readyz" {
			continue
		}
		if !spec[row] {
			missing = append(missing, row)
		}
	}
	for row := range spec {
		if !frozenSet[row] {
			extra = append(extra, row)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	for _, row := range missing {
		t.Errorf("frozen row %q not in OpenAPI spec (add the fragment or drop the row)", row)
	}
	for _, row := range extra {
		t.Errorf("spec row %q not in frozen list (regenerate or remove the route)", row)
	}
}

var probeParamSegment = regexp.MustCompile(`\{[^}]+\}`)

// probePath substitutes a concrete segment for every mux path parameter so a
// bogus-method probe walks the same mux subtree the frozen row registers.
func probePath(pattern string) string {
	return probeParamSegment.ReplaceAllString(pattern, "x")
}

// newCoreFullTestMux builds the test mux exactly like production main does:
// the bare query mux plus the import-cycle-forced extras (CLI-support
// handlers with real in-process behavior, the service-intelligence report
// with nil sources). The 405 probe never executes handlers, so nil stores
// are safe.
func newCoreFullTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux, err := query.NewCoreTestMux()
	if err != nil {
		t.Fatalf("NewCoreTestMux: %v", err)
	}
	(&support.Handlers{}).Mount(mux)
	(&serviceintelhttp.ReportHandler{}).Mount(mux)
	return mux
}

// TestCoreMuxServesFrozenSet probes the production mux construction with a
// bogus method: every frozen path must answer 405 (path registered, method
// rejected by the mux itself, no handler executed) rather than 404. A nil
// handler in the router (an unwired family) or a dropped Mount fails here.
func TestCoreMuxServesFrozenSet(t *testing.T) {
	t.Parallel()
	frozen := loadFrozenServedMethods(t)
	mux := newCoreFullTestMux(t)

	var unserved []string
	for _, row := range frozen {
		_, path, _ := strings.Cut(row, " ")
		req := httptest.NewRequest("FOO", probePath(path), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			unserved = append(unserved, row)
		}
	}
	sort.Strings(unserved)
	for _, row := range unserved {
		t.Errorf("mux does not serve frozen row %q (405 expected, got mux 404)", row)
	}
}

var (
	muxPatternLiteral = regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS) [^"]*?"`)
	routeAssign       = regexp.MustCompile(`^\s*(?:const\s+|var\s+)?(\w+)\s*:?=\s*"(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS) [^"]*?"`)
	pathConstAssign   = regexp.MustCompile(`^\s*(?:const\s+|var\s+)?(\w+)\s*:?=\s*"(/[^"]*)"`)
	bareHandleCall    = regexp.MustCompile(`\.Handle(?:Func)?\(\s*([A-Za-z_]\w*)\s*,`)
	// Bare path literals: mux.Handle("/metrics", ...) registers a path with
	// no method prefix, which muxPatternLiteral (method-first) and
	// bareHandleCall (identifier-first) both miss. Without this, a
	// non-forwarded infrastructure mount is invisible to the drift scan.
	barePathLiteral  = regexp.MustCompile(`\.Handle(?:Func)?\(\s*"(/[^\"]*)"`)
	methodConcatCall = regexp.MustCompile(`\.Handle(?:Func)?\(\s*"(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS) "\s*\+\s*([A-Za-z_]\w*)`)
)

// collectMountedPatterns scans the serving sources for registered patterns:
// string literals on mux.Handle/HandleFunc lines (method-prefixed and bare
// paths alike), plus route-pattern consts referenced by bare-identifier
// Handle calls. A registration the scan cannot resolve to a literal
// (dynamic concatenation, slash doubles) is reported, not silently passed:
// such a mount is a review failure.
func collectMountedPatterns(t *testing.T, roots []string) (mounted map[string]bool, unresolvable []string) {
	t.Helper()
	mounted = map[string]bool{}
	consts := map[string]string{}
	pathConsts := map[string]string{}
	type ref struct {
		file string
		line int
		name string
	}
	var refs []ref
	type concatRef struct {
		file   string
		line   int
		method string
		name   string
	}
	var concatRefs []concatRef
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read source root %s: %v", root, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				if name == "testdata" {
					continue
				}
				sub, unres := collectMountedPatterns(t, []string{filepath.Join(root, name)})
				for k := range sub {
					mounted[k] = true
				}
				unresolvable = append(unresolvable, unres...)
				continue
			}
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			for i, line := range strings.Split(string(raw), "\n") {
				code := line
				if idx := strings.Index(code, "//"); idx >= 0 {
					code = code[:idx]
				}
				if m := routeAssign.FindStringSubmatch(code); m != nil {
					if lit := muxPatternLiteral.FindString(code); lit != "" {
						consts[m[1]] = strings.Trim(lit, `"`)
					}
				}
				if m := pathConstAssign.FindStringSubmatch(code); m != nil {
					pathConsts[m[1]] = m[2]
				}
				if !strings.Contains(code, ".Handle(") && !strings.Contains(code, ".HandleFunc(") {
					continue
				}
				for _, idx := range muxPatternLiteral.FindAllStringIndex(code, -1) {
					lit := code[idx[0]:idx[1]]
					// A literal fragment of a concatenation ("POST " +
					// RouteConst) is resolved by the concat rule below,
					// not mounted as-is.
					rest := strings.TrimLeft(code[idx[1]:], " \t")
					if strings.HasPrefix(rest, "+") {
						continue
					}
					mounted[strings.Trim(lit, `"`)] = true
				}
				for _, m := range bareHandleCall.FindAllStringSubmatch(code, -1) {
					refs = append(refs, ref{file: name, line: i + 1, name: m[1]})
				}
				for _, m := range barePathLiteral.FindAllStringSubmatch(code, -1) {
					mounted[m[1]] = true
				}
				for _, m := range methodConcatCall.FindAllStringSubmatch(code, -1) {
					concatRefs = append(concatRefs, concatRef{file: name, line: i + 1, method: m[1], name: m[2]})
				}
			}
		}
	}
	for _, r := range refs {
		pat, ok := consts[r.name]
		if !ok {
			unresolvable = append(unresolvable, fmt.Sprintf("%s:%d: dynamic pattern %s", r.file, r.line, r.name))
			continue
		}
		mounted[pat] = true
	}
	for _, r := range concatRefs {
		path, ok := pathConsts[r.name]
		if !ok {
			unresolvable = append(unresolvable, fmt.Sprintf("%s:%d: dynamic pattern %s %s", r.file, r.line, r.method, r.name))
			continue
		}
		mounted[r.method+" "+path] = true
	}
	return mounted, unresolvable
}

// TestCoreSourceAddsNoUnfrozenRoute scans the serving sources for mux
// registrations and requires every one to be a frozen row: mounting a route
// without freezing it (spec fragment or not) fails here.
func TestCoreSourceAddsNoUnfrozenRoute(t *testing.T) {
	t.Parallel()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	pkgDir := filepath.Dir(thisFile)
	mounted, unresolvable := collectMountedPatterns(t, []string{
		pkgDir,
		filepath.Join(pkgDir, "..", "cli", "support"),
		// The serving binary mounts one route outside package query: the
		// /metrics scrape endpoint (go/cmd/core-api, ExtraMounts). It is
		// carved out below, never frozen: infrastructure scraping, not
		// forwarded API surface.
		filepath.Join(pkgDir, "..", "..", "cmd", "core-api"),
	})
	for _, u := range unresolvable {
		t.Errorf("unresolvable registration (dynamic pattern): %s", u)
	}
	// The scan must actually resolve the cmd/core-api scrape endpoint: a
	// registration the scanner cannot see passes vacuously. If this fails,
	// the scan roots or the bare-path pattern regressed.
	if !mounted["/metrics"] {
		t.Errorf("drift scan did not resolve /metrics from go/cmd/core-api")
	}
	frozen := loadFrozenServedMethods(t)
	frozenSet := make(map[string]bool, len(frozen))
	for _, row := range frozen {
		frozenSet[row] = true
	}
	// mountedButNotForwarded are source-registered patterns outside the
	// forwarded enumeration, each pinned to its exact mount state by
	// TestCoreMuxMountStatePinsCarveOuts: the doc shells are mounted but
	// never relayed (the satellite answers them dest-side), while the
	// well-known discovery routes are registered only by a handler the
	// serving path never mounts (the satellite never forwards them).
	mountedButNotForwarded := map[string]bool{
		"GET /api/v0/docs":                                    true,
		"GET /api/v0/redoc":                                   true,
		"GET /.well-known/oauth-protected-resource":           true,
		"GET /.well-known/oauth-protected-resource/{rest...}": true,
		// "/metrics" (no method prefix: bare mux.Handle in cmd/core-api)
		// is the Prometheus scrape endpoint: mounted by the binary, never
		// forwarded by the satellite, hence intentionally unfrozen.
		"/metrics": true,
	}
	var extra []string
	for pat := range mounted {
		if !frozenSet[pat] && !mountedButNotForwarded[pat] {
			extra = append(extra, pat)
		}
	}
	sort.Strings(extra)
	for _, pat := range extra {
		t.Errorf("mounted pattern %q not in frozen list (freeze it or remove the route)", pat)
	}
}

// TestCoreMuxMountStatePinsCarveOuts closes the carve-outs in both
// directions. The doc shells must stay mounted (405 on bogus method): if a
// future change unmounts one, its carve-out is unjustified. The well-known
// discovery routes must stay unmounted (mux 404): the satellite never
// forwards them, so mounting them would add unfrozen surface — freeze or
// justify first.
func TestCoreMuxMountStatePinsCarveOuts(t *testing.T) {
	t.Parallel()
	mux := newCoreFullTestMux(t)
	for _, path := range []string{"/api/v0/docs", "/api/v0/redoc"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("FOO", path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s carve-out unjustified: want mux 405 (mounted), got %d", path, rec.Code)
		}
	}
	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, probePath(path), nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s must stay unmounted: want mux 404, got %d", path, rec.Code)
		}
	}
}

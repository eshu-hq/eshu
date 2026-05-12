package postgres

import (
	"context"
	"sort"
	"sync"
	"testing"
)

// recordedReason captures one unresolvedRecorder.record call for assertions.
type recordedReason struct {
	reason string
}

// fakeUnresolvedRecorder records every call and is safe under concurrent use.
type fakeUnresolvedRecorder struct {
	mu       sync.Mutex
	captured []recordedReason
}

func (r *fakeUnresolvedRecorder) record(_ context.Context, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.captured = append(r.captured, recordedReason{reason: reason})
}

func (r *fakeUnresolvedRecorder) reasons() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.captured))
	for i, c := range r.captured {
		out[i] = c.reason
	}
	sort.Strings(out)
	return out
}

// fixtureModuleCallRow returns the JSON shape the parser emits for one
// terraform_modules entry. Mirrors go/internal/parser/hcl/parser.go:192-208.
func fixtureModuleCallRow(name, source, path string) string {
	return `{"name":"` + name + `","source":"` + source + `","path":"` + path + `","lang":"hcl","line_number":1}`
}

// fixtureModuleCallsArray wraps zero or more module-call rows into a JSON
// array — the shape stored at parsed_file_data.terraform_modules.
func fixtureModuleCallsArray(rows ...string) []byte {
	out := "["
	for i, row := range rows {
		if i > 0 {
			out += ","
		}
		out += row
	}
	return []byte(out + "]")
}

// fixtureConfigParserRowAtPath mirrors fixtureConfigParserRow but lets the
// caller set the file path so module-aware joining can route a resource to
// the correct callee directory.
func fixtureConfigParserRowAtPath(resourceType, resourceName, path string) string {
	return `{
        "name":"` + resourceType + `.` + resourceName + `",
        "resource_type":"` + resourceType + `",
        "resource_name":"` + resourceName + `",
        "path":"` + path + `",
        "lang":"hcl",
        "line_number":1
    }`
}

// loaderForBuildModulePrefixMap returns a loader stubbed only enough to
// run buildModulePrefixMap. The helper isolates the module-prefix unit
// tests from the four-input loader path.
func loaderForBuildModulePrefixMap(rows []queueFakeRows) (PostgresDriftEvidenceLoader, *fakeExecQueryer) {
	db := &fakeExecQueryer{queryResponses: rows}
	return PostgresDriftEvidenceLoader{DB: db}, db
}

// runBuildModulePrefixMap executes buildModulePrefixMap with a fake recorder
// and returns the resulting map plus the recorded reasons.
func runBuildModulePrefixMap(t *testing.T, rows []queueFakeRows) (modulePrefixMap, *fakeUnresolvedRecorder) {
	t.Helper()
	loader, _ := loaderForBuildModulePrefixMap(rows)
	rec := &fakeUnresolvedRecorder{}
	out, err := loader.buildModulePrefixMap(context.Background(), "scope-x", "gen-x", rec)
	if err != nil {
		t.Fatalf("buildModulePrefixMap() error = %v", err)
	}
	return out, rec
}

// --- Phase 1: helper unit tests --------------------------------------------

func TestBuildModulePrefixMapSingleLevelCalleeFromLocalSource(t *testing.T) {
	t.Parallel()

	out, rec := runBuildModulePrefixMap(t, []queueFakeRows{{
		rows: [][]any{{fixtureModuleCallsArray(
			fixtureModuleCallRow("vpc", "./modules/vpc", "main.tf"),
		)}},
	}})

	prefixes, ok := out["modules/vpc"]
	if !ok {
		t.Fatalf("prefix map missing key %q (have %v)", "modules/vpc", out)
	}
	if len(prefixes) != 1 || prefixes[0] != "module.vpc" {
		t.Fatalf("prefixes = %v, want [module.vpc]", prefixes)
	}
	if got := rec.reasons(); len(got) != 0 {
		t.Fatalf("recorded reasons = %v, want none", got)
	}
}

func TestBuildModulePrefixMapNestedChainProducesMultiLevelPrefix(t *testing.T) {
	t.Parallel()

	out, rec := runBuildModulePrefixMap(t, []queueFakeRows{{
		rows: [][]any{
			{fixtureModuleCallsArray(fixtureModuleCallRow("platform", "./modules/platform", "main.tf"))},
			{fixtureModuleCallsArray(fixtureModuleCallRow("vpc", "./vpc", "modules/platform/main.tf"))},
		},
	}})

	got, ok := out["modules/platform/vpc"]
	if !ok {
		t.Fatalf("prefix map missing key %q (have %v)", "modules/platform/vpc", out)
	}
	if len(got) != 1 || got[0] != "module.platform.module.vpc" {
		t.Fatalf("nested prefix = %v, want [module.platform.module.vpc]", got)
	}
	if reasons := rec.reasons(); len(reasons) != 0 {
		t.Fatalf("recorded reasons = %v, want none", reasons)
	}
}

func TestBuildModulePrefixMapForwardSlashSemanticsRegression(t *testing.T) {
	t.Parallel()

	// Inputs that would mis-bucket if the helper used path/filepath.Clean
	// (OS-specific separators) instead of path.Clean (forward-slash only):
	//   - double slashes ("./modules//vpc") collapse to single
	//   - trailing slashes ("./modules/vpc/") drop
	//   - "./." segments collapse
	//
	// All inputs must produce the same canonical key "modules/vpc" so the
	// prefix map joins on the cleaned form. This test locks binding
	// constraint A in.
	out, _ := runBuildModulePrefixMap(t, []queueFakeRows{{
		rows: [][]any{
			{fixtureModuleCallsArray(fixtureModuleCallRow("a", "./modules//vpc", "main.tf"))},
			{fixtureModuleCallsArray(fixtureModuleCallRow("b", "./modules/vpc/", "main.tf"))},
			{fixtureModuleCallsArray(fixtureModuleCallRow("c", "./modules/./vpc", "main.tf"))},
		},
	}})
	prefixes, ok := out["modules/vpc"]
	if !ok {
		t.Fatalf("prefix map missing key %q after path.Clean normalization (have %v)", "modules/vpc", out)
	}
	if len(prefixes) != 3 {
		t.Fatalf("prefixes = %v, want 3 distinct callers (a, b, c) collapsed onto one canonical key", prefixes)
	}
	want := []string{"module.a", "module.b", "module.c"}
	if !sliceEqual(prefixes, want) {
		t.Fatalf("prefixes = %v, want %v", prefixes, want)
	}
}

func TestBuildModulePrefixMapRejectsRegistrySource(t *testing.T) {
	t.Parallel()

	out, rec := runBuildModulePrefixMap(t, []queueFakeRows{{
		rows: [][]any{{fixtureModuleCallsArray(
			fixtureModuleCallRow("vpc", "terraform-aws-modules/vpc/aws", "main.tf"),
		)}},
	}})
	if len(out) != 0 {
		t.Fatalf("prefix map = %v, want empty (registry source unresolvable)", out)
	}
	if got := rec.reasons(); len(got) != 1 || got[0] != unresolvedReasonExternalRegistry {
		t.Fatalf("recorded reasons = %v, want [%s]", got, unresolvedReasonExternalRegistry)
	}
}

func TestBuildModulePrefixMapRejectsGitSource(t *testing.T) {
	t.Parallel()

	out, rec := runBuildModulePrefixMap(t, []queueFakeRows{{
		rows: [][]any{{fixtureModuleCallsArray(
			fixtureModuleCallRow("vpc", "git::https://github.com/example/vpc.git", "main.tf"),
		)}},
	}})
	if len(out) != 0 {
		t.Fatalf("prefix map = %v, want empty (git source unresolvable)", out)
	}
	if got := rec.reasons(); len(got) != 1 || got[0] != unresolvedReasonExternalGit {
		t.Fatalf("recorded reasons = %v, want [%s]", got, unresolvedReasonExternalGit)
	}
}

func TestBuildModulePrefixMapRejectsHttpArchive(t *testing.T) {
	t.Parallel()

	out, rec := runBuildModulePrefixMap(t, []queueFakeRows{{
		rows: [][]any{{fixtureModuleCallsArray(
			fixtureModuleCallRow("vpc", "https://example.com/module.zip", "main.tf"),
		)}},
	}})
	if len(out) != 0 {
		t.Fatalf("prefix map = %v, want empty (http archive unresolvable)", out)
	}
	if got := rec.reasons(); len(got) != 1 || got[0] != unresolvedReasonExternalArchive {
		t.Fatalf("recorded reasons = %v, want [%s]", got, unresolvedReasonExternalArchive)
	}
}

func TestBuildModulePrefixMapRejectsCrossRepoEscape(t *testing.T) {
	t.Parallel()

	// "../../other-repo/modules/vpc" cleans to "../other-repo/modules/vpc"
	// after path.Clean on a call site of "main.tf" (Dir = "."), which
	// starts with ".." and thus escapes the repo snapshot root.
	out, rec := runBuildModulePrefixMap(t, []queueFakeRows{{
		rows: [][]any{{fixtureModuleCallsArray(
			fixtureModuleCallRow("vpc", "../../other-repo/modules/vpc", "main.tf"),
		)}},
	}})
	if len(out) != 0 {
		t.Fatalf("prefix map = %v, want empty (cross-repo escape)", out)
	}
	if got := rec.reasons(); len(got) != 1 || got[0] != unresolvedReasonCrossRepoLocal {
		t.Fatalf("recorded reasons = %v, want [%s]", got, unresolvedReasonCrossRepoLocal)
	}
}

func TestBuildModulePrefixMapDetectsCycleAndBreaks(t *testing.T) {
	t.Parallel()

	// Two callees mutually reference each other:
	//   modules/a/main.tf calls "../b" -> modules/b
	//   modules/b/main.tf calls "../a" -> modules/a
	//
	// The walk must record cycle_detected at least once and not exceed
	// maxModulePrefixDepth. The prefix map still contains entries for the
	// initial visits but cycle expansion stops at the second visit.
	out, rec := runBuildModulePrefixMap(t, []queueFakeRows{{
		rows: [][]any{
			{fixtureModuleCallsArray(fixtureModuleCallRow("b", "../b", "modules/a/main.tf"))},
			{fixtureModuleCallsArray(fixtureModuleCallRow("a", "../a", "modules/b/main.tf"))},
		},
	}})
	// Both callees got at least one prefix entry from the first walk.
	if _, ok := out["modules/a"]; !ok {
		t.Fatalf("prefix map missing modules/a (have %v)", out)
	}
	if _, ok := out["modules/b"]; !ok {
		t.Fatalf("prefix map missing modules/b (have %v)", out)
	}
	reasons := rec.reasons()
	foundCycle := false
	for _, r := range reasons {
		if r == unresolvedReasonCycleDetected {
			foundCycle = true
		}
	}
	if !foundCycle {
		t.Fatalf("recorded reasons = %v, want at least one %s", reasons, unresolvedReasonCycleDetected)
	}
}

func TestBuildModulePrefixMapEnforcesDepthBound(t *testing.T) {
	t.Parallel()

	// A chain of maxModulePrefixDepth+1 nested module calls. The expansion
	// must refuse the (maxModulePrefixDepth+1)-th step and increment
	// depth_exceeded. Validates binding constraint B.
	chain := make([]string, 0, maxModulePrefixDepth+1)
	prevDir := "."
	for i := 0; i <= maxModulePrefixDepth; i++ {
		callerFile := "main.tf"
		if prevDir != "." {
			callerFile = prevDir + "/main.tf"
		}
		chain = append(chain, fixtureModuleCallRow(
			"m"+intToA(i),
			"./inner",
			callerFile,
		))
		if prevDir == "." {
			prevDir = "inner"
		} else {
			prevDir = prevDir + "/inner"
		}
	}
	rows := make([][]any, len(chain))
	for i, row := range chain {
		rows[i] = []any{fixtureModuleCallsArray(row)}
	}
	_, rec := runBuildModulePrefixMap(t, []queueFakeRows{{rows: rows}})
	reasons := rec.reasons()
	foundDepth := false
	for _, r := range reasons {
		if r == unresolvedReasonDepthExceeded {
			foundDepth = true
		}
	}
	if !foundDepth {
		t.Fatalf("recorded reasons = %v, want at least one %s", reasons, unresolvedReasonDepthExceeded)
	}
}

func TestBuildModulePrefixMapHandlesEmptyTerraformModulesFact(t *testing.T) {
	t.Parallel()

	out, rec := runBuildModulePrefixMap(t, []queueFakeRows{{rows: [][]any{}}})
	if len(out) != 0 {
		t.Fatalf("prefix map = %v, want empty", out)
	}
	if got := rec.reasons(); len(got) != 0 {
		t.Fatalf("recorded reasons = %v, want none", got)
	}
}

func TestBuildModulePrefixMapHandlesBlankSource(t *testing.T) {
	t.Parallel()

	// Empty source string — parser may emit this when the source attribute
	// is unparseable. Falls back to the external_archive catch-all per ADR.
	out, rec := runBuildModulePrefixMap(t, []queueFakeRows{{
		rows: [][]any{{fixtureModuleCallsArray(
			fixtureModuleCallRow("vpc", "", "main.tf"),
		)}},
	}})
	if len(out) != 0 {
		t.Fatalf("prefix map = %v, want empty (blank source)", out)
	}
	if got := rec.reasons(); len(got) != 1 || got[0] != unresolvedReasonExternalArchive {
		t.Fatalf("recorded reasons = %v, want [%s]", got, unresolvedReasonExternalArchive)
	}
}

// Loader integration tests for module-aware joining live in
// tfstate_drift_evidence_module_integration_test.go (split for the
// CLAUDE.md 500-line cap; same package, same fixtures).

// --- helpers ---------------------------------------------------------------

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// intToA converts a small non-negative int to its decimal string. Avoids
// pulling strconv into the test file for one-shot use.
func intToA(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

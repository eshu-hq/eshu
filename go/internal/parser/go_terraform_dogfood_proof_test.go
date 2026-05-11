package parser

import (
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestGoTerraformDogfoodParseProof times engine.ParsePath against a
// representative small/medium/large Go file from a real Terraform checkout.
// The test is the proof gate for #161: it isolates per-file cost so we can
// confirm the rebuilt parser path (after the goCollectSemanticDeadCodeRoots
// removal in PreScan and the per-file goParentLookup + variableTypeIndex
// fixes) stays bounded on dense, call_expression-heavy Go before launching a
// full repo-scale indexing run.
//
// Skipped unless TF_DOGFOOD_REPO is set, e.g.:
//
//	TF_DOGFOOD_REPO=/Users/asanabria/os-repos/terraform \
//	  go test ./internal/parser -run TestGoTerraformDogfoodParseProof -v -count=1
//
// A CPU profile of the largest file lands under the test's TempDir as
// terraform-large.cpu.pprof; the test logs the absolute path so callers can
// drive `go tool pprof` on it directly.
func TestGoTerraformDogfoodParseProof(t *testing.T) {
	repoRoot := strings.TrimSpace(os.Getenv("TF_DOGFOOD_REPO"))
	if repoRoot == "" {
		t.Skip("TF_DOGFOOD_REPO not set")
	}
	if _, err := os.Stat(repoRoot); err != nil {
		t.Skipf("TF_DOGFOOD_REPO=%q does not exist: %v", repoRoot, err)
	}

	target := filepath.Join(repoRoot, "internal", "terraform")
	candidates, err := collectDogfoodGoFiles(target)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(candidates) < 3 {
		t.Skipf("only %d .go files under %s, need at least 3", len(candidates), target)
	}

	small := pickByLineCount(t, candidates, 150, 400)
	medium := pickByLineCount(t, candidates, 800, 1500)
	large := pickByLineCount(t, candidates, 2000, 1<<30)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}

	t.Logf("dogfood files: small=%s medium=%s large=%s",
		filepath.Base(small.path), filepath.Base(medium.path), filepath.Base(large.path))

	// Warm caches identically across runs by parsing once and discarding before
	// timing the small file. tree_sitter parser objects amortize allocation
	// state on second invocation, so the first measurement is otherwise noisy.
	if _, err := engine.ParsePath(repoRoot, small.path, false, Options{}); err != nil {
		t.Fatalf("warmup ParsePath: %v", err)
	}

	cases := []dogfoodCase{small, medium, large}
	for index, c := range cases {
		start := time.Now()
		if _, err := engine.ParsePath(repoRoot, c.path, false, Options{}); err != nil {
			t.Fatalf("ParsePath(%s): %v", c.path, err)
		}
		elapsed := time.Since(start)
		t.Logf("CASE size=%-6s lines=%-5d wall=%-10s path=%s",
			c.label, c.lines, elapsed, c.path)

		gate := dogfoodGate(c.label)
		if elapsed > gate {
			t.Errorf("%s ParsePath took %s; gate is %s (file=%s)", c.label, elapsed, gate, c.path)
		}

		if index == len(cases)-1 {
			profPath := strings.TrimSpace(os.Getenv("TF_DOGFOOD_CPUPROF"))
			if profPath == "" {
				profPath = filepath.Join(t.TempDir(), "terraform-large.cpu.pprof")
			}
			profileLargest(t, engine, repoRoot, c.path, profPath)
			t.Logf("CPU profile (large, 10x repeat): %s", profPath)
		}
	}
}

// dogfoodCase pairs a Terraform Go file path with the line bucket label and
// raw line count, used purely for log readability and per-bucket gating.
type dogfoodCase struct {
	label string
	path  string
	lines int
}

// dogfoodGate returns the per-file wall-time gate for the given size bucket.
// The numbers are intentionally loose — we are isolating "did the rebuild
// take effect" and "is there a fourth O(n^2) bomb," not enforcing the full
// production performance envelope here.
func dogfoodGate(label string) time.Duration {
	switch label {
	case "small":
		return 500 * time.Millisecond
	case "medium":
		return 2 * time.Second
	case "large":
		return 5 * time.Second
	}
	return 10 * time.Second
}

// profileLargest captures a CPU profile across 10 sequential ParsePath calls
// on the largest dogfood file so a single short run still produces a profile
// dense enough for `go tool pprof` to pinpoint hot helpers.
func profileLargest(t *testing.T, engine *Engine, repoRoot, path, dest string) {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatalf("create cpu profile: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := pprof.StartCPUProfile(f); err != nil {
		t.Fatalf("start cpu profile: %v", err)
	}
	defer pprof.StopCPUProfile()
	for i := range 10 {
		if _, err := engine.ParsePath(repoRoot, path, false, Options{}); err != nil {
			t.Fatalf("ParsePath profile iter %d: %v", i, err)
		}
	}
}

// collectDogfoodGoFiles returns non-test .go files under root sorted by line
// count ascending, so the picker can find a small/medium/large representative
// for the bucket-based proof.
func collectDogfoodGoFiles(root string) ([]dogfoodCase, error) {
	var cases []dogfoodCase
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		cases = append(cases, dogfoodCase{path: path, lines: countLines(data)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].lines < cases[j].lines })
	return cases, nil
}

// countLines returns the number of newline-delimited lines in data, plus one
// if the buffer is non-empty and does not end in a newline.
func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	count := strings.Count(string(data), "\n")
	if data[len(data)-1] != '\n' {
		count++
	}
	return count
}

// TestGoTerraformDogfoodPackageSemanticRootsProof times the Go-specific
// per-package semantic roots prescan (engine.PreScanGoPackageSemanticRoots)
// against a Terraform .go subset. This is the SECOND prescan stage the
// collector runs after the per-language prescan; an O(call_expressions ×
// tree_size) full-tree walk inside ImportedDirectMethodCallRoots saturated
// CPU on Terraform without emitting any facts (#161 follow-up). Skipped
// unless TF_DOGFOOD_REPO is set.
func TestGoTerraformDogfoodPackageSemanticRootsProof(t *testing.T) {
	repoRoot := strings.TrimSpace(os.Getenv("TF_DOGFOOD_REPO"))
	if repoRoot == "" {
		t.Skip("TF_DOGFOOD_REPO not set")
	}
	target := filepath.Join(repoRoot, "internal", "terraform")
	candidates, err := collectDogfoodGoFiles(target)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	subset := pickSubset(candidates, 50)
	if len(subset) == 0 {
		t.Skipf("no .go files under %s", target)
	}
	paths := make([]string, 0, len(subset))
	for _, c := range subset {
		paths = append(paths, c.path)
	}

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}

	profPath := strings.TrimSpace(os.Getenv("TF_DOGFOOD_PRESCAN_CPUPROF"))
	if profPath == "" {
		profPath = filepath.Join(t.TempDir(), "package-prescan.cpu.pprof")
	}
	f, err := os.Create(profPath)
	if err != nil {
		t.Fatalf("create cpu profile: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := pprof.StartCPUProfile(f); err != nil {
		t.Fatalf("start cpu profile: %v", err)
	}

	start := time.Now()
	if _, err := engine.PreScanGoPackageSemanticRoots(repoRoot, paths); err != nil {
		pprof.StopCPUProfile()
		t.Fatalf("PreScanGoPackageSemanticRoots: %v", err)
	}
	elapsed := time.Since(start)
	pprof.StopCPUProfile()

	t.Logf("PRESCAN_PACKAGE files=%d wall=%s avg_per_file=%s profile=%s",
		len(paths), elapsed, elapsed/time.Duration(len(paths)), profPath)

	// Loose acceptance gate: 50 dense Go files (3000-line files included)
	// must finish under 10 seconds. The previous broken path took 80+ CPU
	// minutes on 1927 files; 10s here is a generous ceiling that still
	// catches the O(call_expressions × tree_size) regression.
	if elapsed > 10*time.Second {
		t.Errorf("PreScanGoPackageSemanticRoots over %d files took %s; gate is <10s", len(paths), elapsed)
	}
}

// pickSubset returns a sampled subset of size up to n, biased to include the
// largest files in candidates so the proof exercises dense input. candidates
// is assumed sorted ascending by line count.
func pickSubset(candidates []dogfoodCase, n int) []dogfoodCase {
	if len(candidates) <= n {
		return candidates
	}
	// take the top-n by line count (densest) so the proof maximises
	// pathological per-file pressure on the prescan helpers.
	return candidates[len(candidates)-n:]
}

// pickByLineCount returns the candidate whose line count falls inside
// [minLines, maxLines]. If multiple match, the median is chosen so dense
// outliers do not skew the small/medium buckets.
func pickByLineCount(t *testing.T, candidates []dogfoodCase, minLines, maxLines int) dogfoodCase {
	t.Helper()
	var matches []dogfoodCase
	for _, c := range candidates {
		if c.lines >= minLines && c.lines <= maxLines {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		t.Fatalf("no Terraform file with %d <= lines <= %d", minLines, maxLines)
	}
	label := "medium"
	switch {
	case maxLines <= 400:
		label = "small"
	case minLines >= 2000:
		label = "large"
	}
	picked := matches[len(matches)/2]
	picked.label = label
	return picked
}

package skillgen

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// repoRootDir resolves the repository root from this test file. The
// skill-fragments/, expected/, and specs/surface-inventory.v1.yaml
// directories all live at the repo root; the Go module lives one level
// deeper at go/.
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
}

// loadCanonicalFragments loads the seven fragments from the repo-root
// skill-fragments/ directory. The test depends on these fragments because
// the roundtrip baseline lives next to them; if a fragment is renamed or
// removed, this helper breaks loudly instead of producing a silently
// vacuous assertion.
func loadCanonicalFragments(t *testing.T) []Fragment {
	t.Helper()
	dir := filepath.Join(repoRootDir(t), "skill-fragments")
	fragments, err := LoadFragments(dir)
	if err != nil {
		t.Fatalf("LoadFragments(%s): %v", dir, err)
	}
	if len(fragments) == 0 {
		t.Fatalf("skill-fragments/ is empty; the seven canonical fragments are required")
	}
	return fragments
}

// loadCanonicalCapabilities loads the catalog from
// specs/surface-inventory.v1.yaml at the repo root and returns the
// fully-enabled default Capabilities. Tests that exercise the roundtrip
// baseline or the real catalog use this helper; tests that don't depend
// on the catalog use DefaultCapabilitiesFor with a fixed test list.
func loadCanonicalCapabilities(t *testing.T) Capabilities {
	t.Helper()
	catalogPath := filepath.Join(repoRootDir(t), "specs", "surface-inventory.v1.yaml")
	overridePath := filepath.Join(t.TempDir(), "capabilities.local.yaml")
	// Pass an override path that does not exist; the catalog is the
	// source of truth and the absence of an override means "all enabled".
	caps, err := LoadCapabilities(overridePath, catalogPath)
	if err != nil {
		t.Fatalf("LoadCapabilities(catalog=%s): %v (the canonical surface inventory is the single source of truth for collectors; the test requires it to be readable)", catalogPath, err)
	}
	return caps
}

func TestRenderAll_ProducesAllThreeHosts(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, loadCanonicalCapabilities(t))
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	if got, want := len(results), 3; got != want {
		t.Fatalf("results = %d, want %d", got, want)
	}
	gotHosts := make([]string, 0, len(results))
	for _, r := range results {
		gotHosts = append(gotHosts, string(r.Host))
	}
	wantHosts := []string{"claude-code", "codex", "cursor"}
	sort.Strings(gotHosts)
	if !reflect.DeepEqual(gotHosts, wantHosts) {
		t.Fatalf("hosts = %v, want %v", gotHosts, wantHosts)
	}
}

func TestRenderAll_EmitsByteCitationBlockOnEveryHost(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, loadCanonicalCapabilities(t))
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	for _, r := range results {
		// Every fragment's citation must appear as a comment line.
		// The block is no longer at byte 0 (the frontmatter is now at
		// byte 0 for loader-safe discovery); the test is on presence,
		// not position.
		for _, f := range fragments {
			normalized, err := NormalizeByteCitation(f.ByteCitation, f.SourcePath)
			if err != nil {
				t.Fatalf("NormalizeByteCitation: %v", err)
			}
			line := byteCitationPrefix + normalized + " -->"
			if !bytes.Contains(r.Bytes, []byte(line)) {
				t.Errorf("host %s: missing byte-citation line %q", r.Host, line)
			}
		}
	}
}

// TestRenderAll_FrontmatterIsAtByte0 is the regression catch for the
// Codex/Cursor loader-discovery contract. Both loaders read the leading
// `---` block to discover skills/rules, so the YAML frontmatter MUST be
// at byte 0 in every generated file; the byte-citation block follows
// after the frontmatter.
func TestRenderAll_FrontmatterIsAtByte0(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, loadCanonicalCapabilities(t))
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	for _, r := range results {
		if !bytes.HasPrefix(r.Bytes, []byte("---\n")) {
			t.Errorf("host %s: output does not start with `---\\n` (frontmatter must be at byte 0 for Codex/Cursor loader discovery)\nfirst 200 bytes: %q", r.Host, truncate(string(r.Bytes), 200))
		}
	}
}

func TestRenderAll_ClaudeCodeFrontmatterHasNameAndDescription(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, loadCanonicalCapabilities(t))
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	for _, r := range results {
		switch r.Host {
		case HostClaudeCode, HostCodex:
			if !bytes.Contains(r.Bytes, []byte("name: eshu\n")) {
				t.Errorf("host %s: output missing `name: eshu`", r.Host)
			}
			if !bytes.Contains(r.Bytes, []byte("description: |\n")) {
				t.Errorf("host %s: output missing `description: |`", r.Host)
			}
		case HostCursor:
			if !bytes.Contains(r.Bytes, []byte("alwaysApply: true\n")) {
				t.Errorf("host %s: output missing `alwaysApply: true`", r.Host)
			}
			if !bytes.Contains(r.Bytes, []byte("description: |\n")) {
				t.Errorf("host %s: output missing `description: |`", r.Host)
			}
		}
	}
}

func TestRenderAll_OutputPathsMatchTheHostMatrix(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, loadCanonicalCapabilities(t))
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	wantPaths := map[Host]string{
		HostClaudeCode: ".claude/skills/eshu/SKILL.md",
		HostCursor:     ".cursor/rules/eshu.mdc",
		HostCodex:      ".codex/skills/eshu/SKILL.md",
	}
	for _, r := range results {
		if r.OutputPath != wantPaths[r.Host] {
			t.Errorf("host %s: OutputPath = %q, want %q", r.Host, r.OutputPath, wantPaths[r.Host])
		}
	}
}

func TestRoundtripAgainstCommittedBaseline(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	results, err := RenderAll(fragments, loadCanonicalCapabilities(t))
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	expectedRoot := filepath.Join(repoRootDir(t), "expected")
	if _, err := os.Stat(expectedRoot); err != nil {
		t.Fatalf("expected/ baseline missing at %s: %v", expectedRoot, err)
	}
	drifts, err := CheckDrift(expectedRoot, results)
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if len(drifts) > 0 {
		var b strings.Builder
		b.WriteString("committed baseline drifted from the canonical fragments:\n")
		for _, d := range drifts {
			b.WriteString("  - ")
			b.WriteString(string(d.Host))
			b.WriteString(": ")
			b.WriteString(d.Path)
			b.WriteString(" (")
			b.WriteString(d.Reason)
			b.WriteString(")\n")
		}
		b.WriteString("run `go run ./cmd/skillgen gen` to regenerate the baseline.\n")
		t.Fatal(b.String())
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

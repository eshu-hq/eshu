// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// cutoverMCPOwnershipRelPath is the committed tool-to-contract ownership map
// for eshu-hq/eshu#8873, repo-root-relative so the docs lockstep discovery
// sees this test's read. The tests below keep it honest: every live MCP tool
// must have exactly one row, no row may name a phantom source file or a
// backing route the dispatch tree never serves, and every class must be one
// of the closed EXTRACT/RETAIN/SHARED set with an eshu-mcp destination on
// every EXTRACT row.
const cutoverMCPOwnershipRelPath = "go/cmd/mcp-server/CUTOVER_OWNERSHIP.md"

func cutoverMCPRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func readCutoverMCPDoc(t *testing.T, rel string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(cutoverMCPRepoRoot(t), rel))
	if err != nil {
		t.Fatalf("reading %s: %v, want the committed #8873 ownership artifact", rel, err)
	}
	return string(content)
}

var cutoverMCPBacktickRe = regexp.MustCompile("`([^`]+)`")

var cutoverMCPToolNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]+$`)

// cutoverMCPFamilyRows parses the map's tool-family table. Family rows carry
// six cells (Family | Tools | Backing | Auth | Class | Destination); binary
// wiring rows carry four and are skipped here. Header and separator rows
// never parse to a family row.
func cutoverMCPFamilyRows(t *testing.T, content string) [][]string {
	t.Helper()
	var rows [][]string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitCutoverMCPRow(line)
		if len(cells) != 6 {
			continue
		}
		name := strings.Trim(strings.TrimSpace(cells[0]), "`*")
		if name == "" || name == "Family" || strings.Trim(cells[0], " |-") == "" {
			continue
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		t.Fatal("no six-cell family rows parsed from the #8873 ownership map")
	}
	return rows
}

func splitCutoverMCPRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// cutoverMCPRowTools returns the backtick-quoted tool names in a family row's
// Tools cell. Only that cell is scanned, so env knobs and file paths cited
// elsewhere in the document can never satisfy tool coverage.
func cutoverMCPRowTools(toolsCell string) []string {
	var tools []string
	for _, m := range cutoverMCPBacktickRe.FindAllStringSubmatch(toolsCell, -1) {
		if cutoverMCPToolNameRe.MatchString(m[1]) {
			tools = append(tools, m[1])
		}
	}
	return tools
}

func TestCutoverMCPOwnershipMapExists(t *testing.T) {
	t.Parallel()

	readCutoverMCPDoc(t, cutoverMCPOwnershipRelPath)
}

func TestCutoverMCPOwnershipMapCoversEveryTool(t *testing.T) {
	t.Parallel()

	rows := cutoverMCPFamilyRows(t, readCutoverMCPDoc(t, cutoverMCPOwnershipRelPath))
	seen := map[string]int{}
	for _, row := range rows {
		for _, tool := range cutoverMCPRowTools(row[1]) {
			seen[tool]++
		}
	}
	var missing []string
	for _, tool := range mcp.ReadOnlyTools() {
		if seen[tool.Name] == 0 {
			missing = append(missing, tool.Name)
		}
	}
	for _, name := range []string{router.FindToolName, router.CallToolName} {
		if seen[name] == 0 {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		slices.Sort(missing)
		t.Fatalf("ownership map cites %d tools but misses %d live tools: %s", len(seen), len(missing), strings.Join(missing, ", "))
	}
	var dupes []string
	for tool, n := range seen {
		if n > 1 {
			dupes = append(dupes, tool)
		}
	}
	if len(dupes) != 0 {
		slices.Sort(dupes)
		t.Fatalf("ownership map cites %d tools more than once: %s", len(dupes), strings.Join(dupes, ", "))
	}
	live := map[string]bool{router.FindToolName: true, router.CallToolName: true}
	for _, tool := range mcp.ReadOnlyTools() {
		live[tool.Name] = true
	}
	var phantom []string
	for tool := range seen {
		if !live[tool] {
			phantom = append(phantom, tool)
		}
	}
	if len(phantom) != 0 {
		slices.Sort(phantom)
		t.Fatalf("ownership map cites %d tools no live registry serves: %s", len(phantom), strings.Join(phantom, ", "))
	}
}

func TestCutoverMCPOwnershipMapSourcesExist(t *testing.T) {
	t.Parallel()

	content := readCutoverMCPDoc(t, cutoverMCPOwnershipRelPath)
	root := cutoverMCPRepoRoot(t)
	sourceRe := regexp.MustCompile(`go/(?:cmd/mcp-server|internal/mcp)/\S+\.go`)
	var missing []string
	for _, path := range sourceRe.FindAllString(content, -1) {
		path = strings.Trim(path, "`*.,;)")
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			missing = append(missing, path)
		}
	}
	if len(missing) != 0 {
		slices.Sort(missing)
		t.Fatalf("ownership map cites %d source files missing from the tree: %s", len(missing), strings.Join(missing, ", "))
	}
}

var cutoverMCPRouteRe = regexp.MustCompile(`/api/v0/[A-Za-z0-9/_{}.$-]+`)

func cutoverMCPDispatchCorpus(t *testing.T) string {
	t.Helper()
	root := filepath.Join(cutoverMCPRepoRoot(t), "go", "internal", "mcp")
	var sb strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sb.Write(raw)
		sb.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("walking the mcp dispatch tree: %v", err)
	}
	return sb.String()
}

// concatBuiltRoutes covers the cited routes the dispatch tree serves via
// string concatenation rather than a single literal. Each entry names the
// quoted fragments that must all be present; anything not listed here and
// not an exact quoted literal fails as phantom.
var concatBuiltRoutes = map[string][]string{
	"/api/v0/repositories/{repo_id}/tree":                 {`"/api/v0/repositories/"`, `"/tree"`},
	"/api/v0/services/{service_name}/intelligence-report": {`"/api/v0/services/"`, `"/intelligence-report"`},
}

func corpusHoldsAll(corpus string, fragments []string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(corpus, fragment) {
			return false
		}
	}
	return true
}

func TestCutoverMCPOwnershipBackingRoutesResolve(t *testing.T) {
	t.Parallel()

	rows := cutoverMCPFamilyRows(t, readCutoverMCPDoc(t, cutoverMCPOwnershipRelPath))
	corpus := cutoverMCPDispatchCorpus(t)
	cited := map[string]bool{}
	for _, row := range rows {
		for _, route := range cutoverMCPRouteRe.FindAllString(row[2], -1) {
			cited[strings.Trim(route, ".,;)")] = true
		}
	}
	if len(cited) == 0 {
		t.Fatal("ownership map cites no /api/v0 backing routes")
	}
	var phantom []string
	for route := range cited {
		// Quote-anchored: a cited route must appear as a served route
		// literal, so a strict prefix of a live route (e.g. a truncated
		// spelling) still fails instead of matching inside the longer
		// literal (#9437 review).
		if strings.Contains(corpus, `"`+route+`"`) {
			continue
		}
		if fragments, ok := concatBuiltRoutes[route]; ok && corpusHoldsAll(corpus, fragments) {
			continue
		}
		phantom = append(phantom, route)
	}
	if len(phantom) != 0 {
		slices.Sort(phantom)
		t.Fatalf("ownership map cites %d backing routes the dispatch tree never serves: %s", len(phantom), strings.Join(phantom, ", "))
	}
}

// cutoverMCPBinaryRows parses the map's binary/wiring table. Binary rows
// carry four cells (Component | Files | Class | Destination); the RETAIN
// core-composition rows live here, not in the tool-family table.
func cutoverMCPBinaryRows(t *testing.T, content string) [][]string {
	t.Helper()
	var rows [][]string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitCutoverMCPRow(line)
		if len(cells) != 4 {
			continue
		}
		name := strings.Trim(strings.TrimSpace(cells[0]), "`*")
		if name == "" || name == "Component" || strings.Trim(cells[0], " |-") == "" {
			continue
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		t.Fatal("no four-cell binary rows parsed from the #8873 ownership map")
	}
	return rows
}

func TestCutoverMCPOwnershipClassesClosed(t *testing.T) {
	t.Parallel()

	content := readCutoverMCPDoc(t, cutoverMCPOwnershipRelPath)
	rows := cutoverMCPFamilyRows(t, content)
	allowed := map[string]bool{"EXTRACT": true, "RETAIN": true, "SHARED": true}
	seen := map[string]int{}
	var violations []string
	checkClass := func(cells []string, class, dest, label string) {
		seen[class]++
		if !allowed[class] {
			violations = append(violations, label+": "+class)
			return
		}
		if class == "EXTRACT" && !strings.Contains(dest, "eshu-mcp") {
			violations = append(violations, "EXTRACT row without an eshu-mcp destination: "+label)
		}
		if strings.TrimSpace(dest) == "" {
			violations = append(violations, "row without a destination: "+label)
		}
	}
	for _, row := range rows {
		checkClass(row, strings.TrimSpace(row[4]), row[5], strings.TrimSpace(row[0]))
	}
	for _, row := range cutoverMCPBinaryRows(t, content) {
		checkClass(row, strings.TrimSpace(row[2]), row[3], strings.TrimSpace(row[0]))
	}
	if len(violations) != 0 {
		t.Fatalf("ownership map class violations: %s", strings.Join(violations, "; "))
	}
	for class := range allowed {
		if seen[class] == 0 {
			t.Errorf("ownership map has no %s row, want at least one of each class", class)
		}
	}
}

// TestCutoverMCPDestinationPlanSection keeps the map's pointer to the
// eshu-mcp destination content plan honest without naming that prose-only
// evidence file: like the #8872 lane, the plan itself stays out of the docs
// lockstep (no test reads it), while the map must carry the preparation-only
// destination section with the owning issue reference.
func TestCutoverMCPDestinationPlanSection(t *testing.T) {
	t.Parallel()

	content := readCutoverMCPDoc(t, cutoverMCPOwnershipRelPath)
	for _, marker := range []string{"## Destination content plan", "preparation only", "Refs #8873", "eshu-hq/eshu-mcp"} {
		if !strings.Contains(content, marker) {
			t.Errorf("ownership map destination section misses required marker %q", marker)
		}
	}
}

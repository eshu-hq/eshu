package hcl

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// terragruntIncludeMaxDepth bounds the number of parent include hops the
// walker will follow before giving up. The bound exists so a runaway include
// graph (cycles broken across many files, runaway nesting) cannot stall the
// parser. The choice of 16 mirrors typical Terragrunt monorepo depth observed
// in production layouts; deeper layouts are exceptional and explicitly
// surfaced as a warning rather than silently truncated.
const terragruntIncludeMaxDepth = 16

// terragruntIncludeMaxFileBytes caps how many bytes the walker will read from
// a single include target. Real Terragrunt files are well under this size;
// the bound exists so an attacker-controlled include path pointing at a large
// or unbounded file (/dev/zero, /proc/kcore, a multi-megabyte payload dropped
// in the repo) cannot stall the parser or consume unbounded memory before the
// HCL parser ever sees the bytes.
const terragruntIncludeMaxFileBytes = 1 << 20 // 1 MiB

// terragruntIncludeWarning is a parser-local warning record describing an
// unresolvable terragrunt include chain. The walker emits one warning per
// distinct condition (depth exceeded, cycle, etc.) so downstream packages can
// translate it into a SourceWarning without leaking parser internals.
type terragruntIncludeWarning struct {
	kind   string
	reason string
	source string
}

// resolvedTerragruntRemoteState carries the resolved remote_state row plus
// metadata describing where it came from. The walker constructs one of these
// per terminal terragrunt.hcl that supplied the effective remote_state config.
type resolvedTerragruntRemoteState struct {
	row          map[string]any
	resolvedFrom string
}

// resolveTerragruntRemoteState walks the include chain rooted at startPath and
// returns the first terragrunt.hcl that declares a usable remote_state block.
// The walk has bounded depth and cycle detection. The function reads files
// from disk on demand; it does not retain raw contents beyond the parse.
//
// Returned values:
//   - resolved: the effective remote_state row (or nil if not found)
//   - warnings: one entry per distinct termination reason (depth exceeded,
//     cycle, parse failure)
//
// Missing parent files are not warnings on their own — they simply mean the
// chain ended without finding evidence, which is an acceptable steady state
// for terragrunt.hcl files that do not configure remote state.
func resolveTerragruntRemoteState(startPath string) (*resolvedTerragruntRemoteState, []terragruntIncludeWarning) {
	visited := map[string]struct{}{}
	warnings := make([]terragruntIncludeWarning, 0)
	resolved := walkTerragruntIncludeChain(startPath, 0, visited, &warnings, true)
	return resolved, warnings
}

// walkTerragruntIncludeChain reads the file at path, looks for a remote_state
// block in its body, and otherwise follows include declarations to a parent.
// The depth counter and visited set together guard against runaway recursion
// and include cycles. The isStart flag controls the resolvedFrom label so the
// caller can distinguish self-declared remote_state from inherited evidence.
func walkTerragruntIncludeChain(
	path string,
	depth int,
	visited map[string]struct{},
	warnings *[]terragruntIncludeWarning,
	isStart bool,
) *resolvedTerragruntRemoteState {
	if depth >= terragruntIncludeMaxDepth {
		*warnings = append(*warnings, terragruntIncludeWarning{
			kind:   "terragrunt_include_depth_exceeded",
			reason: "terragrunt include chain exceeded depth bound",
			source: path,
		})
		return nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	if _, seen := visited[absolute]; seen {
		*warnings = append(*warnings, terragruntIncludeWarning{
			kind:   "terragrunt_include_cycle",
			reason: "terragrunt include chain cycle detected",
			source: absolute,
		})
		return nil
	}
	visited[absolute] = struct{}{}

	// Lstat (not Stat) so symlinks register as irregular and are rejected
	// without ever being followed. Walking through symlinks would let an
	// include path bypass the regular-file check by pointing at /dev/null,
	// /dev/zero, /proc/*, or any other special file.
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil
	}
	if !info.Mode().IsRegular() {
		*warnings = append(*warnings, terragruntIncludeWarning{
			kind:   "terragrunt_include_unsafe_file",
			reason: "terragrunt include target is not a regular file",
			source: absolute,
		})
		return nil
	}
	if info.Size() > terragruntIncludeMaxFileBytes {
		*warnings = append(*warnings, terragruntIncludeWarning{
			kind:   "terragrunt_include_unsafe_file",
			reason: "terragrunt include target exceeds size cap",
			source: absolute,
		})
		return nil
	}

	source, err := os.ReadFile(absolute)
	if err != nil {
		return nil
	}
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(source, absolute)
	if diags.HasErrors() {
		return nil
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil
	}

	for _, block := range body.Blocks {
		if block.Type != "remote_state" {
			continue
		}
		row := remoteStateRow(block, source, absolute)
		if row == nil {
			continue
		}
		if isStart {
			return &resolvedTerragruntRemoteState{row: row, resolvedFrom: "self"}
		}
		return &resolvedTerragruntRemoteState{row: row, resolvedFrom: "include_chain"}
	}

	for _, parent := range collectTerragruntIncludeTargets(body, source, absolute) {
		if resolved := walkTerragruntIncludeChain(parent, depth+1, visited, warnings, false); resolved != nil {
			return resolved
		}
	}
	return nil
}

// collectTerragruntIncludeTargets returns the absolute paths of parent
// terragrunt.hcl files referenced by include blocks in the given body. The
// walker prefers literal include paths and falls back to find_in_parent_folders
// references resolved against the directory of the current file. Targets are
// returned in source order so the walker visits the user's first declared
// include first.
func collectTerragruntIncludeTargets(body *hclsyntax.Body, source []byte, currentPath string) []string {
	currentDir := filepath.Dir(currentPath)
	targets := make([]string, 0)
	seen := map[string]struct{}{}

	addTarget := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		targets = append(targets, candidate)
	}

	for _, block := range body.Blocks {
		if block.Type != "include" {
			continue
		}
		pathAttr := block.Body.Attributes["path"]
		if pathAttr == nil {
			continue
		}
		raw := strings.TrimSpace(sourceRange(source, pathAttr.Expr.Range()))
		// Literal string include: path = "..."
		if literal := stripQuotedLiteral(raw); literal != "" {
			addTarget(absoluteIncludePath(currentDir, literal))
			continue
		}
		// find_in_parent_folders("name.hcl") style.
		if name := matchFindInParentFolders(raw); name != "" {
			if found := walkUpForFile(currentDir, name); found != "" {
				addTarget(found)
			}
		}
	}

	// read_terragrunt_config and find_in_parent_folders calls in the body
	// outside include blocks (e.g. inside locals) can reference parent
	// terragrunt.hcl files. Surface those as fallback targets so a remote_state
	// declared in a sibling shared.hcl is still reachable.
	for _, name := range collectNormalizedHelperPaths(string(source), terragruntFindInParentFoldersPattern) {
		if found := walkUpForFile(currentDir, name); found != "" {
			addTarget(found)
		}
	}
	for _, name := range collectNormalizedHelperPaths(string(source), terragruntReadConfigPattern) {
		if found := walkUpForFile(currentDir, name); found != "" {
			addTarget(found)
		}
	}
	return targets
}

// stripQuotedLiteral returns the unquoted contents of a `"..."` string literal
// or "" when the source range is not a single literal. Used to recognise
// include paths that are absolute or repository-relative literal strings.
func stripQuotedLiteral(raw string) string {
	if len(raw) < 2 {
		return ""
	}
	if raw[0] != '"' || raw[len(raw)-1] != '"' {
		return ""
	}
	inner := raw[1 : len(raw)-1]
	if strings.ContainsAny(inner, "${}") {
		return ""
	}
	return inner
}

// matchFindInParentFolders pulls the file name argument out of a
// find_in_parent_folders("name.hcl") expression, returning "" when the
// expression does not match. The pattern reuses the package-level regex so the
// walker stays in lockstep with the existing helper-path extractor.
func matchFindInParentFolders(raw string) string {
	matches := terragruntFindInParentFoldersPattern.FindStringSubmatch(raw)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

// absoluteIncludePath returns the absolute filesystem path for an include
// target. Absolute paths are returned unchanged; relative paths are joined
// against the directory containing the file that declared the include.
func absoluteIncludePath(currentDir, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(currentDir, target))
}

// walkUpForFile mirrors the behavior of Terragrunt's find_in_parent_folders:
// starting from currentDir, walk up the directory tree looking for a sibling
// file with the given name. Returns the absolute path of the first match or
// "" when no match exists before reaching the filesystem root.
func walkUpForFile(currentDir, name string) string {
	dir := currentDir
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		candidate := filepath.Join(parent, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		dir = parent
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	dsLiteral     = regexp.MustCompile(`"([^"\n]{1,300}\.(?:md|json|mdc))"`)
	dsEmbed       = regexp.MustCompile(`//go:embed\s+(\S+\.(?:md|json|mdc))`)
	dsReadCall    = regexp.MustCompile(`\b([Rr]ead\w*|Open\w*|embed|Stat|Lstat)\s*\(`)
	dsJoinCall    = regexp.MustCompile(`\bJoin\s*\(`)
	dsWriteCall   = regexp.MustCompile(`\b[Ww]rite\w*\s*\(`)
	dsIdentBefore = regexp.MustCompile(`(\w+)\s*(:=|=[^=]|:)\s*$`)
	dsPathName    = regexp.MustCompile(`(?i)\b\w*(path|file|doc|page|manifest|bundle|rel|fixture|cassette|dashboard|runbook|matrix|cookbook|audit|guide|inventory|evidence|shield|config|weight|sheet|packet|digest)\w*\s*=[^=]`)
	dsIdent       = regexp.MustCompile(`\b([A-Za-z_]\w*)\b`)
	dsIdentFull   = regexp.MustCompile(`^[A-Za-z_]\w*$`)
	dsMapKey      = regexp.MustCompile(`^\s*:`)
	dsCaseLine    = regexp.MustCompile(`^\s*case\b`)
	dsSingleArg   = regexp.MustCompile(`\(\s*"[^"\n]*"\s*\)`)
	dsJoinDef     = regexp.MustCompile(`^\s*(\w+)\s*(:=|=[^=]|:).*Join\s*\(`)
	dsAssignJoin  = regexp.MustCompile(`^\s*(\w+)\s*(:=|=[^=]).*Join\s*\(`)
	dsTempDef     = regexp.MustCompile(`^\s*(\w+)\s*(:=|=[^=]).*TempDir\s*\(`)
	dsDefineRe    = regexp.MustCompile(`^\s*([\w][\w\s,]*?)\s*:=`)
	dsRangeRe     = regexp.MustCompile(`\bfor\s+(?:(\w+)\s*,\s*)?(\w+)\s*:?=\s*range\b`)
	dsVarRe       = regexp.MustCompile(`^\s*(?:var|const)\s+(\w+)`)
	dsValueDef    = regexp.MustCompile(`(:=|=[^=])\s*$`)
	dsNoBackquote = regexp.MustCompile("`[^`]*`")
	dsPathWord    = regexp.MustCompile(`(?i)\b\w*(path|file|doc|page|manifest|bundle|rel|fixture|cassette|dashboard|runbook|matrix|cookbook|audit|guide|inventory|evidence|shield|config|weight|sheet|packet|digest)\w*\b`)
	dsAllStrings  = regexp.MustCompile(`"([^"\n]*)"`)
)

// dsCode returns the code part of a Go line: anything from the first //
// onward is a comment and must not drive read detection. Literals are still
// extracted from the full line, so a comment that names a doc stays a probe.
func dsCode(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// dsHasWord reports whether word appears as a whole word in line.
func dsHasWord(line, word string) bool {
	if !strings.Contains(line, word) {
		return false
	}
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(line)
}

// dsIsJoin reports whether a code line calls a path join. strings.Join only
// concatenates with a separator, so it must not count: it would mark every
// joined string as a file read and poison identifier flow.
func dsIsJoin(code string) bool {
	return dsJoinCall.MatchString(code) && !strings.Contains(code, "strings.Join")
}

// dsConstDef records a path-named constant whose value resolved to a repo file.
type dsConstDef struct {
	name    string
	rel     string
	file    string
	lineIdx int
}

// dsFlowCand is a literal whose test-local name flows into a read but whose
// target no rule resolved. The discovery test only reports it when the name
// is defined in the same file: a struct field that happens to share an
// identifier with a read is not a flow.
type dsFlowCand struct {
	file    string
	rel     string
	lineIdx int
	raw     string
	name    string
}

// dsJoinSite records a path-named local built with filepath.Join.
type dsJoinSite struct {
	name     string
	file     string
	rel      string
	lineIdx  int
	test     bool
	resolved bool
	dirTree  string
}

// dsMarkNamed records a non-read occurrence of a path: testdata fixtures
// count as inputs even when they sit in tables, everything else is a probe.
// Bare names fall back to the repo root, which is harmless for probes: a
// probe never forces a test run, while a filedir guess would pin the wrong
// package README for every glob vector and fixture name.
func dsMarkNamed(root, path, raw string, mark func(string, string)) bool {
	rel := dsResolve(root, path, raw, false)
	if rel == "" && !strings.Contains(raw, "/") {
		if st, err := os.Stat(filepath.Join(root, raw)); err == nil && !st.IsDir() {
			rel = dsExactRel(root, filepath.Join(root, raw))
		}
	}
	if rel == "" {
		return false
	}
	if strings.Contains(rel, "/testdata/") && strings.HasSuffix(rel, ".md") {
		mark(rel, "read")
	} else {
		mark(rel, "probe")
	}
	return true
}

// dsResolve resolves one path literal against the repo. Full (slash-bearing)
// paths try the repo root first, then the referring file's directory.
// Bare filenames resolve only through the file's own directory when
// fileDirOK holds; otherwise they are caller-relative guesses and the caller
// must route them to the R7 triage list instead.
func dsResolve(root, file, raw string, fileDirOK bool) string {
	var cands []string
	if strings.Contains(raw, "/") {
		cands = []string{filepath.Join(root, raw), filepath.Join(filepath.Dir(file), raw)}
	} else if fileDirOK {
		cands = []string{filepath.Join(filepath.Dir(file), raw)}
	}
	for _, cand := range cands {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return dsExactRel(root, cand)
		}
	}
	return ""
}

// dsChainBases lists the roots a Join trailing chain resolves against.
// Literal-first chains are CWD-relative with a repo-root fallback;
// variable-first climbing chains try the file directory first because the
// ".." segments usually climb out of it, while flat variable chains only
// try the root.
func dsChainBases(root, file string, chain []string, literalFirst bool) []string {
	if literalFirst {
		// CWD-relative first: the test process runs in the package
		// directory. Repo-root-relative chains such as Join("go", ...)
		// resolve through the root fallback.
		return []string{filepath.Dir(file), root}
	}
	for _, seg := range chain {
		if seg == ".." {
			return []string{filepath.Dir(file), root}
		}
	}
	if len(chain) >= 2 {
		return []string{root}
	}
	return nil
}

// dsChainResolve resolves a filepath.Join trailing literal chain to a file.
// Callers exclude TempDir lines entirely.
func dsChainResolve(root, file string, chain []string, literalFirst bool) string {
	for _, base := range dsChainBases(root, file, chain, literalFirst) {
		cand := filepath.Join(append([]string{base}, chain...)...)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return dsExactRel(root, cand)
		}
	}
	return ""
}

// dsChainDir resolves a Join trailing chain to a directory, for classifying
// the tree a dynamically-named read targets. A chain that lands outside the
// docs-set trees (testdata, tests, specs fixtures) needs no R7: no docs-set
// input can hide under a code-tree directory.
func dsChainDir(root, file string, chain []string, literalFirst bool) string {
	for _, base := range dsChainBases(root, file, chain, literalFirst) {
		cand := filepath.Join(append([]string{base}, chain...)...)
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			rel := dsExactRel(root, cand)
			if strings.HasPrefix(rel, "docs/") || strings.HasPrefix(rel, ".agents/") || strings.HasPrefix(rel, "skill-fragments/") || strings.HasPrefix(rel, "expected/") {
				return ""
			}
			return rel
		}
	}
	return ""
}

// dsCallText joins a call's continued lines (up to 19 more) so chains and
// first args resolve across the whole call. Long literal chains into docs
// trees need the headroom; the balance check still terminates the join.
func dsCallText(lines []string, idx int) string {
	callText := dsCode(lines[idx])
	if strings.Count(callText, "(") > strings.Count(callText, ")") {
		for k := idx + 1; k < len(lines) && k < idx+20; k++ {
			callText += " " + dsCode(lines[k])
			if strings.Count(callText, "(") <= strings.Count(callText, ")") {
				break
			}
		}
	}
	return callText
}

// dsDefResolve resolves a path-named constant's value: full paths against
// the root then the file directory, bare names against the file directory
// then by unique repo-wide basename. Constants name repo files; unlike reads
// they are not CWD-relative, so a unique basename is a sound fallback.
func dsDefResolve(root, file, raw string, baseIndex map[string][]string) string {
	if rel := dsResolve(root, file, raw, true); rel != "" {
		return rel
	}
	if !strings.Contains(raw, "/") {
		if rels := baseIndex[raw]; len(rels) == 1 {
			return rels[0]
		}
	}
	return ""
}

// dsManifest reads the canonical lockstep manifest.
func dsManifest(t *testing.T, root string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "specs", "ci-docs-lockstep-paths.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		manifest = append(manifest, line)
	}
	return manifest
}

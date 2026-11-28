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

func isASCIIWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// dsHasWord reports whether word appears as a whole word in line.
// For ASCII word tokens (\w+), this matches RE2's `\b<word>\b` identically
// without compiling a regexp per line.
func dsHasWord(line, word string) bool {
	if word == "" || !strings.Contains(line, word) {
		return false
	}
	if !isASCIIWordByte(word[0]) || !isASCIIWordByte(word[len(word)-1]) {
		return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(line)
	}
	for off := 0; off < len(line); {
		idx := strings.Index(line[off:], word)
		if idx < 0 {
			return false
		}
		start := off + idx
		end := start + len(word)
		leftOK := start == 0 || !isASCIIWordByte(line[start-1])
		rightOK := end == len(line) || !isASCIIWordByte(line[end])
		if leftOK && rightOK {
			return true
		}
		off = start + 1
	}
	return false
}

// dsRecordWordTokens records every maximal ASCII word token (\w+) appearing on
// lineIdx into idents: the first line index it appeared on, or -1 if it
// appeared on two or more lines in the file.
func dsRecordWordTokens(line string, lineIdx int, idents map[string]int) {
	for i := 0; i < len(line); {
		if !isASCIIWordByte(line[i]) {
			i++
			continue
		}
		start := i
		for i < len(line) && isASCIIWordByte(line[i]) {
			i++
		}
		tok := line[start:i]
		prev, ok := idents[tok]
		if !ok {
			idents[tok] = lineIdx
		} else if prev != lineIdx {
			idents[tok] = -1
		}
	}
}

// dsFileTempDirVars computes the temp-dir tainted local variables for a single
// test file using the same 5-pass fixed-point propagation over Join assignments.
func dsFileTempDirVars(lines, stripped []string) map[string]bool {
	type dsEdge struct{ name, first string }
	var vars map[string]bool
	var edges []dsEdge
	for idx, code := range stripped {
		if strings.Contains(code, "TempDir") {
			if m := dsTempDef.FindStringSubmatch(code); m != nil {
				if vars == nil {
					vars = map[string]bool{}
				}
				vars[m[1]] = true
			}
		}
		// Taint tracks real assignments only: a struct field that
		// shares a name is not an alias for the local.
		if strings.Contains(code, "Join") && !strings.Contains(code, "strings.Join") {
			if m := dsAssignJoin.FindStringSubmatch(code); m != nil {
				callText := dsNoBackquote.ReplaceAllString(dsCallText(lines, idx), "")
				first := ""
				if jf := strings.Index(callText, "Join("); jf >= 0 {
					first = callText[jf+len("Join("):]
					if ci := strings.Index(first, ","); ci >= 0 {
						first = strings.TrimSpace(first[:ci])
					}
					if strings.Contains(first, "\"") {
						first = ""
					}
				}
				edges = append(edges, dsEdge{name: m[1], first: first})
			}
		}
	}
	if len(vars) == 0 || len(edges) == 0 {
		return vars
	}
	for range 5 {
		changed := false
		for _, e := range edges {
			if vars[e.first] && !vars[e.name] {
				vars[e.name] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return vars
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
// package README for every glob vector and fixture name. The fallback
// consults the tracked basename universe rather than the bare disk, so an
// on-disk-only copy at the root (untracked scratch or ignored
// materialization) names nothing committable and cannot move any verdict
// (#11634).
func dsMarkNamed(root, path, raw string, tracked map[string]bool, mark func(string, string)) bool {
	rel := dsResolve(root, path, raw, false, tracked)
	if rel == "" && !strings.Contains(raw, "/") && tracked != nil && tracked[raw] {
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
// must route them to the R7 triage list instead. When tracked is non-nil (a
// git checkout), in-root candidates resolve only for index members: an
// on-disk-only copy (untracked scratch or ignored materialization) names
// nothing committable and must not pin a read or a probe (#11634). A nil
// universe keeps the legacy disk behavior for scratch roots. Outside-root
// candidates keep the legacy behavior in both modes: they can never name a
// manifest entry.
func dsResolve(root, file, raw string, fileDirOK bool, tracked map[string]bool) string {
	var cands []string
	if strings.Contains(raw, "/") {
		cands = []string{filepath.Join(root, raw), filepath.Join(filepath.Dir(file), raw)}
	} else if fileDirOK {
		cands = []string{filepath.Join(filepath.Dir(file), raw)}
	}
	for _, cand := range cands {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			// A case alias is not a match; a later candidate may still be exact.
			if rel := dsExactRel(root, cand); rel != "" {
				if tracked == nil || rel == ".." || strings.HasPrefix(rel, "../") || tracked[rel] {
					return rel
				}
			}
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
// Callers exclude TempDir lines entirely. The tracked universe gates
// in-root candidates exactly as in dsResolve (#11634).
func dsChainResolve(root, file string, chain []string, literalFirst bool, tracked map[string]bool) string {
	for _, base := range dsChainBases(root, file, chain, literalFirst) {
		cand := filepath.Join(append([]string{base}, chain...)...)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			if rel := dsExactRel(root, cand); rel != "" {
				if tracked == nil || rel == ".." || strings.HasPrefix(rel, "../") || tracked[rel] {
					return rel
				}
			}
		}
	}
	return ""
}

// dsChainDir resolves a Join trailing chain to a directory, for classifying
// the tree a dynamically-named read targets. A chain that lands outside the
// docs-set trees (testdata, tests, specs fixtures) needs no R7: no docs-set
// input can hide under a code-tree directory. In a git checkout the
// directory must hold a tracked file: an untracked directory must neither
// classify a read nor suppress its fail-closed R7 (#11634). A nil universe
// keeps the legacy disk behavior for scratch roots.
func dsChainDir(root, file string, chain []string, literalFirst bool, tracked map[string]bool) string {
	for _, base := range dsChainBases(root, file, chain, literalFirst) {
		cand := filepath.Join(append([]string{base}, chain...)...)
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			rel := dsExactRel(root, cand)
			if rel == "" {
				continue
			}
			if tracked != nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") && !dsTrackedUnder(tracked, rel) {
				continue
			}
			if strings.HasPrefix(rel, "docs/") || strings.HasPrefix(rel, ".agents/") || strings.HasPrefix(rel, "skill-fragments/") || strings.HasPrefix(rel, "expected/") {
				return ""
			}
			return rel
		}
	}
	return ""
}

// dsTrackedUnder reports whether the tracked universe holds a file at or
// under the root-relative directory rel.
func dsTrackedUnder(tracked map[string]bool, rel string) bool {
	rel = filepath.ToSlash(rel)
	prefix := strings.TrimSuffix(rel, "/") + "/"
	for k := range tracked {
		if k == rel || strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
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
func dsDefResolve(root, file, raw string, baseIndex map[string][]string, tracked map[string]bool) string {
	if rel := dsResolve(root, file, raw, true, tracked); rel != "" {
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

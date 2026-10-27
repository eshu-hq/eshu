// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
			rel = filepath.ToSlash(raw)
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
			rel, _ := filepath.Rel(root, cand)
			return filepath.ToSlash(rel)
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
			rel, _ := filepath.Rel(root, cand)
			return filepath.ToSlash(rel)
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
			rel, _ := filepath.Rel(root, cand)
			rel = filepath.ToSlash(rel)
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

// dsScan walks the go tree once and classifies every docs-set path literal
// the tests name. It returns the read set, the probe set, and fail-closed
// demands for read sites no rule could resolve.
func dsScan(t *testing.T, root, goRoot string) (map[string]bool, map[string]bool, []string) {
	t.Helper()
	read := map[string]bool{}
	probe := map[string]bool{}
	// dsAllowlistData reports whether path is the scanner's own allowlist
	// data file. That file names every entry by construction, and its
	// justification prose mentions read helpers; scanning it would report
	// the allowlist itself as findings, so both walks skip it.
	dsAllowlistData := func(path string) bool {
		return strings.HasSuffix(path, "lockstep_paths_test.go")
	}
	mark := func(rel, kind string) {
		if kind == "read" {
			read[rel] = true
			delete(probe, rel)
			return
		}
		if !read[rel] {
			probe[rel] = true
		}
	}
	var defs []dsConstDef
	var joins []dsJoinSite
	var flowCands []dsFlowCand
	chainDirUses := map[string]map[string]bool{}
	tempDirVars := map[string]map[string]bool{}
	definedIn := map[string]map[string]bool{}
	codeLines := map[string][]string{}
	codeBare := map[string][]string{}
	// Pre-walk test files for temp-dir taint: a local built from a temp-dir
	// variable is itself temp-dir, regardless of textual order. The main
	// walk below reads this map instead of collecting textually-local defs.
	type dsEdge struct{ file, name, first string }
	var edges []dsEdge
	_ = filepath.Walk(goRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, "_test.go") || dsAllowlistData(path) {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		allLines := strings.Split(string(content), "\n")
		for idx := range allLines {
			code := dsCode(allLines[idx])
			if m := dsTempDef.FindStringSubmatch(code); m != nil {
				if tempDirVars[path] == nil {
					tempDirVars[path] = map[string]bool{}
				}
				tempDirVars[path][m[1]] = true
			}
			// Taint tracks real assignments only: a struct field that
			// shares a name is not an alias for the local.
			if m := dsAssignJoin.FindStringSubmatch(code); m != nil && !strings.Contains(code, "strings.Join") {
				callText := dsNoBackquote.ReplaceAllString(dsCallText(allLines, idx), "")
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
				edges = append(edges, dsEdge{file: path, name: m[1], first: first})
			}
		}
		return nil
	})
	for i := 0; i < 5; i++ {
		changed := false
		for _, e := range edges {
			if tempDirVars[e.file] != nil && tempDirVars[e.file][e.first] && !tempDirVars[e.file][e.name] {
				tempDirVars[e.file][e.name] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	testReadNames := map[string]bool{}
	fileReadNames := map[string]map[string]bool{}
	fileHasReads := map[string]bool{}
	baseIndex := map[string][]string{}
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch filepath.Base(p) {
			case ".git", "node_modules", ".venv", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".mdc") {
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			base := filepath.Base(p)
			baseIndex[base] = append(baseIndex[base], rel)
		}
		return nil
	})
	var r7pending []string
	r7seen := map[string]bool{}
	r7add := func(fileRel string, lineIdx int, token, why string) {
		if allowedUnresolvedReads[fileRel+"|"+token] != "" {
			return
		}
		key := fileRel + "|" + token
		if r7seen[key] {
			return
		}
		r7seen[key] = true
		r7pending = append(r7pending, "R7 "+fileRel+":"+strconv.Itoa(lineIdx+1)+" ("+token+"): "+why)
	}
	err := filepath.Walk(goRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || dsAllowlistData(path) {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)
		isTest := strings.HasSuffix(path, "_test.go")
		// Test files always participate: a test can pin a doc through an
		// imported constant alone, with no literal for the trigger to see.
		// Production files need a literal to contribute anything.
		if !isTest && !strings.Contains(text, ".md") && !strings.Contains(text, ".json") && !strings.Contains(text, ".mdc") {
			return nil
		}
		lines := strings.Split(text, "\n")
		stripped := make([]string, len(lines))
		bare := make([]string, len(lines))
		for i, line := range lines {
			stripped[i] = dsCode(line)
			// Identifier searches run on the string-free form so a name
			// that only appears inside a literal never counts as a flow.
			bare[i] = dsAllStrings.ReplaceAllString(stripped[i], "")
		}
		codeLines[path] = stripped
		codeBare[path] = bare
		hasReads := false
		readNames := map[string]bool{}
		defined := map[string]bool{}
		addDef := func(name string) {
			name = strings.TrimSpace(name)
			if name != "" && dsIdentFull.MatchString(name) {
				defined[name] = true
			}
		}
		for _, line := range lines {
			code := dsCode(line)
			if m := dsDefineRe.FindStringSubmatch(code); m != nil {
				for _, part := range strings.Split(m[1], ",") {
					addDef(part)
				}
			}
			if m := dsRangeRe.FindStringSubmatch(code); m != nil {
				addDef(m[1])
				addDef(m[2])
			}
			if m := dsVarRe.FindStringSubmatch(code); m != nil {
				addDef(m[1])
			}
			if (dsReadCall.MatchString(code) || dsIsJoin(code)) && !dsWriteCall.MatchString(code) {
				hasReads = true
				for _, m := range dsIdent.FindAllStringSubmatch(code, -1) {
					readNames[m[1]] = true
				}
			}
		}
		definedIn[path] = defined
		fileHasReads[path] = hasReads
		fileReadNames[path] = readNames
		if isTest {
			for name := range readNames {
				testReadNames[name] = true
			}
		}
		fileRel, _ := filepath.Rel(root, path)
		fileRel = filepath.ToSlash(fileRel)
		for idx, line := range lines {
			code := dsCode(line)
			inTempDir := strings.Contains(code, "TempDir")
			if !inTempDir {
				for v := range tempDirVars[path] {
					if dsHasWord(code, v) {
						inTempDir = true
						break
					}
				}
			}
			readPos := (dsReadCall.MatchString(code) || dsIsJoin(code)) && !dsWriteCall.MatchString(code)
			// Join trailing-chain resolution: a resolved chain covers every
			// literal on the line, so it marks the target and suppresses
			// per-literal R7 noise for its members. Every Join call on the
			// line is tried, so multi-Join expressions resolve each part.
			chainOK := ""
			chainDir := ""
			chainCode := dsNoBackquote.ReplaceAllString(dsCallText(lines, idx), "")
			if readPos && dsIsJoin(code) && !inTempDir {
				rest := chainCode
				for {
					jf := strings.Index(rest, "Join(")
					if jf < 0 {
						break
					}
					args := rest[jf+len("Join("):]
					rest = args
					var chain []string
					for _, m := range dsAllStrings.FindAllStringSubmatch(args, -1) {
						chain = append(chain, m[1])
					}
					if len(chain) == 0 {
						continue
					}
					first := args
					if ci := strings.Index(args, ","); ci >= 0 {
						first = args[:ci]
					}
					literalFirst := strings.Contains(first, "\"")
					if rel := dsChainResolve(root, path, chain, literalFirst); rel != "" {
						chainOK = rel
						break
					}
					if dir := dsChainDir(root, path, chain, literalFirst); dir != "" {
						chainDir = dir
					}
				}
			}
			if chainOK != "" {
				mark(chainOK, "read")
			}
			if chainDir != "" {
				if chainDirUses[path] == nil {
					chainDirUses[path] = map[string]bool{}
				}
				for _, m := range dsIdent.FindAllStringSubmatch(codeBare[path][idx], -1) {
					chainDirUses[path][m[1]] = true
				}
			}
			if m := dsJoinDef.FindStringSubmatch(code); m != nil && dsPathWord.MatchString(m[1]) && !inTempDir && !strings.Contains(code, "strings.Join") && !strings.Contains(chainCode, "TempDir") {
				firstArg := ""
				if jf := strings.Index(chainCode, "Join("); jf >= 0 {
					firstArg = chainCode[jf+len("Join("):]
					if ci := strings.Index(firstArg, ","); ci >= 0 {
						firstArg = strings.TrimSpace(firstArg[:ci])
					}
					if qi := strings.Index(firstArg, "\""); qi >= 0 {
						firstArg = ""
					}
				}
				if _, ok := tempDirVars[path][firstArg]; !ok {
					joins = append(joins, dsJoinSite{name: m[1], file: path, rel: fileRel, lineIdx: idx, test: isTest, resolved: chainOK != "", dirTree: chainDir})
				}
			}
			for _, m := range dsLiteral.FindAllStringSubmatchIndex(line, -1) {
				if m[2] >= len(code) {
					// The literal lives in a comment: attribute it, never read.
					raw := line[m[2]:m[3]]
					if strings.ContainsAny(raw, " #$%!\n`") || strings.Contains(raw, "://") {
						continue
					}
					dsMarkNamed(root, path, raw, mark)
					continue
				}
				raw := line[m[2]:m[3]]
				if strings.ContainsAny(raw, " #$%!*:?\n`") || strings.Contains(raw, "://") {
					continue
				}
				after := line[m[1]:]
				bare := !strings.Contains(raw, "/")
				// Path-named constant definitions feed R3.
				if dsPathName.MatchString(code) {
					if before := dsIdentBefore.FindStringSubmatch(code[:m[0]]); before != nil {
						if rel := dsDefResolve(root, path, raw, baseIndex); rel != "" {
							defs = append(defs, dsConstDef{name: before[1], rel: rel, file: path, lineIdx: idx})
						}
					}
				}
				if !isTest {
					continue
				}
				// R4: map tables of path keys in a file that reads files.
				// Selection tables name repo files, so a bare key that
				// exists at the root resolves there: a package file that
				// merely shares the name must not steal the classification.
				// Slice vectors and switch cases never count.
				if !dsCaseLine.MatchString(code) && dsMapKey.MatchString(after) && hasReads {
					if rel := dsResolve(root, path, raw, true); rel != "" {
						mark(rel, "read")
					}
					continue
				}
				if chainOK != "" {
					continue
				}
				if readPos && !inTempDir {
					// A bare name that cannot name a repo file needs no
					// classification: no root counterpart and no
					// same-basename repo file means negative-test and
					// temp-dir fixtures land here, never repo reads. A
					// chain that already landed in a code tree covers its
					// members the same way.
					unresolvableBare := bare && !dsSingleArg.MatchString(code) && chainDir == ""
					if unresolvableBare {
						if _, err := os.Stat(filepath.Join(root, raw)); err == nil || len(baseIndex[raw]) > 0 {
							r7add(fileRel, idx, raw, "bare path on a multi-arg read; resolve the caller-relative target")
						}
						continue
					}
					if rel := dsResolve(root, path, raw, true); rel != "" {
						mark(rel, "read")
						continue
					}
					if chainDir == "" {
						r7add(fileRel, idx, raw, "unresolved read target; classify the caller-relative path")
					}
					continue
				}
				if hasReads {
					if before := dsIdentBefore.FindStringSubmatch(code[:m[0]]); before != nil && readNames[before[1]] {
						if rel := dsResolve(root, path, raw, true); rel != "" {
							mark(rel, "read")
							continue
						}
						// Only value definitions start an unresolved flow:
						// a struct field or map key that shares an
						// identifier with a read is a label collision.
						if !inTempDir && dsValueDef.MatchString(code[:m[0]]) {
							flowCands = append(flowCands, dsFlowCand{file: path, rel: fileRel, lineIdx: idx, raw: raw, name: before[1]})
						}
						continue
					}
				}
				dsMarkNamed(root, path, raw, mark)
			}
			for _, m := range dsEmbed.FindAllStringSubmatch(line, -1) {
				if rel := dsResolve(root, path, m[1], true); rel != "" {
					mark(rel, "read")
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// R3: a path-named constant referenced anywhere in Go code pins its
	// file. The reference may live in a test, in production, or in another
	// constant alias; any of them couples the file's content to the build.
	// The search runs on string-free lines so a name inside a literal cannot
	// pose as a reference.
	for _, def := range defs {
		ref := regexp.MustCompile(`\b` + regexp.QuoteMeta(def.name) + `\b`)
	found:
		for file, stripped := range codeBare {
			for i, cl := range stripped {
				if file == def.file && i == def.lineIdx {
					continue
				}
				if strings.Contains(cl, def.name) && ref.MatchString(cl) {
					mark(def.rel, "read")
					break found
				}
			}
		}
	}
	// Flow candidates only count when the name is defined in the same file:
	// a struct field that shares an identifier with a read is not a flow.
	for _, d := range defs {
		if definedIn[d.file] == nil {
			definedIn[d.file] = map[string]bool{}
		}
		definedIn[d.file][d.name] = true
	}
	for _, j := range joins {
		if definedIn[j.file] == nil {
			definedIn[j.file] = map[string]bool{}
		}
		definedIn[j.file][j.name] = true
	}
	for _, c := range flowCands {
		// A name already used under a code-tree chain is classified
		// there; its other uses raise their own demands when needed.
		if definedIn[c.file][c.name] && !chainDirUses[c.file][c.name] {
			r7add(c.rel, c.lineIdx, c.raw, "read-flow target unresolvable; classify the caller-relative path")
		}
	}
	// A join site whose line names a constant the scanner already resolved
	// to a read needs no R7: the constant carries the target.
	docConstNames := map[string]bool{}
	for _, def := range defs {
		if read[def.rel] {
			docConstNames[def.name] = true
		}
	}
	// R7a: a path-named Join local that flows into a read on another line is
	// a test input whose target no static rule resolved. Fail closed.
	joinDefAt := map[string]map[int]string{}
	for _, o := range joins {
		if joinDefAt[o.file] == nil {
			joinDefAt[o.file] = map[int]string{}
		}
		joinDefAt[o.file][o.lineIdx] = o.name
	}
	for _, j := range joins {
		if !j.test || j.resolved || j.dirTree != "" {
			continue
		}
		if allowedUnresolvedReads[j.rel+"|"+j.name] != "" {
			continue
		}
		if lines, ok := codeBare[j.file]; ok && j.lineIdx < len(lines) {
			pinned := false
			for _, m := range dsIdent.FindAllStringSubmatch(lines[j.lineIdx], -1) {
				if m[1] != j.name && docConstNames[m[1]] {
					pinned = true
					break
				}
			}
			if pinned {
				continue
			}
		}
		ref := regexp.MustCompile(`\b` + regexp.QuoteMeta(j.name) + `\b`)
		for i, cl := range codeBare[j.file] {
			if i == j.lineIdx {
				continue
			}
			// A second definition of the same name is shadowing or
			// reassignment, not a read of the first value.
			if joinDefAt[j.file][i] == j.name {
				continue
			}
			if (dsReadCall.MatchString(cl) || dsIsJoin(cl)) && !dsWriteCall.MatchString(cl) && ref.MatchString(cl) {
				r7add(j.rel, j.lineIdx, j.name, "join-built path flows into a read at line "+strconv.Itoa(i+1)+"; classify the caller-relative target")
				break
			}
		}
	}
	return read, probe, r7pending
}

// TestDocsTestInputsStayCode is the durable guard behind the P2 on #9215: a
// Go test that reads a docs-set file makes that file code, because editing it
// alone must still run the asserting test. The scanner classifies every
// docs-set path the tests name as read (R1 direct reads, R2 identifier flow,
// R3 referenced path-named constants, R4 map tables in reading files,
// Join-chain resolution) or probe (named but never read). Read files must sit
// in the lockstep manifest; probes must sit in probePaths. Anything the rules
// cannot resolve fails closed as an R7 demand for human classification.
func TestDocsTestInputsStayCode(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	read, probe, unresolved := dsScan(t, root, filepath.Join(root, "go"))
	var demands []string
	for rel := range read {
		if !IsDocsSetPath(rel) {
			continue
		}
		demands = append(demands, rel+": test input stays code, add to the lockstep manifest")
	}
	probes := map[string]struct{}{}
	for _, p := range probePaths {
		probes[p] = struct{}{}
	}
	for rel := range probe {
		if !IsDocsSetPath(rel) {
			continue
		}
		if _, ok := probes[rel]; !ok {
			demands = append(demands, rel+": named-only probe, add to probePaths with verification")
		}
	}
	demands = append(demands, unresolved...)
	slices.Sort(demands)
	for _, d := range demands {
		t.Error(d)
	}
}

// TestLockstepEntriesStayRead closes the ratchet in the other direction: a
// manifest entry with no detected test reader rots into unjustified CI cost
// on docs-only PRs, and a probePaths entry no test names is a stale claim.
// manuallyVerifiedLockstep covers entries whose read shape (multi-hop field
// flow, variable repo roots) no static rule can see.
func TestLockstepEntriesStayRead(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	manifest := dsManifest(t, root)
	read, probe, _ := dsScan(t, root, filepath.Join(root, "go"))
	observed := map[string]bool{}
	for rel := range read {
		observed[rel] = true
	}
	for rel := range probe {
		observed[rel] = true
	}
	for _, p := range manifest {
		if !observed[p] && manuallyVerifiedLockstep[p] == "" {
			t.Errorf("lockstep entry %s has no detected test reader; remove it or justify it in manuallyVerifiedLockstep", p)
		}
	}
	for p := range manuallyVerifiedLockstep {
		if !slices.Contains(manifest, p) {
			t.Errorf("manuallyVerifiedLockstep entry %s is not in the manifest", p)
		}
	}
	for _, p := range probePaths {
		if !observed[p] {
			t.Errorf("probePaths entry %s is never named by any Go test; drop the stale entry", p)
		}
	}
}

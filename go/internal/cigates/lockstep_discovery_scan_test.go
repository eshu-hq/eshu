// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

func dsScanOneFile(root, path string, baseIndex map[string][]string) dsFileScanResult {
	res := dsFileScanResult{path: path}
	content, err := os.ReadFile(path)
	if err != nil {
		res.err = err
		return res
	}
	isTest := strings.HasSuffix(path, "_test.go")
	// Test files always participate: a test can pin a doc through an
	// imported constant alone, with no literal for the trigger to see.
	// Production files need a literal to contribute anything.
	if !isTest && !bytes.Contains(content, []byte(".md")) && !bytes.Contains(content, []byte(".json")) && !bytes.Contains(content, []byte(".mdc")) {
		res.skipped = true
		return res
	}
	lines := strings.Split(string(content), "\n")
	stripped := make([]string, len(lines))
	bare := make([]string, len(lines))
	readLine := make([]bool, len(lines))
	wordLines := make(map[string]int, len(lines))
	for i, line := range lines {
		code := dsCode(line)
		stripped[i] = code
		// Identifier searches run on the string-free form so a name
		// that only appears inside a literal never counts as a flow.
		if strings.ContainsRune(code, '"') {
			bare[i] = dsAllStrings.ReplaceAllString(code, "")
		} else {
			bare[i] = code
		}
		dsRecordWordTokens(bare[i], i, wordLines)
	}
	res.bare = bare
	res.wordLines = wordLines

	var tempVars map[string]bool
	if isTest {
		tempVars = dsFileTempDirVars(lines, stripped)
	}

	hasReads := false
	readNames := map[string]bool{}
	defined := map[string]bool{}
	addDef := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" && dsIdentFull.MatchString(name) {
			defined[name] = true
		}
	}
	for i, code := range stripped {
		if strings.Contains(code, ":=") {
			if m := dsDefineRe.FindStringSubmatch(code); m != nil {
				for _, part := range strings.Split(m[1], ",") {
					addDef(part)
				}
			}
		}
		if strings.Contains(code, "range") {
			if m := dsRangeRe.FindStringSubmatch(code); m != nil {
				addDef(m[1])
				addDef(m[2])
			}
		}
		if strings.Contains(code, "var") || strings.Contains(code, "const") {
			if m := dsVarRe.FindStringSubmatch(code); m != nil {
				addDef(m[1])
			}
		}
		if strings.ContainsRune(code, '(') && (dsReadCall.MatchString(code) || dsIsJoin(code)) && !dsWriteCall.MatchString(code) {
			readLine[i] = true
			hasReads = true
			for _, m := range dsIdent.FindAllStringSubmatch(code, -1) {
				readNames[m[1]] = true
			}
		}
	}
	res.defined = defined
	mark := func(rel, kind string) {
		if kind == "read" {
			res.read = append(res.read, rel)
		} else {
			res.probe = append(res.probe, rel)
		}
	}
	fileRel, _ := filepath.Rel(root, path)
	fileRel = filepath.ToSlash(fileRel)
	for idx, line := range lines {
		code := stripped[idx]
		readPos := readLine[idx]
		hasJoin := strings.Contains(code, "Join")
		hasLitHint := strings.ContainsRune(line, '"') && (strings.Contains(line, ".md") || strings.Contains(line, ".json") || strings.Contains(line, ".mdc"))
		hasEmbed := strings.Contains(line, "go:embed")
		if !hasJoin && !hasLitHint && !hasEmbed {
			continue
		}
		inTempDir := strings.Contains(code, "TempDir")
		if !inTempDir {
			for v := range tempVars {
				if dsHasWord(code, v) {
					inTempDir = true
					break
				}
			}
		}
		// Join trailing-chain resolution: a resolved chain covers every
		// literal on the line, so it marks the target and suppresses
		// per-literal R7 noise for its members. Every Join call on the
		// line is tried, so multi-Join expressions resolve each part.
		chainOK := ""
		chainDir := ""
		if hasJoin {
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
				if res.chainDirUses == nil {
					res.chainDirUses = map[string]bool{}
				}
				for _, m := range dsIdent.FindAllStringSubmatch(bare[idx], -1) {
					res.chainDirUses[m[1]] = true
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
				if _, ok := tempVars[firstArg]; !ok {
					res.joins = append(res.joins, dsJoinSite{name: m[1], file: path, rel: fileRel, lineIdx: idx, test: isTest, resolved: chainOK != "", dirTree: chainDir})
				}
			}
		}
		if hasLitHint {
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
				bareName := !strings.Contains(raw, "/")
				// Path-named constant definitions feed R3.
				if dsPathName.MatchString(code) {
					if before := dsIdentBefore.FindStringSubmatch(code[:m[0]]); before != nil {
						if rel := dsDefResolve(root, path, raw, baseIndex); rel != "" {
							res.defs = append(res.defs, dsConstDef{name: before[1], rel: rel, file: path, lineIdx: idx})
						}
					}
				}
				if !isTest {
					continue
				}
				// R4: map tables of path keys in a file that reads files.
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
					unresolvableBare := bareName && !dsSingleArg.MatchString(code) && chainDir == ""
					if unresolvableBare {
						if dsExactRel(root, filepath.Join(root, raw)) != "" || len(baseIndex[raw]) > 0 {
							res.r7 = append(res.r7, dsR7Item{fileRel: fileRel, lineIdx: idx, token: raw, why: "bare path on a multi-arg read; resolve the caller-relative target"})
						}
						continue
					}
					if rel := dsResolve(root, path, raw, true); rel != "" {
						mark(rel, "read")
						continue
					}
					if chainDir == "" {
						res.r7 = append(res.r7, dsR7Item{fileRel: fileRel, lineIdx: idx, token: raw, why: "unresolved read target; classify the caller-relative path"})
					}
					continue
				}
				if hasReads {
					if before := dsIdentBefore.FindStringSubmatch(code[:m[0]]); before != nil && readNames[before[1]] {
						if rel := dsResolve(root, path, raw, true); rel != "" {
							mark(rel, "read")
							continue
						}
						if !inTempDir && dsValueDef.MatchString(code[:m[0]]) {
							res.flowCands = append(res.flowCands, dsFlowCand{file: path, rel: fileRel, lineIdx: idx, raw: raw, name: before[1]})
						}
						continue
					}
				}
				dsMarkNamed(root, path, raw, mark)
			}
		}
		if hasEmbed {
			for _, m := range dsEmbed.FindAllStringSubmatch(line, -1) {
				if rel := dsResolve(root, path, m[1], true); rel != "" {
					mark(rel, "read")
				}
			}
		}
	}
	return res
}

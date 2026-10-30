// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perl

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// cpanfileName is the only admitted Perl dependency-manifest name; the
// snapshot sibling stays unclaimed.
const cpanfileName = "cpanfile"

// sourceKindCpanfilePerl names the single declaring section of a cpanfile:
// the interpreter pin from a literal requires 'perl' statement.
const sourceKindCpanfilePerl = "cpanfile_perl"

// ExtractCpanfileRuntimeEvidence reads one cpanfile body for a literal
// requires 'perl' interpreter pin, never executing or evaluating it. Only a
// quoted 'perl' module with a quoted version yields a value; any dynamic
// module or version is counted as rejected so the emitter reports the drop
// instead of emitting a guess. Pod and everything after __END__ or __DATA__
// is documentation or data, never a declaration.
func ExtractCpanfileRuntimeEvidence(name string, source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	if !strings.EqualFold(filepath.Base(name), cpanfileName) {
		evidence.State = "unavailable"
		return evidence
	}
	inPod := false
	for number, rawLine := range strings.Split(string(source), "\n") {
		line := strings.TrimSpace(stripCpanfileComment(rawLine))
		if strings.HasPrefix(line, "=") {
			inPod = line != "=cut"
			continue
		}
		if line == "__END__" || line == "__DATA__" {
			break
		}
		if inPod || line == "" {
			continue
		}
		parseCpanfileLine(&evidence, line, number+1)
	}
	return evidence
}

// parseCpanfileLine keeps one literal requires 'perl' pin and rejects any
// requires line it cannot read literally; other keywords and modules stay silent.
func parseCpanfileLine(evidence *shared.RuntimeEvidence, line string, number int) {
	rest, ok := strings.CutPrefix(line, "requires")
	if !ok || (rest != "" && !strings.ContainsRune(" \t('\"", rune(rest[0]))) {
		return
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		evidence.Add(sourceKindCpanfilePerl, "perl", "", number)
		return
	}
	rest = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), ";"))
	if strings.HasPrefix(rest, "(") {
		if !strings.HasSuffix(rest, ")") {
			evidence.Add(sourceKindCpanfilePerl, "perl", "", number)
			return
		}
		rest = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(rest, "("), ")"))
	}
	module, afterModule, ok := readCpanfileQuoted(rest)
	if !ok {
		evidence.Add(sourceKindCpanfilePerl, "perl", "", number)
		return
	}
	if module != "perl" {
		return
	}
	afterModule = strings.TrimSpace(afterModule)
	if versioned, found := strings.CutPrefix(afterModule, "=>"); found {
		afterModule = versioned
	} else if strings.HasPrefix(afterModule, ",") {
		afterModule = afterModule[1:]
	} else {
		evidence.Add(sourceKindCpanfilePerl, "perl", "", number)
		return
	}
	version, tail, ok := readCpanfileQuoted(strings.TrimSpace(afterModule))
	if !ok || strings.TrimSpace(tail) != "" {
		evidence.Add(sourceKindCpanfilePerl, "perl", "", number)
		return
	}
	evidence.Add(sourceKindCpanfilePerl, "perl", version, number)
}

// readCpanfileQuoted reads one single- or double-quoted literal, reporting
// false for anything dynamic: an unquoted token, an unterminated quote, a
// backslash, or a double-quoted interpolation sigil ($ or @).
func readCpanfileQuoted(raw string) (string, string, bool) {
	if len(raw) < 2 || (raw[0] != '\'' && raw[0] != '"') {
		return "", "", false
	}
	quote := raw[0]
	end := strings.IndexByte(raw[1:], quote)
	if end < 0 {
		return "", "", false
	}
	value := raw[1 : 1+end]
	if strings.ContainsAny(value, "\\") || (quote == '"' && strings.ContainsAny(value, "$@")) {
		return "", "", false
	}
	return value, raw[1+end+1:], true
}

// stripCpanfileComment removes a "#" comment starting outside quotes: a
// hash inside a quoted version is version text, not a comment.
func stripCpanfileComment(line string) string {
	var quote rune
	escaped := false
	for index, char := range line {
		switch {
		case escaped:
			escaped = false
		case char == '\\' && quote != 0:
			escaped = true
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '\'' || char == '"':
			quote = char
		case char == '#':
			return line[:index]
		}
	}
	return line
}

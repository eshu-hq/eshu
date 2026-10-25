// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"regexp"
	"strings"
)

var (
	// goDirectivePattern is the version grammar of the go directive: a release
	// "1.22" or "1.22.1", or a pre-release such as "1.21rc1" (Go 1.21+). Any
	// other text is kept as written and reported as an alias, never parsed.
	goDirectivePattern = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?((rc|beta)[0-9]+)?$`)
	// goToolchainPattern is the same grammar behind the "go" name prefix of a
	// toolchain line. "default", "local" and "go1.22.3+auto" are not versions:
	// they name a selection rule, so they stay aliases as written.
	goToolchainPattern = regexp.MustCompile(`^go[0-9]+\.[0-9]+(\.[0-9]+)?((rc|beta)[0-9]+)?$`)
)

// goModDirective collects the occurrences of one directive.
type goModDirective struct {
	section section
	pattern *regexp.Regexp
	// first is the first accepted occurrence; count is how many were accepted.
	// Only the first is kept: a second makes the file ambiguous, and a hostile
	// file may repeat the line tens of thousands of times.
	first declaration
	count int
	// seen counts every occurrence of the keyword, accepted or not: a repeat
	// that is itself malformed still makes the directive ambiguous.
	seen int
}

// extractGoMod reads the go and toolchain directives of one go.mod body. Only
// those two directive keywords at the top level are read: require, replace,
// exclude, retract, godebug and tool lines, and every line inside a "( ... )"
// block, never emit. A directive that appears twice is ambiguous, because the
// go tool refuses such a file, so neither occurrence is emitted. rejected
// counts every value dropped for being malformed, credential-shaped or
// ambiguous.
func extractGoMod(body string) (declarations []declaration, rejected int) {
	body = strings.TrimPrefix(body, "\xef\xbb\xbf")
	directives := map[string]*goModDirective{
		"go":        {section: sectionGoModGo, pattern: goDirectivePattern},
		"toolchain": {section: sectionGoModToolchain, pattern: goToolchainPattern},
	}
	inBlock := false
	eachLine(body, func(number int, line string) bool {
		line = strings.Trim(stripGoComment(line), " \t\r")
		if inBlock {
			inBlock = line != ")"
			return true
		}
		if strings.HasSuffix(line, "(") {
			inBlock = true
			return true
		}
		keyword, directive := goModKeyword(line, directives)
		if directive == nil {
			return true
		}
		directive.seen++
		value, ok := goModValue(line[len(keyword):])
		if !ok || !safeSpec(value) {
			rejected++
			return true
		}
		directive.count++
		if directive.count == 1 {
			directive.first = declaration{
				line: number, section: directive.section, runtime: "go", rawSpec: value,
				whole: !directive.pattern.MatchString(value),
			}
		}
		return true
	})
	for _, name := range [...]string{"go", "toolchain"} {
		directive := directives[name]
		switch {
		case directive.seen > 1:
			// Malformed occurrences were counted as they were read; the
			// accepted ones are dropped with them.
			rejected += directive.count
		case directive.count == 1:
			declarations = append(declarations, directive.first)
		}
	}
	sortByLine(declarations)
	return declarations, rejected
}

// goModKeyword returns the directive keyword a trimmed line starts with and
// its collector, or nil when the line is not a go or toolchain directive. The
// keyword must be followed by a space, a tab or the end of the line, so "gofmt"
// and "toolchains" never match.
func goModKeyword(line string, directives map[string]*goModDirective) (string, *goModDirective) {
	for _, keyword := range [...]string{"toolchain", "go"} {
		if !strings.HasPrefix(line, keyword) {
			continue
		}
		rest := line[len(keyword):]
		if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
			return keyword, directives[keyword]
		}
	}
	return "", nil
}

// goModValue returns the single argument after a go or toolchain keyword, with
// one layer of quotes removed. ok is false for no argument, more than one, or a
// quoted argument holding an escape.
func goModValue(rest string) (string, bool) {
	value := strings.TrimLeft(rest, " \t")
	if value == "" || strings.ContainsAny(value, " \t") {
		return "", false
	}
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '`' && value[len(value)-1] == '`') {
		value = value[1 : len(value)-1]
		if strings.ContainsAny(value, "\\\"`") {
			return "", false
		}
	}
	return value, true
}

// stripGoComment removes a "//" comment that is not inside a quoted string.
func stripGoComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '`':
			quote = c
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return line[:i]
		}
	}
	return line
}

// sortByLine orders declarations by source line (at most two, so an insertion
// pass is enough) to keep the emission order the file's own order.
func sortByLine(d []declaration) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j].line < d[j-1].line; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

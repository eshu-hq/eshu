// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"regexp"
	"sort"
	"strings"
)

const (
	referenceKindEnv  = "env"
	referenceKindFlag = "flag"
)

var (
	concreteEnvPattern = regexp.MustCompile(`\bESHU_[A-Z0-9_]*[A-Z0-9]\b`)
	longFlagPattern    = regexp.MustCompile(`^--[A-Za-z0-9][A-Za-z0-9-]*(?:=.*)?$`)
	fencePattern       = regexp.MustCompile(`^([ ]*)(` + "```+" + `|~~~+)(bash|sh|shell|console)[[:space:]]*$`)
	listItemPattern    = regexp.MustCompile(`^([ ]*)((?:[-+*]|[0-9]+[.)])[[:space:]]+)`)
)

type reference struct {
	Kind     string
	Document string
	Command  string
	Value    string
}

// scanCounts records how much of a page the flag scanner actually inspected.
// AttributedSegments is the denominator -- Eshu command segments whose flags
// were resolved against a command -- and SkippedLines is the gate's own blind
// spot: logical lines that invoke an `eshu` command and fell outside the
// supported command-segment grammar. Reported together, they separate a genuinely clean
// run from a scanner that silently stopped reading shell fences; either number
// alone cannot.
type scanCounts struct {
	AttributedSegments int
	SkippedLines       int
}

// scanMarkdown returns the concrete references cited by one public Markdown
// page together with the coverage counts above.
func scanMarkdown(document string, content string) ([]reference, scanCounts) {
	seen := map[string]reference{}
	counts := scanCounts{}
	for _, name := range concreteEnvPattern.FindAllString(content, -1) {
		ref := reference{Kind: referenceKindEnv, Document: document, Value: name}
		seen[referenceKey(ref)] = ref
	}

	inFence := false
	fenceMarker := ""
	fenceBaseIndent := 0
	listContentIndent := 0
	pending := ""
	flush := func() {
		segments := commandSegments(pending)
		if segments == nil && mentionsEshuCommand(pending) {
			counts.SkippedLines++
		}
		for _, segment := range segments {
			if _, ok := eshuCommandFields(segment); ok {
				counts.AttributedSegments++
			}
			command, flags := flagsFromEshuCommand(segment)
			for _, flag := range flags {
				ref := reference{Kind: referenceKindFlag, Document: document, Command: command, Value: flag}
				seen[referenceKey(ref)] = ref
			}
		}
		pending = ""
	}
	for _, line := range strings.Split(content, "\n") {
		if !inFence {
			if match := listItemPattern.FindStringSubmatch(line); len(match) == 3 {
				markerIndent := len(match[1])
				if markerIndent <= 3 || (listContentIndent > 0 && markerIndent >= listContentIndent) {
					listContentIndent = markerIndent + len(match[2])
				} else if listContentIndent > 0 && markerIndent < listContentIndent {
					listContentIndent = 0
				}
			} else if strings.TrimSpace(line) != "" && listContentIndent > 0 && leadingSpaces(line) < listContentIndent {
				listContentIndent = 0
			}
			match := fencePattern.FindStringSubmatch(line)
			if len(match) == 4 {
				indent := len(match[1])
				if indent > 3 && (listContentIndent == 0 || indent < listContentIndent) {
					continue
				}
				inFence = true
				fenceMarker = match[2]
				fenceBaseIndent = 0
				if indent > 3 {
					fenceBaseIndent = listContentIndent
				}
			}
			continue
		}
		if isFenceClose(line, fenceMarker, fenceBaseIndent+3) {
			flush()
			inFence = false
			fenceMarker = ""
			fenceBaseIndent = 0
			continue
		}
		continued := strings.HasSuffix(strings.TrimRight(line, " \t"), `\`)
		part := strings.TrimSpace(line)
		if continued {
			part = strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(line, " \t"), `\`))
		}
		if pending == "" {
			pending = part
		} else {
			pending += " " + part
		}
		if !continued {
			flush()
		}
	}
	if inFence {
		flush()
	}

	out := make([]reference, 0, len(seen))
	for _, ref := range seen {
		out = append(out, ref)
	}
	sortReferences(out)
	return out, counts
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func isFenceClose(line string, marker string, maxIndent int) bool {
	if marker == "" || maxIndent < 0 {
		return false
	}
	indent := leadingSpaces(line)
	if indent > maxIndent {
		return false
	}
	remainder := line[indent:]
	markerEnd := 0
	for markerEnd < len(remainder) && remainder[markerEnd] == marker[0] {
		markerEnd++
	}
	if markerEnd < len(marker) {
		return false
	}
	for _, char := range remainder[markerEnd:] {
		if char != ' ' && char != '\t' {
			return false
		}
	}
	return true
}

func flagsFromEshuCommand(line string) (string, []string) {
	args, ok := eshuCommandFields(line)
	if !ok {
		return "", nil
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "-") {
		flags := []string{}
		for _, field := range args {
			if !longFlagPattern.MatchString(field) {
				break
			}
			flags = append(flags, strings.SplitN(field, "=", 2)[0])
		}
		sort.Strings(flags)
		return "", flags
	}
	seen := map[string]struct{}{}
	commandFields := []string{}
	for _, field := range args {
		if !strings.HasPrefix(field, "-") && len(seen) == 0 {
			commandFields = append(commandFields, field)
		}
		if !longFlagPattern.MatchString(field) {
			continue
		}
		flag := strings.SplitN(field, "=", 2)[0]
		seen[flag] = struct{}{}
	}
	flags := make([]string, 0, len(seen))
	for flag := range seen {
		flags = append(flags, flag)
	}
	sort.Strings(flags)
	return strings.Join(commandFields, "/"), flags
}

func splitShellFields(line string) []string {
	fields := []string{}
	var token strings.Builder
	var quote byte
	escaped := false
	started := false
	flush := func() {
		if !started {
			return
		}
		fields = append(fields, token.String())
		token.Reset()
		started = false
	}
	for index := 0; index < len(line); index++ {
		char := line[index]
		if escaped {
			token.WriteByte(char)
			started = true
			escaped = false
			continue
		}
		if quote == '\'' {
			if char == quote {
				quote = 0
			} else {
				token.WriteByte(char)
			}
			started = true
			continue
		}
		if char == '\\' {
			escaped = true
			started = true
			continue
		}
		if quote == '"' {
			if char == quote {
				quote = 0
			} else {
				token.WriteByte(char)
			}
			started = true
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			started = true
			continue
		}
		if char == '#' && !started {
			break
		}
		if char == ' ' || char == '\t' {
			flush()
			continue
		}
		token.WriteByte(char)
		started = true
	}
	if escaped {
		token.WriteByte('\\')
	}
	flush()
	return fields
}

func containsUnquotedShellListOperator(line string) bool {
	var quote byte
	escaped := false
	started := false
	for index := 0; index < len(line); index++ {
		char := line[index]
		if escaped {
			escaped = false
			started = true
			continue
		}
		if quote == '\'' {
			if char == quote {
				quote = 0
			}
			started = true
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if quote == '"' {
			if char == quote {
				quote = 0
			}
			started = true
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			started = true
			continue
		}
		if char == '#' && !started {
			break
		}
		if char == ' ' || char == '\t' {
			started = false
			continue
		}
		if char == '|' || char == '&' || char == ';' {
			return true
		}
		started = true
	}
	return false
}

func sortReferences(refs []reference) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		if refs[i].Document != refs[j].Document {
			return refs[i].Document < refs[j].Document
		}
		if refs[i].Command != refs[j].Command {
			return refs[i].Command < refs[j].Command
		}
		return refs[i].Value < refs[j].Value
	})
}

func referenceKey(ref reference) string {
	return ref.Kind + "\x00" + ref.Document + "\x00" + ref.Command + "\x00" + ref.Value
}

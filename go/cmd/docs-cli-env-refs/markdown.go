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

func scanMarkdown(document string, content string) []reference {
	seen := map[string]reference{}
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
		command, flags := flagsFromEshuCommand(pending)
		for _, flag := range flags {
			ref := reference{Kind: referenceKindFlag, Document: document, Command: command, Value: flag}
			seen[referenceKey(ref)] = ref
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
	return out
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
	if containsUnquotedShellListOperator(line) {
		return "", nil
	}
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return "", nil
	}
	if fields[0] == "$" || fields[0] == ">" {
		fields = fields[1:]
	}
	if len(fields) == 0 || fields[0] != "eshu" {
		return "", nil
	}
	if len(fields) > 1 && strings.HasPrefix(fields[1], "-") {
		flags := []string{}
		for _, field := range fields[1:] {
			field = normalizeShellToken(field)
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
	for _, field := range fields[1:] {
		field = normalizeShellToken(field)
		if strings.HasPrefix(field, "#") {
			break
		}
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

func containsUnquotedShellListOperator(line string) bool {
	var quote byte
	escaped := false
	for index := 0; index < len(line); index++ {
		char := line[index]
		if escaped {
			escaped = false
			continue
		}
		if quote == '\'' {
			if char == quote {
				quote = 0
			}
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
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char == '|' || char == '&' || char == ';' {
			return true
		}
	}
	return false
}

func normalizeShellToken(token string) string {
	if len(token) < 2 {
		return token
	}
	first, last := token[0], token[len(token)-1]
	if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
		return token[1 : len(token)-1]
	}
	return token
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

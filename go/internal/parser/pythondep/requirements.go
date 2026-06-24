// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"bufio"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// LangRequirements is the canonical `lang` payload field for pip
// requirements files. It is distinct from `python` so reducers and the
// folder doc tooling can tell the difference between a pip manifest fact
// and a Python source-code fact.
const LangRequirements = "python_requirements"

// ParseRequirements parses one pip requirements file and returns the parser
// engine payload. The dev-vs-runtime scope is inferred from the filename so
// `requirements-dev.txt` and `requirements_test.txt` mark their rows as dev
// dependencies. VCS, path, URL, editable, and malformed entries surface as
// non-`dependency` config_kind rows so the supply-chain reducer cannot
// mis-admit them as registry consumption.
func ParseRequirements(path string) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	payload := basePayload(path, LangRequirements)

	devScope := requirementsDevScopeFromFilename(path)
	section := requirementsSectionFromFilename(path)

	rows := make([]map[string]any, 0)
	scanner := bufio.NewScanner(strings.NewReader(string(source)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNumber := 0
	pendingContinuation := ""
	pendingLine := 0
	for scanner.Scan() {
		lineNumber++
		raw := scanner.Text()
		stripped := strings.TrimRight(raw, " \t\r")
		if strings.HasSuffix(stripped, "\\") {
			pendingContinuation += strings.TrimSuffix(stripped, "\\")
			if pendingLine == 0 {
				pendingLine = lineNumber
			}
			continue
		}
		full := pendingContinuation + stripped
		startLine := lineNumber
		if pendingLine != 0 {
			startLine = pendingLine
		}
		pendingContinuation = ""
		pendingLine = 0

		trimmed := strings.TrimSpace(full)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Inline comments: strip after a space + '#'. We keep the substring
		// before the first " #" so URLs containing # fragments (e.g. egg=)
		// are preserved.
		if index := indexOfInlineComment(trimmed); index >= 0 {
			trimmed = strings.TrimSpace(trimmed[:index])
			if trimmed == "" {
				continue
			}
		}
		if strings.HasPrefix(trimmed, "-") {
			// Pip options like --hash, --extra-index-url, -r, -c are not
			// dependencies on their own. Skip them rather than treating them
			// as malformed so requirement files that mix in options remain
			// clean.
			if !isEditableFlag(trimmed) {
				continue
			}
		}
		row := parseRequirementLine(trimmed, raw, section, devScope, startLine)
		rows = append(rows, row.finish())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	payload["variables"] = rows
	return payload, nil
}

func requirementsDevScopeFromFilename(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	name = strings.TrimSuffix(name, ".txt")
	name = strings.TrimSuffix(name, ".in")
	if name == "requirements" {
		return false
	}
	suffix := strings.TrimPrefix(name, "requirements-")
	if suffix == name {
		suffix = strings.TrimPrefix(name, "requirements_")
	}
	if suffix == name {
		return false
	}
	switch suffix {
	case "dev", "develop", "development", "test", "tests", "testing", "ci", "qa", "lint":
		return true
	}
	return strings.Contains(suffix, "dev") || strings.Contains(suffix, "test")
}

func requirementsSectionFromFilename(path string) string {
	name := strings.ToLower(filepath.Base(path))
	name = strings.TrimSuffix(name, ".txt")
	name = strings.TrimSuffix(name, ".in")
	if name == "" {
		return "requirements"
	}
	return name
}

func isEditableFlag(line string) bool {
	if strings.HasPrefix(line, "-e ") || strings.HasPrefix(line, "--editable ") {
		return true
	}
	return false
}

func indexOfInlineComment(line string) int {
	// Walk the line and find the first '#' that is preceded by whitespace.
	// A '#' inside a URL fragment (e.g. egg=name) is not whitespace-preceded.
	for i := 1; i < len(line); i++ {
		if line[i] == '#' && (line[i-1] == ' ' || line[i-1] == '\t') {
			return i
		}
	}
	return -1
}

func parseRequirementLine(content string, raw string, section string, dev bool, lineNumber int) rowBuilder {
	builder := rowBuilder{
		LineNumber:     lineNumber,
		Section:        section,
		PackageManager: PackageManager,
		Lang:           LangRequirements,
		DevDependency:  dev,
		Raw:            raw,
	}

	editable := false
	if strings.HasPrefix(content, "-e ") {
		editable = true
		content = strings.TrimSpace(strings.TrimPrefix(content, "-e "))
	} else if strings.HasPrefix(content, "--editable ") {
		editable = true
		content = strings.TrimSpace(strings.TrimPrefix(content, "--editable "))
	}

	// Marker isolation: PEP 508 splits on `;`.
	marker := ""
	if index := strings.Index(content, ";"); index >= 0 {
		marker = strings.TrimSpace(content[index+1:])
		content = strings.TrimSpace(content[:index])
	}
	builder.Marker = marker

	if name, extras, source, ok := splitDirectReference(content); ok {
		populateDirectReferenceRow(&builder, name, extras, source, editable)
		return builder
	}

	switch {
	case isVCSRequirement(content):
		populateVCSRow(&builder, content, editable)
		return builder
	case isURLRequirement(content):
		populateURLRow(&builder, content, editable)
		return builder
	case looksLikePathRequirement(content, editable):
		populatePathRow(&builder, content, editable)
		return builder
	}

	name, extras, value, ok := splitNameSpecifier(content)
	if !ok || name == "" {
		builder.Name = ""
		builder.Value = ""
		builder.ConfigKind = configKindMalformed
		builder.Malformed = true
		return builder
	}
	builder.Name = name
	builder.Extras = extras
	builder.Value = value
	builder.ConfigKind = configKindDependency
	return builder
}

func splitDirectReference(content string) (string, []string, string, bool) {
	left, right, ok := strings.Cut(content, "@")
	if !ok {
		return "", nil, "", false
	}
	name, extras, value, valid := splitNameSpecifier(strings.TrimSpace(left))
	if !valid || name == "" || value != "" {
		return "", nil, "", false
	}
	source := strings.TrimSpace(right)
	if source == "" {
		return "", nil, "", false
	}
	return name, extras, source, true
}

func populateDirectReferenceRow(
	builder *rowBuilder,
	name string,
	extras []string,
	source string,
	editable bool,
) {
	builder.Name = name
	builder.Extras = extras
	switch {
	case isVCSRequirement(source):
		populateVCSRow(builder, source, editable)
	case isURLRequirement(source):
		populateURLRow(builder, source, editable)
	case looksLikePathRequirement(source, editable):
		populatePathRow(builder, source, editable)
	default:
		builder.Value = source
		builder.ConfigKind = configKindMalformed
		builder.Malformed = true
		return
	}
	builder.Name = name
	builder.Extras = extras
}

func isVCSRequirement(content string) bool {
	lower := strings.ToLower(content)
	for _, prefix := range []string{"git+", "hg+", "svn+", "bzr+"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func isURLRequirement(content string) bool {
	lower := strings.ToLower(content)
	switch {
	case strings.HasPrefix(lower, "http://"),
		strings.HasPrefix(lower, "https://"),
		strings.HasPrefix(lower, "ftp://"),
		strings.HasPrefix(lower, "file://"):
		return true
	}
	return false
}

func looksLikePathRequirement(content string, editable bool) bool {
	if editable {
		return true
	}
	if strings.HasPrefix(content, "./") || strings.HasPrefix(content, "../") {
		return true
	}
	if strings.HasPrefix(content, "/") {
		return true
	}
	return false
}

func populateVCSRow(builder *rowBuilder, content string, editable bool) {
	builder.SourceKind = "vcs"
	builder.SourceURL = stripFragment(content)
	builder.Name = eggNameFromURL(content)
	builder.Value = content
	if editable {
		builder.ConfigKind = configKindEditable
	} else {
		builder.ConfigKind = configKindVCS
	}
}

func populateURLRow(builder *rowBuilder, content string, editable bool) {
	builder.SourceKind = "url"
	builder.SourceURL = stripFragment(content)
	builder.Name = eggNameFromURL(content)
	builder.Value = content
	if editable {
		builder.ConfigKind = configKindEditable
	} else {
		builder.ConfigKind = configKindURL
	}
}

func populatePathRow(builder *rowBuilder, content string, editable bool) {
	builder.SourceKind = "path"
	builder.Value = content
	builder.Name = filepath.Base(strings.TrimRight(content, "/"))
	if editable {
		builder.ConfigKind = configKindEditable
	} else {
		builder.ConfigKind = configKindPath
	}
	if builder.Name == "" || builder.Name == "." || builder.Name == ".." {
		builder.Name = content
	}
}

func stripFragment(content string) string {
	if index := strings.Index(content, "#"); index >= 0 {
		return content[:index]
	}
	return content
}

func eggNameFromURL(content string) string {
	hash := strings.Index(content, "#")
	if hash < 0 {
		return ""
	}
	fragment := content[hash+1:]
	for _, part := range strings.Split(fragment, "&") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "egg") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// splitNameSpecifier parses "name[extra1,extra2] specifier". It returns the
// declared package name, the extras list (without spaces), and the
// version-specifier string (without surrounding whitespace). It does NOT
// validate that the name conforms to PEP 503 or that the specifier is a
// legal PEP 440 range; that is the reducer's job through packageidentity.
func splitNameSpecifier(content string) (string, []string, string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", nil, "", false
	}

	// First valid identifier character must be a letter or digit. Anything
	// else (e.g. "@@@ junk") is malformed.
	if !isPyPINameRune(rune(trimmed[0])) {
		return "", nil, "", false
	}

	end := 0
	for end < len(trimmed) {
		r := rune(trimmed[end])
		if isPyPINameRune(r) {
			end++
			continue
		}
		break
	}
	name := trimmed[:end]
	if name == "" {
		return "", nil, "", false
	}
	rest := strings.TrimSpace(trimmed[end:])

	var extras []string
	if strings.HasPrefix(rest, "[") {
		closing := strings.Index(rest, "]")
		if closing < 0 {
			return "", nil, "", false
		}
		extras = splitExtras(rest[1:closing])
		rest = strings.TrimSpace(rest[closing+1:])
	}
	return name, extras, normalizeSpecifier(rest), true
}

func splitExtras(content string) []string {
	parts := strings.Split(content, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		extra := strings.TrimSpace(part)
		if extra != "" {
			out = append(out, extra)
		}
	}
	return out
}

func isPyPINameRune(r rune) bool {
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '_', '-', '.':
		return true
	}
	return false
}

func normalizeSpecifier(rest string) string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}
	// Collapse whitespace inside specifiers so "~= 1.26" and ">=4.2 , <5.0"
	// reach the reducer in the canonical "~=1.26" / ">=4.2,<5.0" form.
	var builder strings.Builder
	for _, r := range rest {
		if r == ' ' || r == '\t' {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

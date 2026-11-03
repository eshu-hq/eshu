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
	constraints := isConstraintsFile(path)
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
		if constraints {
			row.applyConstraintPolicy()
		}
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
	if name == "requirements" || name == "constraints" {
		return false
	}
	suffix := name
	for _, prefix := range []string{"requirements-", "requirements_", "constraints-", "constraints_"} {
		if trimmed := strings.TrimPrefix(name, prefix); trimmed != name {
			suffix = trimmed
			break
		}
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

	if name, extras, source, left, ok := splitDirectReference(content); ok {
		populateDirectReferenceRow(&builder, name, extras, source, left, editable)
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
	builder.applyRequirementsPolicy()
	return builder
}

// applyRequirementsPolicy keeps credential-shaped text out of registry rows.
// A version specifier never holds "@" or "://", a PEP 508 extra never holds
// them either, and a PEP 508 marker never holds them (see safeMarker): any
// one proves the line is not the plain pin it parses as — an scp-style
// "user:secret@host:path" line, an extra smuggling URL text, or a marker
// smuggling URL text. The row is demoted to malformed with its text fields
// dropped and its source_cache withheld, mirroring the constraints policy,
// so no requirements row can carry URL userinfo in a fact (#9074).
func (b *rowBuilder) applyRequirementsPolicy() {
	if b.ConfigKind == configKindDependency &&
		(strings.Contains(b.Value, "@") || strings.Contains(b.Value, "://") || extrasHoldURLText(b.Extras)) {
		b.Name = ""
		b.Value = ""
		b.Marker = ""
		b.Extras = nil
		b.Raw = ""
		b.SourceCacheWithheld = true
		b.ConfigKind = configKindMalformed
		b.Malformed = true
		return
	}
	if b.ConfigKind == configKindDependency && !safeMarker(b.Marker) {
		b.Name = ""
		b.Value = ""
		b.Marker = ""
		b.Extras = nil
		b.Raw = ""
		b.SourceCacheWithheld = true
		b.ConfigKind = configKindMalformed
		b.Malformed = true
	}
}

// extrasHoldURLText reports whether any PEP 508 extra holds "@" or "://".
// Extras are identifiers; either marker proves the line is not the plain pin
// it parses as.
func extrasHoldURLText(extras []string) bool {
	for _, extra := range extras {
		if strings.Contains(extra, "@") || strings.Contains(extra, "://") {
			return true
		}
	}
	return false
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

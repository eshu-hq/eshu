// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package parser owns the native Go parser contract and registry metadata.
package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Admission is the level at which discovery admits files bound to a parser
// definition. L0 (not admitted) is the implicit default for any name that no
// definition binds, so it has no constant. L3 (graph projection) is a
// property of the reducer projection and is recorded there, not here.
type Admission string

const (
	// AdmissionContentText (L1) admits matching files into the content store
	// as searchable text only. No structured entities or facts are extracted.
	AdmissionContentText Admission = "L1"
	// AdmissionStructuredFact (L2) admits matching files through a parser that
	// emits structured entities or facts.
	AdmissionStructuredFact Admission = "L2"
)

// Definition describes one parser contract entry.
//
// Ecosystem and Admission are documentation metadata for the generated
// admission table. They do not take part in lookup or in Registry.Fingerprint,
// so labelling a definition never changes what discovery keeps.
type Definition struct {
	ParserKey string
	Language  string
	// Ecosystem is the language key in
	// specs/language-feature-parity-ledger.v1.yaml that owns the bound files,
	// or a cross-cutting name (dockerfile, raw_text, yaml) with no ledger row.
	Ecosystem string
	// Admission is the level at which the bound files are admitted.
	Admission   Admission
	Extensions  []string
	ExactNames  []string
	PrefixNames []string
	Provider    LanguageProvider
}

// Registry is an immutable catalog of parser definitions and lookup indexes.
type Registry struct {
	definitions   []Definition
	byKey         map[string]Definition
	byExtension   map[string]Definition
	byExactName   map[string]Definition
	prefixMatches []prefixDefinition
}

type prefixDefinition struct {
	prefix string
	def    Definition
}

// NewRegistry builds an immutable parser registry from the provided definitions.
func NewRegistry(definitions []Definition) (Registry, error) {
	cloned := make([]Definition, 0, len(definitions))
	byKey := make(map[string]Definition, len(definitions))
	byExtension := make(map[string]Definition)
	byExactName := make(map[string]Definition)
	prefixMatches := make([]prefixDefinition, 0, len(definitions))

	for _, definition := range definitions {
		normalized, err := normalizeDefinition(definition)
		if err != nil {
			return Registry{}, err
		}
		if _, exists := byKey[normalized.ParserKey]; exists {
			return Registry{}, fmt.Errorf("parser key %q already registered", normalized.ParserKey)
		}
		for _, extension := range normalized.Extensions {
			if _, exists := byExtension[extension]; exists {
				return Registry{}, fmt.Errorf("extension %q already registered", extension)
			}
		}
		for _, exactName := range normalized.ExactNames {
			key := strings.ToLower(exactName)
			if _, exists := byExactName[key]; exists {
				return Registry{}, fmt.Errorf("filename %q already registered", exactName)
			}
		}

		byKey[normalized.ParserKey] = normalized
		cloned = append(cloned, normalized)
		for _, extension := range normalized.Extensions {
			byExtension[extension] = normalized
		}
		for _, exactName := range normalized.ExactNames {
			byExactName[strings.ToLower(exactName)] = normalized
		}
		for _, prefix := range normalized.PrefixNames {
			prefixMatches = append(prefixMatches, prefixDefinition{
				prefix: strings.ToLower(prefix),
				def:    normalized,
			})
		}
	}

	slices.SortFunc(cloned, func(left, right Definition) int {
		return strings.Compare(left.ParserKey, right.ParserKey)
	})
	slices.SortFunc(prefixMatches, func(left, right prefixDefinition) int {
		if len(left.prefix) != len(right.prefix) {
			return len(right.prefix) - len(left.prefix)
		}
		return strings.Compare(left.prefix, right.prefix)
	})

	return Registry{
		definitions:   cloned,
		byKey:         byKey,
		byExtension:   byExtension,
		byExactName:   byExactName,
		prefixMatches: prefixMatches,
	}, nil
}

// DefaultRegistry returns the built-in parser metadata catalog for this wave.
func DefaultRegistry() Registry {
	registry, err := NewRegistry(defaultDefinitions())
	if err != nil {
		panic(fmt.Sprintf("default parser registry is invalid: %v", err))
	}
	return registry
}

// LookupByExtension returns the definition registered for one file extension.
func (r Registry) LookupByExtension(extension string) (Definition, bool) {
	normalized := normalizeExtensionKey(extension)
	definition, ok := r.byExtension[normalized]
	return cloneDefinition(definition), ok
}

// LookupByPath returns the definition registered for one file path.
func (r Registry) LookupByPath(path string) (Definition, bool) {
	if isJavaMetadataPath(path) {
		definition, ok := r.byKey["java_metadata"]
		return cloneDefinition(definition), ok
	}
	if isScalaPlayRoutesPath(path) {
		definition, ok := r.byKey["scala"]
		return cloneDefinition(definition), ok
	}

	base := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(base, ".tfvars.json") {
		if definition, ok := r.byKey["hcl"]; ok {
			return cloneDefinition(definition), true
		}
	}
	if definition, ok := r.byExactName[base]; ok {
		return cloneDefinition(definition), true
	}
	for _, candidate := range r.prefixMatches {
		if strings.HasPrefix(base, candidate.prefix) {
			return cloneDefinition(candidate.def), true
		}
	}
	extension := normalizeExtensionKey(filepath.Ext(base))
	if extension == "." {
		return Definition{}, false
	}
	definition, ok := r.byExtension[extension]
	return cloneDefinition(definition), ok
}

func isScalaPlayRoutesPath(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	return normalized == "conf/routes" ||
		strings.HasSuffix(normalized, "/conf/routes") ||
		strings.HasSuffix(normalized, ".routes")
}

func isJavaMetadataPath(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if strings.HasPrefix(normalized, "meta-inf/services/") ||
		strings.Contains(normalized, "/meta-inf/services/") {
		_, serviceName, ok := strings.Cut(normalized, "meta-inf/services/")
		return ok && strings.TrimSpace(serviceName) != ""
	}
	return normalized == "meta-inf/spring/org.springframework.boot.autoconfigure.autoconfiguration.imports" ||
		strings.HasSuffix(normalized, "/meta-inf/spring/org.springframework.boot.autoconfigure.autoconfiguration.imports") ||
		normalized == "meta-inf/spring.factories" ||
		strings.HasSuffix(normalized, "/meta-inf/spring.factories")
}

// LookupByParserKey returns the definition registered for one parser key.
func (r Registry) LookupByParserKey(parserKey string) (Definition, bool) {
	normalized := strings.TrimSpace(parserKey)
	if normalized == "" {
		return Definition{}, false
	}
	definition, ok := r.byKey[normalized]
	return cloneDefinition(definition), ok
}

// Definitions returns the registered definitions in deterministic parser-key order.
func (r Registry) Definitions() []Definition {
	return cloneDefinitions(r.definitions)
}

// Fingerprint returns the parser-set identity for this registry (#7986): a
// SHA-256 over every definition's key, language, extensions, and exact and
// prefix names. Definitions stay sorted by parser key and each binding list is
// normalized and sorted at construction, so the digest is deterministic across
// calls, processes, and release builds: an unchanged parser set keeps every
// freshness hint, while a registration change (a new parser, extension, or
// file name) moves the digest with no hand edit. It covers the registry
// bindings only, not parser logic: a behavior change with identical bindings
// still needs a hand-bumped content version (see the git collector's pin test).
// The hardcoded path-dispatch rules in LookupByPath (java metadata, Scala
// routes, .tfvars.json) also live outside the bindings: adding or changing one
// needs a hand bump too.
func (r Registry) Fingerprint() string {
	h := sha256.New()
	for _, definition := range r.definitions {
		// A hash never returns a write error; the blank assignment keeps
		// errcheck quiet without changing behavior.
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\n",
			definition.ParserKey,
			definition.Language,
			strings.Join(definition.Extensions, ","),
			strings.Join(definition.ExactNames, ","),
			strings.Join(definition.PrefixNames, ","))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ParserKeys returns the registered parser keys in deterministic order.
func (r Registry) ParserKeys() []string {
	keys := make([]string, 0, len(r.definitions))
	for _, definition := range r.definitions {
		keys = append(keys, definition.ParserKey)
	}
	return keys
}

// Extensions returns the registered supported extensions in deterministic order.
func (r Registry) Extensions() []string {
	extensions := make([]string, 0, len(r.byExtension))
	for extension := range r.byExtension {
		extensions = append(extensions, extension)
	}
	slices.Sort(extensions)
	return extensions
}

func normalizeDefinition(definition Definition) (Definition, error) {
	definition.ParserKey = strings.TrimSpace(definition.ParserKey)
	definition.Language = strings.TrimSpace(definition.Language)
	if definition.ParserKey == "" {
		return Definition{}, errors.New("parser key must not be blank")
	}
	if definition.Language == "" {
		return Definition{}, fmt.Errorf("language must not be blank for parser key %q", definition.ParserKey)
	}

	definition.Extensions = normalizeExtensions(definition.Extensions)
	definition.ExactNames = normalizePreservedNames(definition.ExactNames)
	definition.PrefixNames = normalizePreservedNames(definition.PrefixNames)

	return definition, nil
}

func normalizeExtensions(extensions []string) []string {
	if len(extensions) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(extensions))
	normalized := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		canonical := normalizeExtensionKey(extension)
		if canonical == "." {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		normalized = append(normalized, canonical)
	}
	slices.Sort(normalized)
	return normalized
}

func normalizePreservedNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}

	type preservedName struct {
		original string
		lower    string
	}

	seen := make(map[string]struct{}, len(names))
	normalized := make([]preservedName, 0, len(names))
	for _, name := range names {
		canonical := strings.TrimSpace(name)
		if canonical == "" {
			continue
		}
		lower := strings.ToLower(canonical)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		normalized = append(normalized, preservedName{original: canonical, lower: lower})
	}

	slices.SortFunc(normalized, func(left, right preservedName) int {
		if left.lower != right.lower {
			return strings.Compare(left.lower, right.lower)
		}
		return strings.Compare(left.original, right.original)
	})

	result := make([]string, 0, len(normalized))
	for _, name := range normalized {
		result = append(result, name.original)
	}
	return result
}

func normalizeExtensionKey(extension string) string {
	canonical := strings.ToLower(strings.TrimSpace(extension))
	if canonical == "" {
		return ""
	}
	if !strings.HasPrefix(canonical, ".") {
		canonical = "." + canonical
	}
	return canonical
}

func cloneDefinition(definition Definition) Definition {
	cloned := Definition{
		ParserKey: definition.ParserKey,
		Language:  definition.Language,
		Ecosystem: definition.Ecosystem,
		Admission: definition.Admission,
		Provider:  definition.Provider,
	}
	cloned.Extensions = slices.Clone(definition.Extensions)
	cloned.ExactNames = slices.Clone(definition.ExactNames)
	cloned.PrefixNames = slices.Clone(definition.PrefixNames)
	return cloned
}

func cloneDefinitions(definitions []Definition) []Definition {
	cloned := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		cloned = append(cloned, cloneDefinition(definition))
	}
	return cloned
}

// AdmissionDocPath is the repo-relative documentation page that carries the
// generated admission table between AdmissionTableBegin and AdmissionTableEnd.
const AdmissionDocPath = "docs/public/languages/admitted-files.md"

const (
	// AdmissionTableBegin opens the generated region of the documentation page.
	AdmissionTableBegin = "<!-- parser-admission-table:begin (generated by scripts/generate-parser-admission-doc.sh; do not edit) -->"
	// AdmissionTableEnd closes the generated region of the documentation page.
	AdmissionTableEnd = "<!-- parser-admission-table:end -->"
)

// PathDispatch describes one binding that LookupByPath resolves by hardcoded
// path logic instead of an Extensions, ExactNames, or PrefixNames entry.
// AdmissionTableMarkdown renders these beside the table so the generated page
// shows every path the registry admits. A test resolves each example through
// LookupByPath, and an AST test requires the parser keys listed here to equal
// the keys LookupByPath binds through a literal r.byKey["<key>"] lookup.
type PathDispatch struct {
	// Patterns are the human-readable path rules, each rendered as code.
	Patterns []string
	// ParserKey is the definition the rule resolves to.
	ParserKey string
	// Examples are concrete paths that LookupByPath resolves to ParserKey.
	Examples []string
}

// PathDispatches returns the hardcoded path-dispatch bindings of LookupByPath,
// in a fixed order. A new hardcoded rule in LookupByPath must be listed here;
// the AST test fails when a bound key is missing, but it does not check the
// path patterns, so edit Patterns and Examples with the rule.
func PathDispatches() []PathDispatch {
	return []PathDispatch{
		{
			Patterns:  []string{"META-INF/services/<service>", "META-INF/spring.factories", "META-INF/spring/org.springframework.boot.autoconfigure.AutoConfiguration.imports"},
			ParserKey: "java_metadata",
			Examples: []string{
				"src/main/resources/META-INF/services/com.example.Plugin",
				"src/main/resources/META-INF/spring.factories",
				"src/main/resources/META-INF/spring/org.springframework.boot.autoconfigure.AutoConfiguration.imports",
			},
		},
		{
			Patterns:  []string{"conf/routes", "*.routes"},
			ParserKey: "scala",
			Examples:  []string{"conf/routes", "app/conf/routes", "conf/api.routes"},
		},
		{
			Patterns:  []string{"*.tfvars.json"},
			ParserKey: "hcl",
			Examples:  []string{"prod.tfvars.json", "env/stage.auto.tfvars.json"},
		},
	}
}

// AdmissionTableMarkdown renders one Markdown table row per registered
// definition, in parser-key order, followed by a footnote naming the
// path-dispatched bindings that no row column captures, so the documented
// list of admitted file names is the full registry admission truth. The
// output is deterministic and ends with a newline.
func AdmissionTableMarkdown(registry Registry) string {
	var b strings.Builder
	b.WriteString("| Parser key | Ecosystem | Admission | Extensions | Exact file names | File name prefixes |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, definition := range registry.Definitions() {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s |\n",
			definition.ParserKey,
			definition.Ecosystem,
			admissionLabel(definition.Admission),
			codeList(definition.Extensions),
			codeList(definition.ExactNames),
			codeList(definition.PrefixNames),
		)
	}
	b.WriteString("\nPath-dispatched bindings admit these paths through `Registry.LookupByPath` rules\n")
	b.WriteString("that the columns above do not list. A row can look empty and still admit files.\n")
	b.WriteString("Paths are matched case-insensitively:\n\n")
	for _, dispatch := range PathDispatches() {
		fmt.Fprintf(&b, "- %s bind to `%s`.\n", codeList(dispatch.Patterns), dispatch.ParserKey)
	}
	return b.String()
}

// ReplaceAdmissionTable returns doc with the text between the admission table
// markers replaced by table. It returns an error when either marker is missing
// or they are out of order or repeated, so a stale or hand-damaged page fails
// loudly.
func ReplaceAdmissionTable(doc, table string) (string, error) {
	begin := strings.Index(doc, AdmissionTableBegin)
	end := strings.Index(doc, AdmissionTableEnd)
	if begin < 0 || end < 0 || end < begin {
		return "", fmt.Errorf("admission table markers %q and %q not found in order", AdmissionTableBegin, AdmissionTableEnd)
	}
	if strings.Count(doc, AdmissionTableBegin) != 1 || strings.Count(doc, AdmissionTableEnd) != 1 {
		return "", fmt.Errorf("admission table markers must each appear exactly once")
	}
	return doc[:begin+len(AdmissionTableBegin)] + "\n" + table + doc[end:], nil
}

func admissionLabel(level Admission) string {
	switch level {
	case AdmissionContentText:
		return "L1 content text"
	case AdmissionStructuredFact:
		return "L2 structured fact"
	default:
		return string(level)
	}
}

func codeList(values []string) string {
	if len(values) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "`"+value+"`")
	}
	return strings.Join(quoted, ", ")
}

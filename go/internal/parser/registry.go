// Package parser owns the native Go parser contract and registry metadata.
package parser

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Definition describes one parser contract entry.
type Definition struct {
	ParserKey   string
	Language    string
	Extensions  []string
	ExactNames  []string
	PrefixNames []string
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

func defaultDefinitions() []Definition {
	return []Definition{
		{
			ParserKey:  "__dockerfile__",
			Language:   "dockerfile",
			ExactNames: []string{"Dockerfile"},
			PrefixNames: []string{
				"Dockerfile.",
			},
		},
		{
			ParserKey:  "__jenkinsfile__",
			Language:   "groovy",
			ExactNames: []string{"Jenkinsfile"},
			PrefixNames: []string{
				"Jenkinsfile.",
			},
		},
		{
			ParserKey:  "c",
			Language:   "c",
			Extensions: []string{".c"},
		},
		{
			ParserKey:  "c_sharp",
			Language:   "c_sharp",
			Extensions: []string{".cs", ".csx"},
		},
		{
			ParserKey:  "cpp",
			Language:   "cpp",
			Extensions: []string{".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp"},
		},
		{
			ParserKey:  "dart",
			Language:   "dart",
			Extensions: []string{".dart"},
		},
		{
			ParserKey:  "elixir",
			Language:   "elixir",
			Extensions: []string{".ex", ".exs"},
		},
		{
			ParserKey:  "go",
			Language:   "go",
			Extensions: []string{".go"},
		},
		{
			ParserKey:  "groovy",
			Language:   "groovy",
			Extensions: []string{".groovy"},
		},
		{
			ParserKey:  "haskell",
			Language:   "haskell",
			Extensions: []string{".hs"},
		},
		{
			ParserKey:  "hcl",
			Language:   "hcl",
			Extensions: []string{".hcl", ".tf", ".tfvars"},
		},
		{
			ParserKey:  "java",
			Language:   "java",
			Extensions: []string{".java"},
		},
		{
			ParserKey: "java_metadata",
			Language:  "java_metadata",
		},
		{
			ParserKey:  "javascript",
			Language:   "javascript",
			Extensions: []string{".cjs", ".js", ".jsx", ".mjs"},
		},
		{
			ParserKey:  "json",
			Language:   "json",
			Extensions: []string{".json"},
		},
		{
			ParserKey:  "kotlin",
			Language:   "kotlin",
			Extensions: []string{".kt"},
		},
		{
			ParserKey:  "perl",
			Language:   "perl",
			Extensions: []string{".pl", ".pm"},
		},
		{
			ParserKey:  "php",
			Language:   "php",
			Extensions: []string{".php"},
		},
		{
			ParserKey:  "python",
			Language:   "python",
			Extensions: []string{".ipynb", ".py", ".pyw"},
		},
		{
			ParserKey:  "raw_text",
			Language:   "raw_text",
			Extensions: []string{".cnf", ".cfg", ".conf", ".j2", ".jinja", ".jinja2", ".tpl", ".tftpl"},
		},
		{
			ParserKey:  "ruby",
			Language:   "ruby",
			Extensions: []string{".rb"},
		},
		{
			ParserKey:  "rust",
			Language:   "rust",
			Extensions: []string{".rs"},
		},
		{
			ParserKey:  "scala",
			Language:   "scala",
			Extensions: []string{".sc", ".scala"},
		},
		{
			ParserKey:  "sql",
			Language:   "sql",
			Extensions: []string{".sql"},
		},
		{
			ParserKey:  "swift",
			Language:   "swift",
			Extensions: []string{".swift"},
		},
		{
			ParserKey:  "tsx",
			Language:   "tsx",
			Extensions: []string{".tsx"},
		},
		{
			ParserKey:  "typescript",
			Language:   "typescript",
			Extensions: []string{".cts", ".mts", ".ts"},
		},
		{
			ParserKey:  "yaml",
			Language:   "yaml",
			Extensions: []string{".yaml", ".yml"},
		},
	}
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

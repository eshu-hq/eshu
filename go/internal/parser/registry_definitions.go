// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"
)

// defaultDefinitions returns the built-in parser metadata catalog for this wave.
// It is the source-of-truth literal consumed by DefaultRegistry; keep new parser
// registrations here so registry.go stays focused on registry construction and
// lookup logic.
func defaultDefinitions() []Definition {
	return []Definition{
		{
			ParserKey:  "__dockerfile__",
			Language:   "dockerfile",
			Ecosystem:  "dockerfile",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"Dockerfile"},
			PrefixNames: []string{
				"Dockerfile.",
			},
		},
		{
			ParserKey:  "__jenkinsfile__",
			Language:   "groovy",
			Ecosystem:  "groovy",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"Jenkinsfile"},
			PrefixNames: []string{
				"Jenkinsfile.",
			},
		},
		{
			ParserKey:  "c",
			Language:   "c",
			Ecosystem:  "c",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".c"},
		},
		{
			ParserKey:  "c_sharp",
			Language:   "c_sharp",
			Ecosystem:  "csharp",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".cs", ".csx"},
		},
		{
			ParserKey:  "cpp",
			Language:   "cpp",
			Ecosystem:  "cpp",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp", ".hxx"},
		},
		{
			ParserKey:  "dart",
			Language:   "dart",
			Ecosystem:  "dart",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".dart"},
		},
		{
			ParserKey:  "elixir",
			Language:   "elixir",
			Ecosystem:  "elixir",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".ex", ".exs"},
			ExactNames: []string{"mix.lock"},
		},
		{
			ParserKey:  "go",
			Language:   "go",
			Ecosystem:  "go",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".go"},
		},
		{
			ParserKey:  "gomod",
			Language:   "gomod",
			Ecosystem:  "go",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"go.mod", "go.sum", "go.work"},
		},
		{
			ParserKey:  "groovy",
			Language:   "groovy",
			Ecosystem:  "groovy",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".groovy"},
		},
		{
			ParserKey:  "haskell",
			Language:   "haskell",
			Ecosystem:  "haskell",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".hs"},
		},
		{
			ParserKey:  "hcl",
			Language:   "hcl",
			Ecosystem:  "terraform",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".hcl", ".tf", ".tfvars"},
		},
		{
			ParserKey:  "java",
			Language:   "java",
			Ecosystem:  "java",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".java"},
		},
		{
			ParserKey: "java_metadata",
			Language:  "java_metadata",
			Ecosystem: "java",
			Admission: AdmissionStructuredFact,
		},
		{
			ParserKey:  "gradle",
			Language:   "gradle",
			Ecosystem:  "java",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"build.gradle", "build.gradle.kts", "libs.versions.toml"},
		},
		{
			ParserKey:  "javascript",
			Language:   "javascript",
			Ecosystem:  "javascript",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".cjs", ".js", ".jsx", ".mjs"},
		},
		{
			ParserKey:  "json",
			Language:   "json",
			Ecosystem:  "json",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".json", ".jsonc"},
			ExactNames: []string{"composer.lock", "Pipfile.lock", "Package.resolved"},
		},
		{
			ParserKey:  "nuget_project",
			Language:   "nuget_project",
			Ecosystem:  "csharp",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".csproj"},
		},
		{
			ParserKey:  "kotlin",
			Language:   "kotlin",
			Ecosystem:  "kotlin",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".kt", ".kts"},
		},
		{
			ParserKey:  "maven",
			Language:   "maven",
			Ecosystem:  "java",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"pom.xml"},
		},
		{
			ParserKey:  "perl",
			Language:   "perl",
			Ecosystem:  "perl",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".pl", ".pm"},
		},
		{
			ParserKey:  "php",
			Language:   "php",
			Ecosystem:  "php",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".php"},
		},
		{
			ParserKey:  "python",
			Language:   "python",
			Ecosystem:  "python",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".ipynb", ".py", ".pyi", ".pyw"},
		},
		{
			ParserKey:  "python_requirements",
			Language:   "python_requirements",
			Ecosystem:  "python",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"requirements.txt"},
			PrefixNames: []string{
				"requirements-",
				"requirements_",
			},
		},
		{
			ParserKey:  "python_toml",
			Language:   "python_toml",
			Ecosystem:  "python",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"pyproject.toml", "Pipfile", "poetry.lock", "uv.lock"},
		},
		{
			ParserKey:  "raw_text",
			Language:   "raw_text",
			Ecosystem:  "raw_text",
			Admission:  AdmissionContentText,
			Extensions: []string{".cnf", ".cfg", ".conf", ".j2", ".jinja", ".jinja2", ".tpl", ".tftpl"},
			// Puppetfile (r10k) and Berksfile (Berkshelf) have no tree-sitter
			// grammar; the raw_text engine captures their content_body so the
			// relationships engine can extract PUPPET_MODULE_REFERENCE evidence from
			// `mod ... :git => ...` and CHEF_COOKBOOK_DEPENDENCY evidence from
			// `cookbook ..., git: ...` declarations without a dedicated parser.
			//
			// The runtime-version pin files below hold only version strings. They
			// are an explicit name list, never a "*-version" rule, so every
			// admitted name is reviewed. Files that can carry credentials or
			// literal environment values (.npmrc, .pypirc, .netrc, .env*,
			// mise.toml) stay out: the content store has no write-time redaction.
			//
			// The universal build and tool configuration names (#8814) are
			// likewise an explicit list, never an extension rule: .ini, .toml, .xml and
			// .properties stay unadmitted. Taskfile.yml is not listed because the
			// .yml extension already admits it as structured YAML, and an exact
			// name would downgrade it to plain text. phpunit.xml, .mvn/jvm.config
			// and .mvn/maven.config are not listed because they carry
			// environment values and -D properties, and the content store has
			// no write-time redaction. .mvn/extensions.xml is admitted by the
			// path rule in LookupByPath, since its base name is too generic.
			ExactNames: []string{
				"Puppetfile", "Berksfile",
				".nvmrc", ".node-version", ".tool-versions", ".python-version",
				".ruby-version", ".java-version", ".go-version", ".sdkmanrc",
				".swift-version", ".terraform-version",
				"rust-toolchain", "rust-toolchain.toml",
				"Makefile", "GNUmakefile", "justfile",
				".editorconfig", ".gitattributes", ".dockerignore", ".gitignore",
				".eslintrc", ".prettierrc", ".babelrc", ".npmignore",
				"tox.ini", "mypy.ini", "pytest.ini", ".flake8",
				"rustfmt.toml", "clippy.toml", "renovate.json5",
			},
		},
		{
			ParserKey:  "ruby",
			Language:   "ruby",
			Ecosystem:  "ruby",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".rb"},
			ExactNames: []string{"Gemfile", "Gemfile.lock"},
		},
		{
			ParserKey:  "rust",
			Language:   "rust",
			Ecosystem:  "rust",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".rs"},
			ExactNames: []string{"Cargo.toml", "Cargo.lock"},
		},
		{
			ParserKey:  "scala",
			Language:   "scala",
			Ecosystem:  "scala",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".sc", ".scala", ".routes"},
		},
		{
			ParserKey:  "sql",
			Language:   "sql",
			Ecosystem:  "sql",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".sql"},
		},
		{
			ParserKey:  "swift",
			Language:   "swift",
			Ecosystem:  "swift",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".swift"},
		},
		{
			ParserKey:  "tsx",
			Language:   "tsx",
			Ecosystem:  "typescriptjsx",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".tsx"},
		},
		{
			ParserKey:  "typescript",
			Language:   "typescript",
			Ecosystem:  "typescript",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".cts", ".mts", ".ts"},
		},
		{
			ParserKey:  "node_lockfile",
			Language:   "node_lockfile",
			Ecosystem:  "javascript",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"yarn.lock", "pnpm-lock.yaml", "pnpm-lock.yml"},
		},
		{
			ParserKey:  "yaml",
			Language:   "yaml",
			Ecosystem:  "yaml",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".yaml", ".yml"},
			ExactNames: []string{"pubspec.lock"},
		},
	}
}

// Hardcoded path-dispatch rules. LookupByPath resolves these before the
// Extensions, ExactNames and PrefixNames indexes; they live beside the
// definitions so registry.go stays focused on construction and lookup.

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
// in a fixed order. A new hardcoded rule in LookupByPath must be listed here.
// The AST test fails only when LookupByPath binds a key through a literal
// r.byKey["<key>"] lookup that the list omits. It does not check other ways of
// selecting a definition or new patterns for an already-listed key, so a
// reviewer of a LookupByPath change must edit this list, Patterns and Examples
// by hand.
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
			Patterns:  []string{".mvn/extensions.xml"},
			ParserKey: "raw_text",
			Examples:  []string{".mvn/extensions.xml", "module/.mvn/extensions.xml"},
		},
		{
			Patterns:  []string{"*.tfvars.json"},
			ParserKey: "hcl",
			Examples:  []string{"prod.tfvars.json", "env/stage.auto.tfvars.json"},
		},
	}
}

func isScalaPlayRoutesPath(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	return normalized == "conf/routes" ||
		strings.HasSuffix(normalized, "/conf/routes") ||
		strings.HasSuffix(normalized, ".routes")
}

// isMavenExtensionsPath reports whether path is a Maven core extensions file,
// .mvn/extensions.xml, at any depth. The base name extensions.xml is too
// generic to bind by name, so the parent directory is part of the rule.
func isMavenExtensionsPath(path string) bool {
	// Cheap base-name test first: LookupByPath runs once per walked file, and
	// only an extensions.xml needs the allocating normalization below.
	if !strings.EqualFold(filepath.Base(path), "extensions.xml") {
		return false
	}
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	return normalized == ".mvn/extensions.xml" ||
		strings.HasSuffix(normalized, "/.mvn/extensions.xml")
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

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

// defaultDefinitions returns the built-in parser metadata catalog for this wave.
// It is the source-of-truth literal consumed by DefaultRegistry; keep new parser
// registrations here so registry.go stays focused on registry construction and
// lookup logic.
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
			ExactNames: []string{"mix.lock"},
		},
		{
			ParserKey:  "go",
			Language:   "go",
			Extensions: []string{".go"},
		},
		{
			ParserKey:  "gomod",
			Language:   "gomod",
			ExactNames: []string{"go.mod", "go.sum"},
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
			ParserKey:  "gradle",
			Language:   "gradle",
			ExactNames: []string{"build.gradle", "build.gradle.kts"},
		},
		{
			ParserKey:  "javascript",
			Language:   "javascript",
			Extensions: []string{".cjs", ".js", ".jsx", ".mjs"},
		},
		{
			ParserKey:  "json",
			Language:   "json",
			Extensions: []string{".json", ".jsonc"},
			ExactNames: []string{"composer.lock", "Pipfile.lock", "Package.resolved"},
		},
		{
			ParserKey:  "nuget_project",
			Language:   "nuget_project",
			Extensions: []string{".csproj"},
		},
		{
			ParserKey:  "kotlin",
			Language:   "kotlin",
			Extensions: []string{".kt"},
		},
		{
			ParserKey:  "maven",
			Language:   "maven",
			ExactNames: []string{"pom.xml"},
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
			ParserKey:  "python_requirements",
			Language:   "python_requirements",
			ExactNames: []string{"requirements.txt"},
			PrefixNames: []string{
				"requirements-",
				"requirements_",
			},
		},
		{
			ParserKey:  "python_toml",
			Language:   "python_toml",
			ExactNames: []string{"pyproject.toml", "Pipfile", "poetry.lock"},
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
			ExactNames: []string{"Gemfile", "Gemfile.lock"},
		},
		{
			ParserKey:  "rust",
			Language:   "rust",
			Extensions: []string{".rs"},
			ExactNames: []string{"Cargo.toml", "Cargo.lock"},
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
			ParserKey:  "node_lockfile",
			Language:   "node_lockfile",
			ExactNames: []string{"yarn.lock", "pnpm-lock.yaml", "pnpm-lock.yml"},
		},
		{
			ParserKey:  "yaml",
			Language:   "yaml",
			Extensions: []string{".yaml", ".yml"},
			ExactNames: []string{"pubspec.lock"},
		},
	}
}

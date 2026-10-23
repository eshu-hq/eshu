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
			ExactNames: []string{"go.mod", "go.sum"},
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
			ExactNames: []string{"build.gradle", "build.gradle.kts"},
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
			ExactNames: []string{"pyproject.toml", "Pipfile", "poetry.lock"},
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
			ExactNames: []string{"Puppetfile", "Berksfile"},
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

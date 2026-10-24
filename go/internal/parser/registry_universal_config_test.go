// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"
	"testing"
)

// universalConfigNames is the explicit allow-list of universal build and tool
// configuration files the raw_text family admits by exact name (#8814). It is
// a name list, never an extension rule: .ini, .toml, .xml and .properties stay
// unadmitted as extensions.
var universalConfigNames = []string{
	"Makefile",
	"GNUmakefile",
	"justfile",
	".editorconfig",
	".gitattributes",
	".dockerignore",
	".gitignore",
	".eslintrc",
	".prettierrc",
	".babelrc",
	".npmignore",
	"tox.ini",
	"mypy.ini",
	"pytest.ini",
	".flake8",
	"rustfmt.toml",
	"clippy.toml",
	"renovate.json5",
}

func TestDefaultRegistryAdmitsUniversalConfigNamesAsRawText(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, name := range universalConfigNames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, candidate := range []string{
				name,
				"nested/dir/" + name,
				"/abs/repo/" + name,
				strings.ToLower(name),
				strings.ToUpper(name),
			} {
				definition, ok := registry.LookupByPath(candidate)
				if !ok {
					t.Fatalf("LookupByPath(%q) = false, want raw_text", candidate)
				}
				if definition.ParserKey != "raw_text" {
					t.Fatalf("LookupByPath(%q).ParserKey = %q, want raw_text", candidate, definition.ParserKey)
				}
				if definition.Admission != AdmissionContentText {
					t.Fatalf("LookupByPath(%q).Admission = %q, want %q", candidate, definition.Admission, AdmissionContentText)
				}
			}
		})
	}
}

// TestParsePathUniversalConfigNamesEmitNoEntities proves that admission adds
// searchable text only: the raw_text engine returns an empty-entity payload
// for each admitted name, so no entity node can follow from admission.
func TestParsePathUniversalConfigNamesEmitNoEntities(t *testing.T) {
	t.Parallel()

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	names := append(append([]string{}, universalConfigNames...), ".mvn/extensions.xml")
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			target := filepath.Join(root, filepath.FromSlash(name))
			writeTestFile(t, target, "key = value\n")
			got, err := engine.ParsePath(root, target, false, Options{})
			if err != nil {
				t.Fatalf("ParsePath(%q) error = %v, want nil", name, err)
			}
			if got["lang"] != "raw_text" {
				t.Fatalf("ParsePath(%q) lang = %#v, want raw_text", name, got["lang"])
			}
			assertEmptyNamedBucket(t, got, "functions")
			assertEmptyNamedBucket(t, got, "classes")
			assertEmptyNamedBucket(t, got, "variables")
		})
	}
}

func TestDefaultRegistryAdmitsMavenExtensionsOnlyUnderMvnDirectory(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, admitted := range []string{
		".mvn/extensions.xml",
		"module/.mvn/extensions.xml",
		"/abs/repo/.mvn/extensions.xml",
		".MVN/Extensions.XML",
	} {
		definition, ok := registry.LookupByPath(admitted)
		if !ok || definition.ParserKey != "raw_text" || definition.Admission != AdmissionContentText {
			t.Errorf("LookupByPath(%q) = (%q, %v), want raw_text L1", admitted, definition.ParserKey, ok)
		}
	}
	for _, refused := range []string{
		"extensions.xml",
		"src/extensions.xml",
		"mvn/extensions.xml",
		".mvn/other.xml",
		".mvn/wrapper/extensions.xml",
		".mvn/extensions.xml.bak",
		// jvm.config and maven.config can carry -D credential properties
		// (trust-store passwords, gpg passphrases), so they stay out.
		".mvn/jvm.config",
		".mvn/maven.config",
	} {
		if definition, ok := registry.LookupByPath(refused); ok {
			t.Errorf("LookupByPath(%q) = %q, want not admitted", refused, definition.ParserKey)
		}
	}
}

func TestDefaultRegistryKeepsCredentialBearingConfigNamesUnadmitted(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, refused := range []string{
		".env",
		".env.local",
		"dir/.env",
		".npmrc",
		"dir/.npmrc",
		".pypirc",
		".netrc",
		"settings.xml",
		"dir/settings.xml",
		".mvn/settings.xml",
		// PHPUnit <php><env>/<server> children set environment and server
		// values, and Symfony and Laravel suites commonly place DATABASE_URL
		// there. The content store has no write-time redaction, so the names
		// stay out.
		"phpunit.xml",
		"phpunit.xml.dist",
		"mise.toml",
	} {
		if definition, ok := registry.LookupByPath(refused); ok {
			t.Errorf("LookupByPath(%q) = %q, want not admitted", refused, definition.ParserKey)
		}
	}
}

func TestUniversalConfigNamesDoNotAdmitTheirExtensions(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, refused := range []string{
		"other.ini",
		"other.toml",
		"other.xml",
		"other.properties",
		"other.json5",
		"Makefile.in",
		"Makefile.am",
		"gitignore",
		"mymakefile",
	} {
		if definition, ok := registry.LookupByPath(refused); ok {
			t.Errorf("LookupByPath(%q) = %q, want not admitted", refused, definition.ParserKey)
		}
	}
	for _, extension := range []string{".ini", ".toml", ".xml", ".properties", ".json5", ".config"} {
		if definition, ok := registry.LookupByExtension(extension); ok {
			t.Errorf("LookupByExtension(%q) = %q, want not admitted", extension, definition.ParserKey)
		}
	}
}

// TestUniversalConfigNamesLeaveExistingBindingsUnchanged pins names that an
// existing definition already claims. An exact-name entry would win over the
// extension rule and silently downgrade them to L1 raw text, so Taskfile.yml
// is deliberately not registered by name.
func TestUniversalConfigNamesLeaveExistingBindingsUnchanged(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	cases := map[string]string{
		"Taskfile.yml":     "yaml",
		"Taskfile.yaml":    "yaml",
		"taskfile.yml":     "yaml",
		"dir/Taskfile.yml": "yaml",
		".eslintrc.json":   "json",
		".prettierrc.json": "json",
		"renovate.json":    "json",
		".babelrc.js":      "javascript",
		"pom.xml":          "maven",
		"pyproject.toml":   "python_toml",
		"Cargo.toml":       "rust",
		"build.gradle":     "gradle",
		"setup.cfg":        "raw_text",
		"Dockerfile":       "__dockerfile__",
		"Jenkinsfile":      "__jenkinsfile__",
		"requirements.txt": "python_requirements",
		"Puppetfile":       "raw_text",
		"conf/routes":      "scala",
		"prod.tfvars.json": "hcl",
	}
	for candidate, want := range cases {
		definition, ok := registry.LookupByPath(candidate)
		if !ok || definition.ParserKey != want {
			t.Errorf("LookupByPath(%q) = (%q, %v), want %s", candidate, definition.ParserKey, ok, want)
		}
	}
}

func TestUniversalConfigNamesAreListedInAdmissionTable(t *testing.T) {
	t.Parallel()

	table := AdmissionTableMarkdown(DefaultRegistry())
	for _, name := range universalConfigNames {
		if !strings.Contains(table, "`"+name+"`") {
			t.Errorf("admission table does not list %q", name)
		}
	}
	if !strings.Contains(table, "`.mvn/extensions.xml`") {
		t.Error("admission table does not list the .mvn/extensions.xml path rule")
	}
}

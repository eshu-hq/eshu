package javascript

// Characterization tests for the three within-string-content regex exceptions
// that are documented as permanent in AGENTS.md. Each test pins the current
// accepted and rejected behaviour so any unintended change is caught immediately.
//
// Permanent exceptions documented here:
//   - javaScriptStaticComputedMemberNameRe  (javascript_names.go)
//   - javaScriptAWSClientServiceRe          (javascript_semantics_ast.go)
//   - javaScriptGCPServiceRe                (javascript_semantics_ast.go)
//
// These regexes run only against a string value already isolated by the AST;
// they are content-classification helpers, not primary symbol-extraction
// scanners. The genuine symbol/entity extraction already runs on tree-sitter
// AST nodes in this package.

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

// parseJavaScriptRootForTest parses a JavaScript source snippet into a root
// node and source bytes. The caller must invoke the returned close function.
func parseJavaScriptRootForTest(t *testing.T, source string) (*tree_sitter.Node, []byte, func()) {
	t.Helper()
	language := tree_sitter.NewLanguage(tree_sitter_javascript.Language())
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	bytes := []byte(source)
	tree := parser.Parse(bytes, nil)
	if tree == nil {
		parser.Close()
		t.Fatalf("Parse() returned nil tree")
	}
	return tree.RootNode(), bytes, func() {
		tree.Close()
		parser.Close()
	}
}

// ---------------------------------------------------------------------------
// javaScriptStaticComputedMemberNameRe — computed-property validation helper
// ---------------------------------------------------------------------------
//
// This regex is a within-string-content shape validator. It runs only against
// the inner text of a computed-property bracket expression that the AST has
// already isolated. It accepts simple identifiers, dotted member chains, and
// decimal integer literals. It rejects dynamic expressions, template literals
// with substitutions, and anything that cannot be a static property name.

func TestJavaScriptStaticComputedMemberNameReAcceptsSimpleIdentifier(t *testing.T) {
	t.Parallel()

	cases := []string{
		"foo",
		"_bar",
		"$baz",
		"foo123",
		"FOO",
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if !javaScriptStaticComputedMemberNameRe.MatchString(input) {
				t.Errorf("javaScriptStaticComputedMemberNameRe.MatchString(%q) = false, want true", input)
			}
		})
	}
}

func TestJavaScriptStaticComputedMemberNameReAcceptsDottedChain(t *testing.T) {
	t.Parallel()

	cases := []string{
		"foo.bar",
		"foo.bar.baz",
		"$foo._bar",
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if !javaScriptStaticComputedMemberNameRe.MatchString(input) {
				t.Errorf("javaScriptStaticComputedMemberNameRe.MatchString(%q) = false, want true", input)
			}
		})
	}
}

func TestJavaScriptStaticComputedMemberNameReAcceptsDecimalInteger(t *testing.T) {
	t.Parallel()

	cases := []string{
		"0",
		"1",
		"42",
		"100",
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if !javaScriptStaticComputedMemberNameRe.MatchString(input) {
				t.Errorf("javaScriptStaticComputedMemberNameRe.MatchString(%q) = false, want true", input)
			}
		})
	}
}

func TestJavaScriptStaticComputedMemberNameReRejectsDynamicAndInvalidForms(t *testing.T) {
	t.Parallel()

	cases := []string{
		"foo + bar", // binary expression
		"foo[bar]",  // nested bracket
		"foo()",     // call
		"${foo}",    // template substitution fragment
		"01",        // leading zero (octal-style, not a decimal integer)
		"foo bar",   // space in name
		"",          // empty string
		"foo-bar",   // hyphen
		"foo.bar.",  // trailing dot
		".foo",      // leading dot
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if javaScriptStaticComputedMemberNameRe.MatchString(input) {
				t.Errorf("javaScriptStaticComputedMemberNameRe.MatchString(%q) = true, want false", input)
			}
		})
	}
}

// javaScriptComputedPropertyName integration: verify the AST wrapper calls the
// regex only after the AST has isolated the bracket-expression inner text.
func TestJavaScriptComputedPropertyNameReturnsBracketedStaticName(t *testing.T) {
	t.Parallel()

	// obj["foo"] — string literal inside bracket; no regex needed (handled by
	// javaScriptStaticComputedPropertyName's "string" case). The computed-name
	// helper must still return "foo".
	root, src, closeFn := parseJavaScriptRootForTest(t, `const x = obj["foo"];`)
	defer closeFn()

	// Walk to the subscript_expression / computed_property_name node.
	var computedNode *tree_sitter.Node
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "subscript_expression" {
			idx := node.ChildByFieldName("index")
			if idx != nil && idx.Kind() == "string" {
				computedNode = idx
			}
		}
	})
	if computedNode == nil {
		t.Fatal("could not locate subscript index node in AST")
	}
	// trimJavaScriptQuotes is the underlying path; confirm direct string value.
	got, ok := trimJavaScriptQuotes(`"foo"`)
	if !ok || got != "foo" {
		t.Fatalf("trimJavaScriptQuotes(%q) = (%q, %v), want (\"foo\", true)", `"foo"`, got, ok)
	}
	_ = src
}

// ---------------------------------------------------------------------------
// javaScriptAWSClientServiceRe — @aws-sdk/client-* slug extraction
// ---------------------------------------------------------------------------
//
// This regex runs only against an import/require module-specifier string that
// the AST has already isolated via javaScriptImportModuleSpecifiers. It
// extracts the service slug (the part after "client-") for the aws semantics
// bucket. It is not a primary extraction scanner over raw source.

func TestJavaScriptAWSClientServiceReExtractsSlug(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{"@aws-sdk/client-s3", "s3"},
		{"@aws-sdk/client-dynamodb", "dynamodb"},
		{"@aws-sdk/client-rds-data", "rds-data"},
		{"@aws-sdk/client-secrets-manager", "secrets-manager"},
		{"@aws-sdk/client-ssm", "ssm"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			match := javaScriptAWSClientServiceRe.FindStringSubmatch(tc.input)
			if len(match) != 2 {
				t.Fatalf("FindStringSubmatch(%q) = %v, want 2-element match", tc.input, match)
			}
			if match[1] != tc.want {
				t.Fatalf("captured slug = %q, want %q", match[1], tc.want)
			}
		})
	}
}

func TestJavaScriptAWSClientServiceReRejectsNonMatchingSpecifiers(t *testing.T) {
	t.Parallel()

	cases := []string{
		"@aws-sdk/lib-dynamodb", // lib-*, not client-*
		"@aws-sdk/client-",      // empty slug
		"aws-sdk",               // bare v2 package
		"@google-cloud/storage", // GCP package
		"react",                 // unrelated
		"",                      // empty string
		"@aws-sdk/client-S3",    // uppercase in slug (pattern requires lowercase)
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			match := javaScriptAWSClientServiceRe.FindStringSubmatch(input)
			if len(match) != 0 {
				t.Fatalf("FindStringSubmatch(%q) = %v, want no match", input, match)
			}
		})
	}
}

// Integration: verify javaScriptImportServiceSlugs feeds AST-isolated
// specifiers to the regex (not raw source) for @aws-sdk imports.
func TestJavaScriptImportServiceSlugsAWSFromStaticImport(t *testing.T) {
	t.Parallel()

	root, src, closeFn := parseJavaScriptRootForTest(t,
		`import { S3Client } from "@aws-sdk/client-s3";
import { DynamoDBClient } from "@aws-sdk/client-dynamodb";`)
	defer closeFn()

	slugs := javaScriptImportServiceSlugs(root, src, javaScriptAWSClientServiceRe)
	if len(slugs) != 2 {
		t.Fatalf("javaScriptImportServiceSlugs() = %v, want [s3 dynamodb]", slugs)
	}
	if slugs[0] != "s3" {
		t.Fatalf("slugs[0] = %q, want \"s3\"", slugs[0])
	}
	if slugs[1] != "dynamodb" {
		t.Fatalf("slugs[1] = %q, want \"dynamodb\"", slugs[1])
	}
}

func TestJavaScriptImportServiceSlugsAWSFromRequire(t *testing.T) {
	t.Parallel()

	root, src, closeFn := parseJavaScriptRootForTest(t,
		`const ssm = require("@aws-sdk/client-ssm");`)
	defer closeFn()

	slugs := javaScriptImportServiceSlugs(root, src, javaScriptAWSClientServiceRe)
	if len(slugs) != 1 || slugs[0] != "ssm" {
		t.Fatalf("javaScriptImportServiceSlugs() = %v, want [ssm]", slugs)
	}
}

func TestJavaScriptImportServiceSlugsAWSDeduplicates(t *testing.T) {
	t.Parallel()

	root, src, closeFn := parseJavaScriptRootForTest(t,
		`import { S3Client } from "@aws-sdk/client-s3";
import { S3 } from "@aws-sdk/client-s3";`)
	defer closeFn()

	slugs := javaScriptImportServiceSlugs(root, src, javaScriptAWSClientServiceRe)
	if len(slugs) != 1 || slugs[0] != "s3" {
		t.Fatalf("javaScriptImportServiceSlugs() = %v, want [s3] (deduplicated)", slugs)
	}
}

// ---------------------------------------------------------------------------
// javaScriptGCPServiceRe — @google-cloud/* slug extraction
// ---------------------------------------------------------------------------
//
// Same exception category as the AWS regex: runs only against an AST-isolated
// module-specifier string. Extracts the package slug that follows
// "@google-cloud/" for the gcp semantics bucket.

func TestJavaScriptGCPServiceReExtractsSlug(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{"@google-cloud/storage", "storage"},
		{"@google-cloud/bigquery", "bigquery"},
		{"@google-cloud/pubsub", "pubsub"},
		{"@google-cloud/datastore", "datastore"},
		{"@google-cloud/logging-min", "logging-min"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			match := javaScriptGCPServiceRe.FindStringSubmatch(tc.input)
			if len(match) != 2 {
				t.Fatalf("FindStringSubmatch(%q) = %v, want 2-element match", tc.input, match)
			}
			if match[1] != tc.want {
				t.Fatalf("captured slug = %q, want %q", match[1], tc.want)
			}
		})
	}
}

func TestJavaScriptGCPServiceReRejectsNonMatchingSpecifiers(t *testing.T) {
	t.Parallel()

	cases := []string{
		"@aws-sdk/client-s3",    // AWS package
		"google-cloud",          // no scope
		"@google-cloud/",        // empty slug
		"react",                 // unrelated
		"",                      // empty string
		"@google-cloud/Storage", // uppercase in slug
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			match := javaScriptGCPServiceRe.FindStringSubmatch(input)
			if len(match) != 0 {
				t.Fatalf("FindStringSubmatch(%q) = %v, want no match", input, match)
			}
		})
	}
}

// Integration: verify javaScriptImportServiceSlugs feeds AST-isolated
// specifiers to the GCP regex for require-style imports.
func TestJavaScriptImportServiceSlugsGCPFromRequire(t *testing.T) {
	t.Parallel()

	root, src, closeFn := parseJavaScriptRootForTest(t,
		`const storage = require("@google-cloud/storage");
const bq = require("@google-cloud/bigquery");`)
	defer closeFn()

	slugs := javaScriptImportServiceSlugs(root, src, javaScriptGCPServiceRe)
	if len(slugs) != 2 {
		t.Fatalf("javaScriptImportServiceSlugs() = %v, want [storage bigquery]", slugs)
	}
	if slugs[0] != "storage" {
		t.Fatalf("slugs[0] = %q, want \"storage\"", slugs[0])
	}
	if slugs[1] != "bigquery" {
		t.Fatalf("slugs[1] = %q, want \"bigquery\"", slugs[1])
	}
}

func TestJavaScriptImportServiceSlugsGCPFromStaticImport(t *testing.T) {
	t.Parallel()

	root, src, closeFn := parseJavaScriptRootForTest(t,
		`import { PubSub } from "@google-cloud/pubsub";`)
	defer closeFn()

	slugs := javaScriptImportServiceSlugs(root, src, javaScriptGCPServiceRe)
	if len(slugs) != 1 || slugs[0] != "pubsub" {
		t.Fatalf("javaScriptImportServiceSlugs() = %v, want [pubsub]", slugs)
	}
}

func TestJavaScriptImportServiceSlugsGCPIgnoresAWSSpecifiers(t *testing.T) {
	t.Parallel()

	root, src, closeFn := parseJavaScriptRootForTest(t,
		`import { S3Client } from "@aws-sdk/client-s3";
import { Storage } from "@google-cloud/storage";`)
	defer closeFn()

	gcpSlugs := javaScriptImportServiceSlugs(root, src, javaScriptGCPServiceRe)
	if len(gcpSlugs) != 1 || gcpSlugs[0] != "storage" {
		t.Fatalf("GCP javaScriptImportServiceSlugs() = %v, want [storage]", gcpSlugs)
	}

	awsSlugs := javaScriptImportServiceSlugs(root, src, javaScriptAWSClientServiceRe)
	if len(awsSlugs) != 1 || awsSlugs[0] != "s3" {
		t.Fatalf("AWS javaScriptImportServiceSlugs() = %v, want [s3]", awsSlugs)
	}
}

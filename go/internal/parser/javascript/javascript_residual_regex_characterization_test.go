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

// firstComputedPropertyNameNode walks the AST and returns the first
// computed_property_name node, the node kind that the production wrapper
// javaScriptComputedPropertyName is actually called for. Bracketed class-method
// and object-literal keys (`["foo"]() {}`, `{ ["bar"]: 1 }`) produce this node;
// a subscript read (`obj["foo"]`) does not, so the test must build one of the
// former so it exercises the documented wrapper path.
func firstComputedPropertyNameNode(root *tree_sitter.Node) *tree_sitter.Node {
	var found *tree_sitter.Node
	walkNamed(root, func(node *tree_sitter.Node) {
		if found == nil && node.Kind() == "computed_property_name" {
			found = node
		}
	})
	return found
}

// TestJavaScriptComputedPropertyNameWrapperOverRealNode pins the production
// helper javaScriptComputedPropertyName against real computed_property_name
// nodes. This covers the documented wrapper path end to end: the static
// string/number cases resolved by javaScriptStaticComputedPropertyName, the
// dotted-member-chain case validated by javaScriptStaticComputedMemberNameRe,
// and the dynamic cases the helper must reject. A regression in
// javaScriptComputedPropertyName (including the residual regex) fails this test.
func TestJavaScriptComputedPropertyNameWrapperOverRealNode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		want   string
	}{
		// String-literal key — resolved by the "string" case before the regex.
		{"class method string key", `class C { ["foo"]() {} }`, "foo"},
		{"object literal string key", `const o = { ["bar"]: 1 };`, "bar"},
		// Static binary concatenation — resolved by the "binary_expression" case.
		{"object literal concat key", `const o = { ["a" + "b"]: 1 };`, "ab"},
		// Numeric-literal key — resolved by the "number" case.
		{"object literal numeric key", `const o = { [42]: 1 };`, "42"},
		// Dotted member chain — the inner text is NOT resolved by
		// javaScriptStaticComputedPropertyName, so the wrapper falls through to
		// javaScriptStaticComputedMemberNameRe, which accepts the chain. This is
		// the branch that exercises the residual regex.
		{"class method dotted member key", `class C { [Symbol.iterator]() {} }`, "Symbol.iterator"},
		// Dynamic call — rejected by both the static resolver and the regex.
		{"class method dynamic call key", `class C { [getName()]() {} }`, ""},
		// Template substitution — rejected.
		{"object literal template key", "const o = { [`x${y}`]: 1 };", ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, src, closeFn := parseJavaScriptRootForTest(t, tc.source)
			defer closeFn()

			node := firstComputedPropertyNameNode(root)
			if node == nil {
				t.Fatalf("no computed_property_name node found in %q", tc.source)
			}
			if got := javaScriptComputedPropertyName(node, src); got != tc.want {
				t.Fatalf("javaScriptComputedPropertyName() = %q, want %q (source %q)", got, tc.want, tc.source)
			}
		})
	}
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

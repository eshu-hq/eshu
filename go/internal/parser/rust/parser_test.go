package rust

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

func TestParseCapturesRustMaturityContract(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "lib.rs", `use std::{fmt::Display, collections::HashMap};
use crate::domain::Thing as DomainThing;

struct Holder<'a> {
    value: &'a str,
}

enum Status {
    Ready,
}

trait Render<'a> {
    fn render(&self, input: &'a str) -> &'a str;
}

impl<'a> Render<'a> for Holder<'a> where Holder<'a>: Display {
    fn render(&self, input: &'a str) -> &'a str {
        println!("{}", input);
        self.value.trim();
        helper(input);
        Holder::new(input)
    }
}

impl<'a> Holder<'a> {
    fn new(value: &'a str) -> &'a str {
        value
    }
}

fn helper<'a>(input: &'a str) -> &'a str {
    input
}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{IndexSource: true}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertRustBucketName(t, payload, "classes", "Holder")
	assertRustBucketName(t, payload, "classes", "Status")
	assertRustBucketName(t, payload, "traits", "Render")

	assertRustImport(t, payload, "std::{fmt::Display, collections::HashMap}", "", "use")
	assertRustImport(t, payload, "crate::domain::Thing", "DomainThing", "alias")

	renderImpl := assertRustBucketName(t, payload, "impl_blocks", "Holder")
	assertRustStringField(t, renderImpl, "kind", "trait_impl")
	assertRustStringField(t, renderImpl, "trait", "Render")
	assertRustStringField(t, renderImpl, "target", "Holder<'a>")
	assertRustStringSliceField(t, renderImpl, "lifetime_parameters", []string{"a"})
	assertRustStringSliceField(t, renderImpl, "signature_lifetimes", []string{"a"})

	newImpl := assertRustBucketField(t, payload, "impl_blocks", "kind", "inherent_impl")
	assertRustStringField(t, newImpl, "name", "Holder")
	assertRustStringField(t, newImpl, "target", "Holder<'a>")

	renderSignature := assertRustBucketName(t, payload, "functions", "render")
	assertRustStringSliceField(t, renderSignature, "signature_lifetimes", []string{"a"})
	assertRustStringField(t, renderSignature, "return_lifetime", "a")

	render := assertRustBucketFields(t, payload, "functions", map[string]string{
		"name":         "render",
		"impl_context": "Holder",
	})
	assertRustStringField(t, render, "impl_context", "Holder")
	assertRustStringSliceField(t, render, "signature_lifetimes", []string{"a"})
	assertRustStringField(t, render, "return_lifetime", "a")
	if render["source"] == "" {
		t.Fatalf("functions[render][source] = %#v, want indexed source", render["source"])
	}

	newFunction := assertRustBucketName(t, payload, "functions", "new")
	assertRustStringField(t, newFunction, "impl_context", "Holder")
	assertRustStringSliceField(t, newFunction, "signature_lifetimes", []string{"a"})

	helper := assertRustBucketName(t, payload, "functions", "helper")
	assertRustStringSliceField(t, helper, "lifetime_parameters", []string{"a"})
	assertRustStringSliceField(t, helper, "signature_lifetimes", []string{"a"})
	assertRustStringField(t, helper, "return_lifetime", "a")

	assertRustCall(t, payload, "println", "println")
	assertRustCall(t, payload, "trim", "self.value.trim")
	assertRustCall(t, payload, "helper", "helper")
	assertRustCall(t, payload, "new", "Holder::new")
}

func TestPreScanCapturesRustSymbols(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "symbols.rs", `struct Widget;
trait Drawable {}
impl Widget {
    fn draw(&self) {}
}
`)
	parser := newRustParser(t)

	got, err := PreScan(path, parser)
	if err != nil {
		t.Fatalf("PreScan() error = %v, want nil", err)
	}

	want := []string{"draw", "Widget", "Drawable", "Widget"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PreScan() = %#v, want %#v", got, want)
	}
}

func newRustParser(t *testing.T) *tree_sitter.Parser {
	t.Helper()

	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_rust.Language())); err != nil {
		t.Fatalf("SetLanguage(rust) error = %v, want nil", err)
	}
	return parser
}

func writeRustSource(t *testing.T, name string, source string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}
	return path
}

func assertRustBucketName(t *testing.T, payload map[string]any, bucket string, name string) map[string]any {
	t.Helper()

	return assertRustBucketField(t, payload, bucket, "name", name)
}

func assertRustBucketField(t *testing.T, payload map[string]any, bucket string, field string, value string) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		if item[field] == value {
			return item
		}
	}
	t.Fatalf("payload[%q] missing %s=%q in %#v", bucket, field, value, items)
	return nil
}

func assertRustBucketFields(t *testing.T, payload map[string]any, bucket string, fields map[string]string) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		matches := true
		for field, value := range fields {
			if item[field] != value {
				matches = false
				break
			}
		}
		if matches {
			return item
		}
	}
	t.Fatalf("payload[%q] missing fields %#v in %#v", bucket, fields, items)
	return nil
}

func assertRustStringField(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	if got := item[field]; got != want {
		t.Fatalf("item[%q] = %#v, want %#v in %#v", field, got, want, item)
	}
}

func assertRustStringSliceField(t *testing.T, item map[string]any, field string, want []string) {
	t.Helper()

	if got := item[field]; !reflect.DeepEqual(got, want) {
		t.Fatalf("item[%q] = %#v, want %#v in %#v", field, got, want, item)
	}
}

func assertRustImport(t *testing.T, payload map[string]any, name string, alias string, importType string) {
	t.Helper()

	item := assertRustBucketName(t, payload, "imports", name)
	assertRustStringField(t, item, "source", name)
	assertRustStringField(t, item, "alias", alias)
	assertRustStringField(t, item, "import_type", importType)
}

func assertRustCall(t *testing.T, payload map[string]any, name string, fullName string) {
	t.Helper()

	item := assertRustBucketName(t, payload, "function_calls", name)
	assertRustStringField(t, item, "full_name", fullName)
}

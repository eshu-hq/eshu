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

unsafe trait Paint {
    fn paint(&self);
}

impl<'a> Render<'a> for Holder<'a> where Holder<'a>: Display {
    fn render(&self, input: &'a str) -> &'a str {
        println!("{}", input);
        self.value.trim();
        helper(input);
        Holder::new(input)
    }
}

unsafe impl<'a> Paint for Holder<'a> {
    fn paint(&self) {}
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

	assertRustImport(t, payload, "std::fmt::Display", "Display", "use")
	assertRustImport(t, payload, "std::collections::HashMap", "HashMap", "use")
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
	assertRustStringField(t, render, "impl_kind", "trait_impl")
	assertRustStringField(t, render, "trait_context", "Render")
	assertRustStringSliceContains(t, render, "dead_code_root_kinds", "rust.trait_impl_method")
	assertRustStringSliceField(t, render, "signature_lifetimes", []string{"a"})
	assertRustStringField(t, render, "return_lifetime", "a")
	if render["source"] == "" {
		t.Fatalf("functions[render][source] = %#v, want indexed source", render["source"])
	}

	paint := assertRustBucketFields(t, payload, "functions", map[string]string{
		"name":         "paint",
		"impl_context": "Holder",
	})
	assertRustStringField(t, paint, "impl_context", "Holder")
	assertRustStringField(t, paint, "impl_kind", "trait_impl")
	assertRustStringField(t, paint, "trait_context", "Paint")
	assertRustStringSliceContains(t, paint, "dead_code_root_kinds", "rust.trait_impl_method")

	newFunction := assertRustBucketName(t, payload, "functions", "new")
	assertRustStringField(t, newFunction, "impl_context", "Holder")
	assertRustStringField(t, newFunction, "impl_kind", "inherent_impl")
	assertRustNoField(t, newFunction, "trait_context")
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
mod net;
impl Widget {
    fn draw(&self) {}
}
`)
	parser := newRustParser(t)

	got, err := PreScan(path, parser)
	if err != nil {
		t.Fatalf("PreScan() error = %v, want nil", err)
	}

	want := []string{"draw", "Widget", "Drawable", "Widget", "net"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PreScan() = %#v, want %#v", got, want)
	}
}

func TestParseCapturesRustDogfoodMaturityPatterns(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/main.rs", `#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    run().await
}

type Tx = tokio::sync::mpsc::UnboundedSender<String>;
pub(crate) const DEFAULT_ADDR: &str = "127.0.0.1:6142";
static VTABLE: RawWakerVTable = RawWakerVTable::new(clone, wake, wake_by_ref, drop_waker);

macro_rules! assert_ready {
    ($e:expr) => { $e };
}

#[tokio::test]
async fn async_smoke() {
    run().await;
}

#[test]
fn sync_smoke() {}

pub unsafe fn from_raw(raw: *const ()) -> Arc<ThreadWaker> {
    Arc::from_raw(raw as *const ThreadWaker)
}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{IndexSource: true}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	mainFn := assertRustBucketName(t, payload, "functions", "main")
	assertRustBoolField(t, mainFn, "async", true)
	assertRustStringSliceContains(t, mainFn, "dead_code_root_kinds", "rust.tokio_main")
	assertRustStringSliceContains(t, mainFn, "dead_code_root_kinds", "rust.main_function")

	asyncSmoke := assertRustBucketName(t, payload, "functions", "async_smoke")
	assertRustBoolField(t, asyncSmoke, "async", true)
	assertRustStringSliceContains(t, asyncSmoke, "dead_code_root_kinds", "rust.tokio_test")

	syncSmoke := assertRustBucketName(t, payload, "functions", "sync_smoke")
	assertRustStringSliceContains(t, syncSmoke, "dead_code_root_kinds", "rust.test_function")

	fromRaw := assertRustBucketName(t, payload, "functions", "from_raw")
	assertRustBoolField(t, fromRaw, "unsafe", true)
	assertRustStringField(t, fromRaw, "visibility", "pub")

	tx := assertRustBucketName(t, payload, "type_aliases", "Tx")
	assertRustStringField(t, tx, "lang", "rust")

	defaultAddr := assertRustBucketName(t, payload, "variables", "DEFAULT_ADDR")
	assertRustStringField(t, defaultAddr, "variable_kind", "const")
	assertRustStringField(t, defaultAddr, "visibility", "pub(crate)")

	vtable := assertRustBucketName(t, payload, "variables", "VTABLE")
	assertRustStringField(t, vtable, "variable_kind", "static")

	assertRustBucketName(t, payload, "macros", "assert_ready")
}

func TestParseDoesNotMarkLibraryMainFunctionAsRoot(t *testing.T) {
	t.Parallel()

	for _, pathName := range []string{"src/lib.rs", "fixtures/main.rs"} {
		pathName := pathName
		t.Run(pathName, func(t *testing.T) {
			t.Parallel()

			path := writeRustSource(t, pathName, `fn main() {}
`)
			parser := newRustParser(t)

			payload, err := Parse(path, false, shared.Options{}, parser)
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}

			mainFn := assertRustBucketName(t, payload, "functions", "main")
			if _, ok := mainFn["dead_code_root_kinds"]; ok {
				t.Fatalf("library main dead_code_root_kinds = %#v, want absent", mainFn["dead_code_root_kinds"])
			}
		})
	}
}

func TestParseCapturesSameLineRustAttributes(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `#[cfg_attr(test, allow(dead_code))] #[test] fn inline_smoke() {}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	smoke := assertRustBucketName(t, payload, "functions", "inline_smoke")
	assertRustStringSliceContains(t, smoke, "decorators", "#[cfg_attr(test, allow(dead_code))]")
	assertRustStringSliceContains(t, smoke, "decorators", "#[test]")
	assertRustStringSliceContains(t, smoke, "dead_code_root_kinds", "rust.test_function")
}

func TestParseCapturesRustModuleAndImportMaturityPatterns(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `use crate::{self, config::Config as RuntimeConfig, prelude::*, service::{Client, Server}};
use tokio_stream::{self as stream, StreamExt};

pub mod api;
pub(crate) mod worker {
    pub fn run() {}
}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertRustImport(t, payload, "crate", "", "self")
	assertRustImport(t, payload, "crate::config::Config", "RuntimeConfig", "alias")
	assertRustImport(t, payload, "crate::prelude::*", "", "glob")
	assertRustImport(t, payload, "crate::service::Client", "Client", "use")
	assertRustImport(t, payload, "crate::service::Server", "Server", "use")
	assertRustImport(t, payload, "tokio_stream", "stream", "alias")
	assertRustImport(t, payload, "tokio_stream::StreamExt", "StreamExt", "use")

	api := assertRustBucketName(t, payload, "modules", "api")
	assertRustStringField(t, api, "module_kind", "declaration")
	assertRustStringField(t, api, "visibility", "pub")

	worker := assertRustBucketName(t, payload, "modules", "worker")
	assertRustStringField(t, worker, "module_kind", "inline")
	assertRustStringField(t, worker, "visibility", "pub(crate)")
}

func TestParseCapturesRustAttributesAndGenericParameters(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `#[derive(Debug, Clone)]
#[cfg_attr(test, derive(Default))]
pub struct Holder<'a, T, const N: usize> {
    value: &'a T,
}

#[derive(thiserror::Error)]
pub enum AppError<T> {
    Missing(T),
}

#[async_trait::async_trait]
pub trait Render<'a, T> {
    fn render<E>(&self, input: &'a T) -> Result<T, E>;
}

type Cache<'a, T> = &'a [T];
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	holder := assertRustBucketName(t, payload, "classes", "Holder")
	assertRustStringField(t, holder, "visibility", "pub")
	assertRustStringSliceField(t, holder, "lifetime_parameters", []string{"a"})
	assertRustStringSliceField(t, holder, "type_parameters", []string{"T"})
	assertRustStringSliceField(t, holder, "const_parameters", []string{"N"})
	assertRustStringSliceContains(t, holder, "decorators", "#[derive(Debug, Clone)]")
	assertRustStringSliceContains(t, holder, "attribute_paths", "cfg_attr")
	assertRustStringSliceField(t, holder, "derives", []string{"Debug", "Clone"})

	appError := assertRustBucketName(t, payload, "classes", "AppError")
	assertRustStringSliceField(t, appError, "type_parameters", []string{"T"})
	assertRustStringSliceContains(t, appError, "derives", "thiserror::Error")

	render := assertRustBucketName(t, payload, "traits", "Render")
	assertRustStringSliceField(t, render, "lifetime_parameters", []string{"a"})
	assertRustStringSliceField(t, render, "type_parameters", []string{"T"})
	assertRustStringSliceContains(t, render, "attribute_paths", "async_trait::async_trait")

	renderFn := assertRustBucketName(t, payload, "functions", "render")
	assertRustStringSliceField(t, renderFn, "type_parameters", []string{"E"})

	cache := assertRustBucketName(t, payload, "type_aliases", "Cache")
	assertRustStringSliceField(t, cache, "lifetime_parameters", []string{"a"})
	assertRustStringSliceField(t, cache, "type_parameters", []string{"T"})
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
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

func assertRustStringSliceContains(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("item[%q] = %#v, want []string containing %q in %#v", field, item[field], want, item)
	}
	for _, value := range got {
		if value == want {
			return
		}
	}
	t.Fatalf("item[%q] = %#v, want to contain %q in %#v", field, got, want, item)
}

func assertRustBoolField(t *testing.T, item map[string]any, field string, want bool) {
	t.Helper()

	if got := item[field]; got != want {
		t.Fatalf("item[%q] = %#v, want %#v in %#v", field, got, want, item)
	}
}

func assertRustNoField(t *testing.T, item map[string]any, field string) {
	t.Helper()

	if got, ok := item[field]; ok {
		t.Fatalf("item[%q] = %#v, want absent in %#v", field, got, item)
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

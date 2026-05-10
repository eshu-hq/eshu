package rust

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseMarksExactPubRustItemsAsPublicAPIRoots(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `pub fn exported() {}
pub(crate) fn crate_only() {}

pub struct PublicStruct;
pub(super) struct SuperStruct;

pub trait PublicTrait {}
pub(in crate::internal) trait ScopedTrait {}

pub type PublicAlias = String;
type PrivateAlias = String;
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertRustStringSliceContains(t, assertRustBucketName(t, payload, "functions", "exported"), "dead_code_root_kinds", "rust.public_api_item")
	assertRustNoField(t, assertRustBucketName(t, payload, "functions", "crate_only"), "dead_code_root_kinds")
	assertRustStringSliceContains(t, assertRustBucketName(t, payload, "classes", "PublicStruct"), "dead_code_root_kinds", "rust.public_api_item")
	assertRustNoField(t, assertRustBucketName(t, payload, "classes", "SuperStruct"), "dead_code_root_kinds")
	assertRustStringSliceContains(t, assertRustBucketName(t, payload, "traits", "PublicTrait"), "dead_code_root_kinds", "rust.public_api_item")
	assertRustNoField(t, assertRustBucketName(t, payload, "traits", "ScopedTrait"), "dead_code_root_kinds")
	assertRustStringSliceContains(t, assertRustBucketName(t, payload, "type_aliases", "PublicAlias"), "dead_code_root_kinds", "rust.public_api_item")
	assertRustNoField(t, assertRustBucketName(t, payload, "type_aliases", "PrivateAlias"), "dead_code_root_kinds")
}

func TestParseMarksRustBenchmarkRootsConservatively(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "benches/parser.rs", `use criterion::{criterion_group, Criterion};

fn bench_parse(c: &mut Criterion) {
    c.bench_function("parse", |b| b.iter(|| parse()));
}

fn bench_configured(c: &mut Criterion) {}

fn helper() {}

criterion_group!(benches, bench_parse);
criterion_group! {
    name = configured;
    config = Criterion::default();
    targets = bench_configured
}

#[bench]
fn attr_bench(b: &mut test::Bencher) {
    b.iter(|| parse());
}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertRustStringSliceContains(t, assertRustBucketName(t, payload, "functions", "bench_parse"), "dead_code_root_kinds", "rust.benchmark_function")
	assertRustStringSliceContains(t, assertRustBucketName(t, payload, "functions", "bench_configured"), "dead_code_root_kinds", "rust.benchmark_function")
	assertRustStringSliceContains(t, assertRustBucketName(t, payload, "functions", "attr_bench"), "dead_code_root_kinds", "rust.benchmark_function")
	assertRustNoField(t, assertRustBucketName(t, payload, "functions", "helper"), "dead_code_root_kinds")
}

func TestParseCapturesRustModuleDeclarationPathCandidates(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `pub mod api;
pub(crate) mod worker {
    pub fn run() {}
}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	api := assertRustBucketName(t, payload, "modules", "api")
	assertRustStringSliceField(t, api, "declared_path_candidates", []string{"api.rs", "api/mod.rs"})

	worker := assertRustBucketName(t, payload, "modules", "worker")
	assertRustNoField(t, worker, "declared_path_candidates")
}

func TestParsePreservesRustPubUseImportVisibility(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `pub use crate::api::Thing as PublicThing;
use crate::internal::Helper;
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	publicThing := assertRustBucketName(t, payload, "imports", "crate::api::Thing")
	assertRustStringField(t, publicThing, "alias", "PublicThing")
	assertRustStringField(t, publicThing, "visibility", "pub")

	helper := assertRustBucketName(t, payload, "imports", "crate::internal::Helper")
	assertRustNoField(t, helper, "visibility")
}

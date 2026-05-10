package rust

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseCapturesMultilineRustAttributes(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `#[cfg_attr(
    test,
    derive(Default),
)]
#[derive(Debug)]
pub struct Holder;
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	holder := assertRustBucketName(t, payload, "classes", "Holder")
	assertRustStringSliceContains(t, holder, "decorators", "#[derive(Debug)]")
	assertRustStringSliceContains(t, holder, "attribute_paths", "cfg_attr")
	assertRustStringSliceContains(t, holder, "attribute_paths", "derive")
	assertRustStringSliceField(t, holder, "derives", []string{"Debug"})
}

func TestParseDoesNotDuplicateRustItemAttributes(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `#[cfg(feature = "serde")]
mod util;
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	util := assertRustBucketName(t, payload, "modules", "util")
	assertRustStringSliceField(t, util, "decorators", []string{`#[cfg(feature = "serde")]`})
	assertRustStringSliceField(t, util, "attribute_paths", []string{"cfg"})
}

func TestParseTrimsMultilineImplWhereClauseFromTarget(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `struct Compat<T>(T);
trait AsyncRead {}

impl<T> AsyncRead for Compat<T>
where
    T: AsyncRead,
{}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	implBlock := assertRustBucketField(t, payload, "impl_blocks", "kind", "trait_impl")
	assertRustStringField(t, implBlock, "trait", "AsyncRead")
	assertRustStringField(t, implBlock, "name", "Compat")
	assertRustStringField(t, implBlock, "target", "Compat<T>")
	assertRustStringSliceField(t, implBlock, "type_parameters", []string{"T"})
}

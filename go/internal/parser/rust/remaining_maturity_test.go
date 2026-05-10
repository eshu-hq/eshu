package rust

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseCapturesRustConditionalDerivesAndNestedAttributes(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `#[cfg_attr(feature = "serde", derive(Serialize, Deserialize))]
pub struct Holder {
    #[serde(default)]
    value: String,
    #[cfg_attr(feature = "secret", allow(dead_code))]
    hidden: usize,
}

pub enum State {
    #[serde(rename = "ready")]
    Ready,
    #[cfg_attr(feature = "legacy", deprecated)]
    Legacy(String),
}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	holder := assertRustBucketName(t, payload, "classes", "Holder")
	assertRustStringSliceField(t, holder, "conditional_derives", []string{"Serialize", "Deserialize"})

	value := assertRustBucketName(t, payload, "annotations", "Holder.value")
	assertRustStringField(t, value, "owner", "Holder")
	assertRustStringField(t, value, "target_kind", "field")
	assertRustStringSliceField(t, value, "attribute_paths", []string{"serde"})

	hidden := assertRustBucketName(t, payload, "annotations", "Holder.hidden")
	assertRustStringField(t, hidden, "owner", "Holder")
	assertRustStringField(t, hidden, "target_kind", "field")
	assertRustStringSliceField(t, hidden, "attribute_paths", []string{"cfg_attr"})

	ready := assertRustBucketName(t, payload, "annotations", "State.Ready")
	assertRustStringField(t, ready, "owner", "State")
	assertRustStringField(t, ready, "target_kind", "enum_variant")
	assertRustStringSliceField(t, ready, "attribute_paths", []string{"serde"})

	legacy := assertRustBucketName(t, payload, "annotations", "State.Legacy")
	assertRustStringField(t, legacy, "owner", "State")
	assertRustStringField(t, legacy, "target_kind", "enum_variant")
	assertRustStringSliceField(t, legacy, "attribute_paths", []string{"cfg_attr"})
}

func TestParseCapturesRustWhereClauseSemantics(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `pub trait Store<T>
where
    T: Clone + Send,
    for<'a> &'a T: IntoIterator<Item = &'a str>,
    T::Error: std::error::Error,
{
    fn load<E>(&self) -> Result<T, E>
    where
        E: From<T::Error> + Send,
        for<'b> &'b E: Into<String>;
}

impl<T> Store<T> for Cache<T>
where
    T: Clone,
    T::Error: std::fmt::Debug,
{}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	store := assertRustBucketName(t, payload, "traits", "Store")
	assertRustStringSliceContains(t, store, "where_predicates", "T: Clone + Send")
	assertRustStringSliceContains(t, store, "higher_ranked_trait_bounds", "for<'a> &'a T: IntoIterator<Item = &'a str>")
	assertRustStringSliceContains(t, store, "associated_type_constraints", "T::Error: std::error::Error")

	load := assertRustBucketName(t, payload, "functions", "load")
	assertRustStringSliceContains(t, load, "where_predicates", "E: From<T::Error> + Send")
	assertRustStringSliceContains(t, load, "higher_ranked_trait_bounds", "for<'b> &'b E: Into<String>")

	implBlock := assertRustBucketField(t, payload, "impl_blocks", "kind", "trait_impl")
	assertRustStringSliceContains(t, implBlock, "where_predicates", "T: Clone")
	assertRustStringSliceContains(t, implBlock, "associated_type_constraints", "T::Error: std::fmt::Debug")
}

func TestParseCapturesRustPathAttributeModuleAndMacroDeclarations(t *testing.T) {
	t.Parallel()

	path := writeRustSource(t, "src/lib.rs", `#[path = "platform/unix.rs"]
mod os;

cfg_if::cfg_if! {
    if #[cfg(unix)] {
        mod unix;
        use crate::os::UnixHandle;
    } else {
        mod fallback;
        use crate::os::FallbackHandle as Handle;
    }
}
`)
	parser := newRustParser(t)

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	osModule := assertRustBucketName(t, payload, "modules", "os")
	assertRustStringSliceField(t, osModule, "declared_path_candidates", []string{"platform/unix.rs"})
	assertRustStringField(t, osModule, "module_path_source", "path_attribute")

	unixModule := assertRustBucketName(t, payload, "modules", "unix")
	assertRustStringField(t, unixModule, "module_origin", "macro_invocation")
	assertRustStringSliceField(t, unixModule, "declared_path_candidates", []string{"unix.rs", "unix/mod.rs"})

	fallbackModule := assertRustBucketName(t, payload, "modules", "fallback")
	assertRustStringField(t, fallbackModule, "module_origin", "macro_invocation")

	unixImport := assertRustBucketName(t, payload, "imports", "crate::os::UnixHandle")
	assertRustStringField(t, unixImport, "import_origin", "macro_invocation")

	fallbackImport := assertRustBucketName(t, payload, "imports", "crate::os::FallbackHandle")
	assertRustStringField(t, fallbackImport, "alias", "Handle")
	assertRustStringField(t, fallbackImport, "import_origin", "macro_invocation")
}

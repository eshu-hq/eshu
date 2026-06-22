package rust

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestRustLifetimeASTParity locks the lifetime payload fields that the
// tree-sitter AST extraction must reproduce at byte-parity with the prior
// regex behavior. Names are emitted without the leading apostrophe, first-seen
// order is preserved, and duplicates collapse.
func TestRustLifetimeASTParity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name               string
		fnName             string
		source             string
		wantLifetimeParams []string
		wantSignature      []string
		wantReturn         string
	}{
		{
			name:               "single lifetime function",
			fnName:             "helper",
			source:             "fn helper<'a>(input: &'a str) -> &'a str { input }\n",
			wantLifetimeParams: []string{"a"},
			wantSignature:      []string{"a"},
			wantReturn:         "a",
		},
		{
			name:               "multiple distinct lifetimes order preserved",
			fnName:             "pair",
			source:             "fn pair<'a, 'b>(x: &'a str, y: &'b str) -> &'a str { x }\n",
			wantLifetimeParams: []string{"a", "b"},
			wantSignature:      []string{"a", "b"},
			wantReturn:         "a",
		},
		{
			name:               "bounded lifetime parameter",
			fnName:             "bounded",
			source:             "fn bounded<'a, 'b: 'a>(x: &'a str, y: &'b str) -> &'b str { y }\n",
			wantLifetimeParams: []string{"a", "b"},
			wantSignature:      []string{"a", "b"},
			wantReturn:         "b",
		},
		{
			name:               "static lifetime in return is captured",
			fnName:             "konst",
			source:             "fn konst() -> &'static str { \"x\" }\n",
			wantLifetimeParams: nil,
			wantSignature:      []string{"static"},
			wantReturn:         "static",
		},
		{
			name:               "where clause lifetime included in signature",
			fnName:             "wc",
			source:             "fn wc<'a, T>(x: &'a T) -> &'a T where T: Clone + 'a { x }\n",
			wantLifetimeParams: []string{"a"},
			wantSignature:      []string{"a"},
			wantReturn:         "a",
		},
		{
			name:               "multiline generic header",
			fnName:             "longname",
			source:             "fn longname<\n    'a,\n    'b,\n    T,\n>(x: &'a T) -> &'b str {\n    \"\"\n}\n",
			wantLifetimeParams: []string{"a", "b"},
			wantSignature:      []string{"a", "b"},
			wantReturn:         "b",
		},
		{
			name:               "body lifetime token is excluded from signature",
			fnName:             "bodyonly",
			source:             "fn bodyonly() -> char {\n    let c = 'a';\n    c\n}\n",
			wantLifetimeParams: nil,
			wantSignature:      nil,
			wantReturn:         "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parser := newRustParser(t)
			path := writeRustSource(t, "lib.rs", tc.source)
			payload, err := Parse(path, false, shared.Options{}, parser)
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}
			fn := assertRustBucketName(t, payload, "functions", tc.fnName)

			gotParams := stringSliceField(fn, "lifetime_parameters")
			if !reflect.DeepEqual(gotParams, tc.wantLifetimeParams) {
				t.Fatalf("lifetime_parameters = %#v, want %#v", gotParams, tc.wantLifetimeParams)
			}
			gotSignature := stringSliceField(fn, "signature_lifetimes")
			if !reflect.DeepEqual(gotSignature, tc.wantSignature) {
				t.Fatalf("signature_lifetimes = %#v, want %#v", gotSignature, tc.wantSignature)
			}
			gotReturn, _ := fn["return_lifetime"].(string)
			if gotReturn != tc.wantReturn {
				t.Fatalf("return_lifetime = %q, want %q", gotReturn, tc.wantReturn)
			}
		})
	}
}

// TestRustImplLifetimeASTParity locks impl-block lifetime fields.
func TestRustImplLifetimeASTParity(t *testing.T) {
	t.Parallel()

	parser := newRustParser(t)
	path := writeRustSource(t, "lib.rs", `struct Holder<'a> { value: &'a str }
impl<'a> Render<'a> for Holder<'a> where Holder<'a>: Display {
    fn render(&self, input: &'a str) -> &'a str { input }
}
`)
	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	implBlock := assertRustBucketName(t, payload, "impl_blocks", "Holder")
	if got := stringSliceField(implBlock, "lifetime_parameters"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("impl lifetime_parameters = %#v, want [a]", got)
	}
	if got := stringSliceField(implBlock, "signature_lifetimes"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("impl signature_lifetimes = %#v, want [a]", got)
	}

	holder := assertRustBucketName(t, payload, "classes", "Holder")
	if got := stringSliceField(holder, "lifetime_parameters"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("struct lifetime_parameters = %#v, want [a]", got)
	}
}

func stringSliceField(item map[string]any, key string) []string {
	if value, ok := item[key].([]string); ok {
		return value
	}
	return nil
}

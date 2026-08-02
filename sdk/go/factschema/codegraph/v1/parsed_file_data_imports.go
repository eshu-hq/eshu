// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Import is the typed view of one entry in a parsed_file_data "imports" inner
// slice — the per-file import statement every language parser in
// go/internal/parser writes through shared.AppendBucket(payload, "imports", …).
//
// Unlike the closed-shape single-producer keys typed alongside it
// (parsed_file_data.go), "imports" is one of the wide per-language AST buckets:
// roughly thirty parsers write it and each names its own extra fields. That is
// why only the four fields a consumer joins on are named here, with every other
// producer field carried verbatim in the open Attributes pass-through
// (import_type, full_import_name, resolved_source, component_type_assertion,
// end_line, …), the same aws_resource/Attributes shape sdk/go/factschema/AGENTS.md
// prescribes. Naming a per-language field here would silently drop it for every
// other language.
//
// The per-language variance the named fields absorb, and why the two-key
// Name/Source split is enough to normalize it:
//
//   - Go writes the import path in Name and no Source at all
//     ({name: "fmt", line_number: 4, lang: "go"}).
//   - Python and JavaScript/TypeScript write the module in Source and the
//     imported symbol in Name ({name: "Session", source: "requests"}), so a
//     module-only import repeats the module in both keys.
//
// The single normalization rule that covers both — module = Source when set,
// else Name — is applied by the projector's import extractor, NOT here: this
// struct is the wire shape, not the graph shape.
//
// Issue #5691 is the consumer this struct was typed for: before it, nothing in
// the runtime read this bucket into the canonical File-[:IMPORTS]->Module edge
// writer, so a freshly indexed stack carried zero IMPORTS edges.
type Import struct {
	// Name is the imported binding as the parser saw it: the module path for
	// the languages that carry no Source (Go), otherwise the imported symbol
	// ("Session", "Router", "*" for a namespace import). Required in practice —
	// an entry with neither Name nor Source names nothing importable and the
	// extractor skips it.
	Name string `json:"name,omitempty"`

	// Source is the module the binding came from, for the parsers that
	// distinguish module from symbol (Python's `from X import Y`, JavaScript's
	// `import {Y} from "X"`). Optional: Go and the C-family header parsers put
	// the module in Name and omit Source entirely.
	Source string `json:"source,omitempty"`

	// Alias is the local binding name when it differs from Name, or, for the
	// Python parser, the derived local binding of a plain `import X` (its
	// producer fills the alias in even with no explicit `as` clause). Optional.
	Alias string `json:"alias,omitempty"`

	// LineNumber is the 1-based source line the import statement occurs on.
	// Optional: a parser that cannot attribute a line writes none, and the
	// extractor treats the resulting 0 as "unknown line", not line zero.
	LineNumber int `json:"line_number,omitempty"`

	// Attributes carries every import-entry field with no named struct field
	// above, preserving each value's JSON-native Go type. It is what keeps this
	// typed view from dropping the per-language evidence (import_type,
	// full_import_name, lang, resolved_source, end_line, …) that only some
	// parsers write. A hot caller that reads only the named fields can skip
	// rebuilding it with factschema.WithoutAttributesRemainder.
	Attributes map[string]any `json:"-"`
}

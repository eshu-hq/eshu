// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import "embed"

// schemaFS embeds every generated JSON Schema this module checks in under
// schema/. The embed directive lives directly in this package because
// go:embed can only reach files at or below its own package directory;
// schema/ is a direct child of factschema, so no duplicate copy is needed
// here (contrast sdk/go/factschema/fixturepack, which embeds its own copy of
// schema/ because fixturepack cannot reach the sibling directory).
//
//go:embed schema/*.json
var schemaFS embed.FS

// schemaFileSuffix is the fixed suffix every generated schema filename
// carries after its fact-kind wire string (see doc.go: FactKind constants and
// generated schema filenames match the wire kind byte-for-byte).
const schemaFileSuffix = ".v1.schema.json"

// SchemaBytes returns the raw, checked-in JSON Schema bytes for one fact kind
// (for example "aws_resource" or the dotted "incident.record"), read directly
// from this package's own embedded schema/ directory. ok is false when the
// module ships no schema for the kind; callers must not treat a false ok as
// an empty-but-valid schema, since that would silently skip payload
// validation for an unrecognized kind.
func SchemaBytes(kind string) ([]byte, bool) {
	raw, err := schemaFS.ReadFile("schema/" + kind + schemaFileSuffix)
	if err != nil {
		return nil, false
	}
	return raw, true
}

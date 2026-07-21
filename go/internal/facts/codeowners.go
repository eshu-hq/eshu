// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

// CodeownersOwnershipFactKind identifies one CODEOWNERS pattern-to-owners
// mapping: a single line of a repository's CODEOWNERS file, carrying the
// glob pattern, its verbatim owner tokens, and the line's position in the
// file (last-match-wins resolution order). See issue #5419.
const CodeownersOwnershipFactKind = "codeowners.ownership"

// CodeownersSchemaVersionV1 is the first codeowners fact schema.
const CodeownersSchemaVersionV1 = "1.0.0"

var codeownersFactKinds = []string{
	CodeownersOwnershipFactKind,
}

var codeownersSchemaVersions = map[string]string{
	CodeownersOwnershipFactKind: CodeownersSchemaVersionV1,
}

// CodeownersFactKinds returns the accepted codeowners fact kinds in their
// emission order.
func CodeownersFactKinds() []string {
	return slices.Clone(codeownersFactKinds)
}

// CodeownersSchemaVersion returns the schema version for a codeowners fact
// kind.
func CodeownersSchemaVersion(factKind string) (string, bool) {
	version, ok := codeownersSchemaVersions[factKind]
	return version, ok
}

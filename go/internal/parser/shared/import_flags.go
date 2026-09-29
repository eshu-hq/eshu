// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

// Import-entry flag keys. A language parser sets one of these on an "imports"
// bucket entry only when it is true and leaves it absent otherwise, so an
// unflagged entry is byte-identical to the payload before the flag existed.
// The keys ride in the typed Import view's open Attributes pass-through
// (sdk/go/factschema/codegraph/v1), and the projector reads them by these exact
// strings when it carries import flags onto IMPORTS edges (issue #7345).
const (
	// ImportFlagTypeOnly marks an import that exists only for the type checker
	// and never runs: Python's `if TYPE_CHECKING:` branch, TypeScript's
	// `import type` and per-specifier `type` modifier. It cannot close a runtime
	// import cycle.
	ImportFlagTypeOnly = "type_only"

	// ImportFlagDeferred marks an import that runs at call time rather than at
	// module load: a Python import inside a function body. It is the standard
	// way to break a load-time cycle, so a cycle closed only through deferred
	// imports is not a load-time cycle.
	ImportFlagDeferred = "deferred"

	// ImportFlagInferred marks an import whose source the parser synthesized
	// rather than resolved against files on disk, for example Python's "./x"
	// fallback for a relative import whose module is missing. An edge built from
	// it is a guess, so a cycle it closes is ambiguous.
	ImportFlagInferred = "inferred"
)

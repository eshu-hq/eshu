// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package decode owns the query layer's classified fact-decode failure.
//
// Error wraps a classified *factschema.DecodeError so a query handler can read
// the missing field and its classification without importing the
// reducer/projector dead-letter triage types. New builds one from whatever a
// factschema Decode* seam returns, defaulting to the input_invalid
// classification so an unexpected error is treated as non-retryable rather than
// mistaken for a successful decode.
//
// It is a leaf so a handler-family subpackage can classify a decode failure
// without importing the root query package, which it cannot do without an
// import cycle (#6060). It sits here rather than in querycontract because it
// depends on sdk/go/factschema, and querycontract is the package families
// import for types without inheriting a runtime (nested under query/ and
// destuttered to decode for #6642 Part D).
//
// DefaultSchemaMajorVersion and DerefString are the two pieces every
// query-layer factschema decoder shares: the schema version a version-less row
// normalizes to, and the nil-safe *string deref a typed pointer field needs.
// They moved here from the query root for #6642, replacing the per-package
// copies each handler family carried because it could not import root.
package decode

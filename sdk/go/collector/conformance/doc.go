// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package conformance is the public, out-of-tree-runnable collector conformance
// harness for Eshu component packages.
//
// It validates an out-of-tree collector package's manifest proof metadata and
// its collector-sdk/v1alpha1 result fixtures against the manifest-derived host
// contract, then emits a stable machine-readable Report. The package performs
// no file or network I/O and imports no Eshu internal packages, so an external
// collector repository can run conformance in its own CI by importing only
// github.com/eshu-hq/eshu/sdk/go/collector and this package.
//
// When the caller supplies Request.PayloadSchemas (a fact kind mapped to its
// JSON Schema bytes), the harness also validates each fixture fact payload
// against its schema and fails closed on a missing required field, a wrong-typed
// field, or a schema construct outside the supported subset. The subset is a
// deliberately small slice of JSON Schema (typed and nullable properties,
// string-array and nested-object items, string-valued maps) sufficient for the
// checked-in factschema payload schemas; CompileSchema reports whether a schema
// stays inside it. The schema bytes are caller-supplied so this package neither
// reads files nor depends on a schema library — the in-tree host reads the
// canonical schemas from disk and an out-of-tree collector reads them from the
// pinned github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack.
// ValidatePayloadSchemas exposes just that payload-shape pass for runtime hosts
// that have already validated the SDK result contract and must not also apply
// publication-only conformance checks.
//
// The in-tree extension host re-exports this report contract so the same
// verdict is produced inside and outside the Eshu monorepo.
package conformance

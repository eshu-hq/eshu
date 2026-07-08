// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package payloadusage implements Contract System v1 §6 enforcement gate 2
// (docs/internal/design/contract-system-v1.md#6-enforcement-gates): the
// payload-usage manifest.
//
// The schema-diff gate (issue #4569, go/cmd/factschema-diff) catches a
// collector breaking the shape it emits. This package catches the reverse
// break: a graph/query/loader handler starting to require a payload field that
// no declared schema promises, before an external collector discovers it in
// production.
//
// # Derivation
//
// The manifest is DERIVED, not hand-maintained, from three sources:
//
//   - ParseDecodeSeams reads factschema_decode*.go files under the reducer,
//     projector, query, loader, relationships, and replay surfaces and finds
//     every decode<Kind> function's return struct type and the
//     factschema.FactKind* constant it decodes. A gate that read only
//     factschema_decode.go would silently miss a family whose wrappers live in
//     a split file or non-reducer surface.
//   - ParseStructShapes reads the typed struct packages
//     (sdk/go/factschema/{aws,iam,incident}/v1) and extracts each struct's
//     named, JSON-tagged fields (excluding the untyped Attributes pass-through
//     tagged json:"-"), with a required/optional flag matching the same
//     pointer/slice/map-or-omitempty rule the decode seam and the schema
//     generator use.
//   - ScanDecodeUsage AST-walks every configured handler surface and finds
//     which of those fields a handler actually reads, following the decoded
//     value directly (`resource, err := decodeAWSResource(env)` then
//     `resource.Field`), across a function-call boundary when the decoded
//     struct is passed by value into a helper parameter typed with the same
//     qualified struct name, and through a single wrapper-struct field typed
//     as the seam struct (`statement.permission.Actions`, where
//     iamPermissionStatement.permission is an iamv1.Permission).
//
// BuildManifest joins the three into a Manifest. CheckManifest compares each
// kind's used fields against an externally supplied declared-field set (in
// production, the checked-in JSON Schema's properties via
// LoadDeclaredFieldsFromSchemas) and returns one Violation per field a
// handler reads that the schema does not declare.
//
// Gate also runs CheckRawPayloadConvention against loader, relationships, and
// replay sources. That ratchet skips factschema_decode*.go seam files, allows
// only the current explicit exemption list, and fails on a new direct
// .Payload["field"] or payloadString/payloadStrings read.
//
// # Attribution boundary
//
// The usage scan attributes direct reads, reads inside a helper whose
// parameter is typed as the seam struct, and reads through a single wrapper
// struct field typed as the seam struct — the IAM pattern where a decoded
// iamv1.Permission is stored in an iamPermissionStatement wrapper and read as
// `statement.permission.Actions` after the wrapper slice is ranged, or where a
// secretsIAMPrincipal.decoded is read as `principal.decoded.AccountID` (#4668).
//
// It still does NOT follow general multi-hop dataflow: a value returned from a
// call and then wrapped, a range over a map-indexed expression, or a wrapper
// whose seam field is a pointer or slice. Resolving those soundly needs full
// type information this AST-only scan avoids by design, so for such a shape the
// used-field set stays a lower bound. That never produces a false violation —
// BuildManifest joins each recorded read against the attributed struct's
// declared fields and drops anything that does not match — it only leaves a
// real read unattributed. The gate stays sound regardless because every such
// field is in the declared schema (schemas are generated from the same
// structs).
//
// # Entry points
//
// Load runs the full derivation pipeline and returns a Manifest. Gate runs
// Load and then compares the result against sdk/go/factschema/schema/*.json
// after enforcing the raw-payload ratchet, returning any Violations. Both
// accept a Paths struct whose fields default relative to RepoRoot via
// ResolvePaths, so a caller only needs to supply the repository root in the
// common case.
//
// # Callers
//
// go/cmd/payload-usage-manifest is the CLI wrapper (generate and gate
// modes). go/internal/reducer's own TestPayloadUsageManifest is the
// drift-lock test the package's gate command
// (`go test ./internal/reducer -run TestPayloadUsageManifest`) targets,
// calling Gate directly so a red result is investigated from inside the
// package whose handlers it is checking.
package payloadusage

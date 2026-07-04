// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package payloadusage implements Contract System v1 §6 enforcement gate 2
// (docs/internal/design/contract-system-v1.md#6-enforcement-gates): the
// payload-usage manifest.
//
// The schema-diff gate (issue #4569, go/cmd/factschema-diff) catches a
// collector breaking the shape it emits. This package catches the reverse
// break: a reducer handler starting to require a payload field that no
// declared schema promises, before an external collector discovers it in
// production.
//
// # Derivation
//
// The manifest is DERIVED, not hand-maintained, from three sources:
//
//   - ParseDecodeSeams reads every go/internal/reducer/factschema_decode*.go
//     file (globbed, not a single file — families split their decode wrappers
//     across per-family files such as factschema_decode_incident.go as the
//     500-line cap forces a split) and finds every decode<Kind> function's
//     return struct type and the factschema.FactKind* constant it decodes. A
//     gate that read only factschema_decode.go would silently miss a family
//     whose wrappers live in a split file.
//   - ParseStructShapes reads the typed struct packages
//     (sdk/go/factschema/{aws,iam,incident}/v1) and extracts each struct's
//     named, JSON-tagged fields (excluding the untyped Attributes pass-through
//     tagged json:"-"), with a required/optional flag matching the same
//     pointer/slice/map-or-omitempty rule the decode seam and the schema
//     generator use.
//   - ScanDecodeUsage AST-walks every reducer handler file and finds which
//     of those fields a handler actually reads, following the decoded value
//     both directly (`resource, err := decodeAWSResource(env)` then
//     `resource.Field`) and across a function-call boundary when the
//     decoded struct is passed by value into a helper parameter typed with
//     the same qualified struct name.
//
// BuildManifest joins the three into a Manifest. CheckManifest compares each
// kind's used fields against an externally supplied declared-field set (in
// production, the checked-in JSON Schema's properties via
// LoadDeclaredFieldsFromSchemas) and returns one Violation per field a
// handler reads that the schema does not declare.
//
// # Attribution boundary
//
// The usage scan attributes direct reads and reads inside a helper whose
// parameter is typed as the seam struct. It does NOT attribute a field read
// mediated ONLY through a wrapper struct field — the IAM pattern where a
// decoded iamv1.Permission is stored in an iamPermissionStatement wrapper and
// the wrapper (not the bare struct) is passed to a helper, so the read is
// `statement.permission.Actions` rather than `permission.Actions`. As a
// result the manifest's used-field set is a lower bound for the IAM kinds
// (aws_iam_permission, aws_resource_policy_permission). The gate stays sound
// today because every such field is in the declared schema (schemas are
// generated from the same structs), so no violation is missed; but a field
// reachable only through a wrapper would not be caught if a schema drifted.
// Extending attribution to single-field wrapper structs is tracked follow-up
// issue #4668 (part of epic #4566). See README.md "Limitations / attribution
// boundary".
//
// # Entry points
//
// Load runs the full derivation pipeline and returns a Manifest. Gate runs
// Load and then compares the result against sdk/go/factschema/schema/*.json,
// returning any Violations. Both accept a Paths struct whose fields default
// relative to RepoRoot via ResolvePaths, so a caller only needs to supply the
// repository root in the common case.
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

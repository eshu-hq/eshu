// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command factschema-diff is the contracts schema-diff breaking-change gate
// (Contract System v1 §6 enforcement gate 1).
//
// It diffs every generated JSON Schema under sdk/go/factschema/schema/
// against a baseline git ref (default: the merge-base of HEAD against
// origin/main), comparing the UNION of baseline and current schema files, and
// fails when a schema changed in a way that breaks compatibility — a field
// removed or renamed (required, or optional under a fail-closed
// additionalProperties:false schema), a field's type or value space narrowed
// (including a nested map-value or array-item type), an optional field
// promoted to required, a brand-new required field added, or an entire schema
// file deleted — without a corresponding major version bump in the schema's
// title marker ("... (schema version N)"). An additive optional field, or a
// schema file with no counterpart at the baseline ref (a brand-new fact
// kind), is not a break.
package main

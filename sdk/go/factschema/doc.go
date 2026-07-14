// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package factschema defines the versioned collector-reducer payload
// contracts described in Contract System v1
// (docs/internal/design/contract-system-v1.md, §3.1-3.2): the canonical
// Envelope, one typed payload struct per fact kind under
// "<family>/v<major>" (aws/v1, iam/v1, and incident/v1 today), the generated
// JSON Schemas checked in under schema/, and a kind-keyed decode seam so a
// reducer handler always codes against a validated, latest-version struct
// instead of reading map[string]any payload keys by hand.
//
// Fact kinds may be underscore-separated (aws_resource) or dotted
// (incident.record); the incident family is the first with dotted wire kinds.
// The FactKind* constants and the generated schema filenames match the wire
// kind byte-for-byte, dots included, and no decode, schema-generation, or
// drift-lock tooling parses the kind string for a separator.
//
// The module is intentionally independent from Eshu internal Go packages,
// the same constraint github.com/eshu-hq/eshu/sdk/go/collector already
// satisfies: it imports nothing under
// github.com/eshu-hq/eshu/go/internal/..., so both collector repositories
// and the core reducer can depend on it without depending on each other.
//
// A missing required payload field is a classified decode error
// (ClassificationInputInvalid, a *DecodeError naming the field), never a
// zero-value struct — the accuracy backstop this design exists to add
// beneath the existing envelope-level schema-version admission gate.
//
// SchemaBytes embeds schema/*.json directly in this package and returns the
// raw bytes for one fact kind, so a caller (for example a runtime
// conformance test outside this module) can load a committed schema without
// duplicating the schema tree the way sdk/go/factschema/fixturepack must,
// since fixturepack cannot reach this package's schema/ directory with its
// own go:embed directive.
package factschema

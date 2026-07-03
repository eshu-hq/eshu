// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package factschema defines the versioned collector-reducer payload
// contracts described in Contract System v1
// (docs/internal/design/contract-system-v1.md, §3.1-3.2): the canonical
// Envelope, one typed payload struct per fact kind under
// "<family>/v<major>" (starting with aws/v1), the generated JSON Schemas
// checked in under schema/, and a kind-keyed decode seam so a reducer
// handler always codes against a validated, latest-version struct instead
// of reading map[string]any payload keys by hand.
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
package factschema

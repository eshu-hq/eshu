// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package factenvelope adapts Eshu fact envelopes between public collector SDK,
// durable internal, and factschema decode representations.
//
// The package is adapter-only: callers supply host-owned identity and fencing
// fields, while the generated helpers copy the SDK fact fields that are safe to
// persist and clone payload maps before downstream handoff. Version-less
// schema versions normalize only for the factschema decode seam; unsupported
// majors remain visible to Decode functions so version skew fails explicitly.
package factenvelope

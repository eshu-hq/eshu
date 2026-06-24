// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ocrdoc builds source-neutral documentation facts from reviewed local
// OCR results.
//
// The package runs image metadata preflight first, then passes safe PNG, JPEG,
// and first-frame GIF inputs to an injected OCR engine. It emits only
// documentation document and section facts; it does not infer services,
// deployments, ownership, incidents, graph edges, or claim candidates from OCR
// text. Unsafe source locations and source identity fields are redacted before
// persistence. Runtime enablement, sandboxing, telemetry wiring, and dependency
// review remain caller-owned before any hosted extraction path is turned on.
package ocrdoc

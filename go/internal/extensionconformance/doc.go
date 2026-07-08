// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package extensionconformance validates optional component fixtures against
// the manifest and collector SDK contracts.
//
// The package is read-only: it loads a component manifest, derives the
// host-declared SDK result contract, validates operator-supplied fixture
// results, and reports whether findings block publication or hosted
// activation. It does not install components, claim workflow work, write graph
// truth, or run Compose services.
//
// When a manifest fact family declares payloadSchemaRef, the host maps that
// namespaced component fact kind to the referenced factschema fixture-pack
// schema and passes it to the public conformance harness. A schema-invalid
// fixture therefore fails closed before publication or hosted activation.
package extensionconformance

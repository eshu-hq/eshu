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
// The host loads a durable producer-grant snapshot when a manifest declares a
// core-owned fact kind. It passes exact live collector/schema authorization,
// core reducer phases, and registered payload schemas to the public harness.
// Namespaced fact kinds may declare payloadSchemaRef to use a fixture-pack
// schema. A schema-invalid fixture fails closed before publication or hosted
// activation.
package extensionconformance

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
// CLI payload-shape validation is not wired here yet. The public harness
// conformance.Run validates payload shape when the caller supplies PayloadSchemas
// (added in the sdk/go/collector/conformance package), but this host cannot
// supply them today: it passes ReservedFactKinds=CoreFactKinds() and the manifest
// validator requires every declared fact kind to be namespaced, while the fixture
// pack keys its schemas by bare core fact kind. A component therefore declares a
// namespaced kind that a core-keyed schema map can never match, and the manifest
// carries no field mapping a namespaced kind to a core payload shape. Wiring the
// pack anyway would validate nothing while reading as if the CLI checks payloads,
// so PayloadSchemas is left nil until the enabling manifest field lands. That
// mapping and a live host test are tracked in issue #4665 (part of #4566).
package extensionconformance

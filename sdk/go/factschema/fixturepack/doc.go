// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package fixturepack is the versioned, importable payload-conformance fixture
// pack for the Eshu fact-schema contracts (Contract System v1 §3.5,
// docs/internal/design/contract-system-v1.md). It bundles the checked-in JSON
// Schemas for every typed fact kind together with a curated set of valid and
// invalid example payloads per kind, so an out-of-tree collector can pin one
// fixture-pack version and prove in its own CI that it emits exactly the
// payload shapes the target reducer release consumes.
//
// The pack ships inside the sdk/go/factschema module and is released in
// lockstep with it: the fixture-pack "version" IS the factschema module
// version. A collector that pins github.com/eshu-hq/eshu/sdk/go/factschema at a
// tag pins the schemas and fixtures that were valid at that tag together. A
// fixture pack that outlived the schema version it was cut from is stale
// evidence, not a fixture, which is why they share one tag rather than
// versioning independently.
//
// The schemas exposed here are byte-identical to the canonical generated
// artifacts under sdk/go/factschema/schema/*.json; the drift-lock test
// TestFixturePackSchemasMatchCanonical (in the parent module) fails the build
// if the embedded copy and the generated source ever diverge, so the pack can
// never ship a stale schema.
//
// The fixtures are keyed by the core fact-kind wire string (for example
// "aws_resource"). An external collector emitting the same payload SHAPE emits
// it under its own namespaced fact kind (collector-extraction-policy.md: the
// bare core kinds are host-owned and reserved), and maps that namespaced kind
// to the shipped schema shape when it feeds
// conformance.Request.PayloadSchemas. See ExampleValidPayloads and
// SchemaFor for the accessors, and README.md for the versioning and
// cut procedure.
package fixturepack

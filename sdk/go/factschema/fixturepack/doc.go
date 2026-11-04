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
// NVDPagingGolden supplies the canonical two-page source input and exact
// expected provider fact kinds, stable keys and full payload maps. The maps
// carry producer metadata beyond this module's typed CVE and source-snapshot
// fields; the per-kind valid/invalid examples remain the schema-shape fixtures.
//
// OSVSeverityGolden supplies synthetic OSV records and the cvss_vector and
// severity_label a conforming OSV collector writes on vulnerability.cve for
// each, so core and the extracted collector prove the same severity rules.
//
// OSVIdentityEdgesGolden supplies synthetic OSV records and the aliases,
// related, upstream and status a conforming OSV collector writes on
// vulnerability.cve for each. Each edge list is trimmed, de-duplicated and
// sorted; upstream is a directed edge, never an alias, and is absent when it
// has no ids. A DSA bundle of CVEs is upstream, not aliases.
//
// SourceTermsGolden supplies the license and attribution each existing
// vulnerability source's source_snapshot carries (osv, nvd, cisa_kev,
// first_epss). A license is an SPDX identifier, a LicenseRef- identifier,
// or NOASSERTION; it describes the source, not each record, and discloses terms
// without deciding access. Core and the extracted collector replay it against
// their own per-source constants.
package fixturepack

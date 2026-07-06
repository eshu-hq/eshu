// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gcp generates a deterministic, seeded, schema-valid synthetic GCP
// corpus as a v1 cassette (issue #4581; docs/internal/design/
// 4389-ifa-conformance-platform.md, "Public corpora without provider
// access"). It is the first synthetic-provider-corpus generator: contributors
// with no GCP account can produce a committable, shareable, credential-free
// GCP fixture rather than recording (and being blocked from publishing) a
// live collection.
//
// Generate(Options) builds a cassette.File covering the five GCP fact kinds
// sdk/go/factschema/gcp/v1 types (gcp_cloud_resource, gcp_cloud_relationship,
// gcp_collection_warning, gcp_dns_record, gcp_iam_policy_observation),
// encodes each payload through the real sdk/go/factschema Encode* seam so
// every generated payload is schema-valid by construction, and
// canonicalizes the result through replay.Canonicalize with the same
// options go/internal/replay/recorder applies to a live-recorded cassette.
// The same Options.Seed always yields byte-identical output.
//
// The package fails closed on any fact kind with no entry in
// factKindSchemaVersions: generateFact refuses to emit a kind this generator
// has not proven schema coverage for, rather than emitting an unvalidated
// payload.
//
// This package imports only sdk/go/factschema (the contracts module),
// go/internal/replay, and go/internal/replay/cassette. It does not, and must
// not, import go/internal/collector/gcpcloud or any other collector
// internal package (Contract System v1 §3.5): assetTypeInventory
// (asset_types.go) is a deliberately duplicated, static copy of the GCP
// typed-depth extractor registry's asset-type vocabulary, refreshed by hand
// rather than imported.
//
// Generate never touches the network or filesystem and requires no
// credential: every value is derived from Options.Seed and
// Options.ProjectID, so there is no redaction step because nothing sensitive
// is ever produced.
//
// TestParitySyntheticVsRecordedGCPShape (parity_test.go) is a maintainer-run,
// operator-gated check (ESHU_SYNTH_GCP_PARITY=1) comparing this generator's
// fact-kind and asset-type shape against the recorded GCP cassette
// (testdata/cassettes/gcpcloud/supply-chain-demo.json). It is skipped by
// default and in CI, following the same env-var-gated live-smoke precedent as
// go/internal/collector/pagerduty/live_test.go.
//
// DefaultDemoOrgProfile formalizes the demo/conformance corpus identity scheme
// already used by scripts/verify-golden-corpus-gate.sh: ESHU_GITHUB_ORG=acme
// and deterministic repository remotes shaped as github.com/acme/<repo>. Its
// JoinKeyRegistry reserves those cross-family keys, including the
// github.com/acme/lib-common package owner hint that drives cross-repo
// DEPENDS_ON resolution. GenerateDemoOrgCassette is the Go regeneration entry
// point for the first generated family; it returns canonical bytes labeled for
// testdata/cassettes/gcpcloud/supply-chain-demo.json, the demo-corpus manifest
// layout path. WriteDemoOrgCassette materializes those bytes under
// testdata/generated-cassettes/ instead of overwriting the committed
// golden-corpus cassette; committed-path swaps are valid only behind the
// operator-controlled golden-corpus gate.
package gcp

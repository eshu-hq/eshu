// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import "github.com/eshu-hq/eshu/go/internal/scope"

// emitBenchCase binds one collector kind to the credential-free cassette that
// the emit benchmark replays through the real Claim -> ingest -> emit-facts
// path. CassettePath is relative to this package directory.
//
// Cassettes for the nine credentialed collectors are reused from the shipped,
// golden-corpus cassettes under the repo-root testdata/cassettes tree; the
// remaining kinds use dedicated benchmark-only cassettes under
// testdata/emit_bench that the B-7 golden-corpus gate does not scan.
type emitBenchCase struct {
	Kind         scope.CollectorKind
	CassettePath string
}

// emitBenchExemption documents a collector kind that has no Claim -> ingest ->
// emit-facts cycle and therefore cannot carry an emit benchmark. The coverage
// guard treats an exemption as covered only when Reason is non-empty.
type emitBenchExemption struct {
	Kind   scope.CollectorKind
	Reason string
}

// repoRootCassette builds a path to a shipped golden-corpus cassette relative
// to this package directory (go/internal/collector). These are reused verbatim
// and never modified by the benchmark.
func repoRootCassette(dir string) string {
	return "../../../testdata/cassettes/" + dir + "/supply-chain-demo.json"
}

// benchCassette builds a path to a dedicated emit-benchmark cassette.
func benchCassette(name string) string {
	return "testdata/emit_bench/" + name + ".json"
}

// emitBenchCases lists every collector kind that emits facts, paired with the
// cassette that drives its emit benchmark. The coverage guard asserts that
// every scope.AllCollectorKinds() entry appears here or in emitBenchExemptions.
func emitBenchCases() []emitBenchCase {
	return []emitBenchCase{
		{Kind: scope.CollectorGit, CassettePath: benchCassette("git")},
		{Kind: scope.CollectorAWS, CassettePath: repoRootCassette("awscloud")},
		{Kind: scope.CollectorAzure, CassettePath: repoRootCassette("azurecloud")},
		{Kind: scope.CollectorGCP, CassettePath: repoRootCassette("gcpcloud")},
		{Kind: scope.CollectorTerraformState, CassettePath: repoRootCassette("terraformstate")},
		{Kind: scope.CollectorDocumentation, CassettePath: benchCassette("documentation")},
		{Kind: scope.CollectorOCIRegistry, CassettePath: repoRootCassette("ociregistry")},
		{Kind: scope.CollectorPackageRegistry, CassettePath: repoRootCassette("packageregistry")},
		{Kind: scope.CollectorVulnerabilityIntelligence, CassettePath: benchCassette("vulnerability_intelligence")},
		{Kind: scope.CollectorSBOMAttestation, CassettePath: benchCassette("sbom_attestation")},
		{Kind: scope.CollectorSecurityAlert, CassettePath: benchCassette("security_alert")},
		{Kind: scope.CollectorCICDRun, CassettePath: benchCassette("ci_cd_run")},
		{Kind: scope.CollectorPagerDuty, CassettePath: benchCassette("pagerduty")},
		{Kind: scope.CollectorJira, CassettePath: benchCassette("jira")},
		{Kind: scope.CollectorScannerWorker, CassettePath: benchCassette("scanner_worker")},
		{Kind: scope.CollectorSemanticExtraction, CassettePath: benchCassette("semantic_extraction")},
		{Kind: scope.CollectorKubernetesLive, CassettePath: repoRootCassette("kuberneteslive")},
		{Kind: scope.CollectorVaultLive, CassettePath: repoRootCassette("vaultlive")},
		{Kind: scope.CollectorPrometheusMimir, CassettePath: repoRootCassette("prometheusmimir")},
		{Kind: scope.CollectorTempo, CassettePath: benchCassette("tempo")},
		{Kind: scope.CollectorGrafana, CassettePath: benchCassette("grafana")},
		{Kind: scope.CollectorLoki, CassettePath: benchCassette("loki")},
	}
}

// emitBenchExemptions lists collector kinds that are intentionally not covered
// by an emit benchmark, each with a documented reason.
func emitBenchExemptions() []emitBenchExemption {
	return []emitBenchExemption{
		{
			Kind: scope.CollectorWebhook,
			Reason: "webhook is a refresh-trigger collector, not a fact-emitting " +
				"source: it normalizes provider deliveries into refresh triggers " +
				"(see go/internal/webhook) and emits no facts.Envelope generation " +
				"through collector.Service, so there is no emit cycle to benchmark.",
		},
	}
}

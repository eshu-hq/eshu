// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"crypto/sha256"
	"fmt"
)

// noRealConsumerFound2026Q3 documents the #5474 D2 signal-rebuild backfill's
// auditability standard: round 1 disclosed 25 kinds under one shared,
// unfalsifiable reason. A round-2 review found the detector at that time had
// two blind spots — it only matched `== facts.<Kind>` (never
// `!= facts.<Kind>`, the extremely common "skip-unless-this-kind" idiom) and
// could not see raw-JSON storage/postgres readers or `pq.Array`-bound
// `fact_kind = ANY($N)` queries with no locally-declared const — and named 3
// kinds (a 4th, vulnerability.source_snapshot, surfaced during the round-2
// re-verification this comment describes) that were wrongly disclosed
// despite being genuinely, production-wired consumed:
// package_registry.source_hint (go/internal/reducer/package_source_correlation.go:98),
// azure_identity_observation (go/internal/storage/postgres/cloud_identity_policy_evidence.go:85),
// azure_resource_change (go/internal/storage/postgres/cloud_resource_change_evidence.go:90),
// and vulnerability.source_snapshot
// (go/internal/query/supply_chain_impact_readiness_postgres_query.go:179).
// All four are removed from this ledger (kind_real_consumer_dispatch.go now
// matches token.NEQ; kind_real_consumer_postgres_reader.go and
// kind_real_consumer_query_slice.go are the two round-2 additions that would
// have caught them).
//
// Per the #5475 seed entries' auditability standard, every remaining entry
// below now carries its own falsifiable reason: the exact `rg` command run
// against every real-consumer-signal directory (go/internal/reducer,
// go/internal/projector, go/internal/query, go/internal/storage/postgres,
// go/internal/relationships), searching both the kind's facts.<Kind>FactKind
// identifier and its wire string literal, with the confirmed-empty result.
// A kind here still needs its own real consumer (typed decode seam, reducer
// dispatch, or query read model) or a per-kind owner decision to remove it
// from the registry — these are round-2-verified "no live consumer found",
// not "not yet checked."

// grandfatheredUnconsumedKind is the digest-pinned disclosure ledger for
// fact kinds that are in the registry but have no real consumer today. It
// mirrors grandfatheredLanguageParityReadSurfaces in
// read_surface_grandfather.go: a kind here passes the #5474 D2 per-kind
// consumer existence gate (kind_consumer_existence_test.go) without a
// detectably live consumer.
//
// Entry discipline:
//   - Key is the exact fact kind string (e.g. "terraform_state_candidate").
//   - Value is sha256(family + ":" + kind + ":" + disclosureReason).
//     Changing the kind, family, or reason changes the digest and
//     un-grandfathers the entry, forcing a re-evaluation.
//   - Each entry MUST cite the #5475 comment + code anchor that justifies
//     the disclosure, or (for the #5474 signal-rebuild backfill entries)
//     noRealConsumerFound2026Q3.
//   - When a kind gains a real consumer, remove it from this ledger — the
//     gate FAILS if a disclosed kind's real-consumer evidence later becomes
//     true (resolveKindConsumer's contradiction check), so a disclosure and
//     an actual consumer can never silently coexist.
//
// Seed entries (from #5475):
//   - terraform_state_{candidate,provider_binding,warning}: explicitly not
//     consumed — see projector/tfstate_canonical.go:104-106
//   - package_registry.vulnerability_hint: join-key-only — no reducer
//     decode seam, no query read-model consumer
//   - service_catalog.{api_link,dependency,scorecard_definition,
//     scorecard_result,warning}: five kinds with no decode-side consumer
//     today (registry YAML comment ~428-435)
//   - vulnerability.warning: no reducer decode, no query reader (#5462 owns
//     any future consumer) — corrected from vulnerability_intelligence in
//     #5474: the registry's actual family label is vulnerability_intelligence,
//     but the kind's own family key used in this ledger's digest input
//     matches kindDisclosureEntries' Family field.
//   - ci.job, ci.pipeline_definition, ci.warning: emitted by collector but
//     no reducer decode call today (Wave 4d intentionally deferred)
//
// Backfill entries (from #5474's real-consumption signal rebuild): 21
// additional kinds the pre-fix D2 gate passed only via the toothless
// PayloadSchema/pipeline-consumer signals, round-2-verified with per-kind
// falsifiable evidence (see noRealConsumerFound2026Q3). Four kinds round 1
// wrongly put here (package_registry.source_hint, azure_identity_observation,
// azure_resource_change, vulnerability.source_snapshot) were removed once
// round 2 found their real consumers. See
// docs/internal/design/5474-ifa-coverage-backfill-plan.md for the tracked
// backfill plan the remaining 21 still need.
// #nosec G101 -- map values are sha256 source-content digests for the
// consumer-disclosure ledger, not credentials.
var grandfatheredUnconsumedKinds = map[string]string{
	// terraform_state family — projector/tfstate_canonical.go's extractor doc
	// comment. terraform_state_provider_binding gained a real projector
	// consumer in #5446 (terraformStateProviderBindingsByResource) and was
	// removed from this ledger; candidate and warning remain unconsumed.
	"terraform_state_candidate": "5563c81346264cd987969e68c93f4e22eb7862ba80eadea9904cdedcb6afc7e7",
	"terraform_state_warning":   "0ce119d0c368e25ebd19958bedcb460e494c609a01fdaf95be8b5bff8143fed6",

	// package_registry.vulnerability_hint — join-key-only
	"package_registry.vulnerability_hint": "7b8323b2e7fee6e4111dd8358eccf0e563c922ad3bc97741304eeb5360e705a7",

	// service_catalog — five unconsumed kinds
	"service_catalog.api_link":             "5ecf78012c63927e9aa5cc801dcb7df8d379813780437634a71a5fa4ea213ab6",
	"service_catalog.dependency":           "5db4a108aa3787de98418f80603364ae729e8737e457710d1aa93dcb692b404d",
	"service_catalog.scorecard_definition": "eccbec4f20a13d00a43437085ea328cdce7a4b2f4781f4a5c7f8b2019a6b2992",
	"service_catalog.scorecard_result":     "6665f2717625493b9b949bf1575170e0bc9313f023ad022192ce49402cb877c4",
	"service_catalog.warning":              "d15f38d83feac67e85d2988ec8af36efcc705199469f9ed9a2f1e26e2ced2b8e",

	// vulnerability.warning — no consumer (see comment above: no pipeline
	// consumer either — pipelineConsumer was removed in #5474 because it was
	// equally toothless).
	"vulnerability.warning": "396608cc74fe490db6e7ec97c11c710c307af87d849a99c947eee59b95913a87",

	// ci_cd_run kinds — emitted by collector, no reducer decode call (Wave 4d deferred)
	"ci.job":                 "9291dd86572475df194a616ec46caa0cd6bf5900d44edc814c69726feae9363c",
	"ci.pipeline_definition": "94e3e6d031de09f8a5f084ca448269cfef2c74cda9e22d64450ff70c8d390618",
	"ci.warning":             "f7ae3d4ea5b7be1a1ac60c0b89b8a5711ed48c4ff6fae3774329ae0242d8d0e5",

	// #5474 signal-rebuild backfill — round-2-verified, each with its own
	// falsifiable rg-command evidence in kindDisclosureEntries below.
	"aws_dns_record":                                  "1b053df313390f41e2c24b90be866942c7efac322d4493a64a3774989db1654b",
	"aws_iam_access_analyzer_finding":                 "8267f1b1f99314ef295973497d36373fad7211b1c60abb140d71801285d5adf0",
	"aws_iam_instance_profile":                        "c4302b9f5aa99163053fde7048a698be47db987d9f565a795b286524c10d520d",
	"aws_tag_observation":                             "e149c53f64d163051885dde8bdfc670ef1ebe7c7b9df097ee87746a7b6973dd2",
	"aws_warning":                                     "34b02986c03c2ab526ed7814edc1ee255e2c7ca6c6f86d53d633855a3b515f25",
	"azure_collection_warning":                        "bbcc21157a215477b96e07f980c67f2379822e806a8e569a522d09ccf550144a",
	"azure_dns_record":                                "f51d8e6dd4235cf3e5a46ee1da21b74cf7c2f298c274aa23179ae40af5d10597",
	"gcp_collection_warning":                          "6f195acc2bea8448d03257bdf3f13867ca2f21563f8121a38d718b88173a64db",
	"gcp_dns_record":                                  "3e2bcbf325b149581ef1d02bbc19618ba34e8199f9f8bb44004e76a50f1cff63",
	"gcp_iam_policy_observation":                      "f4617527dd25265d8ef3b44e466c56416b0fbdb2160551f7954103a84e6a99c5",
	"incident_routing.applied_alert_route":            "5f5c8e11a7ac1d6b863df90bebd01e31cf8140360cefed0e4ff4078dfbed434a",
	"incident_routing.observed_pagerduty_integration": "af8343c4bae224b45d7e72fa66d00ca5c61e9272315e84c2d33f2e166fd8dfea",
	"k8s_rbac_binding":                                "3c21a5404a9f56d283de9c756fa29d6e956e8e56f708dfde4cc44e619c90206d",
	"k8s_rbac_role":                                   "2573747ed14d6c4278c5f93b07a3e82ec8f73dc74784da47d012586073e29ad0",
	"package_registry.package_artifact":               "a69cf5b389a97ccba6011af05a69b15b068e61e77ee95eda599cb398d6ba44b5",
	"package_registry.registry_event":                 "dc46aefb230f539feec68c4fd3531a50d5e331523baccfca33858ceda6ddfd73",
	"package_registry.repository_hosting":             "e42b971c90d524571b3b3605a89ba5b3d955f957a6b392df157fa103eef24689",
	"vault_auth_mount":                                "fc8cb7c193a71b33969618eac04aeb256ed2ecc84514067159d9dcc6f7368e78",
	"vault_identity_alias":                            "d4764e55b34e45dee4023385fc8609119d44e55a3bfabcab6a48ef2fe8021767",
	"vault_identity_entity":                           "738d59bc2a82269eeba3b28ed698fcd3315addd99d72855a10c1ce3ab497a02c",
	"vault_secret_engine_mount":                       "8f7ca30ced8d2e2d219bc06ddd46d382ee19b58212c2354861f183df2e717ce5",
}

// kindDisclosureDigest computes the SHA-256 digest of the disclosure entry
// (family + ":" + kind + ":" + reason). Changing any component changes the
// digest and un-grandfathers the entry.
func kindDisclosureDigest(family, kind, reason string) string {
	sum := sha256.Sum256([]byte(family + ":" + kind + ":" + reason))
	return fmt.Sprintf("%x", sum)
}

// buildKindDisclosureLedger creates the digest-pinned disclosure ledger from
// per-kind entries. Each entry must cite the family, kind, and the reason
// (e.g. a code anchor like "projector/tfstate_canonical.go:104-106").
// The digests are pre-computed; this function is used by tests for validation.
func buildKindDisclosureLedger(entries []kindDisclosureEntry) map[string]string {
	ledger := make(map[string]string, len(entries))
	for _, e := range entries {
		ledger[e.Kind] = kindDisclosureDigest(e.Family, e.Kind, e.Reason)
	}
	return ledger
}

// kindDisclosureEntry is one disclosure entry in the build-time ledger.
type kindDisclosureEntry struct {
	Family string
	Kind   string
	Reason string
}

// disclosedKindsUnchanged checks that the committed disclosure ledger
// (grandfatheredUnconsumedKinds) is EXACTLY the pinned expected set — same
// keys, same digests, both directions. It is not enough to check that every
// expected entry is present with the right digest (the forward direction):
// without also rejecting an extra key, a kind could be added directly to
// grandfatheredUnconsumedKinds — bypassing kindDisclosureEntries' digest-
// pinned, code-anchored discipline entirely — and isKindDisclosed would
// silently start returning true for it, producing a false-green
// consumer-existence gate result (TestEveryRegistryKindHasConsumerOrDisclosure,
// which calls this function) for a kind nobody ever justified. The reverse
// pass below closes that gap.
func disclosedKindsUnchanged(expected []kindDisclosureEntry) error {
	expectedLedger := buildKindDisclosureLedger(expected)

	// Forward: every expected (pinned, code-anchored) entry must be present
	// in the ledger with a matching digest.
	for kind, expectedDigest := range expectedLedger {
		actualDigest, exists := grandfatheredUnconsumedKinds[kind]
		if !exists {
			return fmt.Errorf("kind %q is in the expected disclosure set but missing from grandfatheredUnconsumedKinds", kind)
		}
		if actualDigest != expectedDigest {
			return fmt.Errorf("kind %q disclosure digest mismatch: ledger=%s, expected=%s", kind, actualDigest, expectedDigest)
		}
	}

	// Reverse: the ledger must not contain any key absent from the pinned
	// expected set. Checked per-key first so the error names the exact
	// offending kind; the trailing length check is a defensive fallback that
	// cannot actually fire once both directions of the per-key checks above
	// pass (a map that is a superset AND a subset of another, key-for-key, is
	// that map), but it keeps the invariant explicit and self-documenting.
	for kind := range grandfatheredUnconsumedKinds {
		if _, ok := expectedLedger[kind]; !ok {
			return fmt.Errorf(
				"kind %q is in grandfatheredUnconsumedKinds but has no matching kindDisclosureEntries entry — "+
					"it bypasses the digest-pinned, code-anchored disclosure discipline; add a kindDisclosureEntries "+
					"entry citing real evidence, or remove it from grandfatheredUnconsumedKinds",
				kind,
			)
		}
	}
	if len(grandfatheredUnconsumedKinds) != len(expectedLedger) {
		return fmt.Errorf(
			"grandfatheredUnconsumedKinds has %d entries but kindDisclosureEntries only pins %d — every disclosure key must have a matching digest-pinned kindDisclosureEntries entry",
			len(grandfatheredUnconsumedKinds), len(expectedLedger),
		)
	}
	return nil
}

// kindDisclosureEntries is the build-time source of truth for which kinds
// are intentionally unconsumed. Tests compute the expected ledger from this
// and assert it matches grandfatheredUnconsumedKinds.
var kindDisclosureEntries = []kindDisclosureEntry{
	{Family: "terraform_state", Kind: "terraform_state_candidate", Reason: "projector/tfstate_canonical.go's extractTerraformStateRows doc comment"},
	{Family: "terraform_state", Kind: "terraform_state_warning", Reason: "projector/tfstate_canonical.go's extractTerraformStateRows doc comment"},
	{Family: "package_registry", Kind: "package_registry.vulnerability_hint", Reason: "join-key-only: facts_active_supply_chain_impact.go:46 filter only, no decode"},
	{Family: "service_catalog", Kind: "service_catalog.api_link", Reason: "no decode-side consumer (registry YAML ~428-435)"},
	{Family: "service_catalog", Kind: "service_catalog.dependency", Reason: "no decode-side consumer (registry YAML ~428-435)"},
	{Family: "service_catalog", Kind: "service_catalog.scorecard_definition", Reason: "no decode-side consumer (registry YAML ~428-435)"},
	{Family: "service_catalog", Kind: "service_catalog.scorecard_result", Reason: "no decode-side consumer (registry YAML ~428-435)"},
	{Family: "service_catalog", Kind: "service_catalog.warning", Reason: "no decode-side consumer (registry YAML ~428-435)"},
	{Family: "vulnerability_intelligence", Kind: "vulnerability.warning", Reason: "no reducer decode, no query reader (#5462 owns)"},
	{Family: "ci_cd_run", Kind: "ci.job", Reason: "emitted by collector, no reducer decode (Wave 4d deferred)"},
	{Family: "ci_cd_run", Kind: "ci.pipeline_definition", Reason: "emitted by collector, no reducer decode (Wave 4d deferred)"},
	{Family: "ci_cd_run", Kind: "ci.warning", Reason: "emitted by collector, no reducer decode (Wave 4d deferred)"},

	// #5474 signal-rebuild backfill, round-2-verified. Each Reason is the
	// exact `rg` commands run against every real-consumer-signal directory
	// (go/internal/reducer, go/internal/projector, go/internal/query,
	// go/internal/storage/postgres, go/internal/relationships) for both the
	// kind's facts.<Kind>FactKind identifier and its wire-string literal,
	// with the confirmed-empty result — independently falsifiable per kind,
	// per noRealConsumerFound2026Q3's auditability standard.
	{Family: "aws_cloud_runtime_drift", Kind: "aws_dns_record", Reason: "round-2 re-verify (2026-07-21): `rg -n \"AWSDNSRecordFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"aws_dns_record\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "secrets_iam_trust_chain", Kind: "aws_iam_access_analyzer_finding", Reason: "round-2 re-verify (2026-07-21): `rg -n \"AWSIAMAccessAnalyzerFindingFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"aws_iam_access_analyzer_finding\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "secrets_iam_trust_chain", Kind: "aws_iam_instance_profile", Reason: "round-2 re-verify (2026-07-21): `rg -n \"AWSIAMInstanceProfileFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"aws_iam_instance_profile\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger); the ONE wire-string hit outside those dirs — iamInstanceProfileRoleResourceTypeInstanceProfile in go/internal/reducer/iam_instance_profile_role_edge_rows.go and go/internal/projector/iam_instance_profile_role_materialization_intents.go — compares a decoded aws_resource struct's ResourceType FIELD against this literal, not the envelope FactKind; it does not consume the aws_iam_instance_profile fact kind"},
	{Family: "aws_cloud_runtime_drift", Kind: "aws_tag_observation", Reason: "round-2 re-verify (2026-07-21): `rg -n \"AWSTagObservationFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"aws_tag_observation\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "aws_cloud_runtime_drift", Kind: "aws_warning", Reason: "round-2 re-verify (2026-07-21): `rg -n \"AWSWarningFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"aws_warning\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "azure_resource_materialization", Kind: "azure_collection_warning", Reason: "round-2 re-verify (2026-07-21): `rg -n \"AzureCollectionWarningFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"azure_collection_warning\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "azure_resource_materialization", Kind: "azure_dns_record", Reason: "round-2 re-verify (2026-07-21): `rg -n \"AzureDNSRecordFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"azure_dns_record\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "gcp_resource_materialization", Kind: "gcp_collection_warning", Reason: "round-2 re-verify (2026-07-21): `rg -n \"GCPCollectionWarningFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"gcp_collection_warning\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "gcp_resource_materialization", Kind: "gcp_dns_record", Reason: "round-2 re-verify (2026-07-21): `rg -n \"GCPDNSRecordFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"gcp_dns_record\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "gcp_resource_materialization", Kind: "gcp_iam_policy_observation", Reason: "round-2 re-verify (2026-07-21): `rg -n \"GCPIAMPolicyObservationFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"gcp_iam_policy_observation\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "incident_routing_materialization", Kind: "incident_routing.applied_alert_route", Reason: "round-2 re-verify (2026-07-21): `rg -n \"IncidentRoutingAppliedAlertRouteFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"incident_routing.applied_alert_route\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "incident_routing_materialization", Kind: "incident_routing.observed_pagerduty_integration", Reason: "round-2 re-verify (2026-07-21): `rg -n \"IncidentRoutingObservedPagerDutyIntegrationFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"incident_routing.observed_pagerduty_integration\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "secrets_iam_trust_chain", Kind: "k8s_rbac_binding", Reason: "round-2 re-verify (2026-07-21): `rg -n \"KubernetesRBACBindingFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"k8s_rbac_binding\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "secrets_iam_trust_chain", Kind: "k8s_rbac_role", Reason: "round-2 re-verify (2026-07-21): `rg -n \"KubernetesRBACRoleFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"k8s_rbac_role\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "package_source_correlation", Kind: "package_registry.package_artifact", Reason: "round-2 re-verify (2026-07-21): `rg -n \"PackageRegistryPackageArtifactFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"package_registry.package_artifact\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "package_source_correlation", Kind: "package_registry.registry_event", Reason: "round-2 re-verify (2026-07-21): `rg -n \"PackageRegistryRegistryEventFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"package_registry.registry_event\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "package_source_correlation", Kind: "package_registry.repository_hosting", Reason: "round-2 re-verify (2026-07-21): `rg -n \"PackageRegistryRepositoryHostingFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"package_registry.repository_hosting\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "secrets_iam_trust_chain", Kind: "vault_auth_mount", Reason: "round-2 re-verify (2026-07-21): `rg -n \"VaultAuthMountFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"vault_auth_mount\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "secrets_iam_trust_chain", Kind: "vault_identity_alias", Reason: "round-2 re-verify (2026-07-21): `rg -n \"VaultIdentityAliasFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"vault_identity_alias\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "secrets_iam_trust_chain", Kind: "vault_identity_entity", Reason: "round-2 re-verify (2026-07-21): `rg -n \"VaultIdentityEntityFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"vault_identity_entity\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
	{Family: "secrets_iam_trust_chain", Kind: "vault_secret_engine_mount", Reason: "round-2 re-verify (2026-07-21): `rg -n \"VaultSecretEngineMountFactKind\" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'` (excluding _test.go) -> 0 matches; `rg -n \"\\\"vault_secret_engine_mount\\\"\"` same dirs -> 0 matches (outside registry/collector/this ledger)"},
}

// isKindDisclosed returns true if kind is pinned in the disclosure ledger at
// its current digest (computed from the expected entries). A disclosed kind
// passes the consumer existence gate without a live consumer.
func isKindDisclosed(kind string) bool {
	_, pinned := grandfatheredUnconsumedKinds[kind]
	return pinned
}

// factKindRegistryConsumerEvidence holds the fields the consumer existence
// gate reads from each FactKindRegistryEntry.
type factKindRegistryConsumerEvidence struct {
	Kind            string
	ReducerDomain   string
	PayloadSchema   string
	AdmissionExempt bool
	ProjectionHook  string
	AdmissionHook   string
}

// kindHasConsumer reports whether a fact kind has a detectable REAL
// consumer, evidenced by real source-code call sites rather than registry
// metadata. For v1, consumer detection uses:
//   - AdmissionExempt → legacy code kinds (file, repository) are
//     deliberately outside the versioned-admission regime but still consumed
//     via the code-graph projection path.
//   - real.hasRealConsumer(entry.Kind) → the kind has an actual decode<Kind>
//     seam call site, a literal fact_kind SQL predicate or facts.<Kind>
//     identifier reference in the query (read-surface) layer, or a
//     case/equality dispatch on the raw envelope kind in the reducer — see
//     loadRealConsumerEvidence and kind_real_consumer.go.
//
// This intentionally does NOT check entry.PayloadSchema (a checked-in JSON
// Schema file path, populated for every kind with a typed struct whether or
// not any code decodes it) or a pipeline-consumer signal derived from
// ReducerDomain/ProjectionHook/AdmissionHook all being non-empty (equally
// registry metadata: terraform_state_candidate carries a full
// ReducerDomain/ProjectionHook/AdmissionHook triple despite having no decode
// call site — go/internal/projector/tfstate_canonical.go:113-116 documents
// it as intentionally unhandled). Both were the #5474 P0 false-green: they
// are populated identically for consumed and unconsumed kinds alike, so
// neither is evidence of consumption.
func kindHasConsumer(entry factKindRegistryConsumerEvidence, real realConsumerEvidence) bool {
	if entry.AdmissionExempt {
		return true
	}
	return real.hasRealConsumer(entry.Kind)
}

// resolveKindConsumer reports whether a fact kind has a real consumer or an
// explicit disclosure. This is the #5474 D2 per-kind consumer existence
// gate's resolution function.
//
// Disclosures are load-bearing: if a kind is BOTH disclosed in
// grandfatheredUnconsumedKinds AND real now reports a real consumer for it,
// that is a contradiction — the disclosure is stale and must be removed —
// and this function FAILS rather than silently letting the (now correct)
// consumer signal paper over it. Without this check, disclosure entries do
// not affect the gate's outcome either way (the #5474 P0 finding: a kind
// gaining a real consumer would keep passing via the disclosure, so the gate
// result never depended on whether the disclosure entries were accurate).
func resolveKindConsumer(entry factKindRegistryConsumerEvidence, real realConsumerEvidence) (ok bool, reason string) {
	return classifyKindConsumer(
		entry.Kind,
		entry.ReducerDomain,
		kindHasConsumer(entry, real),
		isKindDisclosed(entry.Kind),
		entry.AdmissionExempt,
	)
}

// classifyKindConsumer is resolveKindConsumer's pure decision function: it
// takes the already-resolved hasConsumer/disclosed/admissionExempt booleans
// instead of reading grandfatheredUnconsumedKinds or calling
// loadRealConsumerEvidence itself, so tests can drive every combination
// directly against a real, production fact-kind/family pair without
// mutating package-level state.
func classifyKindConsumer(kind, family string, hasConsumer, disclosed, admissionExempt bool) (ok bool, reason string) {
	if disclosed && hasConsumer && !admissionExempt {
		return false, fmt.Sprintf(
			"fact kind %q (family=%q) is disclosed as unconsumed in grandfatheredUnconsumedKinds, "+
				"but a real consumer was detected — the disclosure is stale and must be removed from "+
				"kind_disclosure_ledger.go (grandfatheredUnconsumedKinds and kindDisclosureEntries)",
			kind, family,
		)
	}
	if hasConsumer {
		return true, ""
	}
	if disclosed {
		return true, ""
	}
	return false, fmt.Sprintf(
		"fact kind %q (family=%q) has no detectable consumer and is not disclosed — "+
			"three legal exits: (1) add a consumer (typed decode seam, reducer handler, or query read model), "+
			"(2) add the kind to grandfatheredUnconsumedKinds in kind_disclosure_ledger.go with code-anchor evidence, "+
			"(3) remove the kind from specs/fact-kind-registry.v1.yaml",
		kind, family,
	)
}

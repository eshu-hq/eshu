// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"crypto/sha256"
	"fmt"
)

// noRealConsumerFound2026Q3 is the shared disclosure reason for the #5474 D2
// signal-rebuild backfill: every entry citing it was checked against all
// four real-consumption signals loadRealConsumerEvidence computes (decode
// seam, direct factschema.Decode<Kind> call, query-layer literal fact_kind
// SQL/identifier reference, reducer-level facts.<Kind> dispatch) plus a
// manual repo-wide grep for the kind's wire string and its facts.<Kind>
// constant outside go/internal/facts and go/internal/collector, and none
// found a live consumer. These kinds previously passed the pre-#5474-fix D2
// gate only because it treated a non-empty PayloadSchema path or a
// fully-populated ReducerDomain/ProjectionHook/AdmissionHook triple as
// consumption — both are registry metadata populated identically for
// terraform_state_candidate (proven unconsumed by
// go/internal/projector/tfstate_canonical.go:113-116), so neither is real
// evidence. Each kind here needs its own real consumer (typed decode seam,
// reducer dispatch, or query read model) or a per-kind owner decision to
// remove it from the registry; this shared reason documents that the
// disclosure is a stopgap from a signal-accuracy fix, not a considered
// per-kind design decision the way the #5475 seed entries were.
const noRealConsumerFound2026Q3 = "no decode seam, no literal fact_kind SQL predicate/identifier reference, and no facts.<Kind> reducer dispatch found repo-wide as of #5474's real-consumption signal (kind_real_consumer.go) — collector-emitted only; needs a real consumer or registry removal, owner TBD"

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
// Backfill entries (from #5474's real-consumption signal rebuild, all citing
// noRealConsumerFound2026Q3): 25 additional kinds the pre-fix D2 gate passed
// only via the toothless PayloadSchema/pipeline-consumer signals. See
// docs/internal/design/5474-ifa-coverage-backfill-plan.md for the tracked
// backfill plan these kinds still need.
var grandfatheredUnconsumedKinds = map[string]string{
	// terraform_state family — projector/tfstate_canonical.go:104-106
	"terraform_state_candidate":        "237d492637756acd33f62cbf1664ebdbad6cc83a464ee5a592c372dee36622ba",
	"terraform_state_provider_binding": "e2e87c5f2b5fba399561f9c233aa62cdbb7896c01ac17a5bfa74d9982e5421f5",
	"terraform_state_warning":          "bfb82f94e9bdfff4d7ebaf2a4daad186edbd3bf738de97f7654b91d1b65c77ef",

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

	// #5474 signal-rebuild backfill — see noRealConsumerFound2026Q3.
	"aws_dns_record":                                  "03a734cc9bbabac931db4f1096e6aab2d0510ec865cbedbb2407950db3f038de",
	"aws_iam_access_analyzer_finding":                 "5b1be1c7125859ab72c6dab2fa91632eef43d380d2a1a9a63e6395c34c0c4cab",
	"aws_iam_instance_profile":                        "e7d79f2dd86610fcb0830bcbc8a2c4760ef4757f44bf923a35cedc4bf5c60b95",
	"aws_tag_observation":                             "9b8fe24fef311f4a0f9e0e99095813d5cbe5038e88aa39f2e1b0396cdc6a35a8",
	"aws_warning":                                     "d68bb5446a8889064b7c678543a03748d2d2eda086d17341a2e9d43591b4c70f",
	"azure_collection_warning":                        "2e58111c160fcb3d44c514d27732963f6b9a768562b8864da3fa9bb718de3b50",
	"azure_dns_record":                                "22236777f79f8c1678b9dd7fb9f4aa7c42d899acd741f9f931de6877af59f2b5",
	"azure_identity_observation":                      "4671736d979e181b77194c3b3e59a6a3300ec72c1b2a59226a6acdda08ef7bce",
	"azure_resource_change":                           "f96a8fa4777a2f34967a8f248cbdad9854d59318c7112f96c50a103f73f07737",
	"gcp_collection_warning":                          "bbf565d90d04b725bae02510020911640f885dc38d68e11ab20e093381b7a476",
	"gcp_dns_record":                                  "12dbebf967345587b2445c1be61ecc4fc6d80d545abb6cd337a8d80ee859dbb3",
	"gcp_iam_policy_observation":                      "206c32f881790513041f9f6888819185d563a29860c459a9e75a0f9ce35a5237",
	"incident_routing.applied_alert_route":            "f765c99aa527c86edefb84c7b6f9a5fb05ceb13a290184f2a1ec9641ea3c0365",
	"incident_routing.observed_pagerduty_integration": "b203f56df44d3afc864c288c958a68b0aabd166d8d52fc980d67fa437c2383b3",
	"k8s_rbac_binding":                                "6400a3e340377816fa39e03062ba488756b0d2d9272b180c00b810364897bd0c",
	"k8s_rbac_role":                                   "6a158bc6270316acf67b6366f1647eb0c43465a7b9946c41bd2b5b874d8bc8e0",
	"package_registry.package_artifact":               "ee272c183709b2c09d20a9f2053f6b44278a44f9f229c6ec823fc545c334d8a5",
	"package_registry.registry_event":                 "34ea2e685c495e374d1aa5ea0073fad668c812401cf6e0cbeb91d88a6defdf68",
	"package_registry.repository_hosting":             "1268632c083b22afb79d04399ef1ea8483bdf142460f5871c628a00c89023d75",
	"package_registry.source_hint":                    "bcf797b1f488c1f444db311e4f78a0b6848c44061ea0cf82f29d45f65ece981c",
	"vault_auth_mount":                                "d55a179b0d58824eba5c2566049124b82e8325a9cb89e61b4e3ded0211a5a9b2",
	"vault_identity_alias":                            "84a1e8bd1704ebbaf654cb444df72f89e05152b6e939734400f00a43f5de75b2",
	"vault_identity_entity":                           "cba170f378d3963e265630824ba3460e6c2553cb7802e5ce3b32431a0af230e1",
	"vault_secret_engine_mount":                       "8061b276c4bafc6113a8d221d02ac891d11e2f7fc4ec74b94b9c381bf3f05433",
	"vulnerability.source_snapshot":                   "71b3345b9d13a07bf01b546170fb810cbea4805f25be07059288db82dc3f6916",
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
// matches the expected entries. If an entry's digest drifts, it fails.
func disclosedKindsUnchanged(expected []kindDisclosureEntry) error {
	expectedLedger := buildKindDisclosureLedger(expected)
	for kind, expectedDigest := range expectedLedger {
		actualDigest, exists := grandfatheredUnconsumedKinds[kind]
		if !exists {
			return fmt.Errorf("kind %q is in the expected disclosure set but missing from grandfatheredUnconsumedKinds", kind)
		}
		if actualDigest != expectedDigest {
			return fmt.Errorf("kind %q disclosure digest mismatch: ledger=%s, expected=%s", kind, actualDigest, expectedDigest)
		}
	}
	return nil
}

// kindDisclosureEntries is the build-time source of truth for which kinds
// are intentionally unconsumed. Tests compute the expected ledger from this
// and assert it matches grandfatheredUnconsumedKinds.
var kindDisclosureEntries = []kindDisclosureEntry{
	{Family: "terraform_state", Kind: "terraform_state_candidate", Reason: "projector/tfstate_canonical.go:104-106"},
	{Family: "terraform_state", Kind: "terraform_state_provider_binding", Reason: "projector/tfstate_canonical.go:104-106"},
	{Family: "terraform_state", Kind: "terraform_state_warning", Reason: "projector/tfstate_canonical.go:104-106"},
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

	// #5474 signal-rebuild backfill.
	{Family: "aws_cloud_runtime_drift", Kind: "aws_dns_record", Reason: noRealConsumerFound2026Q3},
	{Family: "secrets_iam_trust_chain", Kind: "aws_iam_access_analyzer_finding", Reason: noRealConsumerFound2026Q3},
	{Family: "secrets_iam_trust_chain", Kind: "aws_iam_instance_profile", Reason: noRealConsumerFound2026Q3},
	{Family: "aws_cloud_runtime_drift", Kind: "aws_tag_observation", Reason: noRealConsumerFound2026Q3},
	{Family: "aws_cloud_runtime_drift", Kind: "aws_warning", Reason: noRealConsumerFound2026Q3},
	{Family: "azure_resource_materialization", Kind: "azure_collection_warning", Reason: noRealConsumerFound2026Q3},
	{Family: "azure_resource_materialization", Kind: "azure_dns_record", Reason: noRealConsumerFound2026Q3},
	{Family: "azure_resource_materialization", Kind: "azure_identity_observation", Reason: noRealConsumerFound2026Q3},
	{Family: "azure_resource_materialization", Kind: "azure_resource_change", Reason: noRealConsumerFound2026Q3},
	{Family: "gcp_resource_materialization", Kind: "gcp_collection_warning", Reason: noRealConsumerFound2026Q3},
	{Family: "gcp_resource_materialization", Kind: "gcp_dns_record", Reason: noRealConsumerFound2026Q3},
	{Family: "gcp_resource_materialization", Kind: "gcp_iam_policy_observation", Reason: noRealConsumerFound2026Q3},
	{Family: "incident_routing_materialization", Kind: "incident_routing.applied_alert_route", Reason: noRealConsumerFound2026Q3},
	{Family: "incident_routing_materialization", Kind: "incident_routing.observed_pagerduty_integration", Reason: noRealConsumerFound2026Q3},
	{Family: "secrets_iam_trust_chain", Kind: "k8s_rbac_binding", Reason: noRealConsumerFound2026Q3},
	{Family: "secrets_iam_trust_chain", Kind: "k8s_rbac_role", Reason: noRealConsumerFound2026Q3},
	{Family: "package_source_correlation", Kind: "package_registry.package_artifact", Reason: noRealConsumerFound2026Q3},
	{Family: "package_source_correlation", Kind: "package_registry.registry_event", Reason: noRealConsumerFound2026Q3},
	{Family: "package_source_correlation", Kind: "package_registry.repository_hosting", Reason: noRealConsumerFound2026Q3},
	{Family: "package_source_correlation", Kind: "package_registry.source_hint", Reason: noRealConsumerFound2026Q3},
	{Family: "secrets_iam_trust_chain", Kind: "vault_auth_mount", Reason: noRealConsumerFound2026Q3},
	{Family: "secrets_iam_trust_chain", Kind: "vault_identity_alias", Reason: noRealConsumerFound2026Q3},
	{Family: "secrets_iam_trust_chain", Kind: "vault_identity_entity", Reason: noRealConsumerFound2026Q3},
	{Family: "secrets_iam_trust_chain", Kind: "vault_secret_engine_mount", Reason: noRealConsumerFound2026Q3},
	{Family: "supply_chain_impact", Kind: "vulnerability.source_snapshot", Reason: noRealConsumerFound2026Q3},
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

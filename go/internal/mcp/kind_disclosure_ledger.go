// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"crypto/sha256"
	"fmt"
)

// grandfatheredUnconsumedKind is the digest-pinned disclosure ledger for
// fact kinds that are in the registry but have no decode-side consumer
// today. It mirrors grandfatheredLanguageParityReadSurfaces in
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
//     the disclosure.
//   - When a kind gains a real consumer, remove it from this ledger — the
//     gate will then fail if it drifts back to unconsumed.
//
// Seed entries (from #5475):
//   - terraform_state_{candidate,provider_binding,warning}: explicitly not
//     consumed — see projector/tfstate_canonical.go:104-106
//   - package_registry.vulnerability_hint: join-key-only — no reducer
//     decode seam, no query read-model consumer
//   - service_catalog.{api_link,dependency,scorecard_definition,
//     scorecard_result,warning}: five kinds with no decode-side consumer
//     today (registry YAML comment ~428-435)
//   - vulnerability_intelligence.warning: no reducer decode, no query
//     reader (#5462 owns any future consumer)
//   - ci_job, ci.pipeline_definition, ci.warning: emitted by collector but
//     no reducer decode call today (Wave 4d intentionally deferred)
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

	// vulnerability_intelligence.warning — no consumer
	"vulnerability.warning": "396608cc74fe490db6e7ec97c11c710c307af87d849a99c947eee59b95913a87",

	// ci_cd_run kinds — emitted by collector, no reducer decode call (Wave 4d deferred)
	"ci.job":                 "9291dd86572475df194a616ec46caa0cd6bf5900d44edc814c69726feae9363c",
	"ci.pipeline_definition": "94e3e6d031de09f8a5f084ca448269cfef2c74cda9e22d64450ff70c8d390618",
	"ci.warning":             "f7ae3d4ea5b7be1a1ac60c0b89b8a5711ed48c4ff6fae3774329ae0242d8d0e5",
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

// pipelineConsumer reports whether the kind flows through the full
// admission→projection→read pipeline. A kind with a ReducerDomain,
// ProjectionHook, and real AdmissionHook (not "none") is consumed at the
// pre-typing raw-payload level — the reducer domain's handler processes it,
// the projection hook writes durable rows, and the admission hook validates
// it on ingest.
func (e factKindRegistryConsumerEvidence) pipelineConsumer() bool {
	return e.ReducerDomain != "" && e.ProjectionHook != "" && e.AdmissionHook != "" && e.AdmissionHook != "none"
}

// kindHasConsumer reports whether a fact kind has a detectable consumer.
// For v1, consumer detection uses these signals from the generated registry:
//   - PayloadSchema non-empty → the kind has a typed decode seam (factschema
//     struct + decode wrapper → consumed at the reducer level).
//   - AdmissionExempt → legacy code kinds (file, repository) are
//     deliberately outside the versioned-admission regime but still consumed.
//   - Pipeline consumer → the kind has a non-empty ReducerDomain,
//     ProjectionHook, and a real (non-"none") AdmissionHook — meaning it
//     flows through the full admission→projection→read pipeline and is
//     consumed at the pre-typing raw-payload level.
func kindHasConsumer(entry factKindRegistryConsumerEvidence) bool {
	if entry.PayloadSchema != "" {
		return true
	}
	if entry.AdmissionExempt {
		return true
	}
	if entry.pipelineConsumer() {
		return true
	}
	return false
}

// resolveKindConsumer reports whether a fact kind has a real consumer or an
// explicit disclosure. This is the #5474 D2 per-kind consumer existence gate's
// resolution function.
func resolveKindConsumer(entry factKindRegistryConsumerEvidence) (ok bool, reason string) {
	if kindHasConsumer(entry) {
		return true, ""
	}
	if isKindDisclosed(entry.Kind) {
		return true, ""
	}
	return false, fmt.Sprintf(
		"fact kind %q (family=%q) has no detectable consumer and is not disclosed — "+
			"three legal exits: (1) add a consumer (typed decode seam, reducer handler, or query read model), "+
			"(2) add the kind to grandfatheredUnconsumedKinds in kind_disclosure_ledger.go with code-anchor evidence, "+
			"(3) remove the kind from specs/fact-kind-registry.v1.yaml",
		entry.Kind, entry.ReducerDomain,
	)
}

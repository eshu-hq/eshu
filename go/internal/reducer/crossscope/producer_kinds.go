// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// producerCollectorKindByDomain maps a producer REDUCER domain to the
// collector kind of the ingestion scopes whose ACTIVATION publishes that
// domain's output to a cross-scope consumer.
//
// The mapping is the load-bearing translation in this file, so both entries
// are grounded in code rather than in a plausible-looking string:
//
//   - container_image_identity -> oci_registry. The identity domain's canonical
//     support rows are read back through
//     container_image_identity_current_support_facts_for (migration 092c),
//     which only returns a row once the owning scope's generation is the
//     scope's active generation AND that generation's status is 'active'.
//     The scopes carrying image manifests are the OCI registry collector's,
//     planned with scope.CollectorOCIRegistry by
//     internal/coordinator/oci/registry/planner.go and projected by
//     internal/projector/canonical/oci_registry.go.
//   - ci_cd_run_correlation -> ci_cd_run. Its intent is triggered by ci.run
//     evidence (internal/projector/cicd/run/correlation/reducer_intent.go),
//     emitted by the hosted CI collectors under scope.CollectorCICDRun
//     (internal/collector/cicdrun/ghactionsruntime and
//     .../gitlabciruntime).
//
// The values are plain strings so this leaf package does not import the
// scope package; producer_kinds_test pins them against the scope constants.
// A producer domain absent from this map resolves to no collector kind and
// is answered as ready rather than blocked: the consumer commits its best
// available answer instead of waiting on a scope kind nobody has shown it
// depends on. What this mapping does NOT capture: container_image_identity
// intents are also enqueued in aws, azure, gcp, git, and sbom_attestation
// scopes, so identity output can be published by a scope this map does not
// name; neither the floor nor the #11000 wait covers those.
var producerCollectorKindByDomain = map[reducercontract.Domain]string{
	reducercontract.DomainContainerImageIdentity: "oci_registry",
	reducercontract.DomainCICDRunCorrelation:     "ci_cd_run",
}

// CollectorKindForProducerDomain maps a producer reducer domain to the
// collector kind whose ingestion scopes publish that domain's output, so a
// cross-scope wait can be keyed and woken by producer domain alone
// (#11000). It reads the single package map the handler floor, the audit,
// the park, and both Ack-path wakes share, so none of them can drift apart
// on which scope kind a producer publishes through. The second result is
// false when the domain maps to no kind: an unmapped domain counts as
// ready, never as blocked.
func CollectorKindForProducerDomain(producer reducercontract.Domain) (string, bool) {
	kind, ok := producerCollectorKindByDomain[producer]
	return kind, ok
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// This file holds cohesive adapter groups that DefaultHandlers embeds. The
// groups were split out of defaults.go to keep that file within the repository
// file-size limit. Embedding promotes every field, so existing field access
// (handlers.FieldName) and reducer registry wiring are unchanged; only
// struct-literal construction nests fields under their group. Each group type
// is exported because cmd/reducer constructs them from outside this package.

// DriftHandlers groups the provider config-vs-state and cloud-runtime drift
// adapters. They are optional: a nil member makes the corresponding drift domain
// short-circuit or stay unregistered rather than drop evidence or admit findings
// with no durable truth surface.
type DriftHandlers struct {
	// Terraform config-vs-state drift adapters (chunk #43; durable write added
	// issue #5442). TerraformBackendResolver, DriftEvidenceLoader, DriftWriter,
	// and DriftLogger must all be non-nil for the registry to register
	// DomainConfigStateDrift: a missing resolver/loader would leave the
	// handler with no observable input, and a missing writer would admit
	// findings with no durable truth surface — the same "no consumer-less
	// kind" bar the AWS/multi-cloud runtime drift adapters below hold.
	TerraformBackendResolver *tfstatebackend.Resolver
	DriftEvidenceLoader      DriftEvidenceLoader
	DriftWriter              TerraformConfigStateDriftFindingWriter
	DriftLogger              *slog.Logger

	// AWS cloud-runtime drift adapters (issue #39). Both must be non-nil for
	// the registry to register DomainAWSCloudRuntimeDrift; missing either one
	// would either drop evidence before publication or admit findings with no
	// durable truth surface.
	AWSCloudRuntimeDriftEvidenceLoader AWSCloudRuntimeDriftEvidenceLoader
	AWSCloudRuntimeDriftWriter         AWSCloudRuntimeDriftFindingWriter
	AWSCloudRuntimeDriftLogger         *slog.Logger

	// Multi-cloud runtime drift adapters (issues #1997, #1998). Both must be
	// non-nil for the registry to register DomainMultiCloudRuntimeDrift; missing
	// either one would either drop provider-neutral drift evidence before
	// publication or admit findings with no durable truth surface. The path
	// mirrors the AWS drift adapters but joins on canonical cloud_resource_uid so
	// AWS, GCP, and Azure share one drift domain.
	MultiCloudRuntimeDriftEvidenceLoader MultiCloudRuntimeDriftEvidenceLoader
	MultiCloudRuntimeDriftWriter         MultiCloudRuntimeDriftFindingWriter
	MultiCloudRuntimeDriftLogger         *slog.Logger
}

// CloudInventoryHandlers groups the provider cloud-inventory admission adapters
// that admit canonical cloud resources and attach optional evidence sharing a
// cloud_resource_uid.
type CloudInventoryHandlers struct {
	// Cloud inventory admission adapters (issues #1997, #1998). Both must be
	// non-nil for the registry to register DomainCloudInventoryAdmission; missing
	// either one would either drop provider cloud-inventory facts before
	// admission or admit canonical identities with no durable truth surface.
	// CloudInventoryGenerationCheck is optional and supersedes stale generations
	// before any load or write.
	CloudInventoryEvidenceLoader  CloudInventoryEvidenceLoader
	CloudInventoryAdmissionWriter CloudInventoryAdmissionWriter
	CloudInventoryGenerationCheck GenerationFreshnessCheck
	// CloudInventoryTagEvidenceLoader is optional; when set, tag-evidence
	// fingerprints (e.g. azure_tag_observation) attach to the canonical resource
	// sharing their cloud_resource_uid. A nil loader leaves the AWS/GCP resource
	// admission path unchanged.
	CloudInventoryTagEvidenceLoader CloudTagEvidenceLoader
	// CloudInventoryIdentityPolicyEvidenceLoader is optional; when set,
	// identity-policy evidence (e.g. azure_identity_observation) attaches to the
	// canonical resource sharing its cloud_resource_uid. A nil loader leaves the
	// resource admission path unchanged.
	CloudInventoryIdentityPolicyEvidenceLoader CloudIdentityPolicyEvidenceLoader
	// CloudInventoryResourceChangeEvidenceLoader is optional; when set,
	// provider resource-change facts attach sanitized freshness evidence onto
	// admitted canonical resources. Change evidence never admits resources or
	// finalizes tombstones on its own.
	CloudInventoryResourceChangeEvidenceLoader CloudResourceChangeEvidenceLoader
}

// SearchDocumentHandlers groups the curated search-document projection adapters
// (design 430). It loads indexed content and writes derived EshuSearchDocument
// facts; it performs no graph write.
type SearchDocumentHandlers struct {
	// Curated search-document projection (design 430). Both must be non-nil for
	// the registry to register DomainEshuSearchDocument; it loads the scope's
	// indexed content and writes derived EshuSearchDocument facts.
	EshuSearchDocumentSourceLoader SearchDocumentSourceLoader
	EshuSearchDocumentWriter       SearchDocumentWriter
	EshuSearchDocumentLogger       *slog.Logger
}

// KubernetesHandlers groups the live-Kubernetes correlation writer plus the
// pod-template node and live-workload edge materializers (issue #388).
type KubernetesHandlers struct {
	// KubernetesCorrelationWriter persists live Kubernetes correlation decisions
	// (exact, derived, ambiguous, unresolved, stale, rejected) plus a drift kind
	// for kubernetes_live.* facts joined to deployment-source image evidence.
	KubernetesCorrelationWriter KubernetesCorrelationWriter

	// KubernetesWorkloadNodeWriter materializes kubernetes_live.pod_template facts
	// into canonical KubernetesWorkload graph nodes (issue #388). It must be
	// non-nil alongside FactLoader for the registry to register
	// DomainKubernetesWorkloadMaterialization; missing either one would drop every
	// pod-template fact before it reaches the graph. The handler also publishes the
	// canonical-nodes-committed phase through GraphProjectionPhasePublisher so the
	// later live-workload edge slice can gate on it exactly like the AWS
	// relationship edge gates on the CloudResource node phase (#805).
	KubernetesWorkloadNodeWriter KubernetesWorkloadNodeWriter

	// KubernetesNamespaceNodeWriter materializes kubernetes_live.namespace
	// facts into canonical KubernetesNamespace graph nodes, binding an
	// Environment node only for a namespace whose label declared a
	// recognized environment (issue #5434). It must be non-nil alongside
	// FactLoader for the registry to register
	// DomainKubernetesNamespaceMaterialization; missing either one would drop
	// every namespace fact before it reaches the graph.
	KubernetesNamespaceNodeWriter KubernetesNamespaceNodeWriter

	// KubernetesCorrelationEdgeWriter projects exact live-workload correlation
	// decisions into canonical RUNS_IMAGE edges between a KubernetesWorkload node
	// and the digest-addressed OCI source node it runs (issue #388 PR3). It must be
	// non-nil alongside FactLoader for the registry to register
	// DomainKubernetesCorrelationMaterialization; missing either one would drop
	// every correlation materialization intent before it reaches the graph. The
	// handler also gates on ReadinessLookup so edges never resolve against
	// uncommitted KubernetesWorkload nodes.
	KubernetesCorrelationEdgeWriter KubernetesCorrelationEdgeWriter
}

// CrossplaneHandlers groups the Crossplane Claim -> XRD SATISFIED_BY edge
// writer (issue #5347).
type CrossplaneHandlers struct {
	// CrossplaneSatisfiedByEdgeWriter projects resolved Crossplane
	// classification decisions into canonical SATISFIED_BY edges between a
	// K8sResource node (the Claim) and the CrossplaneXRD node it resolved
	// against. It must be non-nil alongside FactLoader for the registry to
	// register DomainCrossplaneSatisfiedByMaterialization; missing either one
	// would drop every classification intent before it reaches the graph.
	CrossplaneSatisfiedByEdgeWriter CrossplaneSatisfiedByEdgeWriter
	// CrossplaneRedriveTargetLedger records a target Claim scope as durably
	// satisfied for one XRD (group, claim_kind) identity, once the handler
	// has actually committed a SATISFIED_BY edge for a claim matching that
	// identity -- never at enqueue time (issue #5476 P1 follow-up: writing
	// it when the cross-scope redrive sweep merely ENQUEUES the intent let a
	// later dead-lettered intent permanently and silently suppress every
	// future redrive for that identity, since auto-retry-on-dead-letter is
	// disabled by default). Optional: nil is safe (no-op), matching
	// PriorGenerationCheck.
	CrossplaneRedriveTargetLedger CrossplaneRedriveTargetLedgerWriter
}

// SupplyChainSecurityHandlers groups the supply-chain, secrets/IAM, and
// endpoint-presence adapters. The presence members back the cross-scope
// projection gates and stay nil unless their feature is enabled.
type SupplyChainSecurityHandlers struct {
	// SBOMAttestationAttachmentWriter persists SBOM and attestation document
	// attachment decisions for digest-keyed image evidence.
	SBOMAttestationAttachmentWriter SBOMAttestationAttachmentWriter

	// SupplyChainImpactWriter persists vulnerability impact findings with
	// explicit package, SBOM, image, and repository evidence paths.
	SupplyChainImpactWriter SupplyChainImpactWriter

	// SecurityAlertReconciliationWriter persists provider alert comparison
	// state without promoting provider alerts into impact truth.
	SecurityAlertReconciliationWriter SecurityAlertReconciliationWriter

	// SecretsIAMTrustChainEvidenceLoader loads the bounded AWS IAM,
	// Kubernetes, and Vault source-fact packet used by the secrets/IAM reducer
	// read-model domain.
	SecretsIAMTrustChainEvidenceLoader SecretsIAMTrustChainEvidenceLoader

	// SecretsIAMTrustChainWriter persists identity-trust-chain,
	// privilege-posture, secret-access-path, and posture-gap reducer facts.
	SecretsIAMTrustChainWriter SecretsIAMTrustChainWriter

	// SecretsIAMGraphWriter projects exact reducer-owned secrets/IAM
	// read-model rows (identity_trust_chain, secret_access_path) into the four
	// SecretsIAM* node families and the five resolvable SECRETS_IAM_* edge
	// families (ADR #1314 §4). It must be non-nil alongside FactLoader for the
	// registry to register DomainSecretsIAMGraphProjection; missing either one
	// keeps the domain unregistered so no projection intent is silently dropped.
	// It defaults to nil: live graph writes stay OFF until the target-bound
	// activation record binds approval to one deployment and captures flag-on
	// proof before cmd/reducer's opt-in flag is set.
	SecretsIAMGraphWriter SecretsIAMGraphWriter

	// EndpointPresenceWriter records uid-exact presence for committed
	// CloudResource and KubernetesWorkload nodes so the cross-scope secrets/IAM
	// projection gate can prove an endpoint committed (issue #1380). It is nil
	// unless the secrets/IAM graph projection feature is enabled, so the default
	// hot materializer paths carry no extra write.
	EndpointPresenceWriter EndpointPresenceWriter

	// EndpointPresenceLookup answers uid-exact cross-scope endpoint readiness for
	// the secrets/IAM projection gate (issue #1380). Nil disables gating; it is
	// wired only when the projection feature is enabled.
	EndpointPresenceLookup EndpointPresenceLookup

	// APIEndpointRepoPathPresenceWriter records property-keyed (repo_id, path)
	// :Endpoint presence after workload materialization commits, so the
	// handles_route projection gate can prove a specific endpoint exists (#2809).
	// It is independent of EndpointPresenceWriter: it is wired only when the
	// handles_route endpoint-presence gate is enabled and feeds ONLY the workload
	// materialization handler — the cloud/Kubernetes materializers must never
	// receive it, so they emit no uid presence unless secrets/IAM is enabled. Nil
	// (the default when the gate is off) makes presence publication a no-op.
	APIEndpointRepoPathPresenceWriter EndpointPresenceWriter

	// APIEndpointRepoPathPresenceLookup answers property-keyed (repo_id, path)
	// :Endpoint presence for the handles_route readiness gate (#2809). It is
	// independent of EndpointPresenceLookup so the handles_route gate is toggled
	// solely by its own kill switch, never by the secrets/IAM flag. Nil disables
	// the gate, leaving handles_route byte-identical to its pre-#2809 behavior.
	APIEndpointRepoPathPresenceLookup EndpointPresenceLookup
}

// IncidentRoutingHandlers groups the PagerDuty incident-routing materialization
// adapters and the durable incident-to-repository correlation adapters (#2161).
type IncidentRoutingHandlers struct {
	// IncidentRoutingEvidenceLoader loads PagerDuty incident-routing packets from
	// incident.record facts, Terraform-source PagerDutyDeclaration content rows,
	// Terraform-state routing facts, and optional live PagerDuty routing facts.
	// It must be non-nil alongside IncidentRoutingEvidenceWriter to register
	// DomainIncidentRoutingMaterialization; missing either one would drop every
	// incident-routing graph materialization intent before it reaches graph truth.
	IncidentRoutingEvidenceLoader IncidentRoutingEvidenceLoader

	// IncidentRoutingEvidenceWriter projects exact PagerDuty routing evidence into
	// canonical IncidentRoutingEvidence graph nodes and evidence relationships.
	IncidentRoutingEvidenceWriter IncidentRoutingEvidenceWriter

	// AppliedPagerDutyServiceRoutingLoader loads applied PagerDuty service
	// routing facts (provider service id + Terraform backend locator) for the
	// incident-repository correlation domain. It must be non-nil alongside
	// IncidentRepositoryCorrelationWriter to register
	// DomainIncidentRepositoryCorrelation; the BackendRepositoryResolver is also
	// required so the durable backend-locator-to-repository join can run.
	AppliedPagerDutyServiceRoutingLoader AppliedPagerDutyServiceRoutingLoader

	// BackendRepositoryResolver resolves a Terraform backend locator to its
	// single owning config repository for the incident-repository correlation
	// domain. A nil resolver leaves every correlation unresolved (no durable
	// edge), so it must be wired for the domain to emit edges.
	BackendRepositoryResolver BackendRepositoryResolver

	// IncidentRepositoryCorrelationWriter persists durable
	// incident-routing-to-repository correlation decisions. It must be non-nil
	// alongside AppliedPagerDutyServiceRoutingLoader to register
	// DomainIncidentRepositoryCorrelation.
	IncidentRepositoryCorrelationWriter IncidentRepositoryCorrelationWriter
}

// CodeEvidenceHandlers groups the value-flow taint, cross-function interproc,
// and function-summary materialization adapters.
type CodeEvidenceHandlers struct {
	// CodeTaintEvidenceLoader loads resolved value-flow taint findings from
	// code_taint_evidence facts. It must be non-nil alongside
	// CodeTaintEvidenceWriter to register DomainCodeTaintEvidence; missing either
	// drops every taint-evidence intent before it reaches graph truth.
	CodeTaintEvidenceLoader CodeTaintEvidenceLoader

	// CodeTaintEvidenceWriter projects taint findings into CodeTaintEvidence graph
	// nodes attached to their Function.
	CodeTaintEvidenceWriter CodeTaintEvidenceWriter

	// CodeInterprocEvidenceLoader loads the raw code_interproc_evidence fact
	// envelopes; the handler decodes + quarantines them (Contract System v1
	// Wave 4f S2). Non-nil alongside the writer to register
	// DomainCodeInterprocEvidence.
	CodeInterprocEvidenceLoader CodeInterprocEvidenceFactLoader

	// CodeInterprocEvidenceWriter projects cross-function findings into
	// TAINT_FLOWS_TO edges between Function nodes.
	CodeInterprocEvidenceWriter CodeInterprocEvidenceWriter

	// CodeFunctionSummaryLoader loads value-flow Effects from
	// code_function_summary facts. Non-nil alongside the writer to register
	// DomainCodeFunctionSummary.
	CodeFunctionSummaryLoader CodeFunctionSummaryLoader

	// CodeFunctionSummaryWriter persists the resolved function-summary snapshot to
	// the durable store for cross-repo composition.
	CodeFunctionSummaryWriter CodeFunctionSummaryWriter

	// CodeFunctionSourceLoader loads param-level taint sources from
	// code_function_source facts. Optional; when present alongside the source
	// writer, the function-summary handler also persists sources.
	CodeFunctionSourceLoader CodeFunctionSourceLoader

	// CodeFunctionSourceWriter persists param-level taint sources to the durable
	// store for the cross-repo fixpoint.
	CodeFunctionSourceWriter CodeFunctionSourceWriter

	// CodeFunctionGraphIDLoader loads the FunctionID->graph-uid map from
	// code_function_summary facts. Optional; when present alongside the graph-id
	// writer, the function-summary handler also persists the uid map.
	CodeFunctionGraphIDLoader CodeFunctionGraphIDLoader

	// CodeFunctionGraphIDWriter persists the FunctionID->graph-uid map to the
	// durable store for the cross-repo fixpoint's TAINT_FLOWS_TO projection.
	CodeFunctionGraphIDWriter CodeFunctionGraphIDWriter

	// ValueFlowFixpointProjector projects durable value-flow fixpoint findings
	// after function summaries, sources, and graph ids are persisted.
	ValueFlowFixpointProjector ValueFlowFixpointProjector

	// CodeInterprocProjectedEdgeLedger records and enumerates source Function uids
	// of projected TAINT_FLOWS_TO edges for anchored-delete retraction.
	CodeInterprocProjectedEdgeLedger CodeInterprocProjectedEdgeLedger

	// CodeTaintEvidenceProjectedNodeLedger records and enumerates node uids of
	// projected CodeTaintEvidence nodes for anchored-delete retraction.
	CodeTaintEvidenceProjectedNodeLedger CodeTaintEvidenceProjectedNodeLedger
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/truth"

// configStateDriftDomainDefinition returns the additive DomainDefinition for
// terraform_config_state_drift. The drift domain is intentionally NOT part of
// DefaultDomainDefinitions because its handler requires three adapters
// (TerraformBackendResolver, DriftEvidenceLoader, DriftLogger) that the
// production reducer binary wires explicitly. Registering the domain without
// those adapters silently drops every intent — the additive pattern keeps the
// catalog honest about what the runtime can actually serve. See defaults.go
// (implementedDefaultDomainDefinitions) for the wiring gate.
func configStateDriftDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainConfigStateDrift,
		Summary: "correlate Terraform config (parsed HCL) against state snapshots to detect five drift kinds",
		// Durable truth surface (issue #5442): every admitted per-address
		// finding and every ambiguous-owner rejection is written as a
		// reducer_terraform_config_state_drift_finding Postgres fact (see
		// terraform_config_state_drift_writer.go), read back through
		// POST /api/v0/terraform/config-state-drift/findings and the
		// list_terraform_config_state_drift_findings MCP tool. Graph
		// projection stays deferred: this mirrors the AWS and multi-cloud
		// runtime drift domains, which are Postgres-only with graph
		// projection explicitly gated behind a separate Cypher-shape and
		// performance proof (see docs/public/reference/cypher-performance.md)
		// rather than assumed free. CanonicalWrite stays false because that
		// field means a canonical GRAPH write; it is not the durability flag
		// (this is a documentation-accuracy correction, not a functional
		// switch — CanonicalWrite's only production consumer is a Validate()
		// OR-check already satisfied by CounterEmit:true). CounterEmit
		// declares the v1 truth surface: bounded metric counters +
		// structured logs remain a parallel signal alongside the durable
		// write, not a replacement for it.
		//
		// The previous version of this comment cited "design doc §10" for
		// the graph-projection deferral. That citation was wrong when it was
		// written: §10 of docs/internal/design/391-observability-coverage-
		// correlation.md (still present, 624 lines) covers an unrelated
		// state_to_cloud_arn rule pack, not this domain's graph-projection
		// decision — commit 52d998301 deleted a different document entirely
		// (docs/superpowers/plans/2026-05-10-tfstate-config-state-drift-
		// design.md), so the deletion is not why the old citation was wrong.
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: false,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "config_state_drift",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// eshuSearchDocumentDomainDefinition returns the additive definition for the
// curated search-document projection (design 430). It is a Postgres read-model
// projection: it emits derived fact records, performs no canonical graph write,
// and exposes bounded operator counters and logs.
func eshuSearchDocumentDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainEshuSearchDocument,
		Summary: "project curated EshuSearchDocument records from indexed content for the search lane",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: false,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "eshu_search_document",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}

// codeImportRepoEdgeDomainDefinition returns the additive definition for the
// code-import repo-edge projection. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// FactLoader, package-ownership loader, and RepoDependencyIntentWriter;
// registering it without them would silently drop every intent. The domain emits
// no canonical fact of its own: it rides the shared repo-dependency projection
// lane, so its canonical surface is the DEPENDS_ON edge that lane writes
// (issue #3642).
func codeImportRepoEdgeDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainCodeImportRepoEdge,
		Summary: "project repo-to-repo DEPENDS_ON edges from per-file external import sources correlated to package-registry ownership",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: false,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "repo_dependency",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}

// packageSourceCorrelationDomainDefinition returns the additive definition for
// the package source-correlation classifier. Source hints remain provenance-only
// ownership and publication candidates, while Git manifest dependencies matched
// to package registry identity are durable consumption facts.
func packageSourceCorrelationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainPackageSourceCorrelation,
		Summary: "classify package-registry ownership, publication, and consumption correlations",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "package_source_correlation",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}

// containerImageIdentityDomainDefinition returns the additive definition for
// digest-keyed image identity decisions. The domain writes durable reducer
// facts only for digest-proven or single tag-to-digest registry observations;
// ambiguous, missing, and stale tag cases remain explicit decision facts.
func containerImageIdentityDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainContainerImageIdentity,
		Summary: "join Git, OCI registry, and runtime image references into digest-keyed image identity",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "container_image_identity",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// cicdRunCorrelationDomainDefinition returns the additive definition for
// CI/CD run correlation. The domain writes durable reducer facts for all
// outcomes, but exact canonical writes require an explicit artifact identity
// anchor rather than CI success or shell text.
func cicdRunCorrelationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainCICDRunCorrelation,
		Summary: "correlate CI/CD runs, artifacts, and environments with artifact identity evidence",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "ci_cd_run_correlation",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// serviceCatalogCorrelationDomainDefinition returns the additive definition for
// service-catalog correlation. The domain writes durable reducer facts for all
// outcomes, while catalog names, owners, and declared dependencies remain
// provenance until repository or stronger runtime evidence corroborates them.
func serviceCatalogCorrelationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainServiceCatalogCorrelation,
		Summary: "correlate service-catalog entities with repository and ownership evidence",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "service_catalog_correlation",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}

// sbomAttestationAttachmentDomainDefinition returns the additive definition for
// SBOM and attestation attachment. The domain writes durable reducer facts for
// all outcomes, but canonical attachment requires an explicit digest subject.
func sbomAttestationAttachmentDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainSBOMAttestationAttachment,
		Summary: "attach SBOM and attestation evidence to image digests when subject evidence is explicit",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "sbom_attestation_attachment",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// supplyChainImpactDomainDefinition returns the additive definition for
// vulnerability impact findings. The domain writes durable reducer facts for
// all statuses and keeps CVSS, EPSS, KEV, package, runtime, and deployment
// signals separate so callers can see missing evidence.
func supplyChainImpactDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainSupplyChainImpact,
		Summary: "publish reducer-owned vulnerability impact findings with explicit evidence paths",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "supply_chain_impact",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// awsCloudRuntimeDriftDomainDefinition returns the additive definition for
// AWS runtime drift publication. The domain consumes admitted
// aws_cloud_runtime_drift candidates and writes durable reducer facts, but it
// deliberately does not declare graph writes until the drift node and query
// surface shape are frozen in the active ADR.
func awsCloudRuntimeDriftDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainAWSCloudRuntimeDrift,
		Summary: "publish admitted AWS runtime orphan and unmanaged drift findings as canonical reducer facts",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "aws_cloud_runtime_drift",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerAppliedDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// multiCloudRuntimeDriftDomainDefinition returns the additive definition for the
// provider-neutral runtime drift publication path (issues #1997, #1998). The
// domain consumes admitted multi_cloud_runtime_drift candidates keyed on
// canonical cloud_resource_uid and writes durable reducer facts for AWS, GCP, and
// Azure through one drift path. Like the AWS drift domain it deliberately does
// not declare graph writes until the drift node and query surface shape are
// frozen in the active ADR.
func multiCloudRuntimeDriftDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainMultiCloudRuntimeDrift,
		Summary: "publish admitted multi-cloud runtime orphan, unmanaged, ambiguous, and unknown drift findings as canonical reducer facts",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "multi_cloud_runtime_drift",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerAppliedDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// cloudInventoryAdmissionDomainDefinition returns the additive definition for
// the shared multi-cloud inventory identity admission path. The domain consumes
// aws_resource, gcp_cloud_resource, and azure_cloud_resource source facts and
// writes durable reducer-owned canonical CloudResource identity facts, but it
// deliberately does not declare graph writes: canonical node/edge projection and
// the multi-cloud drift join are deferred follow-ups (issues #1997, #1998).
func cloudInventoryAdmissionDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainCloudInventoryAdmission,
		Summary: "admit provider cloud-inventory facts into the shared canonical cloud_resource_uid keyspace",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "cloud_resource_identity",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerAppliedDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// observabilityCoverageCorrelationDomainDefinition returns the additive
// definition for observability coverage correlation. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// FactLoader and ObservabilityCoverageCorrelationWriter; registering it without
// them would silently drop every intent. The domain writes durable reducer facts
// for all outcomes and stays graph-neutral: coverage edges remain provenance
// until an exact uid/ARN match proves them, and PR1 writes no graph edge at all.
// See issue #391 for the design and the six-outcome correlation contract.
func observabilityCoverageCorrelationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainObservabilityCoverageCorrelation,
		Summary: "correlate observability coverage of monitored cloud resources versus uncovered gaps",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "observability_coverage_correlation",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerAppliedDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// kubernetesCorrelationDomainDefinition returns the additive definition for
// live Kubernetes correlation. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// FactLoader and KubernetesCorrelationWriter; registering it without them would
// silently drop every intent. The domain writes durable reducer facts for all
// outcomes and stays graph-neutral: derived image links and ambiguous selector
// edges remain provenance until an exact digest/owner-reference match proves
// them, and PR1 writes no graph edge at all. It correlates the live observed
// cluster (LayerObservedResource) against deployment-source declarations
// (LayerSourceDeclaration). See issue #388 for the design and the six-outcome
// correlation contract.
func kubernetesCorrelationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainKubernetesCorrelation,
		Summary: "correlate live Kubernetes workloads to deployment-source image and identity evidence with drift classification",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "kubernetes_correlation",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

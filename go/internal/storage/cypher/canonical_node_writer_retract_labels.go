// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// --- Phase A: Retract stale nodes ---
//
// These label sets group the entity labels the canonical retract phase scans by
// source domain (code, infra, terraform, cloudformation, sql, data, oci,
// package_registry). They live in a sibling file to keep canonical_node_writer.go
// under the repository file-size cap.

const (
	canonicalNodeRefreshFilePathBatchSize          = 100
	canonicalNodeRefreshEntityContainmentBatchSize = 50
)

var canonicalNodeRetractCodeEntityLabels = map[string]struct{}{
	"Function":               {},
	"Class":                  {},
	"Variable":               {},
	"Interface":              {},
	"Trait":                  {},
	"Struct":                 {},
	"Enum":                   {},
	"Macro":                  {},
	"Union":                  {},
	"Record":                 {},
	"Property":               {},
	"Annotation":             {},
	"Typedef":                {},
	"TypeAlias":              {},
	"TypeAnnotation":         {},
	"Component":              {},
	"ImplBlock":              {},
	"Protocol":               {},
	"ProtocolImplementation": {},
	"ShellCommand":           {},
}

var canonicalNodeRetractInfraEntityLabels = map[string]struct{}{
	"K8sResource":           {},
	"ArgoCDApplication":     {},
	"ArgoCDApplicationSet":  {},
	"AtlantisProject":       {},
	"AtlantisWorkflow":      {},
	"GitlabPipeline":        {},
	"GitlabJob":             {},
	"CrossplaneXRD":         {},
	"CrossplaneComposition": {},
	// CrossplaneClaim is retained here solely to reap legacy nodes: no writer
	// has emitted this label since #5347 (a Claim is edge-only -- it stays a
	// K8sResource node and the SATISFIED_BY edge to its CrossplaneXRD is the
	// classification), but a graph provisioned before #5347 can still hold
	// nodes carrying the literal CrossplaneClaim label. On a full
	// reconciliation generation the Claim re-projects as a K8sResource node
	// plus a SATISFIED_BY edge in the current generation, and this entry lets
	// the retract phase DETACH DELETE the stale CrossplaneClaim node from the
	// prior generation. Dropping this entry would orphan those legacy nodes
	// forever for any deployment that upgrades straight from a pre-#5347
	// binary to a post-#5478 one without an intermediate release that still
	// retracted the label (issue #5478).
	"CrossplaneClaim":        {},
	"KustomizeOverlay":       {},
	"FluxKustomization":      {},
	"FluxGitRepository":      {},
	"FluxOCIRepository":      {},
	"FluxBucket":             {},
	"FluxHelmRelease":        {},
	"FluxHelmRepository":     {},
	"HelmChart":              {},
	"HelmValues":             {},
	"HelmValueDefinition":    {},
	"HelmTemplateValueUsage": {},
}

var canonicalNodeRetractTerraformEntityLabels = map[string]struct{}{
	"TerraformResource":     {},
	"TerraformModule":       {},
	"TerraformVariable":     {},
	"TerraformOutput":       {},
	"TerraformDataSource":   {},
	"TerraformProvider":     {},
	"TerraformLocal":        {},
	"TerraformBackend":      {},
	"TerraformImport":       {},
	"TerraformMovedBlock":   {},
	"TerraformRemovedBlock": {},
	"TerraformCheck":        {},
	"TerraformLockProvider": {},
	"TerragruntConfig":      {},
	"TerragruntDependency":  {},
	"TerragruntInput":       {},
	"TerragruntLocal":       {},
}

var canonicalNodeRetractCloudFormationEntityLabels = map[string]struct{}{
	"CloudFormationResource":  {},
	"CloudFormationParameter": {},
	"CloudFormationOutput":    {},
}

var canonicalNodeRetractSQLEntityLabels = map[string]struct{}{
	"SqlTable":     {},
	"SqlView":      {},
	"SqlFunction":  {},
	"SqlTrigger":   {},
	"SqlIndex":     {},
	"SqlColumn":    {},
	"SqlMigration": {},
}

var canonicalNodeRetractDataEntityLabels = map[string]struct{}{
	"DataAsset":        {},
	"DataColumn":       {},
	"AnalyticsModel":   {},
	"DashboardAsset":   {},
	"DataQualityCheck": {},
	"QueryExecution":   {},
	"DataContract":     {},
	"DataOwner":        {},
}

var canonicalNodeRetractOCIEntityLabels = map[string]struct{}{
	"ContainerImage":               {},
	"ContainerImageDescriptor":     {},
	"ContainerImageIndex":          {},
	"ContainerImageTagObservation": {},
	"OciImageDescriptor":           {},
	"OciImageIndex":                {},
	"OciImageManifest":             {},
	"OciImageReferrer":             {},
	"OciImageTagObservation":       {},
	"OciRegistryRepository":        {},
}

var canonicalNodeRetractPackageRegistryEntityLabels = map[string]struct{}{
	"Package":                          {},
	"PackageDependency":                {},
	"PackageRegistryPackage":           {},
	"PackageRegistryPackageDependency": {},
	"PackageRegistryPackageVersion":    {},
	"PackageVersion":                   {},
}

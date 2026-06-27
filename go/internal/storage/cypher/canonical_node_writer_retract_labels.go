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
	"CrossplaneClaim":       {},
	"KustomizeOverlay":      {},
	"HelmChart":             {},
	"HelmValues":            {},
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
	"SqlTable":    {},
	"SqlView":     {},
	"SqlFunction": {},
	"SqlTrigger":  {},
	"SqlIndex":    {},
	"SqlColumn":   {},
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

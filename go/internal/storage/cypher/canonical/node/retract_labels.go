// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package node

import (
	"sort"
)

// --- Phase A: Retract stale nodes ---
//
// These label sets group the entity labels the canonical retract phase scans by
// source domain (code, infra, terraform, cloudformation, sql, data, oci,
// package_registry). They live in a sibling file to keep canonical_node_writer.go
// under the repository file-size cap.

const (
	RefreshFilePathBatchSize          = 100
	RefreshEntityContainmentBatchSize = 50
)

var RetractCodeEntityLabels = map[string]struct{}{
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

var RetractInfraEntityLabels = map[string]struct{}{
	"K8sResource":          {},
	"ArgoCDApplication":    {},
	"ArgoCDApplicationSet": {},
	"AtlantisProject":      {},
	"AtlantisWorkflow":     {},
	"GitlabPipeline":       {},
	"GitlabJob":            {},
	"DockerfileInstall":    {},
	// Incident-declaration content entity (#5954); retracted with the other
	// declared-infrastructure labels so a delta projection does not strand it.
	"PagerDutyDeclaration":  {},
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

var RetractTerraformEntityLabels = map[string]struct{}{
	"TerraformResource":     {},
	"TerraformBlock":        {},
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

var RetractCloudFormationEntityLabels = map[string]struct{}{
	"CloudFormationResource":  {},
	"CloudFormationParameter": {},
	"CloudFormationOutput":    {},
	// Extended CloudFormation labels (#5954). A label that materializes but is
	// missing here is never retracted, so a delta or reconciliation projection
	// leaves the previous generation's nodes behind and they accumulate as
	// orphans -- the mirror of the silent-skip that kept them out of the graph
	// in the first place.
	"CloudFormationCondition": {},
	"CloudFormationImport":    {},
	"CloudFormationExport":    {},
}

var RetractSQLEntityLabels = map[string]struct{}{
	"SqlTable":     {},
	"SqlView":      {},
	"SqlFunction":  {},
	"SqlTrigger":   {},
	"SqlIndex":     {},
	"SqlColumn":    {},
	"SqlMigration": {},
}

var RetractDataEntityLabels = map[string]struct{}{
	"DataAsset":        {},
	"DataColumn":       {},
	"AnalyticsModel":   {},
	"DashboardAsset":   {},
	"DataQualityCheck": {},
	"QueryExecution":   {},
	"DataContract":     {},
	"DataOwner":        {},
}

var RetractOCIEntityLabels = map[string]struct{}{
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

var RetractPackageRegistryEntityLabels = map[string]struct{}{
	"Package":                          {},
	"PackageDependency":                {},
	"PackageRegistryPackage":           {},
	"PackageRegistryPackageDependency": {},
	"PackageRegistryPackageVersion":    {},
	"PackageVersion":                   {},
}

// RetractEntityLabels returns the sorted union of every entity label family
// the canonical retract phase scans. It lives here beside the family sets it
// unions so a label that materializes but is missing from every family is
// caught at the source: delta and reconciliation projections would otherwise
// strand the previous generation's nodes.
func RetractEntityLabels() []string {
	labels := make(map[string]struct{})
	for _, family := range []map[string]struct{}{
		RetractCodeEntityLabels,
		RetractInfraEntityLabels,
		RetractTerraformEntityLabels,
		RetractCloudFormationEntityLabels,
		RetractSQLEntityLabels,
		RetractDataEntityLabels,
		RetractOCIEntityLabels,
		RetractPackageRegistryEntityLabels,
	} {
		for label := range family {
			labels[label] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(labels))
	for label := range labels {
		sorted = append(sorted, label)
	}
	sort.Strings(sorted)
	return sorted
}

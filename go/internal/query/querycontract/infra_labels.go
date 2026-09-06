// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package querycontract owns the infrastructure node-label taxonomy shared by
// the root query package and the impact/ subpackage for #6060. The label
// list moved here from infra.go so impact/resource-investigation reads can
// gate interpolated Cypher labels without importing the root package.
package querycontract

// AllInfraLabels enumerates every node label the infrastructure search
// surface may match. It is the single home for the taxonomy; the root
// package aliases it as allInfraLabels.
var AllInfraLabels = []string{
	"CloudResource",
	"K8sResource",
	"KustomizeOverlay",
	"TerraformResource",
	"TerraformStateResource",
	"TerraformModule",
	"TerraformVariable",
	"TerraformOutput",
	"TerraformDataSource",
	"TerraformProvider",
	"TerraformLocal",
	"TerraformBackend",
	"TerraformImport",
	"TerraformMovedBlock",
	"TerraformRemovedBlock",
	"TerraformCheck",
	"TerraformLockProvider",
	"TerraformBlock",
	"TerragruntConfig",
	"TerragruntDependency",
	"CloudFormationResource",
	"ArgoCDApplication",
	"ArgoCDApplicationSet",
	"CrossplaneXRD",
	"CrossplaneComposition",
	"HelmChart",
	"HelmValues",
}

// infraLabelSet indexes AllInfraLabels for O(1) membership tests.
var infraLabelSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(AllInfraLabels))
	for _, label := range AllInfraLabels {
		set[label] = struct{}{}
	}
	return set
}()

// InfraLabelAllowed reports whether label is a known infrastructure node
// label. It gates any label interpolated into a Cypher pattern so the label
// text is never attacker-influenced.
func InfraLabelAllowed(label string) bool {
	_, ok := infraLabelSet[label]
	return ok
}

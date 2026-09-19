// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

// Labels is the closed set of entity-derived infrastructure labels mirrored
// into infra_resource_entities. Each one is written to the canonical graph only
// by the source-local canonical node writer, from a content_entities row whose
// entity_type equals the label, with the dimension properties promoted
// verbatim from the entity metadata (storage/cypher/canonical_node_writer_*.go).
// That single-writer property is what makes the table's per-label and
// per-dimension counts equal the graph's.
//
// Labels NOT listed here stay graph-only, and the query layer keeps reading
// them from the graph:
//
//   - CloudResource and TerraformStateResource have no content_entities row;
//     other collectors write them.
//   - TerraformModule and TerraformOutput have a second writer, the Terraform
//     state projector (storage/cypher/tfstate_canonical_writer.go), which
//     MERGEs nodes under those labels with no content_entities row. Mirroring
//     only the content-derived subset would undercount them.
//
// The query package pins this list against querycontract.AllInfraLabels so a
// taxonomy change fails a test instead of silently dropping a label.
var Labels = []string{
	"K8sResource",
	"KustomizeOverlay",
	"TerraformResource",
	"TerraformVariable",
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

// dimensionColumns are the metadata keys copied into same-named columns. They
// are every node property the infra aggregate readers filter or group on.
var dimensionColumns = []string{
	"kind",
	"resource_type",
	"data_type",
	"provider",
	"environment",
	"resource_service",
	"resource_category",
	"service_kind",
}

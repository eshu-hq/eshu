// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

// Labels is the closed set of infrastructure labels mirrored into
// infra_resource_entities. The canonical node writer writes each of them from a
// content_entities row whose entity_type equals the label, with the dimension
// properties promoted verbatim from the entity metadata
// (storage/cypher/canonical_node_writer_*.go). The table therefore holds
// exactly the content-derived nodes of each label, with the same values.
//
// For most labels the canonical node writer is the only writer, so the table
// holds every node. TerraformModule and TerraformOutput have a second writer,
// the Terraform state projector (storage/cypher/tfstate_canonical_writer.go),
// which MERGEs nodes with evidence_source 'projector/tfstate' and no content
// row. The query layer reads those few nodes from the graph through an indexed
// evidence_source seek and adds them to the table's counts.
//
// storage/cypher/canonical_kustomize_edges.go also MERGEs KustomizeOverlay by
// uid, but only to set base_refs on overlays of the same materialization, whose
// nodes its entities phase already created from content rows, so it never adds
// a node the table lacks.
//
// CloudResource and TerraformStateResource are not listed: they have no
// content_entities row, because other collectors write them, and the query
// layer reads them from the graph.
//
// The query package pins this list against querycontract.AllInfraLabels so a
// taxonomy change fails a test instead of silently dropping a label.
var Labels = []string{
	"K8sResource",
	"KustomizeOverlay",
	"TerraformResource",
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

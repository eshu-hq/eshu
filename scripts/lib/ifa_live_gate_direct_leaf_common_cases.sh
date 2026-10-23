#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2034
# Slice C/D live-leaf common seams (#6228), split out of
# scripts/lib/ifa_live_gate_selector_cases.sh when that file reached 507 of
# the blocking 500-line cap. Fourth split, same mechanism as the
# negative/determinism-only/fault-only splits above it: variable-path source
# plus an rg pin in the parent, because internal/cigates/scripttrigger.go's
# sourced-to-triggered drift walk cannot resolve the path, so nothing else
# would notice the source line being deleted. Unlike those three splits this
# module APPENDS to the parent's ifa_live_gate_common_seams array instead of
# declaring its own: the entries are common (both live gates) by the same
# per-path-type division that put them in the common table, and a second
# array would need a second consumer loop for no additional precision. A
# deleted source leaves the registry lockstep loop iterating without these
# eight seams, which is exactly what the parent's rg pin fails closed on.

# built_from (#6228 slice C live leaf): writer, cassette, expected-edge set,
# and cells are new common seams beside the siblings; the Odù stays in the
# determinism-only table (the fault cells drive the committed cassette JSON,
# never the Odù source).
ifa_live_gate_common_seams+=(
	'go/internal/storage/cypher/edge/writer/provenance.go|go/internal/storage/cypher/edge/writer/provenance.go'
	'testdata/cassettes/builtfrom/**|testdata/cassettes/builtfrom/ifa-built-from-family.json'
	'go/internal/ifa/testdata/builtfrom/**|go/internal/ifa/testdata/builtfrom/ifa-built-from-family-expected-edges.json'
	'scripts/lib/ifa_fault_injection_built_from_cells.sh|scripts/lib/ifa_fault_injection_built_from_cells.sh'
)
# cloud_resource_container_image (#6228 slice D live leaf): writer,
# cassette, expected-edge set, and cells are new common seams beside the
# siblings; the Odù stays in the determinism-only table (the fault cells
# drive the committed cassette JSON, never the Odù source).
ifa_live_gate_common_seams+=(
	'go/internal/storage/cypher/cloud/aws/image/container_edge_writer.go|go/internal/storage/cypher/cloud/aws/image/container_edge_writer.go'
	'testdata/cassettes/cloudresourcecontainerimage/**|testdata/cassettes/cloudresourcecontainerimage/ifa-cloud-resource-container-image-family.json'
	'go/internal/ifa/testdata/cloudresourcecontainerimage/**|go/internal/ifa/testdata/cloudresourcecontainerimage/ifa-cloud-resource-container-image-family-expected-edges.json'
	'scripts/lib/ifa_fault_injection_cloud_resource_container_image_cells.sh|scripts/lib/ifa_fault_injection_cloud_resource_container_image_cells.sh'
)
# crossplane_satisfied_by (#6228 crossplane live leaf): writer,
# cassette, expected-edge set, and cells are new common seams beside the
# siblings; the Odù stays in the determinism-only table (the fault cells
# drive the committed cassette JSON, never the Odù source).
ifa_live_gate_common_seams+=(
	'go/internal/storage/cypher/canonical/crossplane/edges.go|go/internal/storage/cypher/canonical/crossplane/edges.go'
	'testdata/cassettes/crossplanesatisfiedby/**|testdata/cassettes/crossplanesatisfiedby/ifa-crossplane-satisfied-by-family.json'
	'go/internal/ifa/testdata/crossplanesatisfiedby/**|go/internal/ifa/testdata/crossplanesatisfiedby/ifa-crossplane-satisfied-by-family-expected-edges.json'
	'scripts/lib/ifa_fault_injection_crossplane_satisfied_by_cells.sh|scripts/lib/ifa_fault_injection_crossplane_satisfied_by_cells.sh'
)
# derived_from + code_interproc_evidence + code_taint_evidence (#6228
# derived_from, code_interproc_evidence, and code_taint_evidence live
# leaves): writer,
# cassette, expected-edge set, and cells are new common seams beside the
# siblings; the Odù stays in the determinism-only table (the fault cells
# drive the committed cassette JSON, never the Odù source).
ifa_live_gate_common_seams+=(
	'go/internal/storage/cypher/edge/writer/derived_from.go|go/internal/storage/cypher/edge/writer/derived_from.go'
	'testdata/cassettes/derivedfrom/**|testdata/cassettes/derivedfrom/ifa-derived-from-family.json'
	'go/internal/ifa/testdata/derivedfrom/**|go/internal/ifa/testdata/derivedfrom/ifa-derived-from-family-expected-edges.json'
	'scripts/lib/ifa_fault_injection_derived_from_cells.sh|scripts/lib/ifa_fault_injection_derived_from_cells.sh'
)
# observability_coverage (#6228 observability_coverage live leaf): writer,
# cassette, expected-edge set, and cells are new common seams beside the
# siblings; the Odù stays in the determinism-only table (the fault cells
# drive the committed cassette JSON, never the Odù source).
ifa_live_gate_common_seams+=(
	'go/internal/storage/cypher/edge/writer/observability_coverage.go|go/internal/storage/cypher/edge/writer/observability_coverage.go'
	'testdata/cassettes/obscoverage/**|testdata/cassettes/obscoverage/ifa-observability-coverage-family.json'
	'go/internal/ifa/testdata/obscoverage/**|go/internal/ifa/testdata/obscoverage/ifa-observability-coverage-family-expected-edges.json'
	'scripts/lib/ifa_fault_injection_observability_coverage_cells.sh|scripts/lib/ifa_fault_injection_observability_coverage_cells.sh'
)
ifa_live_gate_common_seams+=(
	'go/internal/storage/cypher/edge/writer/code_interproc_evidence.go|go/internal/storage/cypher/edge/writer/code_interproc_evidence.go'
	'testdata/cassettes/codeinterproc/**|testdata/cassettes/codeinterproc/ifa-code-interproc-evidence-family.json'
	'go/internal/ifa/testdata/codeinterproc/**|go/internal/ifa/testdata/codeinterproc/ifa-code-interproc-evidence-family-expected-edges.json'
	'scripts/lib/ifa_fault_injection_code_interproc_evidence_cells.sh|scripts/lib/ifa_fault_injection_code_interproc_evidence_cells.sh'
)
ifa_live_gate_common_seams+=(
	'go/internal/storage/cypher/edge/writer/code_taint_evidence.go|go/internal/storage/cypher/edge/writer/code_taint_evidence.go'
	'testdata/cassettes/codetaint/**|testdata/cassettes/codetaint/ifa-code-taint-evidence-family.json'
	'go/internal/ifa/testdata/codetaint/**|go/internal/ifa/testdata/codetaint/ifa-code-taint-evidence-family-expected-edges.json'
	'scripts/lib/ifa_fault_injection_code_taint_evidence_cells.sh|scripts/lib/ifa_fault_injection_code_taint_evidence_cells.sh'
)
# incident_routing_evidence (#6228 incident_routing_evidence live leaf):
# writer, cassette, expected-edge set, and cells are new common seams
# beside the siblings; the Odù stays in the determinism-only table (the
# fault cells drive the committed cassette JSON, never the Odù source).
ifa_live_gate_common_seams+=(
	'go/internal/storage/cypher/edge/writer/incident_routing_evidence.go|go/internal/storage/cypher/edge/writer/incident_routing_evidence.go'
	'testdata/cassettes/incidentrouting/**|testdata/cassettes/incidentrouting/ifa-incident-routing-evidence-family.json'
	'go/internal/ifa/testdata/incidentroutingevidence/**|go/internal/ifa/testdata/incidentroutingevidence/ifa-incident-routing-evidence-family-expected-edges.json'
	'scripts/lib/ifa_fault_injection_incident_routing_evidence_cells.sh|scripts/lib/ifa_fault_injection_incident_routing_evidence_cells.sh'
)

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "go.opentelemetry.io/otel/trace"

// This file holds CanonicalNodeWriter's With* builder options, split out of
// canonical_node_writer.go to stay under the repo's 500-line file cap. See
// canonical_node_writer.go for the struct definition, NewCanonicalNodeWriter,
// and Write.

// WithTracer records canonical graph-write spans when tracing is configured.
func (w *CanonicalNodeWriter) WithTracer(tracer trace.Tracer) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.tracer = tracer
	return w
}

// WithEntityBatchSize overrides the per-statement row batch size used only for
// canonical entity upserts. Other canonical phases keep the writer's default
// batch size.
func (w *CanonicalNodeWriter) WithEntityBatchSize(batchSize int) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	if batchSize > 0 {
		w.entityBatchSize = batchSize
	}
	return w
}

// WithTerraformStateOwnershipResolver injects the port used to scope the
// #5443 MATCHES_STATE edge to the config repository that owns a Terraform
// state resource's backend. Optional: a nil resolver (the default) means no
// MATCHES_STATE edges are written and every TerraformStateResource node's
// config_repo_id property stays null, which is a safe, honest "ownership not
// resolved" state rather than a wrong match. See tfstate_state_match_edge.go.
func (w *CanonicalNodeWriter) WithTerraformStateOwnershipResolver(resolver TerraformStateOwnershipResolver) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.tfStateOwnershipResolver = resolver
	return w
}

// WithTerraformStateConfigMatchResolver injects the port used to detect
// whether a #5443 MATCHES_STATE edge candidate is ambiguous: (repo_id, name)
// carries no uniqueness constraint (tf_resource_unique is (name, path,
// line_number)), so two Terraform roots in one monorepo can both declare the
// same address. Optional: a nil resolver (the default) leaves
// ConfigMatchAmbiguous at its zero value for every row, matching every unit
// test in this package that constructs rows directly without exercising
// resolver wiring. Every cmd/* canonical-writer wiring site (cmd/projector,
// cmd/ingester, cmd/bootstrap-index) always wires a real resolver, so this
// default only affects hand-built test fixtures, never a production write
// path. See tfstate_state_match_edge.go.
func (w *CanonicalNodeWriter) WithTerraformStateConfigMatchResolver(resolver TerraformStateConfigMatchResolver) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.tfStateConfigMatchResolver = resolver
	return w
}

// WithKustomizeOverlayResolver injects the port used to rebuild the #5445
// EXTENDS_BASE edge set for a repo's full KustomizeOverlay set every cycle
// that touches or delta-deletes any of them. Optional: a nil resolver (the
// default) means the base_refs node property still gets written for touched
// overlays, but no EXTENDS_BASE edge is ever written or retracted -- a safe,
// honest "edge rebuild not resolved" state rather than a wrong or partial
// edge set. See KustomizeOverlayResolver and kustomizeExtendsBaseEdgeStatements
// (canonical_kustomize_edges.go).
func (w *CanonicalNodeWriter) WithKustomizeOverlayResolver(resolver KustomizeOverlayResolver) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.kustomizeOverlayResolver = resolver
	return w
}

// WithFileBatchSize overrides the per-statement row batch size used only for
// canonical file upserts. Other canonical phases keep the writer's default
// batch size.
func (w *CanonicalNodeWriter) WithFileBatchSize(batchSize int) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	if batchSize > 0 {
		w.fileBatchSize = batchSize
	}
	return w
}

// WithEntityLabelBatchSize overrides the per-statement row batch size for one
// canonical entity label while leaving other entity labels on the default
// entity batch size.
func (w *CanonicalNodeWriter) WithEntityLabelBatchSize(label string, batchSize int) *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	if label == "" || batchSize <= 0 {
		return w
	}
	if w.entityLabelBatchSizes == nil {
		w.entityLabelBatchSizes = make(map[string]int)
	}
	w.entityLabelBatchSizes[label] = batchSize
	return w
}

// WithEntityContainmentInEntityUpsert keeps entity node and file containment
// writes in the same statement. Use only for backends whose batch MERGE support
// requires the file MATCH context to preserve row-bound entity identity.
func (w *CanonicalNodeWriter) WithEntityContainmentInEntityUpsert() *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.entityContainmentInEntityUpsert = true
	w.entityContainmentBatchAcrossFiles = false
	return w
}

// WithBatchedEntityContainmentInEntityUpsert keeps entity node and containment
// writes in one MERGE-first batch whose rows carry file_path. Use only with
// backends that have proven row-safe `SET += row.props` support in the
// generalized UNWIND/MERGE hot path.
func (w *CanonicalNodeWriter) WithBatchedEntityContainmentInEntityUpsert() *CanonicalNodeWriter {
	if w == nil {
		return nil
	}
	w.entityContainmentInEntityUpsert = true
	w.entityContainmentBatchAcrossFiles = true
	return w
}

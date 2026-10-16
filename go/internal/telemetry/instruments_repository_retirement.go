// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Closed outcome values for eshu_dp_canonical_repository_retirements_total
// (#7324). The canonical writer records one of them only when its
// path-conflict retirement statement deleted a Repository node.
const (
	// RepositoryRetirementOutcomeClean means the retired Repository carried
	// no relationships, so the retirement dropped nothing.
	RepositoryRetirementOutcomeClean = "clean"
	// RepositoryRetirementOutcomeDroppedRelationships means the backend
	// reported at least one relationship deleted with the retired node. The
	// count covers both directions, including the retired node's own
	// projector edges; direction and type are not attributed.
	RepositoryRetirementOutcomeDroppedRelationships = "dropped_relationships"
)

// registerCanonicalRepositoryRetirements registers the path-conflict
// Repository retirement counter (#7324), the retired-relationship split
// counter (#7324 item: per-direction/per-type split), and the Repository
// stub-creation counter (#7446) on inst.
func registerCanonicalRepositoryRetirements(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.CanonicalRepositoryRetirements, err = meter.Int64Counter(
		"eshu_dp_canonical_repository_retirements_total",
		metric.WithDescription("Different-id Repository nodes the canonical writer retired at a re-projected path, by outcome (clean, dropped_relationships) (#7324)"),
	); err != nil {
		return fmt.Errorf("register CanonicalRepositoryRetirements counter: %w", err)
	}
	if inst.CanonicalRepositoryRetiredRelationships, err = meter.Int64Counter(
		"eshu_dp_canonical_repository_retired_relationships_total",
		metric.WithDescription("Relationships the canonical writer's path-conflict retirement dropped with the retired Repository, by closed direction (incoming, outgoing) and owner class (reducer, projector, other) (#7324)"),
	); err != nil {
		return fmt.Errorf("register CanonicalRepositoryRetiredRelationships counter: %w", err)
	}
	if inst.CanonicalRepositoryStubsCreated, err = meter.Int64Counter(
		"eshu_dp_canonical_repository_stubs_created_total",
		metric.WithDescription("Repository nodes a shared-edge writer MERGE-created by id because no node held that id, by writer (repo_dependency, submodule_pin) (#7446)"),
	); err != nil {
		return fmt.Errorf("register CanonicalRepositoryStubsCreated counter: %w", err)
	}
	return nil
}

// Closed writer values for eshu_dp_canonical_repository_stubs_created_total
// (#7446): the shared-edge writers whose upsert MERGEs a Repository by id.
const (
	// RepositoryStubWriterRepoDependency is the repo_dependency domain's
	// upserts (DEPENDS_ON and the typed repository relationships).
	RepositoryStubWriterRepoDependency = "repo_dependency"
	// RepositoryStubWriterSubmodulePin is the PINS_SUBMODULE edge upsert.
	RepositoryStubWriterSubmodulePin = "submodule_pin"
)

// AttrWriter returns a writer attribute naming the closed shared-edge writer
// that recorded a Repository stub creation.
func AttrWriter(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionWriter, v)
}

// Closed direction values for
// eshu_dp_canonical_repository_retired_relationships_total (#7324 item:
// per-direction split): the dropped edge's direction from the retired node.
const (
	// RetiredRelationshipDirectionIncoming means the retired node is the
	// edge's end node.
	RetiredRelationshipDirectionIncoming = "incoming"
	// RetiredRelationshipDirectionOutgoing means the retired node is the
	// edge's start node.
	RetiredRelationshipDirectionOutgoing = "outgoing"
)

// Closed owner-class values for
// eshu_dp_canonical_repository_retired_relationships_total (#7324 item:
// per-type split), derived from the dropped edge's evidence_source. The raw
// relationship type never becomes a label: its open-ended set would blow up
// the series count, so it stays in the log's capped per-type map.
const (
	// RetiredRelationshipClassProjector means the edge's evidence_source
	// starts with projector/ (the retired node's own projector edges).
	RetiredRelationshipClassProjector = "projector"
	// RetiredRelationshipClassReducer means the edge carries a present
	// evidence_source outside projector/ (reducer, resolver, and other
	// writers' edges, which nothing re-arms).
	RetiredRelationshipClassReducer = "reducer"
	// RetiredRelationshipClassOther means the edge carries no
	// evidence_source.
	RetiredRelationshipClassOther = "other"
)

// AttrDirection returns a direction attribute naming the closed direction
// of a dropped edge from the retired Repository.
func AttrDirection(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionDirection, v)
}

// AttrEdgeClass returns an edge-class attribute naming the closed owner
// family of a dropped edge.
func AttrEdgeClass(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionEdgeClass, v)
}

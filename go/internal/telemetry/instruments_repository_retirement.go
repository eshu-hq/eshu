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
// Repository retirement counter (#7324) and the Repository stub-creation
// counter (#7446) on inst.
func registerCanonicalRepositoryRetirements(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.CanonicalRepositoryRetirements, err = meter.Int64Counter(
		"eshu_dp_canonical_repository_retirements_total",
		metric.WithDescription("Different-id Repository nodes the canonical writer retired at a re-projected path, by outcome (clean, dropped_relationships) (#7324)"),
	); err != nil {
		return fmt.Errorf("register CanonicalRepositoryRetirements counter: %w", err)
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

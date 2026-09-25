// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"fmt"
	"slices"
	"strings"
)

// neo4jRetiredUniqueConstraints names the Neo4j uniqueness constraints whose
// key was narrower than the canonical uid identity (#7095). The canonical
// writer MERGEs these labels on uid, and uid hashes repo, path, entity type,
// name, and start line. It upserts a moved block's new uid before
// entity_retract deletes the prior generation's node, so a key on (name, path)
// or (path) alone saw two nodes and failed the delta with
// ConstraintValidationFailed. Uniqueness for these labels rests on
// <label>_uid_unique, as it already did on NornicDB for the composite forms
// (nornicDBSchemaConstraint); #7097 retired the three single-property ones
// there too (nornicDBRetiredUniqueConstraints).
//
// The CREATE statements stay in schemaConstraints; each dialect filters the
// ones it retires out of the emitted list.
var neo4jRetiredUniqueConstraints = []string{
	"kustomize_unique",
	"helm_chart_unique",
	"helm_values_unique",
	"tf_module_unique",
	"tg_config_unique",
}

// nornicDBRetiredUniqueConstraints names the NornicDB uniqueness constraints
// retired for the same reason as neo4jRetiredUniqueConstraints (#7097).
// NornicDB never created the composite tf_module_unique and helm_chart_unique
// (nornicDBSchemaConstraint drops the composite form), so only the three
// single-property path constraints narrowed the uid identity. NornicDB reports
// the resulting UNIQUE violation as Neo.TransientError.Transaction.Outdated, so
// the work item retried instead of dead-lettering.
//
// Unlike Neo4j, no replacement path index is added: on the pinned NornicDB the
// delta entity retract filters repo_id, evidence_source, path IN $file_paths
// and generation_id together, and that shape is a label scan with or without a
// path index or constraint. NornicDB does seek a path index for path = $p and
// for IN alone, so the retract is a pre-existing gap, not an unanchorable
// shape. An index would cost writes and a backfill for no read today
// (evidence 7097).
var nornicDBRetiredUniqueConstraints = []string{
	"kustomize_unique",
	"helm_values_unique",
	"tg_config_unique",
}

// retiredConstraintDrops removes the named constraints from a store that an
// older bootstrap already created them on. IF EXISTS keeps it idempotent on a
// fresh store and on every later bootstrap.
func retiredConstraintDrops(names []string) []string {
	drops := make([]string, 0, len(names))
	for _, name := range names {
		drops = append(drops, fmt.Sprintf("DROP CONSTRAINT %s IF EXISTS", name))
	}
	return drops
}

// neo4jRetiredConstraintPathIndexes keep the reads the dropped path
// constraints served. The canonical delta entity retract
// (canonicalNodeRetractDeltaEntityTemplate) filters `n.path IN $file_paths`,
// and PROFILE on neo4j:2026-community showed it seeking the unique path index
// for these three labels. The composite (name, path) constraints on
// TerraformModule and HelmChart served no read, since no query filters those
// labels by name and path together, so they get no replacement.
var neo4jRetiredConstraintPathIndexes = []string{
	"CREATE INDEX kustomize_overlay_path IF NOT EXISTS FOR (ko:KustomizeOverlay) ON (ko.path)",
	"CREATE INDEX helm_values_path IF NOT EXISTS FOR (hv:HelmValues) ON (hv.path)",
	"CREATE INDEX terragrunt_config_path IF NOT EXISTS FOR (tg:TerragruntConfig) ON (tg.path)",
}

// isRetiredConstraint reports whether cypher creates one of the named retired
// uniqueness constraints.
func isRetiredConstraint(cypher string, names []string) bool {
	fields := strings.Fields(cypher)
	if len(fields) < 3 || !strings.EqualFold(fields[0], "CREATE") || !strings.EqualFold(fields[1], "CONSTRAINT") {
		return false
	}
	return slices.Contains(names, fields[2])
}

// isNeo4jRetiredConstraint reports whether cypher creates one of the retired
// Neo4j uniqueness constraints.
func isNeo4jRetiredConstraint(cypher string) bool {
	return isRetiredConstraint(cypher, neo4jRetiredUniqueConstraints)
}

// isNornicDBRetiredConstraint reports whether cypher creates one of the
// retired NornicDB uniqueness constraints.
func isNornicDBRetiredConstraint(cypher string) bool {
	return isRetiredConstraint(cypher, nornicDBRetiredUniqueConstraints)
}

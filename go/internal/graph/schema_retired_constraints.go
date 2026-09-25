// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"fmt"
	"strings"
)

// neo4jRetiredUniqueConstraints names the Neo4j uniqueness constraints whose
// key was narrower than the canonical uid identity (#7095). The canonical
// writer MERGEs these labels on uid, and uid hashes repo, path, entity type,
// name, and start line. It upserts a moved block's new uid before
// entity_retract deletes the prior generation's node, so a key on (name, path)
// or (path) alone saw two nodes and failed the delta with
// ConstraintValidationFailed. Uniqueness for these labels rests on
// <label>_uid_unique, as it already did on NornicDB, which drops the composite
// forms (nornicDBSchemaConstraint).
//
// The CREATE statements stay in schemaConstraints because NornicDB still emits
// the three single-property ones and its schema must not move in this change.
var neo4jRetiredUniqueConstraints = []string{
	"kustomize_unique",
	"helm_chart_unique",
	"helm_values_unique",
	"tf_module_unique",
	"tg_config_unique",
}

// neo4jRetiredConstraintDrops removes the retired constraints from a store that
// an older bootstrap already created them on. IF EXISTS keeps it idempotent on
// a fresh store and on every later bootstrap.
func neo4jRetiredConstraintDrops() []string {
	drops := make([]string, 0, len(neo4jRetiredUniqueConstraints))
	for _, name := range neo4jRetiredUniqueConstraints {
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

// isNeo4jRetiredConstraint reports whether cypher creates one of the retired
// Neo4j uniqueness constraints.
func isNeo4jRetiredConstraint(cypher string) bool {
	fields := strings.Fields(cypher)
	if len(fields) < 3 || fields[0] != "CREATE" || fields[1] != "CONSTRAINT" {
		return false
	}
	for _, name := range neo4jRetiredUniqueConstraints {
		if fields[2] == name {
			return true
		}
	}
	return false
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

var retractableGraphEdgeTypes = []edgetype.EdgeType{
	edgetype.Aliases,
	edgetype.AllowsEgress,
	edgetype.AllowsIngress,
	edgetype.AtlantisDependsOn,
	edgetype.Calls,
	edgetype.CanAssume,
	edgetype.CanEscalateTo,
	edgetype.CanPerform,
	edgetype.Contains,
	edgetype.CorrelatesDeployableUnit,
	edgetype.DefinesJob,
	edgetype.DependsOn,
	edgetype.DeploysFrom,
	edgetype.DiscoversConfigIn,
	edgetype.Documents,
	edgetype.EvidencesRepositoryRelationship,
	edgetype.Executes,
	edgetype.ExecutesShell,
	edgetype.Explains,
	edgetype.ExtendsBase,
	edgetype.GrantsAccessTo,
	edgetype.HandlesRoute,
	edgetype.HasColumn,
	edgetype.HasDeploymentEvidence,
	edgetype.HasRole,
	edgetype.HelmValueReference,
	edgetype.Implements,
	edgetype.Imports,
	edgetype.Indexes,
	edgetype.Inherits,
	edgetype.Instantiates,
	edgetype.InvokesCloudAction,
	edgetype.LogsTo,
	edgetype.Manages,
	edgetype.MatchesState,
	edgetype.Migrates,
	edgetype.Needs,
	edgetype.Overrides,
	edgetype.ProvisionsDependencyFor,
	edgetype.QueriesTable,
	edgetype.ReadsConfigFrom,
	edgetype.ReadsFrom,
	edgetype.ReferencesTable,
	edgetype.ReconcilesFrom,
	edgetype.References,
	edgetype.RunsImage,
	edgetype.RunsIn,
	edgetype.RunsOn,
	edgetype.SatisfiedBy,
	edgetype.SecretsIamUsesServiceAccount,
	edgetype.TaintFlowsTo,
	edgetype.TargetsEnvironment,
	edgetype.To,
	edgetype.Triggers,
	edgetype.WritesTo,
	edgetype.Uses,
	edgetype.UsesMetaclass,
	edgetype.UsesModule,
	edgetype.UsesProfile,
	edgetype.UsesWorkflow,
}

// RetractableEdgeTypes returns the sorted, de-duplicated set of graph
// relationship types the static canonical and reducer edge retract paths can
// remove.
//
// It is the lockstep source of truth for replay depth coverage (#4370): the
// replay-coverage gate requires a delta/tombstone replay scenario for every
// retractable edge type, and a lockstep test keeps
// specs/replay-depth-requirements.v1.yaml byte-equal to this set.
func RetractableEdgeTypes() []string {
	seen := make(map[string]struct{}, len(retractableGraphEdgeTypes))
	for _, edgeType := range retractableGraphEdgeTypes {
		seen[string(edgeType)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for edgeType := range seen {
		out = append(out, edgeType)
	}
	sort.Strings(out)
	return out
}

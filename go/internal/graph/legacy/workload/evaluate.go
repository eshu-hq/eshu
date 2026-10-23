// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workload

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// Blocked reasons name why a name-only Workload node is not eligible for
// retirement. The empty string means eligible. The values are a cross-file
// contract with the admin route body, the retirement counter outcome labels,
// and the runbook.
const (
	// BlockedNone marks an eligible candidate.
	BlockedNone = ""
	// BlockedHasInstances marks a node some WorkloadInstance still references,
	// by INSTANCE_OF edge or by its workload_id property.
	BlockedHasInstances = "has_instances"
	// BlockedNoTwin marks a node with no repository-scoped, generation-stamped
	// twin that the same repository defines.
	BlockedNoTwin = "no_twin"
	// BlockedRelationships marks a node with an incident relationship outside
	// the safe set (DEFINES from its own repository, SAME_NAME).
	BlockedRelationships = "relationships"
)

const (
	relDefines  = "DEFINES"
	relSameName = "SAME_NAME"
	labelRepo   = "Repository"
	labelWorkld = "Workload"
)

// Candidate is a Workload node whose id has the exact legacy handle shape
// (workload:<name>) and carries no generation_id.
type Candidate struct {
	ID     string
	Name   string
	RepoID string
}

// Relationship is one relationship incident on a candidate, seen from the
// candidate: Outgoing is true when the candidate is the start node.
type Relationship struct {
	Type        string
	Outgoing    bool
	OtherID     string
	OtherLabels []string
}

// Facts is everything Evaluate needs, read from the graph in bounded,
// identity-anchored queries. Every map is keyed by the node id it describes.
type Facts struct {
	Candidates []Candidate
	// EdgeInstances holds candidate ids a WorkloadInstance reaches by an
	// INSTANCE_OF edge.
	EdgeInstances map[string]bool
	// PropInstances holds candidate ids a WorkloadInstance names in its
	// workload_id property; the handle resolver joins instances this way.
	PropInstances map[string]bool
	// Twins maps a generation-stamped scoped Workload id to the ids of the
	// Repository nodes that DEFINE it.
	Twins map[string][]string
	// Relationships maps a candidate id to every relationship incident on it.
	Relationships map[string][]Relationship
}

// Verdict is the retirement decision for one candidate.
type Verdict struct {
	Candidate
	// TwinID is the scoped id the candidate would be retired in favor of. It
	// is set even when the twin is missing, so a report can name what was
	// looked for.
	TwinID string
	// Relationships counts the incident relationships outside the safe set,
	// by type. It is empty for an eligible node.
	Relationships map[string]int
	Eligible      bool
	// BlockedReason is one of the Blocked constants; the first that applies
	// wins, in the order has_instances, no_twin, relationships.
	BlockedReason string
}

// IsLegacyID reports whether id is exactly the legacy handle shape for name:
// the prefix plus the name and nothing else. A blank name never matches, and a
// repository-scoped id never matches.
func IsLegacyID(id, name string) bool {
	if id == "" || name == "" {
		return false
	}
	return id == workloadid.FormatWorkloadHandle(name)
}

// TwinID returns the repository-scoped id for the same name and repository,
// built by the production constructor. It is empty when either part is blank
// or the name carries a colon, so it can never equal a node id.
func TwinID(repoID, name string) string {
	return workloadid.NewWorkloadID(repoID, name).String()
}

// Evaluate decides every candidate against the facts. It is pure: the same
// Facts give the same Verdicts, in candidate order.
func Evaluate(facts Facts) []Verdict {
	verdicts := make([]Verdict, 0, len(facts.Candidates))
	for _, c := range facts.Candidates {
		verdicts = append(verdicts, evaluateOne(c, facts))
	}
	return verdicts
}

func evaluateOne(c Candidate, facts Facts) Verdict {
	v := Verdict{Candidate: c, TwinID: TwinID(c.RepoID, c.Name), Relationships: map[string]int{}}
	switch {
	case facts.EdgeInstances[c.ID] || facts.PropInstances[c.ID]:
		v.BlockedReason = BlockedHasInstances
		return v
	case v.TwinID == "" || !slices.Contains(facts.Twins[v.TwinID], c.RepoID):
		v.BlockedReason = BlockedNoTwin
		return v
	}
	for _, rel := range facts.Relationships[c.ID] {
		if !safeRelationship(c, rel) {
			v.Relationships[rel.Type]++
		}
	}
	if len(v.Relationships) > 0 {
		v.BlockedReason = BlockedRelationships
		return v
	}
	v.Eligible = true
	return v
}

// safeRelationship reports whether a retirement may drop rel with the node:
// DEFINES arriving from the candidate's own Repository, or SAME_NAME to
// another Workload in either direction. Anything else carries evidence the
// twin does not necessarily hold. The Neo4j delete statement encodes the same
// rule; keep them together.
func safeRelationship(c Candidate, rel Relationship) bool {
	switch rel.Type {
	case relSameName:
		return slices.Contains(rel.OtherLabels, labelWorkld)
	case relDefines:
		return !rel.Outgoing && c.RepoID != "" && rel.OtherID == c.RepoID && slices.Contains(rel.OtherLabels, labelRepo)
	}
	return false
}

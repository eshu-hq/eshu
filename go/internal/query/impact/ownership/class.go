// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

// Node is one graph node on a traversal path, decoded to the properties the
// ownership check reads. The caller decodes it from a nodes(path) element (or
// a resolved anchor); this package never sees driver types.
type Node struct {
	// ID is the node's `id` property. Repository and WorkloadInstance are keyed
	// on it; CloudResource and TerraformStateResource set it equal to uid.
	ID string
	// UID is the node's `uid` property, the key the CloudResource and
	// TerraformStateResource ownership statements match on.
	UID string
	// Name is carried for callers that render the node; the check ignores it.
	Name string
	// RepoID is the node's durable `repo_id` property, read for the
	// repository-owned classes.
	RepoID string
	// Labels are the node's graph labels; they select the ownership class.
	Labels []string
}

// Class is the ownership rule a node is judged by. The zero value is
// ClassUngranted: a label the check does not recognize has no owner a grant
// could bind to, so it is denied by default. There is no third state.
type Class int

const (
	// ClassUngranted covers every label with no ownership rule (Platform,
	// Endpoint, CloudAction, EvidenceArtifact, TerraformOutput, DataAsset,
	// CidrBlock, SecretsIAMSecretMetadataPath, and any unknown label).
	ClassUngranted Class = iota
	// ClassRepository is a Repository node: admitted when its id is granted.
	ClassRepository
	// ClassRepoOwned is a label carrying a durable repo_id: admitted when that
	// repo_id is granted.
	ClassRepoOwned
	// ClassWorkloadInstance is repo-owned like ClassRepoOwned, and a
	// WorkloadInstance whose own repo_id fails is rescued when it has a
	// DEPLOYMENT_SOURCE edge to a granted Repository.
	ClassWorkloadInstance
	// ClassCloudResource is admitted when a WorkloadInstance whose repo_id is
	// granted USES it. A CloudResource with no such owner is ungranted.
	ClassCloudResource
	// ClassTerraformStateResource is admitted when a TerraformResource whose
	// repo_id is granted MATCHES_STATE it.
	ClassTerraformStateResource
)

// repoOwnedLabels are the labels whose writers set a durable repo_id. A
// WorkloadInstance is repo-owned too but has its own class for the rescue.
var repoOwnedLabels = map[string]struct{}{
	"Workload":           {},
	"TerraformResource":  {},
	"TerraformModule":    {},
	"KubernetesWorkload": {},
	"Function":           {},
	"SqlTable":           {},
	"ShellCommand":       {},
}

// ClassOf returns the ownership class for a node's labels. Precedence is
// fixed so a multi-label node is judged by one rule: Repository first, then
// WorkloadInstance, the repo-owned labels, CloudResource, and
// TerraformStateResource. Anything else is ClassUngranted.
func ClassOf(labels []string) Class {
	has := func(want string) bool {
		for _, label := range labels {
			if label == want {
				return true
			}
		}
		return false
	}
	switch {
	case has("Repository"):
		return ClassRepository
	case has("WorkloadInstance"):
		return ClassWorkloadInstance
	}
	for _, label := range labels {
		if _, ok := repoOwnedLabels[label]; ok {
			return ClassRepoOwned
		}
	}
	switch {
	case has("CloudResource"):
		return ClassCloudResource
	case has("TerraformStateResource"):
		return ClassTerraformStateResource
	}
	return ClassUngranted
}

// classMetricLabel is the closed node_label value an ownership statement's
// duration is recorded under.
func classMetricLabel(class Class) string {
	switch class {
	case ClassWorkloadInstance:
		return "WorkloadInstance"
	case ClassCloudResource:
		return "CloudResource"
	case ClassTerraformStateResource:
		return "TerraformStateResource"
	default:
		return "other"
	}
}

// statementKey is the property the class's ownership statement matches on:
// id for a WorkloadInstance (its writer sets no uid), uid otherwise, falling
// back to id when uid is absent (both writers set id = uid).
func statementKey(class Class, node Node) string {
	if class == ClassWorkloadInstance {
		return node.ID
	}
	if node.UID != "" {
		return node.UID
	}
	return node.ID
}

package reducer

import "fmt"

// RepoDependencyRow is one canonical repository DEPENDS_ON edge payload.
type RepoDependencyRow struct {
	DependencyName string
	RepoID         string
	TargetRepoID   string
}

// WorkloadDependencyRow is one canonical workload DEPENDS_ON edge payload.
type WorkloadDependencyRow struct {
	DependencyName   string
	RepoID           string
	TargetRepoID     string
	TargetWorkloadID string
	WorkloadID       string
}

// BuildRepoDependencyRows builds deduplicated repository dependency edge rows
// from repo descriptors, a per-repo dependency name list, and a name-to-ID map
// for target repositories.
func BuildRepoDependencyRows(
	descriptors []RepoDescriptor,
	dependenciesByRepo map[string][]string,
	targetRepoIDs map[string]string,
) []RepoDependencyRow {
	seen := make(map[string]struct{})
	var rows []RepoDependencyRow

	for _, descriptor := range descriptors {
		deps := dependenciesByRepo[descriptor.RepoID]
		for _, depName := range deps {
			targetRepoID := targetRepoIDs[depName]
			if targetRepoID == "" {
				continue
			}
			edgeKey := descriptor.RepoID + "|" + targetRepoID
			if _, ok := seen[edgeKey]; ok {
				continue
			}
			seen[edgeKey] = struct{}{}
			rows = append(rows, RepoDependencyRow{
				DependencyName: depName,
				RepoID:         descriptor.RepoID,
				TargetRepoID:   targetRepoID,
			})
		}
	}

	return rows
}

// BuildWorkloadDependencyRows builds deduplicated workload dependency edge rows
// from repo descriptors, a per-repo dependency name list, and a name-to-ID map
// for target repositories.
func BuildWorkloadDependencyRows(
	descriptors []RepoDescriptor,
	dependenciesByRepo map[string][]string,
	targetRepoIDs map[string]string,
) []WorkloadDependencyRow {
	seen := make(map[string]struct{})
	var rows []WorkloadDependencyRow

	for _, descriptor := range descriptors {
		deps := dependenciesByRepo[descriptor.RepoID]
		for _, depName := range deps {
			targetRepoID := targetRepoIDs[depName]
			if targetRepoID == "" {
				continue
			}
			targetWorkloadID := fmt.Sprintf("workload:%s", depName)
			edgeKey := descriptor.WorkloadID + "|" + targetRepoID
			if _, ok := seen[edgeKey]; ok {
				continue
			}
			seen[edgeKey] = struct{}{}
			rows = append(rows, WorkloadDependencyRow{
				DependencyName:   depName,
				RepoID:           descriptor.RepoID,
				TargetRepoID:     targetRepoID,
				TargetWorkloadID: targetWorkloadID,
				WorkloadID:       descriptor.WorkloadID,
			})
		}
	}

	return rows
}

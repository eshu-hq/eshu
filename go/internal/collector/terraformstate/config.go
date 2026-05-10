package terraformstate

import (
	"encoding/json"
	"fmt"
	"strings"
)

type collectorConfiguration struct {
	Discovery discoveryConfiguration `json:"discovery"`
}

type discoveryConfiguration struct {
	Graph                bool                             `json:"graph"`
	Seeds                []seedConfig                     `json:"seeds"`
	LocalRepos           []string                         `json:"local_repos"`
	LocalStateCandidates localStateCandidatesPolicyConfig `json:"local_state_candidates"`
}

type localStateCandidatesPolicyConfig struct {
	Mode     string                      `json:"mode"`
	Approved []localStateCandidateConfig `json:"approved"`
	Ignored  []localStateCandidateConfig `json:"ignored"`
}

type localStateCandidateConfig struct {
	RepoID string `json:"repo_id"`
	Path   string `json:"path"`
}

type seedConfig struct {
	Kind                    string `json:"kind"`
	Path                    string `json:"path"`
	RepoID                  string `json:"repo_id"`
	Bucket                  string `json:"bucket"`
	Key                     string `json:"key"`
	Region                  string `json:"region"`
	VersionID               string `json:"version_id"`
	DynamoDBTable           string `json:"dynamodb_table"`
	LegacyDynamoDBLockTable string `json:"dynamodb_lock_table"`
}

// ParseDiscoveryConfig parses the Terraform-state collector-instance JSON used
// by workflow configuration into the resolver's typed discovery config.
func ParseDiscoveryConfig(raw string) (DiscoveryConfig, error) {
	var parsed collectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSON(raw)), &parsed); err != nil {
		return DiscoveryConfig{}, fmt.Errorf("terraform state discovery configuration: %w", err)
	}
	config := DiscoveryConfig{
		Graph:      parsed.Discovery.Graph,
		LocalRepos: normalizedRepoIDs(parsed.Discovery.LocalRepos),
		Seeds:      make([]DiscoverySeed, 0, len(parsed.Discovery.Seeds)),
		LocalStateCandidates: LocalStateCandidatePolicy{
			Mode:     localStateCandidateMode(parsed.Discovery.LocalStateCandidates.Mode),
			Approved: localStateCandidateRefs(parsed.Discovery.LocalStateCandidates.Approved),
			Ignored:  localStateCandidateRefs(parsed.Discovery.LocalStateCandidates.Ignored),
		},
	}
	for _, seed := range parsed.Discovery.Seeds {
		config.Seeds = append(config.Seeds, DiscoverySeed{
			Kind:          BackendKind(strings.ToLower(strings.TrimSpace(seed.Kind))),
			Path:          strings.TrimSpace(seed.Path),
			RepoID:        strings.TrimSpace(seed.RepoID),
			Bucket:        strings.TrimSpace(seed.Bucket),
			Key:           strings.TrimSpace(seed.Key),
			Region:        strings.TrimSpace(seed.Region),
			VersionID:     strings.TrimSpace(seed.VersionID),
			DynamoDBTable: seedDynamoDBTable(seed),
		})
	}
	return config, nil
}

func localStateCandidateMode(value string) LocalStateCandidateMode {
	mode := LocalStateCandidateMode(strings.ToLower(strings.TrimSpace(value)))
	if mode == "" {
		return LocalStateCandidateModeDiscoverOnly
	}
	return mode
}

func localStateCandidateRefs(configs []localStateCandidateConfig) []LocalStateCandidateRef {
	refs := make([]LocalStateCandidateRef, 0, len(configs))
	for _, config := range configs {
		ref := (LocalStateCandidateRef{
			RepoID:       config.RepoID,
			RelativePath: config.Path,
		}).normalized()
		if ref.RepoID == "" || ref.RelativePath == "" {
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

func seedDynamoDBTable(seed seedConfig) string {
	if table := strings.TrimSpace(seed.DynamoDBTable); table != "" {
		return table
	}
	return strings.TrimSpace(seed.LegacyDynamoDBLockTable)
}

func normalizeJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "{}"
	}
	return trimmed
}

package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

type terraformStateCollectorConfiguration struct {
	Discovery terraformStateDiscoveryConfiguration `json:"discovery"`
	AWS       terraformStateAWSConfiguration       `json:"aws"`
}

type terraformStateDiscoveryConfiguration struct {
	Graph      bool                       `json:"graph"`
	Seeds      []terraformStateSeedConfig `json:"seeds"`
	LocalRepos []string                   `json:"local_repos"`
}

type terraformStateSeedConfig struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	RepoID string `json:"repo_id"`
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	Region string `json:"region"`
}

type terraformStateAWSConfiguration struct {
	RoleARN string `json:"role_arn"`
}

func validateTerraformStateCollectorConfiguration(raw string) error {
	var cfg terraformStateCollectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &cfg); err != nil {
		return fmt.Errorf("terraform_state configuration: %w", err)
	}

	if !cfg.Discovery.Graph && len(cfg.Discovery.Seeds) == 0 && len(cfg.Discovery.LocalRepos) == 0 {
		return fmt.Errorf("terraform_state configuration discovery must enable graph, seeds, or local_repos")
	}

	for index, repoID := range cfg.Discovery.LocalRepos {
		if strings.TrimSpace(repoID) == "" {
			return fmt.Errorf("terraform_state discovery local_repos %d must not be blank", index)
		}
	}

	usesS3 := false
	for index, seed := range cfg.Discovery.Seeds {
		kind := strings.ToLower(strings.TrimSpace(seed.Kind))
		switch kind {
		case "local":
			if strings.TrimSpace(seed.Path) == "" {
				return fmt.Errorf("terraform_state discovery seed %d local path must not be blank", index)
			}
		case "s3":
			usesS3 = true
			if strings.TrimSpace(seed.Bucket) == "" {
				return fmt.Errorf("terraform_state discovery seed %d s3 bucket must not be blank", index)
			}
			if strings.TrimSpace(seed.Key) == "" {
				return fmt.Errorf("terraform_state discovery seed %d s3 key must not be blank", index)
			}
			if strings.TrimSpace(seed.Region) == "" {
				return fmt.Errorf("terraform_state discovery seed %d s3 region must not be blank", index)
			}
		default:
			return fmt.Errorf("terraform_state discovery seed %d kind %q is unsupported", index, seed.Kind)
		}
	}

	if usesS3 && strings.TrimSpace(cfg.AWS.RoleARN) == "" {
		return fmt.Errorf("terraform_state configuration aws.role_arn is required for s3 seeds")
	}

	return nil
}

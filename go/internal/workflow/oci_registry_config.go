package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxOCIRegistryTagLimit = 100

type ociRegistryCollectorConfiguration struct {
	Targets []ociRegistryTargetConfiguration `json:"targets"`
}

type ociRegistryTargetConfiguration struct {
	Provider      string   `json:"provider"`
	Registry      string   `json:"registry"`
	BaseURL       string   `json:"base_url"`
	RepositoryKey string   `json:"repository_key"`
	Repository    string   `json:"repository"`
	Region        string   `json:"region"`
	RegistryID    string   `json:"registry_id"`
	RegistryHost  string   `json:"registry_host"`
	References    []string `json:"references"`
	TagLimit      int      `json:"tag_limit"`
}

// ValidateOCIRegistryCollectorConfiguration checks the claim-planned OCI
// registry target list without resolving credentials or provider SDK state.
func ValidateOCIRegistryCollectorConfiguration(raw string) error {
	var decoded ociRegistryCollectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &decoded); err != nil {
		return fmt.Errorf("decode OCI registry collector configuration: %w", err)
	}
	if len(decoded.Targets) == 0 {
		return fmt.Errorf("OCI registry collector configuration requires targets")
	}
	for i, target := range decoded.Targets {
		if err := validateOCIRegistryTargetConfiguration(target); err != nil {
			return fmt.Errorf("targets[%d]: %w", i, err)
		}
	}
	return nil
}

func validateOCIRegistryTargetConfiguration(target ociRegistryTargetConfiguration) error {
	provider := strings.TrimSpace(target.Provider)
	if provider == "" {
		return fmt.Errorf("provider is required")
	}
	if err := validateOCIRegistryTargetEndpoint(provider, target); err != nil {
		return err
	}
	if strings.Trim(strings.TrimSpace(target.Repository), "/") == "" {
		return fmt.Errorf("repository is required")
	}
	if target.TagLimit < 0 || target.TagLimit > maxOCIRegistryTagLimit {
		return fmt.Errorf("tag_limit must be between 0 and %d", maxOCIRegistryTagLimit)
	}
	for i, reference := range target.References {
		if strings.TrimSpace(reference) == "" {
			return fmt.Errorf("references[%d] must not be blank", i)
		}
	}
	return nil
}

func validateOCIRegistryTargetEndpoint(provider string, target ociRegistryTargetConfiguration) error {
	switch provider {
	case "dockerhub", "ghcr":
		return nil
	case "ecr":
		if strings.TrimSpace(target.Registry) != "" {
			return nil
		}
		if strings.TrimSpace(target.RegistryHost) != "" {
			return nil
		}
		if strings.TrimSpace(target.RegistryID) != "" && strings.TrimSpace(target.Region) != "" {
			return nil
		}
		return fmt.Errorf("ecr target requires registry, registry_host, or registry_id with region")
	case "jfrog":
		if strings.TrimSpace(target.BaseURL) != "" && strings.TrimSpace(target.RepositoryKey) != "" {
			return nil
		}
		return fmt.Errorf("jfrog target requires base_url with repository_key")
	case "harbor":
		if strings.TrimSpace(target.BaseURL) != "" {
			return nil
		}
		return fmt.Errorf("harbor target requires base_url")
	case "google_artifact_registry", "azure_container_registry":
		if strings.TrimSpace(target.Registry) != "" {
			return nil
		}
		if strings.TrimSpace(target.RegistryHost) != "" {
			return nil
		}
		return fmt.Errorf("%s target requires registry or registry_host", provider)
	default:
		return fmt.Errorf("unsupported provider %q", provider)
	}
}

package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
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
	if err := validateOCIRegistryTargetRepository(provider, target); err != nil {
		return err
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

func validateOCIRegistryTargetRepository(provider string, target ociRegistryTargetConfiguration) error {
	repository := strings.ToLower(strings.Trim(strings.TrimSpace(target.Repository), "/"))
	if strings.Contains(repository, "//") {
		return fmt.Errorf("%s repository must not contain empty path segments", provider)
	}
	switch provider {
	case "dockerhub", "ecr", "jfrog", "azure_container_registry":
		return nil
	case "ghcr":
		if !strings.Contains(repository, "/") {
			return fmt.Errorf("ghcr repository must include owner and image name")
		}
	case "harbor":
		if !strings.Contains(repository, "/") {
			return fmt.Errorf("harbor repository must include project and image path")
		}
	case "google_artifact_registry":
		if strings.Count(repository, "/") < 2 {
			return fmt.Errorf("google artifact registry repository must include project, repository, and image path")
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
		if strings.TrimSpace(target.RepositoryKey) == "" {
			return fmt.Errorf("jfrog target requires repository_key")
		}
		if err := validateOCIRegistryURL("jfrog base_url", target.BaseURL, false); err != nil {
			return err
		}
		return nil
	case "harbor":
		if err := validateOCIRegistryURL("harbor base_url", target.BaseURL, true); err != nil {
			return err
		}
		return nil
	case "google_artifact_registry", "azure_container_registry":
		host := strings.TrimSpace(firstNonBlank(target.RegistryHost, target.Registry))
		if host == "" {
			return fmt.Errorf("%s target requires registry or registry_host", provider)
		}
		return validateOCIRegistryHost(provider, host)
	default:
		return fmt.Errorf("unsupported provider %q", provider)
	}
}

func validateOCIRegistryURL(field string, raw string, requireHTTPS bool) error {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse %s: %w", field, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must include scheme and host", field)
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https", field)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include credentials", field)
	}
	return nil
}

func validateOCIRegistryHost(provider string, raw string) error {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return fmt.Errorf("%s host is required", provider)
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse %s host: %w", provider, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("%s host must use https and include a host", provider)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s host must not include credentials", provider)
	}
	switch provider {
	case "google_artifact_registry":
		if !strings.HasSuffix(parsed.Hostname(), "-docker.pkg.dev") {
			return fmt.Errorf("google artifact registry host must end with -docker.pkg.dev")
		}
	case "azure_container_registry":
		if !strings.HasSuffix(parsed.Hostname(), ".azurecr.io") {
			return fmt.Errorf("azure container registry host must end with .azurecr.io")
		}
	}
	return nil
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

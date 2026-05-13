package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	maxPackageRegistryPackageLimit = 100
	maxPackageRegistryVersionLimit = 200
)

type packageRegistryCollectorConfiguration struct {
	Targets []packageRegistryTargetConfiguration `json:"targets"`
}

type packageRegistryTargetConfiguration struct {
	Provider     string   `json:"provider"`
	Ecosystem    string   `json:"ecosystem"`
	Registry     string   `json:"registry"`
	ScopeID      string   `json:"scope_id"`
	Namespace    string   `json:"namespace"`
	Packages     []string `json:"packages"`
	PackageLimit int      `json:"package_limit"`
	VersionLimit int      `json:"version_limit"`
	Visibility   string   `json:"visibility"`
	SourceURI    string   `json:"source_uri"`
	MetadataURL  string   `json:"metadata_url"`
}

// ValidatePackageRegistryCollectorConfiguration checks the claim-planned
// package-registry target list without resolving private credentials or
// connecting to a registry feed.
func ValidatePackageRegistryCollectorConfiguration(raw string) error {
	var decoded packageRegistryCollectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &decoded); err != nil {
		return fmt.Errorf("decode package registry collector configuration: %w", err)
	}
	if len(decoded.Targets) == 0 {
		return fmt.Errorf("package registry collector configuration requires targets")
	}
	for i, target := range decoded.Targets {
		if err := validatePackageRegistryTargetConfiguration(target); err != nil {
			return fmt.Errorf("targets[%d]: %w", i, err)
		}
	}
	return nil
}

func validatePackageRegistryTargetConfiguration(target packageRegistryTargetConfiguration) error {
	if strings.TrimSpace(target.Provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if err := validatePackageRegistryEcosystem(target.Ecosystem); err != nil {
		return err
	}
	if err := validatePackageRegistryURL("registry", target.Registry, true); err != nil {
		return err
	}
	if strings.TrimSpace(target.ScopeID) == "" {
		return fmt.Errorf("scope_id is required")
	}
	if target.PackageLimit < 0 || target.PackageLimit > maxPackageRegistryPackageLimit {
		return fmt.Errorf("package_limit must be between 0 and %d", maxPackageRegistryPackageLimit)
	}
	if target.VersionLimit < 0 || target.VersionLimit > maxPackageRegistryVersionLimit {
		return fmt.Errorf("version_limit must be between 0 and %d", maxPackageRegistryVersionLimit)
	}
	if strings.TrimSpace(target.MetadataURL) != "" {
		if err := validatePackageRegistryURL("metadata_url", target.MetadataURL, true); err != nil {
			return err
		}
	}
	for i, pkg := range target.Packages {
		if strings.TrimSpace(pkg) == "" {
			return fmt.Errorf("packages[%d] must not be blank", i)
		}
	}
	return nil
}

func validatePackageRegistryEcosystem(raw string) error {
	switch strings.TrimSpace(raw) {
	case "npm", "pypi", "gomod", "maven", "nuget", "generic":
		return nil
	case "":
		return fmt.Errorf("ecosystem is required")
	default:
		return fmt.Errorf("unsupported ecosystem %q", strings.TrimSpace(raw))
	}
}

func validatePackageRegistryURL(field, raw string, requireHTTPS bool) error {
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

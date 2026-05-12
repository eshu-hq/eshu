package packageregistry

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultRuntimePollInterval = 5 * time.Minute
	defaultPackageLimit        = 20
	maxPackageLimit            = 100
	defaultVersionLimit        = 20
	maxVersionLimit            = 200
)

// RuntimeConfig describes one future package-registry collector runtime.
type RuntimeConfig struct {
	CollectorInstanceID string
	PollInterval        time.Duration
	Targets             []TargetConfig
}

// TargetConfig describes one bounded package-registry feed or package scope.
type TargetConfig struct {
	Provider     string
	Ecosystem    Ecosystem
	Registry     string
	ScopeID      string
	Namespace    string
	Packages     []string
	PackageLimit int
	VersionLimit int
	Visibility   Visibility
	SourceURI    string
	FencingToken int64
}

func (c RuntimeConfig) validated() (RuntimeConfig, error) {
	collectorID := strings.TrimSpace(c.CollectorInstanceID)
	if collectorID == "" {
		return RuntimeConfig{}, fmt.Errorf("collector instance ID is required")
	}
	pollInterval := c.PollInterval
	if pollInterval == 0 {
		pollInterval = defaultRuntimePollInterval
	}
	if pollInterval < 0 {
		return RuntimeConfig{}, fmt.Errorf("poll interval must not be negative")
	}
	if len(c.Targets) == 0 {
		return RuntimeConfig{}, fmt.Errorf("at least one package-registry target is required")
	}

	targets := make([]TargetConfig, 0, len(c.Targets))
	for i, target := range c.Targets {
		validated, err := target.validated()
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("target %d: %w", i, err)
		}
		targets = append(targets, validated)
	}
	return RuntimeConfig{
		CollectorInstanceID: collectorID,
		PollInterval:        pollInterval,
		Targets:             targets,
	}, nil
}

func (t TargetConfig) validated() (TargetConfig, error) {
	provider := strings.TrimSpace(t.Provider)
	if provider == "" {
		return TargetConfig{}, fmt.Errorf("provider is required")
	}
	ecosystem := Ecosystem(strings.TrimSpace(string(t.Ecosystem)))
	if ecosystem == "" {
		return TargetConfig{}, fmt.Errorf("ecosystem is required")
	}
	registry := normalizeRegistry(t.Registry)
	if registry == "" {
		return TargetConfig{}, fmt.Errorf("registry is required")
	}
	scopeID := strings.TrimSpace(t.ScopeID)
	if scopeID == "" {
		return TargetConfig{}, fmt.Errorf("scope_id is required")
	}
	packageLimit := t.PackageLimit
	if packageLimit == 0 {
		packageLimit = defaultPackageLimit
	}
	if packageLimit < 0 || packageLimit > maxPackageLimit {
		return TargetConfig{}, fmt.Errorf("package_limit must be between 0 and %d", maxPackageLimit)
	}
	versionLimit := t.VersionLimit
	if versionLimit == 0 {
		versionLimit = defaultVersionLimit
	}
	if versionLimit < 0 || versionLimit > maxVersionLimit {
		return TargetConfig{}, fmt.Errorf("version_limit must be between 0 and %d", maxVersionLimit)
	}

	return TargetConfig{
		Provider:     provider,
		Ecosystem:    ecosystem,
		Registry:     registry,
		ScopeID:      scopeID,
		Namespace:    strings.TrimSpace(t.Namespace),
		Packages:     compactPackageList(t.Packages),
		PackageLimit: packageLimit,
		VersionLimit: versionLimit,
		Visibility:   t.Visibility,
		SourceURI:    strings.TrimSpace(t.SourceURI),
		FencingToken: t.FencingToken,
	}, nil
}

func compactPackageList(packages []string) []string {
	compacted := make([]string, 0, len(packages))
	seen := map[string]bool{}
	for _, pkg := range packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" || seen[pkg] {
			continue
		}
		seen[pkg] = true
		compacted = append(compacted, pkg)
	}
	return compacted
}

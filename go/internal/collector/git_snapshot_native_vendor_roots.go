package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
)

const (
	repoDiscoveryConfigRelPath = ".eshu/discovery.json"
	vendorRootsConfigRelPath   = ".eshu/vendor-roots.json"
)

type repoDiscoveryConfig struct {
	IgnoredPathGlobs   []repoDiscoveryRule `json:"ignored_path_globs"`
	PreservedPathGlobs []repoDiscoveryRule `json:"preserved_path_globs"`
	VendorRoots        []repoDiscoveryRule `json:"vendor_roots"`
	KeepRoots          []repoDiscoveryRule `json:"keep_roots"`
}

type repoDiscoveryRule struct {
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

func discoveryOptionsWithRepoDiscoveryConfig(
	repoPath string,
	opts discovery.Options,
) (discovery.Options, error) {
	for _, source := range []struct {
		relPath              string
		defaultIgnoredReason string
	}{
		{relPath: vendorRootsConfigRelPath, defaultIgnoredReason: "user-vendor-root"},
		{relPath: repoDiscoveryConfigRelPath, defaultIgnoredReason: "user-discovery-rule"},
	} {
		config, ok, err := readRepoDiscoveryConfig(repoPath, source.relPath)
		if err != nil {
			return discovery.Options{}, err
		}
		if !ok {
			continue
		}
		opts, err = applyRepoDiscoveryConfig(opts, config, source.relPath, source.defaultIgnoredReason)
		if err != nil {
			return discovery.Options{}, err
		}
	}
	return dedupeRepoDiscoveryOptions(opts), nil
}

func readRepoDiscoveryConfig(repoPath string, relPath string) (repoDiscoveryConfig, bool, error) {
	configPath := filepath.Join(repoPath, relPath)
	contents, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return repoDiscoveryConfig{}, false, nil
		}
		return repoDiscoveryConfig{}, false, fmt.Errorf("read %s: %w", relPath, err)
	}

	var config repoDiscoveryConfig
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return repoDiscoveryConfig{}, false, fmt.Errorf("parse %s: %w", relPath, err)
	}
	return config, true, nil
}

func applyRepoDiscoveryConfig(
	opts discovery.Options,
	config repoDiscoveryConfig,
	relPath string,
	defaultIgnoredReason string,
) (discovery.Options, error) {
	for _, root := range config.IgnoredPathGlobs {
		next, err := repoDiscoveryIgnoredRule(root, defaultIgnoredReason, relPath, "ignored_path_globs")
		if err != nil {
			return discovery.Options{}, err
		}
		opts.IgnoredPathGlobs = append(opts.IgnoredPathGlobs, next)
	}
	for _, root := range config.VendorRoots {
		next, err := repoDiscoveryIgnoredRule(root, "user-vendor-root", relPath, "vendor_roots")
		if err != nil {
			return discovery.Options{}, err
		}
		opts.IgnoredPathGlobs = append(opts.IgnoredPathGlobs, next)
	}
	for _, root := range config.PreservedPathGlobs {
		pattern, err := validateRepoDiscoveryPattern(root.Path)
		if err != nil {
			return discovery.Options{}, fmt.Errorf("validate %s preserved_path_globs path %q: %w", relPath, root.Path, err)
		}
		opts.PreservedPathGlobs = append(opts.PreservedPathGlobs, pattern)
	}
	for _, root := range config.KeepRoots {
		pattern, err := validateRepoDiscoveryPattern(root.Path)
		if err != nil {
			return discovery.Options{}, fmt.Errorf("validate %s keep_roots path %q: %w", relPath, root.Path, err)
		}
		opts.PreservedPathGlobs = append(opts.PreservedPathGlobs, pattern)
	}
	return opts, nil
}

func repoDiscoveryIgnoredRule(
	root repoDiscoveryRule,
	defaultReason string,
	relPath string,
	field string,
) (discovery.PathGlobRule, error) {
	pattern, err := validateRepoDiscoveryPattern(root.Path)
	if err != nil {
		return discovery.PathGlobRule{}, fmt.Errorf("validate %s %s path %q: %w", relPath, field, root.Path, err)
	}
	reason := strings.TrimSpace(root.Reason)
	if reason == "" {
		reason = defaultReason
	}
	return discovery.PathGlobRule{
		Pattern: pattern,
		Reason:  reason,
	}, nil
}

func dedupeRepoDiscoveryOptions(opts discovery.Options) discovery.Options {
	ignoredSeen := make(map[string]struct{}, len(opts.IgnoredPathGlobs))
	ignored := opts.IgnoredPathGlobs[:0]
	for _, rule := range opts.IgnoredPathGlobs {
		key := strings.TrimSpace(rule.Pattern) + "\x00" + strings.TrimSpace(rule.Reason)
		if _, ok := ignoredSeen[key]; ok {
			continue
		}
		ignoredSeen[key] = struct{}{}
		ignored = append(ignored, rule)
	}
	opts.IgnoredPathGlobs = ignored

	preservedSeen := make(map[string]struct{}, len(opts.PreservedPathGlobs))
	preserved := opts.PreservedPathGlobs[:0]
	for _, pattern := range opts.PreservedPathGlobs {
		key := strings.TrimSpace(pattern)
		if _, ok := preservedSeen[key]; ok {
			continue
		}
		preservedSeen[key] = struct{}{}
		preserved = append(preserved, pattern)
	}
	opts.PreservedPathGlobs = preserved
	return opts
}

func validateRepoDiscoveryPattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(filepath.ToSlash(pattern))
	if pattern == "" {
		return "", errors.New("path is required")
	}
	if strings.HasPrefix(pattern, "/") {
		return "", errors.New("path must be repository-relative")
	}
	for strings.HasPrefix(pattern, "./") {
		pattern = strings.TrimPrefix(pattern, "./")
	}
	for _, segment := range strings.Split(pattern, "/") {
		if segment == ".." {
			return "", errors.New("path must not contain '..'")
		}
	}
	cleaned := filepath.ToSlash(filepath.Clean(pattern))
	if cleaned == "." || cleaned == "" {
		return "", errors.New("path is required")
	}
	if strings.HasSuffix(pattern, "/**") && !strings.HasSuffix(cleaned, "/**") {
		cleaned += "/**"
	}
	return cleaned, nil
}

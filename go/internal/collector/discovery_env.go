package collector

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
)

const (
	discoveryIgnoredPathGlobsEnv   = "ESHU_DISCOVERY_IGNORED_PATH_GLOBS"
	discoveryPreservedPathGlobsEnv = "ESHU_DISCOVERY_PRESERVED_PATH_GLOBS"
)

// LoadDiscoveryOptionsFromEnv parses operator-supplied discovery path-glob
// overlays. Entries may be comma or newline separated; ignored entries may use
// "pattern=reason" and default to "env-ignore" when no reason is supplied.
func LoadDiscoveryOptionsFromEnv(getenv func(string) string) (discovery.Options, error) {
	if getenv == nil {
		return discovery.Options{}, nil
	}
	ignored, err := parseDiscoveryIgnoredPathGlobs(getenv(discoveryIgnoredPathGlobsEnv))
	if err != nil {
		return discovery.Options{}, err
	}
	preserved := parseDiscoveryPathGlobList(getenv(discoveryPreservedPathGlobsEnv))
	return discovery.Options{
		IgnoredPathGlobs:   ignored,
		PreservedPathGlobs: preserved,
	}, nil
}

func parseDiscoveryIgnoredPathGlobs(raw string) ([]discovery.PathGlobRule, error) {
	entries := parseDiscoveryPathGlobList(raw)
	rules := make([]discovery.PathGlobRule, 0, len(entries))
	for _, entry := range entries {
		pattern, reason, _ := strings.Cut(entry, "=")
		pattern = strings.TrimSpace(pattern)
		reason = strings.TrimSpace(reason)
		if pattern == "" {
			return nil, fmt.Errorf("%s contains an ignored glob with empty pattern", discoveryIgnoredPathGlobsEnv)
		}
		if reason == "" {
			reason = "env-ignore"
		}
		rules = append(rules, discovery.PathGlobRule{
			Pattern: pattern,
			Reason:  reason,
		})
	}
	return rules, nil
}

func parseDiscoveryPathGlobList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		values = append(values, field)
	}
	return values
}

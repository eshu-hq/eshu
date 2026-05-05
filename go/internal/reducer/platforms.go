package reducer

import (
	"fmt"
	"strings"
)

// CanonicalPlatformInput holds the parameters for building a canonical
// platform identifier.
type CanonicalPlatformInput struct {
	Kind        string
	Provider    string
	Name        string
	Environment string
	Region      string
	Locator     string
}

// PlatformDescriptor describes one inferred infrastructure platform.
type PlatformDescriptor struct {
	PlatformID          string
	PlatformKind        string
	PlatformProvider    string
	PlatformName        string
	PlatformEnvironment string
	PlatformRegion      string
	PlatformLocator     string
}

// InfrastructurePlatformInput holds Terraform graph signals for platform
// inference.
type InfrastructurePlatformInput struct {
	DataTypes     []string
	DataNames     []string
	ModuleSources []string
	ModuleNames   []string
	ResourceTypes []string
	ResourceNames []string
	RepoName      string
}

// nonPlatformIdentifiers are generic names that should not be used as platform
// names.
var nonPlatformIdentifiers = map[string]struct{}{
	"alerts":             {},
	"current":            {},
	"default":            {},
	"eks":                {},
	"ingress":            {},
	"main":               {},
	"pagerduty":          {},
	"pipeline":           {},
	"private":            {},
	"private-regionless": {},
	"public":             {},
	"terraform_state":    {},
}

// InferRuntimePlatformKind infers a runtime platform kind from workload
// resource kinds.
func InferRuntimePlatformKind(resourceKinds []string) string {
	k8sKinds := map[string]struct{}{
		"deployment":  {},
		"service":     {},
		"statefulset": {},
		"daemonset":   {},
	}
	for _, kind := range resourceKinds {
		normalized := strings.TrimSpace(strings.ToLower(kind))
		if normalized == "" {
			continue
		}
		if _, ok := k8sKinds[normalized]; ok {
			return "kubernetes"
		}
	}
	return ""
}

// CanonicalPlatformID builds a canonical platform identifier. Returns empty
// string when the input lacks sufficient signal to form a stable ID.
func CanonicalPlatformID(input CanonicalPlatformInput) string {
	kind := normalizeToken(input.Kind)
	provider := normalizeToken(input.Provider)
	name := normalizeToken(input.Name)
	environment := normalizeToken(input.Environment)
	region := normalizeToken(input.Region)
	locator := normalizeToken(input.Locator)

	discriminator := locator
	if discriminator == "" {
		discriminator = name
	}
	if discriminator == "" && (environment == "" || region == "") {
		return ""
	}

	return fmt.Sprintf("platform:%s:%s:%s:%s:%s",
		orNone(kind),
		orNone(provider),
		orNone(discriminator),
		orNone(environment),
		orNone(region),
	)
}

// InferInfrastructurePlatformDescriptor returns a platform descriptor for
// infra repos when the signal is explicit enough. Returns nil when no platform
// can be inferred.
func InferInfrastructurePlatformDescriptor(input InfrastructurePlatformInput) *PlatformDescriptor {
	normalizedRT := normalizeSlice(input.ResourceTypes)
	normalizedMS := normalizeSlice(input.ModuleSources)

	platformKind := InferInfrastructureRuntimeFamilyKind(normalizedRT, normalizedMS)
	if platformKind == "" {
		return nil
	}

	family := LookupRuntimeFamily(platformKind)
	var platformProvider string
	if family != nil {
		platformProvider = family.Provider
	}

	// AWS provider fallback from data types or resource types.
	if platformProvider == "" {
		normalizedDT := normalizeSlice(input.DataTypes)
		for _, v := range append(normalizedDT, normalizedRT...) {
			if strings.HasPrefix(v, "aws_") {
				platformProvider = "aws"
				break
			}
		}
	}

	platformName := choosePlatformName(
		normalizeSliceTrimmed(input.ResourceNames),
		normalizeSliceTrimmed(input.DataNames),
		normalizeSliceTrimmed(input.ModuleNames),
		input.RepoName,
	)
	if platformName == "" {
		return nil
	}

	platformLocator := "cluster/" + platformName
	platformID := CanonicalPlatformID(CanonicalPlatformInput{
		Kind:     platformKind,
		Provider: platformProvider,
		Name:     platformName,
		Locator:  platformLocator,
	})
	if platformID == "" {
		return nil
	}

	return &PlatformDescriptor{
		PlatformID:       platformID,
		PlatformKind:     platformKind,
		PlatformProvider: platformProvider,
		PlatformName:     platformName,
		PlatformLocator:  platformLocator,
	}
}

// choosePlatformName picks a stable platform name from explicit cluster-like
// identifiers, falling back to repo name.
func choosePlatformName(resourceNames, dataNames, moduleNames []string, repoName string) string {
	candidates := make([]string, 0, len(resourceNames)+len(moduleNames))
	candidates = append(candidates, resourceNames...)
	candidates = append(candidates, dataNames...)
	candidates = append(candidates, moduleNames...)

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		normalized := strings.ToLower(candidate)
		if _, ok := nonPlatformIdentifiers[normalized]; ok {
			continue
		}
		if strings.HasPrefix(normalized, "aws_") {
			continue
		}
		return candidate
	}

	name := strings.TrimSpace(repoName)
	if name == "" {
		return ""
	}
	return name
}

func normalizeToken(s string) string {
	v := strings.TrimSpace(strings.ToLower(s))
	return v
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func normalizeSlice(values []string) []string {
	result := make([]string, 0, len(values))
	for _, v := range values {
		s := strings.TrimSpace(strings.ToLower(v))
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

func normalizeSliceTrimmed(values []string) []string {
	result := make([]string, 0, len(values))
	for _, v := range values {
		s := strings.TrimSpace(v)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

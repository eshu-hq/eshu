package packageregistry

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var pypiNormalizePattern = regexp.MustCompile(`[-_.]+`)

// NormalizePackageIdentity applies ecosystem-specific package identity rules
// before facts are assigned stable keys.
func NormalizePackageIdentity(identity PackageIdentity) (NormalizedPackageIdentity, error) {
	ecosystem := Ecosystem(strings.TrimSpace(string(identity.Ecosystem)))
	if ecosystem == "" {
		return NormalizedPackageIdentity{}, fmt.Errorf("package ecosystem must not be blank")
	}
	registry := normalizeRegistry(identity.Registry)
	if registry == "" {
		return NormalizedPackageIdentity{}, fmt.Errorf("package registry must not be blank")
	}
	rawName := strings.TrimSpace(identity.RawName)
	if rawName == "" {
		return NormalizedPackageIdentity{}, fmt.Errorf("package name must not be blank")
	}
	namespace := strings.TrimSpace(identity.Namespace)
	classifier := strings.TrimSpace(identity.Classifier)

	normalizedName, normalizedNamespace, err := normalizeNameForEcosystem(ecosystem, rawName, namespace)
	if err != nil {
		return NormalizedPackageIdentity{}, err
	}

	packageID := packageIDFor(ecosystem, registry, normalizedNamespace, normalizedName)
	return NormalizedPackageIdentity{
		Ecosystem:      ecosystem,
		Registry:       registry,
		RawName:        rawName,
		NormalizedName: normalizedName,
		Namespace:      normalizedNamespace,
		Classifier:     classifier,
		PackageID:      packageID,
	}, nil
}

func normalizeRegistry(raw string) string {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Host != "" {
		host := strings.ToLower(parsed.Host)
		path := strings.Trim(parsed.EscapedPath(), "/")
		if path == "" {
			return host
		}
		return host + "/" + path
	}
	host, path, ok := strings.Cut(trimmed, "/")
	if !ok {
		return strings.ToLower(host)
	}
	return strings.ToLower(host) + "/" + strings.Trim(path, "/")
}

func normalizeNameForEcosystem(ecosystem Ecosystem, rawName, namespace string) (string, string, error) {
	switch ecosystem {
	case EcosystemNPM:
		name := strings.ToLower(rawName)
		if strings.HasPrefix(name, "@") {
			scope, packageName, ok := strings.Cut(strings.TrimPrefix(name, "@"), "/")
			if !ok || strings.TrimSpace(scope) == "" || strings.TrimSpace(packageName) == "" {
				return "", "", fmt.Errorf("scoped npm package must use @scope/name")
			}
			return "@" + scope + "/" + packageName, scope, nil
		}
		return name, "", nil
	case EcosystemPyPI:
		return pypiNormalizePattern.ReplaceAllString(strings.ToLower(rawName), "-"), "", nil
	case EcosystemGoModule:
		return rawName, goModuleNamespace(rawName), nil
	case EcosystemMaven:
		if namespace == "" {
			return "", "", fmt.Errorf("maven package identity requires group namespace")
		}
		return rawName, namespace, nil
	case EcosystemNuGet:
		return strings.ToLower(rawName), "", nil
	case EcosystemGeneric:
		return rawName, namespace, nil
	default:
		return "", "", fmt.Errorf("package ecosystem %q is unsupported", ecosystem)
	}
}

func goModuleNamespace(modulePath string) string {
	lastSlash := strings.LastIndex(modulePath, "/")
	if lastSlash < 0 {
		return ""
	}
	prefix := modulePath[:lastSlash]
	suffix := modulePath[lastSlash+1:]
	if len(suffix) > 1 && suffix[0] == 'v' && allDigits(suffix[1:]) {
		return prefix
	}
	return prefix
}

func allDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func packageIDFor(ecosystem Ecosystem, registry, namespace, normalizedName string) string {
	switch ecosystem {
	case EcosystemMaven:
		return fmt.Sprintf("%s://%s/%s:%s", ecosystem, registry, namespace, normalizedName)
	default:
		return fmt.Sprintf("%s://%s/%s", ecosystem, registry, normalizedName)
	}
}

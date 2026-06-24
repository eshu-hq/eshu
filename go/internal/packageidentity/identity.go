// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Ecosystem identifies the package-native contract used to normalize identity.
type Ecosystem string

const (
	// EcosystemNPM identifies npm package metadata.
	EcosystemNPM Ecosystem = "npm"
	// EcosystemPyPI identifies Python package metadata.
	EcosystemPyPI Ecosystem = "pypi"
	// EcosystemGoModule identifies Go module proxy metadata.
	EcosystemGoModule Ecosystem = "gomod"
	// EcosystemMaven identifies Maven repository metadata.
	EcosystemMaven Ecosystem = "maven"
	// EcosystemNuGet identifies NuGet V3 feed metadata.
	EcosystemNuGet Ecosystem = "nuget"
	// EcosystemComposer identifies PHP Composer package metadata.
	EcosystemComposer Ecosystem = "composer"
	// EcosystemRubyGems identifies RubyGems package metadata.
	EcosystemRubyGems Ecosystem = "rubygems"
	// EcosystemCargo identifies Rust Cargo crate metadata.
	EcosystemCargo Ecosystem = "cargo"
	// EcosystemSwift identifies Swift Package Manager source package metadata.
	EcosystemSwift Ecosystem = "swift"
	// EcosystemHex identifies Hex package metadata.
	EcosystemHex Ecosystem = "hex"
	// EcosystemPub identifies Dart Pub package metadata.
	EcosystemPub Ecosystem = "pub"
	// EcosystemOS identifies distro package metadata.
	EcosystemOS Ecosystem = "os"
	// EcosystemGeneric identifies provider-specific generic package metadata.
	EcosystemGeneric Ecosystem = "generic"
)

// RawIdentity is the source-observed package identity tuple.
type RawIdentity struct {
	Ecosystem        Ecosystem
	Registry         string
	RawName          string
	Namespace        string
	Version          string
	Classifier       string
	PURL             string
	BOMRef           string
	PackageManager   string
	SourcePath       string
	SourceSpecificID string
}

// Identity is the normalized package identity used for stable joins.
type Identity struct {
	Ecosystem        Ecosystem
	Registry         string
	RawName          string
	NormalizedName   string
	Namespace        string
	Version          string
	Classifier       string
	PURL             string
	BOMRef           string
	PackageManager   string
	SourcePath       string
	SourceSpecificID string
	PackageID        string
}

var pypiNormalizePattern = regexp.MustCompile(`[-_.]+`)

// Normalize applies ecosystem-specific identity rules and returns a stable
// package identity. It preserves raw fields for source debugging.
func Normalize(raw RawIdentity) (Identity, error) {
	ecosystem := NormalizeEcosystem(raw.Ecosystem)
	if ecosystem == "" {
		return Identity{}, fmt.Errorf("package ecosystem must not be blank")
	}
	registry := NormalizeRegistry(raw.Registry)
	if registry == "" {
		return Identity{}, fmt.Errorf("package registry must not be blank")
	}
	if ecosystem == EcosystemPub && registry == "pub.dartlang.org" {
		registry = "pub.dev"
	}
	rawName := strings.TrimSpace(raw.RawName)
	if rawName == "" {
		return Identity{}, fmt.Errorf("package name must not be blank")
	}
	namespace := strings.TrimSpace(raw.Namespace)
	classifier := strings.TrimSpace(raw.Classifier)
	version := strings.TrimSpace(raw.Version)
	packageManager := normalizePackageManager(raw.PackageManager, ecosystem)

	normalizedName, normalizedNamespace, err := normalizeName(ecosystem, rawName, namespace)
	if err != nil {
		return Identity{}, err
	}
	if ecosystem == EcosystemSwift {
		registry = normalizedNamespace
	}
	packageID := packageIDFor(ecosystem, registry, normalizedNamespace, normalizedName)
	purl := strings.TrimSpace(raw.PURL)
	if purl == "" {
		purl = packageURLFor(ecosystem, packageManager, registry, normalizedNamespace, normalizedName, version)
	}
	bomRef := strings.TrimSpace(raw.BOMRef)
	if bomRef == "" {
		bomRef = firstNonBlank(purl, packageID)
	}

	return Identity{
		Ecosystem:        ecosystem,
		Registry:         registry,
		RawName:          rawName,
		NormalizedName:   normalizedName,
		Namespace:        normalizedNamespace,
		Version:          version,
		Classifier:       classifier,
		PURL:             purl,
		BOMRef:           bomRef,
		PackageManager:   packageManager,
		SourcePath:       strings.TrimSpace(raw.SourcePath),
		SourceSpecificID: strings.TrimSpace(raw.SourceSpecificID),
		PackageID:        packageID,
	}, nil
}

// NormalizeEcosystem converts package-manager aliases into Eshu's canonical
// ecosystem names.
func NormalizeEcosystem(value Ecosystem) Ecosystem {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "npm", "node", "nodejs", "javascript", "typescript":
		return EcosystemNPM
	case "pypi", "pip", "python":
		return EcosystemPyPI
	case "go", "golang", "gomod", "go-module", "go_module":
		return EcosystemGoModule
	case "maven", "gradle", "java":
		return EcosystemMaven
	case "nuget", "dotnet", ".net":
		return EcosystemNuGet
	case "composer", "packagist", "php":
		return EcosystemComposer
	case "rubygems", "gem", "ruby":
		return EcosystemRubyGems
	case "cargo", "crate", "crates", "crates.io", "rust":
		return EcosystemCargo
	case "swift", "swifturl", "swiftpm", "spm", "swift-package-manager":
		return EcosystemSwift
	case "hex", "hexpm", "hex.pm":
		return EcosystemHex
	case "pub", "pub.dev", "dart", "dart-pub":
		return EcosystemPub
	case "os", "apk", "alpine", "deb", "debian", "rpm", "rhel", "ubuntu":
		return EcosystemOS
	case "generic":
		return EcosystemGeneric
	default:
		return ""
	}
}

// NormalizeRegistry normalizes a source registry host/path while preserving
// source-specific path casing.
func NormalizeRegistry(raw string) string {
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

func normalizeName(ecosystem Ecosystem, rawName, namespace string) (string, string, error) {
	switch ecosystem {
	case EcosystemNPM:
		return normalizeNPMName(rawName)
	case EcosystemPyPI:
		return pypiNormalizePattern.ReplaceAllString(strings.ToLower(rawName), "-"), "", nil
	case EcosystemGoModule:
		return rawName, goModuleNamespace(rawName), nil
	case EcosystemMaven:
		if namespace == "" {
			return "", "", fmt.Errorf("maven package identity requires group namespace")
		}
		return rawName, namespace, nil
	case EcosystemComposer:
		return normalizeTwoSegmentName(rawName, namespace, "composer")
	case EcosystemNuGet:
		return strings.ToLower(rawName), "", nil
	case EcosystemRubyGems, EcosystemCargo, EcosystemPub, EcosystemOS:
		return strings.ToLower(rawName), "", nil
	case EcosystemSwift:
		return normalizeSwiftName(rawName, namespace)
	case EcosystemHex:
		return strings.ToLower(rawName), strings.ToLower(strings.TrimSpace(namespace)), nil
	case EcosystemGeneric:
		return rawName, namespace, nil
	default:
		return "", "", fmt.Errorf("package ecosystem %q is unsupported", ecosystem)
	}
}

func normalizeNPMName(rawName string) (string, string, error) {
	name := strings.ToLower(rawName)
	if strings.HasPrefix(name, "@") {
		scope, packageName, ok := strings.Cut(strings.TrimPrefix(name, "@"), "/")
		if !ok || strings.TrimSpace(scope) == "" || strings.TrimSpace(packageName) == "" {
			return "", "", fmt.Errorf("scoped npm package must use @scope/name")
		}
		return "@" + scope + "/" + packageName, scope, nil
	}
	return name, "", nil
}

func normalizeTwoSegmentName(rawName, namespace, ecosystem string) (string, string, error) {
	value := strings.ToLower(strings.TrimSpace(rawName))
	ns := strings.ToLower(strings.TrimSpace(namespace))
	if ns == "" {
		left, right, ok := strings.Cut(value, "/")
		if !ok || strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
			return "", "", fmt.Errorf("%s package identity requires namespace/name", ecosystem)
		}
		return left + "/" + right, left, nil
	}
	if strings.HasPrefix(value, ns+"/") {
		return value, ns, nil
	}
	name := strings.TrimLeft(value, "/")
	return ns + "/" + name, ns, nil
}

func normalizeSwiftName(rawName, namespace string) (string, string, error) {
	ns := strings.ToLower(strings.TrimSuffix(NormalizeRegistry(namespace), ".git"))
	if ns == "" {
		return "", "", fmt.Errorf("swift package identity requires source namespace")
	}
	name := strings.ToLower(strings.Trim(strings.TrimSuffix(strings.TrimSpace(rawName), ".git"), "/"))
	name = strings.TrimPrefix(name, ns+"/")
	if strings.Contains(name, "/") {
		segments := strings.Split(strings.Trim(name, "/"), "/")
		name = segments[len(segments)-1]
	}
	if name == "" {
		return "", "", fmt.Errorf("swift package name must not be blank")
	}
	return name, ns, nil
}

func packageIDFor(ecosystem Ecosystem, registry, namespace, normalizedName string) string {
	switch ecosystem {
	case EcosystemMaven:
		return fmt.Sprintf("%s://%s/%s:%s", ecosystem, registry, namespace, normalizedName)
	case EcosystemSwift:
		return fmt.Sprintf("%s://%s/%s", ecosystem, namespace, normalizedName)
	case EcosystemHex:
		if namespace != "" {
			return fmt.Sprintf("%s://%s/%s/%s", ecosystem, registry, namespace, normalizedName)
		}
		return fmt.Sprintf("%s://%s/%s", ecosystem, registry, normalizedName)
	default:
		return fmt.Sprintf("%s://%s/%s", ecosystem, registry, normalizedName)
	}
}

func packageURLFor(ecosystem Ecosystem, packageManager, registry, namespace, normalizedName, version string) string {
	purlType := purlTypeFor(ecosystem, packageManager)
	path := purlPathFor(ecosystem, registry, namespace, normalizedName)
	purl := "pkg:" + purlType + "/" + path
	if version != "" {
		purl += "@" + version
	}
	return purl
}

func purlTypeFor(ecosystem Ecosystem, packageManager string) string {
	switch ecosystem {
	case EcosystemGoModule:
		return "golang"
	case EcosystemRubyGems:
		return "gem"
	case EcosystemOS:
		if packageManager != "" && packageManager != string(EcosystemOS) {
			return packageManager
		}
		return "generic"
	default:
		return string(ecosystem)
	}
}

func purlPathFor(ecosystem Ecosystem, registry, namespace, normalizedName string) string {
	switch ecosystem {
	case EcosystemNPM:
		return encodeNPMNameForPURL(normalizedName)
	case EcosystemMaven:
		return strings.Trim(namespace, "/") + "/" + strings.Trim(normalizedName, "/")
	case EcosystemSwift:
		return strings.Trim(namespace, "/") + "/" + strings.Trim(normalizedName, "/")
	case EcosystemHex:
		if namespace != "" {
			return strings.Trim(namespace, "/") + "/" + strings.Trim(normalizedName, "/")
		}
		return strings.Trim(normalizedName, "/")
	case EcosystemOS:
		return strings.Trim(registry, "/") + "/" + strings.Trim(normalizedName, "/")
	default:
		return strings.Trim(normalizedName, "/")
	}
}

func encodeNPMNameForPURL(name string) string {
	if strings.HasPrefix(name, "@") {
		return "%40" + strings.TrimPrefix(name, "@")
	}
	return name
}

func normalizePackageManager(raw string, ecosystem Ecosystem) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value != "" {
		return value
	}
	return string(ecosystem)
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

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

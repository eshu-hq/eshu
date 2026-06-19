package packageidentity

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultRegistry returns the canonical registry host used when a package
// reference carries no explicit registry. It is the single source of truth for
// registry defaults so every collector that derives a PackageID from a bare
// purl lands on the same canonical identity. The values match the per-collector
// tables in the vulnerability-intelligence and security-alerts collectors; keep
// them in lockstep when adding an ecosystem.
func DefaultRegistry(ecosystem Ecosystem) string {
	switch NormalizeEcosystem(ecosystem) {
	case EcosystemNPM:
		return "registry.npmjs.org"
	case EcosystemPyPI:
		return "pypi.org/simple"
	case EcosystemGoModule:
		return "proxy.golang.org"
	case EcosystemMaven:
		return "repo.maven.apache.org/maven2"
	case EcosystemNuGet:
		return "api.nuget.org/v3/index.json"
	case EcosystemComposer:
		return "repo.packagist.org"
	case EcosystemRubyGems:
		return "rubygems.org"
	case EcosystemCargo:
		return "crates.io"
	case EcosystemSwift:
		return "swift-package-manager"
	case EcosystemHex:
		return "hex.pm"
	case EcosystemPub:
		return "pub.dev"
	case EcosystemOS:
		return "os"
	default:
		return "unknown"
	}
}

// PackageIDFromPURL parses a Package URL and returns the canonical PackageID
// produced by Normalize, filling in the default registry for the ecosystem when
// the purl carries none. It lets SBOM and other purl-only sources correlate
// with vulnerability and package-registry facts on the same identity. It
// returns an empty string and a nil error for a blank or non-purl input so
// callers can treat "no canonical id" as a normal, non-fatal case.
func PackageIDFromPURL(purl string) (string, error) {
	raw, ok := parsePURLToRawIdentity(purl)
	if !ok {
		return "", nil
	}
	if raw.Registry == "" {
		raw.Registry = DefaultRegistry(raw.Ecosystem)
	}
	identity, err := Normalize(raw)
	if err != nil {
		return "", fmt.Errorf("normalize purl %q: %w", purl, err)
	}
	return identity.PackageID, nil
}

// parsePURLToRawIdentity decodes a pkg: purl into the identity fields Normalize
// consumes. It mirrors the purl parsing the OSV collector performs: it strips
// the version, qualifiers, and subpath, decodes the path, and folds an npm
// scope segment back into the name. It reports false for blank or non-purl
// input.
func parsePURLToRawIdentity(purl string) (RawIdentity, bool) {
	trimmed := strings.TrimSpace(purl)
	if !strings.HasPrefix(trimmed, "pkg:") {
		return RawIdentity{}, false
	}
	body := strings.TrimPrefix(trimmed, "pkg:")
	body, _, _ = strings.Cut(body, "?")
	body, _, _ = strings.Cut(body, "#")
	version := ""
	if lastAt := strings.LastIndex(body, "@"); lastAt > strings.LastIndex(body, "/") {
		version = body[lastAt+1:]
		body = body[:lastAt]
	}
	purlType, path, ok := strings.Cut(body, "/")
	if !ok {
		return RawIdentity{}, false
	}
	decodedPath, err := url.PathUnescape(path)
	if err != nil {
		decodedPath = path
	}
	ecosystem := canonicalPURLEcosystem(purlType)
	parts := strings.Split(decodedPath, "/")
	name := parts[len(parts)-1]
	namespace := strings.Join(parts[:len(parts)-1], "/")
	registry := ""
	packageManager := ""
	if ecosystem == EcosystemGoModule {
		name = decodedPath
		namespace = ""
	}
	if ecosystem == EcosystemOS {
		registry = namespace
		namespace = ""
		packageManager = strings.ToLower(strings.TrimSpace(purlType))
	}
	if ecosystem == EcosystemNPM && strings.HasPrefix(namespace, "@") {
		name = namespace + "/" + name
		namespace = strings.TrimPrefix(namespace, "@")
	}
	if name == "" {
		return RawIdentity{}, false
	}
	return RawIdentity{
		Ecosystem:      ecosystem,
		Registry:       registry,
		RawName:        name,
		Namespace:      namespace,
		Version:        strings.TrimSpace(version),
		PURL:           trimmed,
		PackageManager: packageManager,
	}, true
}

// canonicalPURLEcosystem maps a purl type token to the canonical ecosystem,
// falling back to the lowercased token when no canonical alias applies.
func canonicalPURLEcosystem(purlType string) Ecosystem {
	if normalized := NormalizeEcosystem(Ecosystem(purlType)); normalized != "" {
		return normalized
	}
	return Ecosystem(strings.ToLower(strings.TrimSpace(purlType)))
}

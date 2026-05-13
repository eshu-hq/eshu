package packageregistry

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ParseGoModuleProxyMetadata parses one offline GOPROXY metadata bundle into
// reported package, version, dependency, and artifact observations.
func ParseGoModuleProxyMetadata(ctx MetadataParserContext, document []byte) (ParsedMetadata, error) {
	if err := validateParserContext(ctx, EcosystemGoModule, "go module proxy"); err != nil {
		return ParsedMetadata{}, err
	}
	var bundle goModuleProxyBundle
	if err := json.Unmarshal(document, &bundle); err != nil {
		return ParsedMetadata{}, fmt.Errorf("parse go module proxy metadata: %w", err)
	}
	module := strings.TrimSpace(bundle.Module)
	if module == "" {
		return ParsedMetadata{}, fmt.Errorf("go module proxy metadata module must not be blank")
	}
	version := firstNonBlank(bundle.Info.Version, bundle.Version)

	identity := PackageIdentity{Ecosystem: EcosystemGoModule, Registry: ctx.Registry, RawName: module}
	parsed := ParsedMetadata{
		Packages: []PackageObservation{packageObservation(ctx, identity)},
	}
	if version != "" {
		versionObservation := versionObservation(ctx, identity, version)
		versionObservation.PublishedAt = parseTimestamp(bundle.Info.Time)
		if sanitizedZipURL := sanitizeURL(bundle.ZipURL); sanitizedZipURL != "" {
			versionObservation.ArtifactURLs = []string{sanitizedZipURL}
		}
		parsed.Versions = append(parsed.Versions, versionObservation)
		parsed.Artifacts = append(parsed.Artifacts, goModuleArtifact(ctx, identity, version, bundle))
	}
	parsed.Dependencies = goModuleDependencies(ctx, identity, version, bundle.Mod)
	return parsed, nil
}

type goModuleProxyBundle struct {
	Module  string       `json:"module"`
	Version string       `json:"version"`
	Info    goModuleInfo `json:"info"`
	Mod     string       `json:"mod"`
	ZipURL  string       `json:"zip_url"`
	Sum     string       `json:"sum"`
}

type goModuleInfo struct {
	Version string `json:"Version"`
	Time    string `json:"Time"`
}

func goModuleArtifact(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	bundle goModuleProxyBundle,
) PackageArtifactObservation {
	hashes := map[string]string{}
	if strings.TrimSpace(bundle.Sum) != "" {
		hashes["sum"] = strings.TrimSpace(bundle.Sum)
	}
	if len(hashes) == 0 {
		hashes = nil
	}
	return artifactObservation(
		ctx,
		identity,
		version,
		firstNonBlank(artifactKey(bundle.ZipURL), identity.RawName+"/@v/"+version+".zip"),
		"zip",
		bundle.ZipURL,
		identity.RawName+"/@v/"+version+".zip",
		0,
		hashes,
	)
}

func goModuleDependencies(
	ctx MetadataParserContext,
	identity PackageIdentity,
	version string,
	mod string,
) []PackageDependencyObservation {
	requirements := parseGoModRequires(mod)
	sort.Slice(requirements, func(i, j int) bool {
		return requirements[i].module < requirements[j].module
	})
	dependencies := make([]PackageDependencyObservation, 0, len(requirements))
	for _, requirement := range requirements {
		dependencyType := "runtime"
		if requirement.indirect {
			dependencyType = "indirect"
		}
		dependencies = append(dependencies, dependencyObservation(
			ctx,
			identity,
			version,
			PackageIdentity{Ecosystem: EcosystemGoModule, Registry: ctx.Registry, RawName: requirement.module},
			requirement.version,
			dependencyType,
		))
	}
	return dependencies
}

type goModRequirement struct {
	module   string
	version  string
	indirect bool
}

func parseGoModRequires(mod string) []goModRequirement {
	var requirements []goModRequirement
	inRequireBlock := false
	for _, line := range strings.Split(mod, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			continue
		case trimmed == "require (":
			inRequireBlock = true
			continue
		case inRequireBlock && trimmed == ")":
			inRequireBlock = false
			continue
		case strings.HasPrefix(trimmed, "require "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "require "))
		case !inRequireBlock:
			continue
		}
		requirement, ok := parseGoModRequireLine(trimmed)
		if ok {
			requirements = append(requirements, requirement)
		}
	}
	return requirements
}

func parseGoModRequireLine(line string) (goModRequirement, bool) {
	indirect := strings.Contains(line, "// indirect")
	beforeComment, _, _ := strings.Cut(line, "//")
	fields := strings.Fields(beforeComment)
	if len(fields) < 2 {
		return goModRequirement{}, false
	}
	return goModRequirement{
		module:   fields[0],
		version:  fields[1],
		indirect: indirect,
	}, true
}

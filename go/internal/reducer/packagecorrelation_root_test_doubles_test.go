// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// The builders below are the staying-root twins of the same-named fixtures
// in packagecorrelation's source_test.go (hint, repository, package, and
// manifest builders plus boolPtr) and publication_test.go
// (packageRegistryPackageVersionFact), kept for the supply-chain,
// security-alert, code-import, and admission tests that stayed in the reducer
// root when package correlation moved (#6061). Go test files cannot share
// unexported symbols across a package boundary, so each side keeps its own
// copy; the bodies are byte-identical by construction and must stay that way.

// boolPtr is the staying-root twin of the same-named helper in
// packagecorrelation's source_test.go, kept for the
// staying supply-chain tests that build *bool fields without importing the
// family package.

func packageSourceHintFact(packageID, hintKind, normalizedURL string, observedAt time.Time) facts.Envelope {
	return facts.Envelope{
		FactKind:   facts.PackageRegistrySourceHintFactKind,
		ObservedAt: observedAt,
		Payload: map[string]any{
			"package_id":        packageID,
			"hint_kind":         hintKind,
			"normalized_url":    normalizedURL,
			"raw_url":           normalizedURL,
			"confidence_reason": "test",
		},
	}
}

func packageSourceRepositoryFact(
	repositoryID string,
	repositoryName string,
	remoteURL string,
	tombstone bool,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactKind:      factload.FactKindRepository,
		ObservedAt:    observedAt,
		IsTombstone:   tombstone,
		StableFactKey: "repository:" + repositoryID,
		Payload: map[string]any{
			"graph_id":   repositoryID,
			"name":       repositoryName,
			"remote_url": remoteURL,
		},
	}
}

func packageRegistryPackageFact(
	packageID string,
	ecosystem string,
	normalizedName string,
	namespace string,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactID:        "package-fact:" + packageID,
		FactKind:      facts.PackageRegistryPackageFactKind,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "package_registry"},
		StableFactKey: "package:" + packageID,
		Payload: map[string]any{
			"package_id":      packageID,
			"ecosystem":       ecosystem,
			"normalized_name": normalizedName,
			"namespace":       namespace,
		},
	}
}

func packageRegistryPackageVersionFact(
	factID string,
	packageID string,
	versionID string,
	version string,
	publishedAt time.Time,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"package_id": packageID,
		"version_id": versionID,
		"version":    version,
	}
	if !publishedAt.IsZero() {
		payload["published_at"] = publishedAt.UTC().Format(time.RFC3339)
	}
	return facts.Envelope{
		FactID:        factID,
		FactKind:      facts.PackageRegistryPackageVersionFactKind,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "package_registry"},
		StableFactKey: "package-version:" + versionID,
		Payload:       payload,
	}
}

func packageManifestDependencyFact(
	repositoryID string,
	repositoryName string,
	relativePath string,
	dependencyName string,
	packageManager string,
	dependencyRange string,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactID:        "manifest-dep:" + repositoryID + ":" + dependencyName,
		FactKind:      factload.FactKindContentEntity,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		StableFactKey: "content_entity:" + repositoryID + ":" + dependencyName,
		Payload: map[string]any{
			"repo_id":       repositoryID,
			"relative_path": relativePath,
			"entity_type":   "Variable",
			"entity_name":   dependencyName,
			"entity_metadata": map[string]any{
				"config_kind":     "dependency",
				"package_manager": packageManager,
				"section":         "dependencies",
				"value":           dependencyRange,
			},
			"repo_name": repositoryName,
		},
	}
}

// packageManifestDependencyFactWithMetadata builds a content-entity manifest
// dependency fact envelope for supply-chain and security-alert tests that
// stayed in the reducer root when package correlation moved to
// internal/reducer/packagecorrelation (#6061). Its twin of the same name
// lives in packagecorrelation/source_test.go for the
// moved family tests; the two bodies are byte-identical by construction and
// must stay that way — a fixture that drifts from the moved copy silently
// changes what the staying tests cover. It uses only facts and the
// staying factKindContentEntity const, never family code.
func packageManifestDependencyFactWithMetadata(
	repositoryID string,
	repositoryName string,
	relativePath string,
	dependencyName string,
	packageManager string,
	dependencyRange string,
	observedAt time.Time,
	metadata map[string]any,
) facts.Envelope {
	metadata["config_kind"] = "dependency"
	metadata["package_manager"] = packageManager
	metadata["value"] = dependencyRange
	if _, ok := metadata["section"]; !ok {
		metadata["section"] = "dependencies"
	}
	return facts.Envelope{
		FactID:        "manifest-dep:" + repositoryID + ":" + dependencyName,
		FactKind:      factKindContentEntity,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		StableFactKey: "content_entity:" + repositoryID + ":" + dependencyName,
		Payload: map[string]any{
			"repo_id":         repositoryID,
			"relative_path":   relativePath,
			"entity_type":     "Variable",
			"entity_name":     dependencyName,
			"entity_metadata": metadata,
			"repo_name":       repositoryName,
		},
	}
}

func boolPtr(value bool) *bool {
	return &value
}

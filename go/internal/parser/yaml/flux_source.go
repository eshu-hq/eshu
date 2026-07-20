// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"strings"
)

// fluxSourceToolkitGroup is the Flux CD apiVersion group for the
// source.toolkit.fluxcd.io source-of-truth CRDs (GitRepository,
// OCIRepository, Bucket). These CRs previously fell through to the generic
// k8s_resources parser (parseK8sResource), which drops every nested spec
// field except a handful of well-known ones -- spec.url and spec.ref, the
// fields a Flux Kustomization's sourceRef actually reconciles against, were
// silently lost. Dedicated walkers capture them as typed evidence (issue
// #5360).
const fluxSourceToolkitGroup = "source.toolkit.fluxcd.io/"

// isFluxGitRepository reports whether a document is a Flux CD GitRepository
// custom resource: apiVersion group "source.toolkit.fluxcd.io" (any version)
// with kind "GitRepository".
func isFluxGitRepository(apiVersion string, kind string) bool {
	return strings.HasPrefix(apiVersion, fluxSourceToolkitGroup) && kind == "GitRepository"
}

// isFluxOCIRepository reports whether a document is a Flux CD OCIRepository
// custom resource: apiVersion group "source.toolkit.fluxcd.io" (any version)
// with kind "OCIRepository".
func isFluxOCIRepository(apiVersion string, kind string) bool {
	return strings.HasPrefix(apiVersion, fluxSourceToolkitGroup) && kind == "OCIRepository"
}

// isFluxBucket reports whether a document is a Flux CD Bucket custom
// resource: apiVersion group "source.toolkit.fluxcd.io" (any version) with
// kind "Bucket".
func isFluxBucket(apiVersion string, kind string) bool {
	return strings.HasPrefix(apiVersion, fluxSourceToolkitGroup) && kind == "Bucket"
}

// parseFluxGitRepository captures a Flux GitRepository CR's reconciliation
// target: spec.url and spec.ref (branch/tag/semver/commit -- Flux resolves
// exactly one of these per revision, so each is captured independently and
// only when present).
//
// Fields are parsed defensively: an absent or empty field is omitted from the
// row, never fabricated.
func parseFluxGitRepository(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	return parseFluxSourceRepository(document, metadata, path, lineNumber)
}

// parseFluxOCIRepository captures a Flux OCIRepository CR's reconciliation
// target: spec.url (an oci:// reference) and spec.ref (tag/semver/digest --
// digest is captured under ref_commit alongside GitRepository's commit SHA
// since both identify an immutable content-addressed revision).
//
// Fields are parsed defensively: an absent or empty field is omitted from the
// row, never fabricated.
func parseFluxOCIRepository(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	return parseFluxSourceRepository(document, metadata, path, lineNumber)
}

// parseFluxSourceRepository is the shared spec.url/spec.ref extraction for
// GitRepository and OCIRepository -- both nest an identical url/ref shape
// under spec, differing only in what the url scheme and ref fields mean
// semantically (git branch/tag/commit vs. OCI tag/semver/digest).
//
// Identity boundary: a source CR that uses metadata.generateName instead of
// metadata.name has an empty name here (never a fabricated "<nil>"), so its
// row identity is (repo_id, path, label, name="", start_line). That is unique
// because two same-label entities cannot share a start line in one file --
// multi-document YAML forces a distinct `---` document start line per entity.
// This breaks only if a kind:List item-expansion ever reused the parent
// document's line for its items, which this parser does not do.
func parseFluxSourceRepository(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	spec, _ := document["spec"].(map[string]any)
	ref, _ := spec["ref"].(map[string]any)

	row := map[string]any{
		// name is a base-identity field, kept present for row-shape stability
		// (goldens/cassettes) even when empty. metadata.name is absent whenever
		// the manifest uses metadata.generateName; cleanYAMLString yields ""
		// rather than the "<nil>" that fmt.Sprint(nil) would fabricate.
		"name":        cleanYAMLString(metadata["name"]),
		"line_number": lineNumber,
		"path":        path,
		"lang":        "yaml",
	}
	// generateName is emitted as evidence only when present (omit-when-absent),
	// so an empty name is honestly explained by the literal manifest field
	// rather than looking like dropped data.
	if generateName := cleanYAMLString(metadata["generateName"]); generateName != "" {
		row["generate_name"] = generateName
	}
	// metadata.namespace is injected at apply-time far more often than it is
	// written in the manifest, so an absent namespace is the common case: omit
	// it, never fabricate "<nil>" (fmt.Sprint(nil)) or an empty string.
	if namespace := cleanYAMLString(metadata["namespace"]); namespace != "" {
		row["namespace"] = namespace
	}
	if url := cleanYAMLString(spec["url"]); url != "" {
		row["url"] = url
	}
	if branch := cleanYAMLString(ref["branch"]); branch != "" {
		row["ref_branch"] = branch
	}
	if tag := cleanYAMLString(ref["tag"]); tag != "" {
		row["ref_tag"] = tag
	}
	if semver := cleanYAMLString(ref["semver"]); semver != "" {
		row["ref_semver"] = semver
	}
	if commit := cleanYAMLString(ref["commit"]); commit != "" {
		row["ref_commit"] = commit
	}
	if digest := cleanYAMLString(ref["digest"]); digest != "" {
		// OCIRepository.spec.ref.digest identifies an immutable revision the
		// same way GitRepository.spec.ref.commit does; folding it into
		// ref_commit keeps one field name for "pinned to an exact revision"
		// across both source kinds instead of a source-kind-specific key.
		row["ref_commit"] = digest
	}
	if labels := collectMetadataLabels(metadata); labels != "" {
		row["labels"] = labels
	}
	return row
}

// parseFluxBucket captures a Flux Bucket CR's object-storage coordinates:
// spec.bucketName, spec.endpoint, spec.provider.
//
// Fields are parsed defensively: an absent or empty field is omitted from the
// row, never fabricated.
func parseFluxBucket(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	spec, _ := document["spec"].(map[string]any)

	row := map[string]any{
		// name kept present for row-shape stability; "" (not "<nil>") when the
		// manifest uses metadata.generateName instead of metadata.name.
		"name":        cleanYAMLString(metadata["name"]),
		"line_number": lineNumber,
		"path":        path,
		"lang":        "yaml",
	}
	if generateName := cleanYAMLString(metadata["generateName"]); generateName != "" {
		row["generate_name"] = generateName
	}
	// metadata.namespace is injected at apply-time far more often than it is
	// written in the manifest, so an absent namespace is the common case: omit
	// it, never fabricate "<nil>" (fmt.Sprint(nil)) or an empty string.
	if namespace := cleanYAMLString(metadata["namespace"]); namespace != "" {
		row["namespace"] = namespace
	}
	if bucketName := cleanYAMLString(spec["bucketName"]); bucketName != "" {
		row["bucket_name"] = bucketName
	}
	if endpoint := cleanYAMLString(spec["endpoint"]); endpoint != "" {
		row["endpoint"] = endpoint
	}
	if provider := cleanYAMLString(spec["provider"]); provider != "" {
		row["provider"] = provider
	}
	if labels := collectMetadataLabels(metadata); labels != "" {
		row["labels"] = labels
	}
	return row
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/environment"
)

// This file broadens deployment-environment detection beyond the directory
// regexes in projection.go (environmentPathPatterns): the Helm
// values-<env>.yaml/values.<env>.yaml filename convention, and the
// destination namespace ArgoCD Applications/ApplicationSets and Kustomize
// overlays already declare but that deploymentEnvironments never read
// (issue #5444). Every environment candidate here is admitted only when it
// resolves through the canonical environment-alias contract
// (go/internal/environment): environment.IsKnownToken gates which raw values
// count as a real environment, and environment.Canonical folds recognized
// aliases (production/staging/development) to their canonical form. An
// unrecognized filename suffix or namespace never becomes an environment --
// the contract's no-invention rule is absolute.

// helmValuesFilenameEnvironment infers a Helm values override file's
// environment from its filename convention -- "values-prod.yaml" or
// "values.prod.yaml" -> "prod" -- gated through environment.IsKnownToken so a
// values-schema, values-example, or values-template file never invents an
// environment. Returns "" for the base values.yaml/values.yml, for any
// suffix outside the known-token set, and for non-values/non-YAML
// filenames. Matching is on the WHOLE suffix after "values-"/"values.", not
// a token split, so "values-production-notes.yaml" is correctly rejected
// rather than matching "production" as a false positive.
func helmValuesFilenameEnvironment(relativePath string) string {
	filename := filepath.Base(relativePath)
	lower := strings.ToLower(filename)
	ext := filepath.Ext(lower)
	if ext != ".yaml" && ext != ".yml" {
		return ""
	}
	base := strings.TrimSuffix(lower, ext)
	for _, sep := range []string{"values-", "values."} {
		suffix, cut := strings.CutPrefix(base, sep)
		if !cut || suffix == "" {
			continue
		}
		if environment.IsKnownToken(suffix) {
			return environment.Canonical(suffix)
		}
	}
	return ""
}

// namespaceEnvironment resolves a Kubernetes/ArgoCD namespace string to a
// canonical environment name, gated through the environment-alias contract.
// It admits the namespace as an environment only when the whole normalized
// namespace, or one of its `-`/`_`/`.`-delimited tokens, is a known
// environment token (environment.IsKnownToken) -- so "kube-system" or
// "my-app" never invent an environment, while "production", "app-prod", and
// "myapp_staging" resolve to their canonical form. Token matching is exact
// (map lookup), so a substring like "product" never falsely matches "prod".
// This mirrors the token-scan gate environmentFromArtifactPath already uses
// for artifact-path evidence (cross_repo_evidence_artifacts.go).
func namespaceEnvironment(namespace string) string {
	normalized := environment.Normalize(namespace)
	if normalized == "" {
		return ""
	}
	if environment.IsKnownToken(normalized) {
		return environment.Canonical(normalized)
	}
	for _, token := range strings.FieldsFunc(normalized, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	}) {
		if environment.IsKnownToken(token) {
			return environment.Canonical(token)
		}
	}
	return ""
}

// namespaceEnvironmentBuckets lists the parsed_file_data buckets and the
// field within each row that carries a destination/target namespace:
// argocd_applications and argocd_applicationsets rows carry dest_namespace
// (go/internal/parser/yaml/argocd.go), and kustomize_overlays rows carry
// namespace (go/internal/parser/yaml/kustomize_semantics.go). Deliberately
// excludes the generic k8s_resources namespace field: an arbitrary
// Deployment/Service namespace (e.g. "default", an app-named namespace) is
// workload placement evidence, not deployment-environment evidence, and
// treating it as one would over-admit environments the alias contract's
// no-invention rule forbids.
var namespaceEnvironmentBuckets = []struct {
	bucket string
	field  string
}{
	{bucket: "argocd_applications", field: "dest_namespace"},
	{bucket: "argocd_applicationsets", field: "dest_namespace"},
	{bucket: "kustomize_overlays", field: "namespace"},
}

// collectNamespaceEnvironmentsFromFileData extracts canonical environment
// names from the ArgoCD destination namespace and Kustomize overlay
// namespace fields already present in a file fact's parsed_file_data, gating
// every candidate through namespaceEnvironment. Returns nil when fileData is
// nil or carries no admissible namespace evidence -- never invents an
// environment from an unrecognized namespace.
func collectNamespaceEnvironmentsFromFileData(fileData map[string]any) []string {
	if fileData == nil {
		return nil
	}
	var environments []string
	for _, target := range namespaceEnvironmentBuckets {
		for _, item := range sliceValue(fileData[target.bucket]) {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if env := namespaceEnvironment(payloadStr(row, target.field)); env != "" {
				environments = append(environments, env)
			}
		}
	}
	return environments
}

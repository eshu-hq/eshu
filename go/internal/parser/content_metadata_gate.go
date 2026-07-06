// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"
)

// contentMetadataGatedExtensions are the file-extension suffixes that can, by
// extension alone, change inferContentMetadata's result (see isYAMLSuffix,
// isHCLSuffix, isJinjaSuffix, isTerraformTemplateSuffix in
// templated_detection.go). Any file whose last suffix is in this set MUST run
// through inferContentMetadata; shouldSkipContentMetadata never skips it.
var contentMetadataGatedExtensions = map[string]struct{}{
	".yaml": {}, ".yml": {},
	".hcl": {}, ".tf": {}, ".tfvars": {},
	".tpl": {}, ".tftpl": {},
	".jinja": {}, ".jinja2": {}, ".j2": {},
}

// contentMetadataGatedPathSegments are the lower-cased path segments that
// inferRootFamily, ansibleArtifactType, and isIACRelevant match on regardless
// of file extension:
//   - roles/playbooks/handlers/tasks/group_vars/host_vars/inventory/inventories
//     -> ansible_jinja root family (inferRootFamily) and ansible_* artifact
//     types (ansibleArtifactType), both path-only checks.
//   - dagster/assets/data_quality/data_lakehouse -> dagster_jinja root family.
//   - chart/templates/argocd -> helm_argo root family (chart+templates pair,
//     or argocd alone; templates alone also gates the "_helpers.tpl" root
//     family check).
//   - iac -> isIACRelevant's final fallback (hasPart(parts, "iac")), which can
//     flip IACRelevant to true for a file that matched no other rule.
//
// A file under any of these segments MUST run through inferContentMetadata
// even when its extension looks like plain source code (Ansible/Dagster
// modules are frequently .py/.js under these directories).
var contentMetadataGatedPathSegments = []string{
	"roles", "playbooks", "handlers", "tasks",
	"group_vars", "host_vars", "inventory", "inventories",
	"dagster", "assets", "data_quality", "data_lakehouse",
	"chart", "templates", "argocd",
	"iac",
}

// contentMetadataAnsiblePlaybookMarkers mirrors isAnsiblePlaybookContent's
// substring checks. That function is path-independent: any file whose raw
// content contains one of these substrings (case-insensitive) is classified
// ansible_playbook regardless of extension or directory, so the gate must
// never skip a file matching one of them.
var contentMetadataAnsiblePlaybookMarkers = []string{
	"\nhosts:",
	"\nroles:",
	"\nvars_files:",
	"\nimport_playbook:",
}

// shouldSkipContentMetadata reports whether ParsePath can skip
// inferContentMetadata and use the contentMetadata{} zero value
// (ArtifactType="", TemplateDialect="", IACRelevant=false) without changing
// the persisted result.
//
// inferContentMetadata runs three regex scans (goLineControlRE,
// tfTemplatefileRE, and an internal re-scan inside inferRootFamily) that cost
// roughly 7.5ms on a large PHP/JS file -- about 5% of ParsePath wall time
// across the corpus (issue #4768). Most files carry no IaC/template signal at
// all, so this gate lets ParsePath skip that work for the common case while
// leaving every path that could legitimately produce a non-zero
// contentMetadata untouched.
//
// This function is intentionally conservative: it returns false (run the
// real inference) whenever ANY condition below holds, and true only when NONE
// of them hold. Each condition traces to a specific predicate in
// templated_detection.go -- see the package-level var comments above and the
// exhaustive equivalence test in content_metadata_gate_test.go, which asserts
// byte-identical results against the unconditional inferContentMetadata call
// for every branch (including a proof that widening the gate to ignore path
// segments is caught as a regression).
func shouldSkipContentMetadata(path string, content string) bool {
	suffixes := splitSuffixes(path)
	if len(suffixes) > 0 {
		if _, gated := contentMetadataGatedExtensions[suffixes[len(suffixes)-1]]; gated {
			return false
		}
	}

	name := strings.ToLower(filepath.Base(path))
	if name == "chart.yaml" || strings.HasPrefix(name, "values.") {
		return false
	}
	if name == "dockerfile" || strings.HasPrefix(name, "dockerfile.") {
		return false
	}
	if isDockerComposeFilename(name) {
		return false
	}

	parts := pathParts(path)
	if hasPart(parts, contentMetadataGatedPathSegments...) {
		return false
	}
	if hasPart(parts, ".github") && hasPart(parts, "workflows") {
		return false
	}

	lowered := strings.ToLower(content)
	for _, marker := range contentMetadataAnsiblePlaybookMarkers {
		if strings.Contains(lowered, marker) {
			return false
		}
	}

	return true
}

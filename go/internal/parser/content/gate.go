// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"path/filepath"
	"strings"
)

// contentMetadataGatedExtensions are the file-extension suffixes that can, by
// extension alone, change Infer's result (see isYAMLSuffix,
// isHCLSuffix, isJinjaSuffix, isTerraformTemplateSuffix, isRawConfigSuffix,
// and the ".kcl" special case in infer.go). A file is gated
// (never skipped) if ANY of its dot-suffixes is in this set -- not just the
// last one. infer.go itself checks suffixes this way:
// inferArtifactType and inferRootFamily both call anySuffix(suffixes, ...)
// over the FULL suffix list returned by SplitSuffixes, so a multi-dot name
// like "vars.tf.json" (suffixes [".tf", ".json"]) still resolves through the
// ".tf" match even though ".json" is the last suffix. Checking only the last
// suffix is a correctness bug: it would skip "vars.tf.json" and
// "main.tf.json" (real terraform_hcl, iac_relevant=true) because their last
// suffix is ".json".
//
// ".conf"/".cfg"/".cnf" (isRawConfigSuffix) must be included: inferArtifactType
// resolves anySuffix(suffixes, isRawConfigSuffix) to generic_config (or
// nginx_config/apache_config by content substring, or a "_template" variant),
// all of which set IACRelevant=true and a non-empty ArtifactType.
//
// ".kcl" must be included: Infer's non-YAML branch
// (line ~107) explicitly excludes ".kcl" from the "treat as plain source"
// switch case (`!isYAMLSuffix(lastSuffix) && lastSuffix != ".kcl"`), so a
// ".kcl" file carrying Go-template or Jinja markers resolves through the same
// bucket switch as YAML (go_template_yaml/jinja_yaml, IACRelevant=true)
// instead of being treated as inert non-YAML source.
var contentMetadataGatedExtensions = map[string]struct{}{
	".yaml": {}, ".yml": {},
	".hcl": {}, ".tf": {}, ".tfvars": {},
	".tpl": {}, ".tftpl": {},
	".jinja": {}, ".jinja2": {}, ".j2": {},
	".conf": {}, ".cfg": {}, ".cnf": {},
	".kcl": {},
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
// A file under any of these segments MUST run through Infer
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

// ShouldSkip reports whether ParsePath can skip Infer and use the Metadata{}
// zero value (ArtifactType="", TemplateDialect="", IACRelevant=false)
// without changing the persisted result.
//
// Infer runs three regex scans (goLineControlRE,
// tfTemplatefileRE, and an internal re-scan inside inferRootFamily) that cost
// roughly 7.5ms on a large PHP/JS file -- about 5% of ParsePath wall time
// across the corpus (issue #4768). Most files carry no IaC/template signal at
// all, so this gate lets ParsePath skip that work for the common case while
// leaving every path that could legitimately produce a non-zero
// Metadata untouched.
//
// This function is intentionally conservative: it returns false (run the
// real inference) whenever ANY condition below holds, and true only when NONE
// of them hold. Each condition traces to a specific predicate in
// infer.go, derived from a full line-by-line audit of every
// branch in that file (Infer, inferRootFamily,
// inferArtifactType, ansibleArtifactType, isAnsiblePlaybookContent,
// persistedArtifactType, isIACRelevant, and every is*Suffix/isDockerCompose*
// helper), not just the branches exercised by hand-picked examples:
//
//   - Extension checks mirror anySuffix semantics exactly: infer.go
//     never looks at only the last dot-suffix (inferArtifactType and
//     inferRootFamily both call anySuffix(suffixes, ...) over the FULL suffix
//     list), so this gate checks every suffix SplitSuffixes returns, not just
//     the last one. A last-suffix-only check is a correctness bug for
//     multi-dot names like "vars.tf.json" (suffixes [".tf", ".json"]), which
//     is real terraform_hcl via its ".tf" suffix despite ".json" being last.
//   - The nginx/apache path-segment checks inside inferArtifactType
//     (hasPart(parts, "apache"/"httpd"/"mods-available"/"nginx")) only ever
//     run inside the isRawConfigSuffix branch, so once ".conf"/".cfg"/".cnf"
//     are gated extensions those path segments need no separate check here --
//     any file that could reach them already fails the extension check.
//   - Go-template/Jinja/GitHub-Actions/Terraform marker content
//     (goExpressionRE/jinjaStatementRE/githubActionsExprRE/tfInterpolationRE/
//     tfDirectiveRE/tfTemplatefileRE) can flip Infer's internal
//     "bucket" variable to "unknown_templated" even for a plain-extension
//     file (the `!isYAMLSuffix(lastSuffix) && lastSuffix != ".kcl"` branch),
//     but persistedArtifactType's default case returns "" for
//     artifactType=="plain_text" regardless of bucket, and
//     inferArtifactType resolves "plain_text" for any path matching none of
//     its suffix/basename/path-segment cases -- so this content signal alone
//     never changes the persisted result for a file that already clears
//     every other check in this function. Verified directly:
//     Infer("main.py", "x = ${{ foo }}") and equivalent Jinja/
//     Go-template/templatefile() probes on .py/.go/.js all return the zero
//     Metadata{}.
//
// See the package-level var comments above and the exhaustive generative
// differential test in gate_test.go
// (TestShouldSkipContentMetadataGeneratedEquivalence), which enumerates a
// cartesian product of extensions (including multi-dot shapes), directory
// signals, and content signals and asserts byte-identical results against
// the unconditional Infer call for every generated case --
// including a proof that a too-wide, extension-only (no path-segment check)
// gate is caught as a regression.
func ShouldSkip(path string, content string) bool {
	suffixes := SplitSuffixes(path)
	for _, suffix := range suffixes {
		if _, gated := contentMetadataGatedExtensions[suffix]; gated {
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

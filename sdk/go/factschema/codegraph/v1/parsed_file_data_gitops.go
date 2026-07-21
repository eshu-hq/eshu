// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// This file types five more closed-shape parsed_file_data inner keys
// (Contract System v1 §7 incremental migration, issue #5445 slice 1),
// following the same pattern as parsed_file_data.go and
// parsed_file_data_terraform.go: name what go/internal/relationships reads,
// leave everything else in an open Attributes pass-through, and leave
// File.ParsedFileData itself an open map[string]any so the wire schema stays
// byte-identical.
//
// Every string field below is a comma-joined CSV string on the wire, not a
// JSON array -- both the HCL and YAML parsers (go/internal/parser/hcl,
// go/internal/parser/yaml) always emit these multi-value fields as
// strings.Join(values, ","), and go/internal/relationships splits them back
// out with csvValues/tupleCSVValues at the call site. That CSV-join/split
// convention is business logic owned by the relationships package, not the
// payload contract, so it stays a plain string field here, matching the wire
// shape exactly.

// HelmChart is the typed view of one entry in a parsed_file_data
// "helm_charts" inner slice: a Helm Chart.yaml's dependency metadata
// (go/internal/parser/yaml/helm.go parseHelmChart, one row per Chart.yaml).
// Only the three fields discoverStructuredHelmEvidence
// (go/internal/relationships/structured_family_evidence.go) reads are named.
type HelmChart struct {
	// Name is the chart's declared name (Chart.yaml `name:`).
	Name string `json:"name,omitempty"`
	// Dependencies is the comma-joined, sorted set of declared subchart
	// dependency names (Chart.yaml `dependencies[].name`).
	Dependencies string `json:"dependencies,omitempty"`
	// DependencyRepositories is the comma-joined, sorted, deduplicated set of
	// normalized dependency repository references
	// (Chart.yaml `dependencies[].repository`, normalizeHelmRepositoryRef).
	DependencyRepositories string `json:"dependency_repositories,omitempty"`
	// Attributes carries every producer field with no named struct field
	// above (line_number, version, app_version, chart_type, description,
	// path, lang), preserving each value's JSON-native Go type.
	Attributes map[string]any `json:"-"`
}

// HelmValues is the typed view of one entry in a parsed_file_data
// "helm_values" inner slice: a Helm values file's base summary row
// (go/internal/parser/yaml/helm.go parseHelmValues, one row per values*.yaml
// file). Only the two fields discoverStructuredHelmEvidence
// (go/internal/relationships/structured_family_evidence.go) reads are named.
type HelmValues struct {
	// Name is the values file's base name with its extension stripped (for
	// example "values" for values.yaml, "values-prod" for values-prod.yaml).
	Name string `json:"name,omitempty"`
	// ImageRepositories is the comma-joined set of container image
	// repositories collectHelmImageRepositories found in the values
	// document.
	ImageRepositories string `json:"image_repositories,omitempty"`
	// Attributes carries every producer field with no named struct field
	// above (line_number, top_level_keys, path, lang), preserving each
	// value's JSON-native Go type.
	Attributes map[string]any `json:"-"`
}

// ArgoCDApplication is the typed view of one entry in a parsed_file_data
// "argocd_applications" inner slice: an ArgoCD Application manifest
// (go/internal/parser/yaml/argocd.go parseArgoCDApplication). Only the
// fields discoverStructuredArgoCDEvidence
// (go/internal/relationships/structured_family_evidence.go,
// argoApplicationSourceRefs) reads are named: the plural *_repos/*_paths/
// *_roots/*_revisions fields for a multi-source Application (spec.sources),
// their singular fallbacks for a single-source Application (spec.source),
// and the three destination fields.
type ArgoCDApplication struct {
	// Name is the Application's metadata.name.
	Name string `json:"name,omitempty"`
	// SourceRepo is the single-source spec.source.repoURL, set only when the
	// Application has exactly one source (appendArgoApplicationSourceFields'
	// "no sources" branch always sets this key, possibly to "").
	SourceRepo string `json:"source_repo,omitempty"`
	// SourceRepos is the comma-joined repoURL of every spec.sources entry
	// (or the single spec.source, joined as a one-element list), set only
	// when at least one source has a non-empty repoURL.
	SourceRepos string `json:"source_repos,omitempty"`
	// SourcePath is the single-source spec.source.path fallback, read only
	// when SourceRepos resolves to exactly one repo and SourcePaths is
	// empty.
	SourcePath string `json:"source_path,omitempty"`
	// SourcePaths is the comma-joined path of every source entry, index
	// -aligned with SourceRepos.
	SourcePaths string `json:"source_paths,omitempty"`
	// SourceRoot is the single-source normalized root-directory fallback,
	// read only when SourceRepos resolves to exactly one repo and
	// SourceRoots is empty.
	SourceRoot string `json:"source_root,omitempty"`
	// SourceRoots is the comma-joined normalized root directory of every
	// source entry, index-aligned with SourceRepos.
	SourceRoots string `json:"source_roots,omitempty"`
	// SourceRevision is the single-source spec.source.targetRevision
	// fallback, read only when SourceRepos resolves to exactly one repo and
	// SourceRevisions is empty.
	SourceRevision string `json:"source_revision,omitempty"`
	// SourceRevisions is the comma-joined targetRevision of every source
	// entry, index-aligned with SourceRepos.
	SourceRevisions string `json:"source_revisions,omitempty"`
	// DestName is spec.destination.name.
	DestName string `json:"dest_name,omitempty"`
	// DestNamespace is spec.destination.namespace.
	DestNamespace string `json:"dest_namespace,omitempty"`
	// DestServer is spec.destination.server.
	DestServer string `json:"dest_server,omitempty"`
	// Attributes carries every producer field with no named struct field
	// above (line_number, namespace, project, labels, sync_policy,
	// sync_policy_options, path, lang), preserving each value's JSON-native
	// Go type.
	Attributes map[string]any `json:"-"`
}

// ArgoCDApplicationSet is the typed view of one entry in a parsed_file_data
// "argocd_applicationsets" inner slice: an ArgoCD ApplicationSet manifest
// (go/internal/parser/yaml/argocd.go parseArgoCDApplicationSet). Only the
// fields two independent consumers read are named:
// discoverStructuredArgoCDEvidence
// (go/internal/relationships/structured_family_evidence.go) and
// structuredApplicationSetGeneratorRepos
// (go/internal/relationships/argocd_generator_config.go, an intentionally
// separate read site the two-phase per-commit backfill uses -- see that
// file's doc comment).
type ArgoCDApplicationSet struct {
	// Name is the ApplicationSet's metadata.name.
	Name string `json:"name,omitempty"`
	// GeneratorSourceRepos is the comma-joined, deduplicated set of
	// spec.generators[].git.repoURL values -- the config repositories a git
	// file/directory generator reads to discover template inputs.
	GeneratorSourceRepos string `json:"generator_source_repos,omitempty"`
	// GeneratorSourcePaths is the comma-joined, deduplicated set of
	// generator file/directory glob paths.
	GeneratorSourcePaths string `json:"generator_source_paths,omitempty"`
	// GeneratorSourceRoots is the comma-joined, deduplicated set of
	// normalized root directories derived from GeneratorSourcePaths.
	GeneratorSourceRoots string `json:"generator_source_roots,omitempty"`
	// SourceRoots is the comma-joined, deduplicated set of normalized root
	// directories across BOTH generator and template paths combined, read as
	// a fallback when GeneratorSourceRoots or TemplateSourceRoots is empty.
	SourceRoots string `json:"source_roots,omitempty"`
	// TemplateSourceRepos is the comma-joined, deduplicated set of
	// spec.template.spec.source(s).repoURL values -- the repositories the
	// rendered Application(s) deploy from.
	TemplateSourceRepos string `json:"template_source_repos,omitempty"`
	// TemplateSourcePaths is the comma-joined, deduplicated set of
	// spec.template.spec.source(s).path values.
	TemplateSourcePaths string `json:"template_source_paths,omitempty"`
	// TemplateSourceRoots is the comma-joined, deduplicated set of normalized
	// root directories derived from TemplateSourcePaths.
	TemplateSourceRoots string `json:"template_source_roots,omitempty"`
	// DestName is spec.template.spec.destination.name.
	DestName string `json:"dest_name,omitempty"`
	// DestNamespace is spec.template.spec.destination.namespace.
	DestNamespace string `json:"dest_namespace,omitempty"`
	// DestServer is spec.template.spec.destination.server.
	DestServer string `json:"dest_server,omitempty"`
	// Attributes carries every producer field with no named struct field
	// above (line_number, namespace, generators, project, source_repos,
	// source_paths, path, lang), preserving each value's JSON-native Go
	// type.
	Attributes map[string]any `json:"-"`
}

// FluxGitRepository is the typed view of one entry in a parsed_file_data
// "flux_git_repositories" inner slice: a Flux CD GitRepository custom
// resource (go/internal/parser/yaml/flux_source.go
// parseFluxSourceRepository). Only Name and URL are named -- the two fields
// discoverStructuredFluxEvidence
// (go/internal/relationships/flux_evidence.go) reads to resolve spec.url by
// strict normalized-URL equality to a catalog repository.
type FluxGitRepository struct {
	// Name is metadata.name, empty when the manifest uses
	// metadata.generateName instead (cleanYAMLString never fabricates a
	// "<nil>" placeholder).
	Name string `json:"name,omitempty"`
	// URL is spec.url, the Git remote Flux reconciles from. Omitted from the
	// producer row entirely when spec.url is absent or empty (never an empty
	// string), so an absent URL here means "no url declared," not "url
	// decoded as empty."
	URL string `json:"url,omitempty"`
	// Attributes carries every producer field with no named struct field
	// above (line_number, path, lang, generate_name, namespace, ref_branch,
	// ref_tag, ref_semver, ref_commit, labels), preserving each value's
	// JSON-native Go type.
	Attributes map[string]any `json:"-"`
}

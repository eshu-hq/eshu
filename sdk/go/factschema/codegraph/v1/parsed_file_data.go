// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// This file types the closed-shape, single-producer inner keys of a File
// fact's parsed_file_data map (Contract System v1 §7 incremental migration,
// issue #4750 S1). File.ParsedFileData itself stays an open map[string]any —
// these structs type only the specific inner keys the code-graph-core reducer
// READS, decoded on demand through the parent factschema package's
// DecodeParsedFileData* accessors (decode_parsed_file_data.go), mirroring the
// aws_resource/Attributes open-object pattern (sdk/go/factschema/AGENTS.md):
// type what a consumer joins on, pass everything else through untyped so no
// producer field is dropped and the wire schema for parsed_file_data stays
// byte-identical (no schema major bump).
//
// The first five keys below (GomodState through DockerfileStage) are #4750
// S1's original low-risk batch: each is produced by exactly one parser
// subsystem with a stable, closed element shape, unlike the wide
// per-language AST buckets (imports/functions/function_calls/classes/variables/
// framework_semantics) whose element shape is a union of many independently
// evolving per-language field sets, deferred to later #4750 increments. The
// four dead-producer keys the issue originally listed (docker_compose_services,
// github_actions_workflow_triggers, github_actions_reusable_workflow_refs,
// jenkins_pipeline_calls) have zero producers on this branch and are tracked
// separately by cleanup issue #4771, not typed here.
//
// ImageOverride (added by issue #5440) follows the same closed-shape,
// single-key-per-struct pattern but is typed proactively, ahead of a reducer
// read: #5440 only adds the parser-side image_overrides bucket, and #5441
// (graph projection) is the first consumer. The accessor exists from day one
// so #5441 decodes through a typed struct rather than a raw map lookup.

// GomodState is the typed view of a parsed_file_data "gomod_state" inner key,
// the per-file parse-state envelope the Go module manifest parser emits
// (go/internal/parser/gomod/parser.go for go.mod, gomod/gosum.go for go.sum).
//
// Only the two fields the cross-repo-export reducer reads are named: State (the
// "parsed"/"malformed" discriminator) and ModulePath (the declared module path
// goModuleDeclaredPath resolves, go/internal/reducer/
// code_call_materialization_cross_repo_export.go). Every other producer field
// (go_version, toolchain, the *_count tallies, replaced_modules, parse_error,
// checksum_count, ambiguous_entry) differs between the go.mod and go.sum
// producers and is carried verbatim in the open Attributes pass-through so the
// accessor drops no evidence — mirroring aws_resource.Attributes. ModulePath is
// present only on a parsed go.mod, so it is optional; State is present on every
// gomod_state envelope both producers emit.
type GomodState struct {
	// State is the parse-state discriminator ("parsed" or "malformed") both the
	// go.mod and go.sum producers stamp on every gomod_state envelope. Required:
	// a gomod_state key with no state is a malformed producer emission.
	State string `json:"state"`

	// ModulePath is the declared module path from a parsed go.mod's module
	// directive. Optional: go.sum and malformed go.mod envelopes omit it, and
	// the reducer treats an absent module_path as "not a resolvable Go module
	// declaration" rather than a failure.
	ModulePath *string `json:"module_path,omitempty"`

	// Attributes carries every gomod_state field with no named struct field
	// above (go_version, toolchain, require_count, replace_count, exclude_count,
	// retract_count, indirect_count, replaced_modules, parse_error,
	// checksum_count, ambiguous_entry, ...), preserving each value's JSON-native
	// Go type. It is the open pass-through that keeps the accessor from dropping
	// producer evidence the reducer does not yet read.
	Attributes map[string]any `json:"-"`
}

// SCIPFunctionCall is the typed view of one edge in a parsed_file_data
// "function_calls_scip" inner slice, the cross-file caller/callee reference the
// SCIP index importer emits (go/internal/parser/scip_parser.go
// appendSCIPReference). The SCIP importer is the single producer of this key
// and writes a closed, stable edge shape, so every field the SCIP code-call
// extractor reads (go/internal/reducer/code_call_materialization_index_rows.go
// extractSCIPCodeCallRows) is named here with no open pass-through: unlike the
// polymorphic AST buckets, this element has no per-language variance to carry.
//
// The *Line fields are int: the producer writes Go ints, and a Postgres JSONB
// round trip yields float64, both of which the parent module's assignField
// coerces into an int field.
type SCIPFunctionCall struct {
	// CallerSymbol is the SCIP symbol string of the enclosing definition the
	// reference occurs in.
	CallerSymbol string `json:"caller_symbol,omitempty"`
	// CallerFile is the absolute path of the file the reference occurs in; the
	// extractor resolves it to a caller entity id via the file/line index.
	CallerFile string `json:"caller_file,omitempty"`
	// CallerLine is the definition line of the enclosing caller symbol.
	CallerLine int `json:"caller_line,omitempty"`
	// CalleeSymbol is the SCIP symbol string of the referenced definition.
	CalleeSymbol string `json:"callee_symbol,omitempty"`
	// CalleeFile is the absolute path of the referenced definition's file.
	CalleeFile string `json:"callee_file,omitempty"`
	// CalleeLine is the definition line of the referenced callee symbol.
	CalleeLine int `json:"callee_line,omitempty"`
	// CalleeName is the short display name derived from the callee symbol.
	CalleeName string `json:"callee_name,omitempty"`
	// RefLine is the line the reference (the call site) occurs on, used as the
	// edge's dedupe key component and provenance ref_line.
	RefLine int `json:"ref_line,omitempty"`
}

// DockerfileStage is the typed view of one entry in a parsed_file_data
// "dockerfile_stages" inner slice, a Dockerfile FROM stage the Dockerfile
// runtime-metadata parser emits (go/internal/parser/dockerfile/metadata.go
// stageMap). It is a single-producer, closed shape. The workload-signal reducer
// (go/internal/reducer/candidate_loader.go) currently only tests the slice for
// presence, so this struct exists to make the stage shape a typed contract for
// that presence check and any future stage-field consumer; the runtime fields
// beyond the always-present identity block are optional because stageMap emits
// them only when non-empty (addOptional).
type DockerfileStage struct {
	// Name is the stage's resolved name (its AS alias, else its base image, else
	// a synthesized stage_N). Always emitted.
	Name string `json:"name,omitempty"`
	// LineNumber is the source line of the FROM instruction. Always emitted.
	LineNumber int `json:"line_number,omitempty"`
	// StageIndex is the zero-based order of this FROM in the file. Always
	// emitted.
	StageIndex int `json:"stage_index,omitempty"`
	// BaseImage is the image reference before any tag/digest. Always emitted.
	BaseImage string `json:"base_image,omitempty"`
	// BaseTag is the image tag, empty when the reference is untagged or
	// digest-pinned. Always emitted (may be "").
	BaseTag string `json:"base_tag,omitempty"`
	// Alias is the AS alias, empty when the stage is unnamed. Always emitted
	// (may be "").
	Alias string `json:"alias,omitempty"`
	// Path is filepath.Base(Name), the stage's path-shaped identity. Always
	// emitted.
	Path string `json:"path,omitempty"`
	// Attributes carries the optional runtime fields stageMap only emits when
	// non-empty (platform, copies_from, workdir, entrypoint, cmd, user,
	// healthcheck) plus the "lang" tag, preserving their JSON-native types.
	Attributes map[string]any `json:"-"`
}

// ImageOverride is the typed view of one entry in a parsed_file_data
// "image_overrides" inner slice, one declared container image override the
// Helm values and Kustomize image-list parsers each emit
// (go/internal/parser/yaml/image_overrides.go collectHelmImageOverrides /
// collectKustomizeImageOverrides, issue #5440). It carries the per-image
// tag/digest version truth that helm_values[].image_repositories and
// kustomize_overlays[].image_refs intentionally discard from their own
// stable, tag-less identity buckets. Both producers write the identical
// closed row shape, so every field is named here with no open Attributes
// pass-through -- unlike DockerfileStage above, no third producer can add an
// unlisted field to this key.
type ImageOverride struct {
	// Name's meaning is SOURCE-DEPENDENT -- do not read it as "the image"
	// without checking Source first. For a Helm row, Name is the declared
	// repository (which may itself carry an inline tag/digest, e.g.
	// "repo:v1"). For a Kustomize row, Name is the images[].name MATCH
	// TARGET -- the image reference Kustomize is patching, NOT the image
	// that actually gets deployed. When a Kustomize row's newName is set
	// (Repository != Name), Name is the REPLACED image, not the deployed
	// one; Repository is what actually ships. Repository is the only field
	// safe to use for image identity across BOTH sources -- use it, not
	// Name, unless the caller has already branched on Source and
	// specifically wants the Kustomize match target. This is a one-bucket,
	// two-meaning field by construction (issue #5440 review); splitting it
	// into source-specific fields is a design decision issue #5441 owns.
	Name string `json:"name,omitempty"`
	// Repository is the resolved, version-stripped image repository: Helm's
	// repository with any inline tag/digest removed
	// (normalizeContainerImageRepository), or Kustomize's newName when set,
	// else name.
	Repository string `json:"repository,omitempty"`
	// Tag is the declared image tag: a Helm sibling `tag:` key (or a tag
	// parsed out of an inline "repo:tag" repository string when no sibling
	// key is present) or Kustomize's newTag. Empty when no tag is declared.
	Tag string `json:"tag,omitempty"`
	// Digest is the declared image digest in full "sha256:..." form: a Helm
	// sibling `digest:` key (or a digest parsed out of an inline
	// "repo@sha256:..." repository string) or Kustomize's digest. Empty when
	// no digest is declared.
	Digest string `json:"digest,omitempty"`
	// Environment is the inferred deployment environment ("prod", "staging",
	// ...), or "" when it cannot be confidently inferred. See
	// go/internal/parser/yaml/image_overrides.go for the conservative,
	// filename/path-based inference rule; issue #5444 owns broader
	// environment detection.
	Environment string `json:"environment,omitempty"`
	// Source identifies the producing parser: "helm" or "kustomize".
	Source string `json:"source,omitempty"`
	// Path is the source file path.
	Path string `json:"path,omitempty"`
	// LineNumber is the declaring document's source line number.
	LineNumber int `json:"line_number,omitempty"`
	// Lang is always "yaml", both producers being YAML-family parsers.
	Lang string `json:"lang,omitempty"`
}

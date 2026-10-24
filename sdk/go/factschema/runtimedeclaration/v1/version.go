// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Closed spec_kind members for Version.SpecKind. The set is closed for
// producers but tolerant for readers: a reader that meets a member it does not
// know must keep the row and treat it as not comparable, never fail the decode.
const (
	// SpecKindExact is a full version such as "20.11.1". VersionMajor,
	// VersionMinor and VersionPatch are set.
	SpecKindExact = "exact"
	// SpecKindPartial is a version with fewer than three components such as
	// "20" or "3.2". Only the components written are set.
	SpecKindPartial = "partial"
	// SpecKindRange is a comparator expression such as ">=18 <21". The
	// Range* fields are set where known and the version parts stay nil.
	SpecKindRange = "range"
	// SpecKindAlias is a named reference such as "lts/iron", "node",
	// "system" or "stable". Aliases are never resolved offline.
	SpecKindAlias = "alias"
	// SpecKindUnresolved is a spec holding an unexpanded placeholder such as
	// "${X}" or "${{ inputs.node-version }}". raw_spec carries it verbatim.
	SpecKindUnresolved = "unresolved"
)

// Version is the schema-version-1 typed payload for the
// "runtime_declaration.version" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md, issue #8815, epic #8807).
//
// One Version fact is one runtime version a repository declares in one file:
// a version-pin file such as ".nvmrc" or ".tool-versions" today, and later
// package.json, go.mod, Dockerfile and CI setup inputs. A line that lists
// several versions (".tool-versions": "nodejs 20.1.0 18.0.0") is one fact per
// token, told apart by OrderIndex.
//
// The nine required fields are RepoID, RelativePath, ScopeDir, LineNumber,
// OrderIndex, SourceKind, Runtime, RawSpec and SpecKind. Every other field is
// an omitempty pointer so an absent value stays distinct from zero: the
// version parts, the range bounds, ToolName and Distribution are never
// zero-filled.
//
// Version parts (VersionMajor, VersionMinor, VersionPatch) are set only for
// SpecKindExact and SpecKindPartial. A range row leaves all three nil and
// carries RangeLowerMajor, RangeUpperMajor, RangeLower, RangeUpper and the two
// inclusive flags instead, so a comparison such as "major below 20" can
// exclude ranges, aliases and unresolved rows instead of guessing.
type Version struct {
	// CollectorInstanceID identifies the collector run that emitted this
	// fact. Optional: it is run metadata, not part of the declaration.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// RepoID is the canonical repository identifier that holds the declaring
	// file. Required.
	RepoID string `json:"repo_id"`

	// RelativePath is the declaring file's repository-relative path, slash
	// separated. Required.
	RelativePath string `json:"relative_path"`

	// ScopeDir is the directory of the declaring file: "." for the repository
	// root, otherwise slash separated, repository-relative, cleaned, with no
	// leading "./" and no trailing "/". It is never a governed workspace.
	// Required.
	ScopeDir string `json:"scope_dir"`

	// LineNumber is the 1-based line of the declaration within RelativePath.
	// Required.
	LineNumber int `json:"line_number"`

	// OrderIndex is the 0-based position of the token on its line, so
	// "nodejs 20.1.0 18.0.0" is two facts on the same line. Required.
	OrderIndex int `json:"order_index"`

	// SourceKind names the kind of source the declaration came from, for
	// example "nvmrc" or "tool_versions". One value per file name, snake
	// case, open set. Required.
	SourceKind string `json:"source_kind"`

	// Runtime is the canonical runtime name: node, go, python, ruby, java,
	// rust or terraform, lower case. An unknown tool name is carried verbatim
	// and lower cased, so the set is open. Required.
	Runtime string `json:"runtime"`

	// ToolName is the verbatim tool token when it differed from Runtime, for
	// example "nodejs" for runtime "node". Optional.
	ToolName *string `json:"tool_name,omitempty"`

	// RawSpec is the trimmed version token exactly as written, for example
	// "v20.11.1" or "17.0.2-tem". Required.
	RawSpec string `json:"raw_spec"`

	// SpecKind is one of the SpecKind constants. Required. Readers must
	// tolerate a member they do not know.
	SpecKind string `json:"spec_kind"`

	// VersionMajor, VersionMinor and VersionPatch are the parsed numeric
	// components, set only for SpecKindExact and SpecKindPartial. A leading
	// "v" is dropped from the parts but kept in RawSpec. Optional.
	VersionMajor *int `json:"version_major,omitempty"`
	VersionMinor *int `json:"version_minor,omitempty"`
	VersionPatch *int `json:"version_patch,omitempty"`

	// Distribution is the vendor suffix of a distribution-qualified version,
	// for example "tem" for sdkman's "17.0.2-tem". Optional.
	Distribution *string `json:"distribution,omitempty"`

	// RangeLower and RangeUpper are the raw bound text of a range, for
	// example ">=18" and "<21". For a disjunction such as "^14 || >=16"
	// RangeLower is the whole spec and RangeUpper is nil. Optional.
	RangeLower *string `json:"range_lower,omitempty"`
	RangeUpper *string `json:"range_upper,omitempty"`

	// RangeLowerMajor and RangeUpperMajor are the integer major of each
	// bound. Optional.
	RangeLowerMajor *int `json:"range_lower_major,omitempty"`
	RangeUpperMajor *int `json:"range_upper_major,omitempty"`

	// RangeLowerInclusive and RangeUpperInclusive say whether each bound
	// includes its major. Optional.
	RangeLowerInclusive *bool `json:"range_lower_inclusive,omitempty"`
	RangeUpperInclusive *bool `json:"range_upper_inclusive,omitempty"`
}

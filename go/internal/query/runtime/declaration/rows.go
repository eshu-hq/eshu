// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Row is one declared runtime version: one RuntimeDeclaration node. There is no
// effective-version view in v1, so a repository with a root .nvmrc and a
// services/web/.nvmrc returns both rows, each with its scope_dir ("." at the
// repository root).
//
// The version and range parts are nullable pointers that serialize as explicit
// nulls. version_major, version_minor and version_patch are set only for
// "exact" and "partial" spec kinds; a "range" row leaves them null and carries
// the range_* parts, and "alias" and "unresolved" rows carry neither (the
// runtime_declaration.version contract, #8815). A reader must not treat a null
// version part as zero.
type Row struct {
	RepoID              string  `json:"repo_id"`
	RepoName            string  `json:"repo_name,omitempty"`
	RelativePath        string  `json:"relative_path"`
	ScopeDir            string  `json:"scope_dir"`
	LineNumber          int     `json:"line_number"`
	OrderIndex          int     `json:"order_index"`
	Runtime             string  `json:"runtime"`
	SourceKind          string  `json:"source_kind"`
	ToolName            *string `json:"tool_name"`
	RawSpec             string  `json:"raw_spec"`
	SpecKind            string  `json:"spec_kind"`
	VersionMajor        *int    `json:"version_major"`
	VersionMinor        *int    `json:"version_minor"`
	VersionPatch        *int    `json:"version_patch"`
	Distribution        *string `json:"distribution"`
	RangeLower          *string `json:"range_lower"`
	RangeUpper          *string `json:"range_upper"`
	RangeLowerMajor     *int    `json:"range_lower_major"`
	RangeUpperMajor     *int    `json:"range_upper_major"`
	RangeLowerInclusive *bool   `json:"range_lower_inclusive"`
	RangeUpperInclusive *bool   `json:"range_upper_inclusive"`
}

// rowFromGraph maps one page row.
func rowFromGraph(row map[string]any) Row {
	return Row{
		RepoID:              querycontract.StringVal(row, "repo_id"),
		RelativePath:        querycontract.StringVal(row, "relative_path"),
		ScopeDir:            querycontract.StringVal(row, "scope_dir"),
		LineNumber:          querycontract.IntVal(row, "line_number"),
		OrderIndex:          querycontract.IntVal(row, "order_index"),
		Runtime:             querycontract.StringVal(row, "runtime"),
		SourceKind:          querycontract.StringVal(row, "source_kind"),
		ToolName:            nullableString(row, "tool_name"),
		RawSpec:             querycontract.StringVal(row, "raw_spec"),
		SpecKind:            querycontract.StringVal(row, "spec_kind"),
		VersionMajor:        nullableInt(row, "version_major"),
		VersionMinor:        nullableInt(row, "version_minor"),
		VersionPatch:        nullableInt(row, "version_patch"),
		Distribution:        nullableString(row, "distribution"),
		RangeLower:          nullableString(row, "range_lower"),
		RangeUpper:          nullableString(row, "range_upper"),
		RangeLowerMajor:     nullableInt(row, "range_lower_major"),
		RangeUpperMajor:     nullableInt(row, "range_upper_major"),
		RangeLowerInclusive: nullableBool(row, "range_lower_inclusive"),
		RangeUpperInclusive: nullableBool(row, "range_upper_inclusive"),
	}
}

// cursorKey returns the keyset position of row.
func (r Row) cursorKey() cursorKey {
	return cursorKey{
		Version: cursorVersion, RepoID: r.RepoID, RelativePath: r.RelativePath,
		LineNumber: r.LineNumber, Runtime: r.Runtime, OrderIndex: r.OrderIndex,
	}
}

func nullableString(row map[string]any, key string) *string {
	if row[key] == nil {
		return nil
	}
	value := querycontract.StringVal(row, key)
	return &value
}

func nullableInt(row map[string]any, key string) *int {
	if row[key] == nil {
		return nil
	}
	value := querycontract.IntVal(row, key)
	return &value
}

func nullableBool(row map[string]any, key string) *bool {
	if row[key] == nil {
		return nil
	}
	value := querycontract.BoolVal(row, key)
	return &value
}

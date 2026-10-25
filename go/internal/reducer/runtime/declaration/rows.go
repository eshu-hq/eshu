// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"fmt"
	"sort"
	"strings"

	runtimedeclarationv1 "github.com/eshu-hq/eshu-sdk/factschema/runtimedeclaration/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// ExtractRows decodes every runtime_declaration.version envelope through the
// typed seam and returns one write row per declaration key, sorted by key so
// batches are deterministic. A fact missing a required field is quarantined
// (never zero-filled); an unsupported schema major is returned as a fatal
// error for durable triage. A fact whose identity strings are blank after
// trimming is dropped, because it cannot name a node.
//
// Two facts with the same (repo_id, relative_path, line_number, runtime,
// order_index) collapse to one row carrying the later fact, so a replayed
// generation projects the same count.
//
// Nullable version parts stay nil in the row. The writer SETs them, so a nil
// removes the property instead of storing 0, which would read as a real
// version below every threshold.
func ExtractRows(envelopes []facts.Envelope) ([]map[string]any, []factdecode.QuarantinedFact, error) {
	byKey := make(map[string]map[string]any)
	var quarantined []factdecode.QuarantinedFact
	for _, env := range envelopes {
		if env.FactKind != facts.RuntimeDeclarationVersionFactKind || env.IsTombstone {
			continue
		}
		version, err := schemadecode.DecodeRuntimeDeclarationVersion(env)
		if err != nil {
			q, isQuarantine, fatal := factdecode.PartitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		row, key, ok := declarationRow(version)
		if !ok {
			continue
		}
		byKey[key] = row
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, byKey[key])
	}
	return rows, quarantined, nil
}

func declarationRow(v runtimedeclarationv1.Version) (map[string]any, string, bool) {
	repoID := strings.TrimSpace(v.RepoID)
	relativePath := strings.TrimSpace(v.RelativePath)
	runtime := strings.TrimSpace(v.Runtime)
	if repoID == "" || relativePath == "" || runtime == "" {
		return nil, "", false
	}
	key := fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%d", repoID, relativePath, v.LineNumber, runtime, v.OrderIndex)
	return map[string]any{
		"repo_id":               repoID,
		"relative_path":         relativePath,
		"line_number":           v.LineNumber,
		"runtime":               runtime,
		"order_index":           v.OrderIndex,
		"scope_dir":             v.ScopeDir,
		"source_kind":           v.SourceKind,
		"tool_name":             optionalString(v.ToolName),
		"raw_spec":              v.RawSpec,
		"spec_kind":             v.SpecKind,
		"version_major":         optionalInt(v.VersionMajor),
		"version_minor":         optionalInt(v.VersionMinor),
		"version_patch":         optionalInt(v.VersionPatch),
		"distribution":          optionalString(v.Distribution),
		"range_lower":           optionalString(v.RangeLower),
		"range_upper":           optionalString(v.RangeUpper),
		"range_lower_major":     optionalInt(v.RangeLowerMajor),
		"range_upper_major":     optionalInt(v.RangeUpperMajor),
		"range_lower_inclusive": optionalBool(v.RangeLowerInclusive),
		"range_upper_inclusive": optionalBool(v.RangeUpperInclusive),
	}, key, true
}

// The optional helpers return an untyped nil for a nil pointer, so the Bolt
// driver sends a Cypher null rather than a typed nil it cannot encode.
func optionalInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func optionalString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func optionalBool(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

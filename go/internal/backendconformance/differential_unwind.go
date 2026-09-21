// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// unwindVarPattern matches the batch variable of a UNWIND batch statement:
// UNWIND $rows AS row. Only the first UNWIND clause binds the explosion; a
// later UNWIND over a derived collection stays inside the group key, so a
// regrouped inner batch still splits groups loudly instead of pairing
// vacuously.
var unwindVarPattern = regexp.MustCompile(`(?i)\bUNWIND\s+\$([A-Za-z_][A-Za-z0-9_]*)\s+AS\b`)

// unwindBatchVar returns the batch variable of statement, or "" when the
// statement is not an UNWIND batch.
func unwindBatchVar(statement string) string {
	match := unwindVarPattern.FindStringSubmatch(statement)
	if match == nil {
		return ""
	}
	return match[1]
}

// inFilterPattern matches an IN-list filter variable: WHERE x IN $files.
// Only a variable referenced exactly once in the statement binds the
// explosion — a repeated reference may depend on the list beyond membership
// (ordinal access, projection), so multi-use variables keep whole-batch
// pairing.
var inFilterPattern = regexp.MustCompile(`(?i)\bIN\s+\$([A-Za-z_][A-Za-z0-9_]*)`)

// inFilterVar returns the single-use IN-list variable of statement, or ""
// when the statement has none, has several, repeats the variable, or is an
// UNWIND batch (the UNWIND path owns grouping there). A lone IN reference is
// a membership filter by construction, so element-wise pairing cannot hide
// order truth; anything else keeps whole-batch pairing.
func inFilterVar(statement string) string {
	if unwindBatchVar(statement) != "" {
		return ""
	}
	matches := inFilterPattern.FindAllStringSubmatch(statement, -1)
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		seen[match[1]] = struct{}{}
	}
	if len(seen) != 1 {
		return ""
	}
	for variable := range seen {
		if strings.Count(statement, "$"+variable) != 1 {
			return ""
		}
		return variable
	}
	return ""
}

// explodeRecordGroups returns the comparison groups one record belongs to:
// normally just its fingerprint, but a batch statement whose batch variable
// binds a parameter list expands into one group per element (the statement
// text plus the scalar parameters plus that element). UNWIND batches bind
// the UNWIND variable; IN-list filters (stale-generation retraction passes)
// bind a single-use IN variable. Two legs that slice the same rows or files
// into different batches pair element-wise; a leg that never attempts an
// element leaves its group one-sided.
//
// The element view reuses [DifferentialFingerprint] with the element folded
// back under its variable name, keeping the group key deterministic,
// reportable, and allowlist-matchable. A batch variable bound to a non-list,
// a missing variable, an empty batch, or unparseable parameters falls back
// to the whole fingerprint: an unrecognized shape compares strictly instead
// of pairing vacuously.
func explodeRecordGroups(fp DifferentialFingerprint) []DifferentialFingerprint {
	variable := unwindBatchVar(fp.Statement)
	if variable == "" {
		variable = inFilterVar(fp.Statement)
	}
	if variable == "" {
		return []DifferentialFingerprint{fp}
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(fp.Parameters), &params); err != nil {
		return []DifferentialFingerprint{fp}
	}
	raw, ok := params[variable].([]any)
	if !ok || len(raw) == 0 {
		return []DifferentialFingerprint{fp}
	}
	groups := make([]DifferentialFingerprint, 0, len(raw))
	for _, element := range raw {
		key, err := explodeElementKey(fp.Statement, params, variable, element)
		if err != nil {
			return []DifferentialFingerprint{fp}
		}
		groups = append(groups, key)
	}
	return groups
}

// explodeElementKey folds one batch element back under its variable name
// next to the scalar parameters and encodes the element view. JSON encoding
// sorts map keys, so the same element in different batch positions keys
// identically; lists inside the view sort by encoding, so the same logical
// write with evidence in a different order keys identically too.
func explodeElementKey(statement string, params map[string]any, variable string, element any) (DifferentialFingerprint, error) {
	view := make(map[string]any, len(params))
	for key, value := range params {
		if key == variable {
			continue
		}
		view[key] = value
	}
	view[variable] = element
	encoded, err := json.Marshal(sortElementLists(view))
	if err != nil {
		return DifferentialFingerprint{}, fmt.Errorf("encode batch element view: %w", err)
	}
	return DifferentialFingerprint{Statement: statement, Parameters: string(encoded)}, nil
}

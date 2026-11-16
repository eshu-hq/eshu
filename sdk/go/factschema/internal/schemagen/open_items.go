// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	"encoding/json"
	"fmt"
)

// withItemTypes rewrites the element schema of one array property in an
// already generated schema to a closed type union such as ["string","object"].
//
// An open Go []any reflects to "items": true, which the collector conformance
// validator's schema subset refuses to compile (sdk/go/collector/conformance
// compiles items as a property schema). A field whose collectors disagree on
// the element shape, such as vulnerability.cve cwes (id strings from NVD and
// GitLab Gemnasium, {"cwe_id": ...} objects from the CVE List), needs a union
// instead. The output keeps reflectSchema's deterministic indentation and
// trailing newline so the checked-in artifact stays byte-stable.
func withItemTypes(schema []byte, property string, types ...string) ([]byte, error) {
	var generic map[string]any
	if err := json.Unmarshal(schema, &generic); err != nil {
		return nil, fmt.Errorf("schemagen: unmarshal schema to set %s items: %w", property, err)
	}
	properties, _ := generic["properties"].(map[string]any)
	prop, ok := properties[property].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schemagen: schema has no property %q to set item types on", property)
	}
	union := make([]any, 0, len(types))
	for _, name := range types {
		union = append(union, name)
	}
	prop["items"] = map[string]any{"type": union}

	out, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("schemagen: marshal schema after setting %s items: %w", property, err)
	}
	return append(out, '\n'), nil
}

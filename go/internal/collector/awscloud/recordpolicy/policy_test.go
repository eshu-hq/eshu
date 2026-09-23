// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpolicy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

const schemaDir = "../../../../../sdk/go/factschema/schema"

// schemaKeys walks every aws_*.json contract schema and collects each
// declared property key at any depth (object properties, array items,
// additionalProperties sub-schemas and $defs).
func schemaKeys(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(schemaDir, "aws_*.json"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no aws schemas under %s: %v", schemaDir, err)
	}
	keys := map[string]struct{}{}
	var walk func(node any)
	walk = func(node any) {
		object, ok := node.(map[string]any)
		if !ok {
			return
		}
		if properties, ok := object["properties"].(map[string]any); ok {
			for key, child := range properties {
				keys[key] = struct{}{}
				walk(child)
			}
		}
		for _, nested := range []string{"items", "additionalProperties"} {
			walk(object[nested])
		}
		if defs, ok := object["$defs"].(map[string]any); ok {
			for _, def := range defs {
				walk(def)
			}
		}
	}
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		walk(doc)
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// TestPolicyCoversEveryAWSSchemaKey is the table-completeness gate: a payload
// key the contracts module declares but the policy does not classify would
// be opaque at record time -- silently losing truth -- so it fails here
// first, naming the key.
func TestPolicyCoversEveryAWSSchemaKey(t *testing.T) {
	policy := recordpolicy.Policy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var missing []string
	for _, key := range schemaKeys(t) {
		if _, ok := policy.Fields[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d aws/v1 schema key(s) are not classified by recordpolicy.Policy: %v", len(missing), missing)
	}
}

// TestPolicyIsCopiedPerCall: adjusting one call's table must not leak into
// the next caller.
func TestPolicyIsCopiedPerCall(t *testing.T) {
	first := recordpolicy.Policy()
	first.Fields["account_id"] = recordpseudo.ClassKeep
	if recordpolicy.Policy().Fields["account_id"] != recordpseudo.ClassAccount {
		t.Fatal("Policy() returned a shared map")
	}
	if recordpolicy.Policy().Fields["attributes"] != recordpseudo.ClassKeep {
		t.Fatal("attributes must be Keep so the engine recurses per child key")
	}
}

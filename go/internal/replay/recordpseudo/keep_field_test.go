// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// TestKeepFieldsIgnoreExactOnlyTokens (round 4 F11): a Keep value equal to
// a short learned name or tag value, or to a numeric tag value, is kept; a
// Keep value equal to a substitutable (four-plus character) name is still
// rewritten, which the sibling-join test depends on.
func TestKeepFieldsIgnoreExactOnlyTokens(t *testing.T) {
	key := mustKey(t, keyA)
	policy := recordpseudo.Policy{Fields: map[string]recordpseudo.Class{
		"kind": recordpseudo.ClassKeep, "kind2": recordpseudo.ClassKeep, "count": recordpseudo.ClassKeep, "owner": recordpseudo.ClassKeep,
		"name": recordpseudo.ClassIdent, "tags": recordpseudo.ClassTagValue,
	}}
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("scope", nil, map[string]any{
			"kind": "abc", "name": "abc",
			"kind2": "xyz", "count": "7", "owner": "payments-team",
			"tags": map[string]any{"Name": "xyz", "Tier": "7", "Owner": "payments-team"},
		}),
	}}, key, policy)
	p := envs[0][0].Payload
	for field, want := range map[string]string{"kind": "abc", "kind2": "xyz", "count": "7"} {
		if got := fmt.Sprint(p[field]); got != want {
			t.Errorf("Keep %s rewritten to shape %q", field, shapeOf(got))
		}
	}
	mustMatch(t, "name", fmt.Sprint(p["name"]), `^`+hexName+`$`)
	mustMatch(t, "Keep owner equal to a long tag value", fmt.Sprint(p["owner"]), `^t[0-9a-f]{11}$`)
	tags, _ := p["tags"].(map[string]any)
	for _, tagKey := range []string{"Name", "Tier", "Owner"} {
		mustMatch(t, "tag "+tagKey, fmt.Sprint(tags[tagKey]), `^[tn][0-9a-f]{11}$`)
	}
}

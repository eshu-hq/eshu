// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// TestShortWholeNamesAreRewrittenInComposites (round 3 F2): a name of three
// characters is exact-only for free text, but it is still a whole component
// of the stable key, the source uri and the record id, and those composites
// must not keep it raw. Regions and numeric qualifiers stay intact.
func TestShortWholeNamesAreRewrittenInComposites(t *testing.T) {
	key := mustKey(t, keyA)
	roleARN := "arn:aws:iam::" + acct + ":role/abc"
	gen := generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
		"account_id": acct, "region": "us-east-1", "name": "abc", "arn": roleARN,
	})
	gen = withRefs(gen, "aws:lambda:"+acct+":us-east-1:function:abc:1", "aws://iam/"+acct+"/role/abc", "abc")
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	env := envs[0][0]
	mustMatch(t, "name", fmt.Sprint(env.Payload["name"]), `^`+hexName+`$`)
	mustMatch(t, "arn", fmt.Sprint(env.Payload["arn"]), `^arn:aws:iam::`+pseudoAcct+`:role/`+hexName+`$`)
	for label, composite := range map[string]string{"stable_fact_key": env.StableFactKey, "source_uri": env.SourceRef.SourceURI, "source_record_id": env.SourceRef.SourceRecordID} {
		if hasComponent(composite, "abc") {
			t.Errorf("%s keeps the short name as a whole component: shape %q", label, shapeOf(composite))
		}
	}
	mustMatch(t, "stable_fact_key", env.StableFactKey, `^aws:lambda:`+pseudoAcct+`:us-east-1:function:`+hexName+`:1$`)
	mustMatch(t, "source_uri", env.SourceRef.SourceURI, `^aws://iam/`+pseudoAcct+`/role/`+hexName+`$`)
	if !strings.Contains(env.StableFactKey, ":us-east-1:") {
		t.Errorf("region lost from the stable key")
	}
}

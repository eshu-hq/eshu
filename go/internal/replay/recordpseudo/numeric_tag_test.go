// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// TestShortNumericTagValuesAreKept (round 5 F13): a numeric tag value of
// fewer than four digits is kept like a numeric name of that length, so a
// Name=317 tag cannot turn the name field into a tag pseudonym while the
// ARN and the composites keep 317. A longer numeric tag value still takes
// the tag form.
func TestShortNumericTagValuesAreKept(t *testing.T) {
	key := mustKey(t, keyA)
	gen := generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
		"account_id": acct,
		"name":       "317",
		"arn":        "arn:aws:iam::" + acct + ":role/317",
		"tags":       map[string]any{"Name": "317", "CostCenter": "4711"},
	})
	gen = withRefs(gen, "aws:iam:"+acct+":role:317", "aws://iam/"+acct+"/role/317", "317")
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	env := envs[0][0]
	tags, _ := env.Payload["tags"].(map[string]any)
	for label, got := range map[string]string{
		"name": fmt.Sprint(env.Payload["name"]), "tag Name": fmt.Sprint(tags["Name"]), "source_record_id": env.SourceRef.SourceRecordID,
	} {
		if got != "317" {
			t.Errorf("%s: short numeric value rewritten to shape %q", label, shapeOf(got))
		}
	}
	mustMatch(t, "arn", fmt.Sprint(env.Payload["arn"]), `^arn:aws:iam::`+pseudoAcct+`:role/317$`)
	mustMatch(t, "stable_fact_key", env.StableFactKey, `^aws:iam:`+pseudoAcct+`:role:317$`)
	mustMatch(t, "source_uri", env.SourceRef.SourceURI, `^aws://iam/`+pseudoAcct+`/role/317$`)
	mustMatch(t, "long numeric tag value", fmt.Sprint(tags["CostCenter"]), `^t[0-9a-f]{11}$`)
}

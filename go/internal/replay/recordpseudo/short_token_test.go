// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// TestShortTokensNeverRewriteStructure (P1): a tag value "1", a tag value
// "us" and a resource named "east" must not rewrite the region inside
// scope ids, ARNs, Keep fields or ECR hosts; the short alphabetic values
// are still pseudonymized where they are the whole value.
func TestShortTokensNeverRewriteStructure(t *testing.T) {
	key := mustKey(t, keyA)
	lambdaARN := "arn:aws:lambda:us-east-1:" + acct + ":function:img-resizer"
	src := &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:lambda", map[string]string{"region": "us-east-1", "account_id": acct}, map[string]any{
			"account_id": acct,
			"region":     "us-east-1",
			"arn":        lambdaARN,
			"name":       "east",
			"image_uri":  acct + ".dkr.ecr.us-east-1.amazonaws.com/payments-api:latest",
			"tags":       map[string]any{"Name": "east", "Tier": "1", "Project": "us", "Version": "7"},
		}),
	}}
	gens, envs, _ := wrapGens(t, src, key, recordpolicy.Policy())
	payload := envs[0][0].Payload
	mustMatch(t, "scope_id", gens[0].Scope.ScopeID, `^aws:`+pseudoAcct+`:us-east-1:lambda$`)
	mustMatch(t, "partition_key", gens[0].Scope.PartitionKey, `^aws:`+pseudoAcct+`:us-east-1:lambda$`)
	if gens[0].Scope.Metadata["region"] != "us-east-1" {
		t.Errorf("scope metadata region shape %q", shapeOf(gens[0].Scope.Metadata["region"]))
	}
	if got, _ := payload["region"].(string); got != "us-east-1" {
		t.Errorf("region keep field shape %q, want us-east-1", shapeOf(got))
	}
	mustMatch(t, "arn", fmt.Sprint(payload["arn"]), `^arn:aws:lambda:us-east-1:`+pseudoAcct+`:function:`+hexName+`$`)
	mustMatch(t, "image_uri", fmt.Sprint(payload["image_uri"]), `^`+pseudoAcct+`\.dkr\.ecr\.us-east-1\.amazonaws\.com/`+hexName+`:latest$`)
	mustMatch(t, "name", fmt.Sprint(payload["name"]), `^`+hexName+`$`)
	tags, _ := payload["tags"].(map[string]any)
	for _, tagKey := range []string{"Name", "Project"} {
		mustMatch(t, "tag "+tagKey, fmt.Sprint(tags[tagKey]), `^[tn][0-9a-f]{11}$`)
	}
	// Numeric tag values under four digits carry no customer data and are
	// kept (round-5 F13), exactly like numeric names of that length.
	for tagKey, want := range map[string]string{"Tier": "1", "Version": "7"} {
		if got := fmt.Sprint(tags[tagKey]); got != want {
			t.Errorf("tag %s: short numeric value rewritten to shape %q", tagKey, shapeOf(got))
		}
	}
}

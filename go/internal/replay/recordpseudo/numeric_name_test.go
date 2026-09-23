// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// TestNumericNamesAreLearned (round 3 F4): a purely numeric resource name
// is customer data and is pseudonymized in its field, its ARN and every
// composite, as a whole component only: account digits, the region and a
// different numeric ARN qualifier are untouched.
func TestNumericNamesAreLearned(t *testing.T) {
	key := mustKey(t, keyA)
	roleARN := "arn:aws:iam::" + acct + ":role/1234"
	gen := generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
		"account_id":  acct,
		"region":      "us-east-1",
		"name":        "1234",
		"arn":         roleARN,
		"instance_id": "i-0123401234abcdef0",
		"resources":   []any{"arn:aws:lambda:us-east-1:" + acct + ":function:img-resizer:12"},
	})
	gen = withRefs(gen, "aws:iam:"+acct+":role:1234", "aws://iam/"+acct+"/role/1234", "1234")
	gens, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	env := envs[0][0]
	mustMatch(t, "name", fmt.Sprint(env.Payload["name"]), `^`+hexName+`$`)
	mustMatch(t, "arn", fmt.Sprint(env.Payload["arn"]), `^arn:aws:iam::`+pseudoAcct+`:role/`+hexName+`$`)
	mustMatch(t, "stable_fact_key", env.StableFactKey, `^aws:iam:`+pseudoAcct+`:role:`+hexName+`$`)
	mustMatch(t, "source_uri", env.SourceRef.SourceURI, `^aws://iam/`+pseudoAcct+`/role/`+hexName+`$`)
	mustMatch(t, "source_record_id", env.SourceRef.SourceRecordID, `^`+hexName+`$`)
	mustMatch(t, "scope_id", gens[0].Scope.ScopeID, `^aws:`+pseudoAcct+`:us-east-1:iam$`)
	mustMatch(t, "instance_id", fmt.Sprint(env.Payload["instance_id"]), `^i-[0-9a-f]{17}$`)
	resources, _ := env.Payload["resources"].([]any)
	mustMatch(t, "qualifier", fmt.Sprint(resources[0]), `^arn:aws:lambda:us-east-1:`+pseudoAcct+`:function:`+hexName+`:12$`)
}

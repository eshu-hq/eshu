// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// TestNumericNamesNeverRewriteQualifiers (round 4 F10): a numeric name
// never rewrites a numeric ARN qualifier (a Lambda version, an ECS
// task-definition revision), a composite's trailing qualifier or a Keep
// version. A name of fewer than four digits is not learned at all; a
// longer one applies only outside the ":"-qualifier position of an ARN.
func TestNumericNamesNeverRewriteQualifiers(t *testing.T) {
	key := mustKey(t, keyA)
	one := generation("aws:"+acct+":us-east-1:lambda", nil, map[string]any{
		"account_id": acct,
		"name":       "1",
		"version":    "1",
		"arn":        "arn:aws:lambda:us-east-1:" + acct + ":function:img-resizer:1",
		"resources":  []any{"arn:aws:ecs:us-east-1:" + acct + ":task-definition/payments:1"},
	})
	one = withRefs(one, "aws:lambda:"+acct+":us-east-1:function:img-resizer:1", "aws://lambda/"+acct+"/function/img-resizer/1", "1")
	long := generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
		"account_id": acct,
		"name":       "1234",
		"arn":        "arn:aws:iam::" + acct + ":role/1234",
		"resources":  []any{"arn:aws:lambda:us-east-1:" + acct + ":function:other-fn:1234"},
	})
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{one, long}}, key, recordpolicy.Policy())

	env := envs[0][0]
	mustMatch(t, "lambda arn", fmt.Sprint(env.Payload["arn"]), `^arn:aws:lambda:us-east-1:`+pseudoAcct+`:function:`+hexName+`:1$`)
	resources, _ := env.Payload["resources"].([]any)
	mustMatch(t, "ecs revision", fmt.Sprint(resources[0]), `^arn:aws:ecs:us-east-1:`+pseudoAcct+`:task-definition/`+hexName+`:1$`)
	mustMatch(t, "stable_fact_key", env.StableFactKey, `^aws:lambda:`+pseudoAcct+`:us-east-1:function:`+hexName+`:1$`)
	if got := fmt.Sprint(env.Payload["version"]); got != "1" {
		t.Errorf("Keep version rewritten to shape %q", shapeOf(got))
	}

	env = envs[1][0]
	mustMatch(t, "role name", fmt.Sprint(env.Payload["arn"]), `^arn:aws:iam::`+pseudoAcct+`:role/`+hexName+`$`)
	resources, _ = env.Payload["resources"].([]any)
	mustMatch(t, "qualifier equal to a long numeric name", fmt.Sprint(resources[0]), `^arn:aws:lambda:us-east-1:`+pseudoAcct+`:function:`+hexName+`:1234$`)
}

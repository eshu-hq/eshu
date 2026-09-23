// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// TestNumericNameAfterTypedColonIsAName (round 5 F12): for services whose
// type token is ":"-joined (lambda function:NAME, rds db:NAME, logs
// log-group:NAME) the component right after the type token is the
// resource name, not a qualifier. A numeric name there is pseudonymized in
// every ARN like it is in the name field and the composites; a qualifier
// after the name (:7, :*) stays.
func TestNumericNameAfterTypedColonIsAName(t *testing.T) {
	key := mustKey(t, keyA)
	const name = "48213"
	gen := generation("aws:"+acct+":us-east-1:lambda", nil, map[string]any{
		"account_id": acct,
		"name":       name,
		"arn":        "arn:aws:lambda:us-east-1:" + acct + ":function:" + name,
		"resources": []any{
			"arn:aws:lambda:us-east-1:" + acct + ":function:" + name + ":7",
			"arn:aws:rds:us-east-1:" + acct + ":db:" + name,
			"arn:aws:logs:us-east-1:" + acct + ":log-group:" + name + ":*",
			"arn:aws:lambda:us-east-1:" + acct + ":function:img-resizer:1",
		},
	})
	gen = withRefs(gen, "aws:lambda:"+acct+":us-east-1:function:"+name, "aws://lambda/"+acct+"/function/"+name, name)
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	env := envs[0][0]
	mustMatch(t, "name", fmt.Sprint(env.Payload["name"]), `^`+hexName+`$`)
	mustMatch(t, "own arn", fmt.Sprint(env.Payload["arn"]), `^arn:aws:lambda:us-east-1:`+pseudoAcct+`:function:`+hexName+`$`)
	resources, _ := env.Payload["resources"].([]any)
	if len(resources) != 4 {
		t.Fatalf("resources = %v", resources)
	}
	mustMatch(t, "lambda with version", fmt.Sprint(resources[0]), `^arn:aws:lambda:us-east-1:`+pseudoAcct+`:function:`+hexName+`:7$`)
	mustMatch(t, "rds", fmt.Sprint(resources[1]), `^arn:aws:rds:us-east-1:`+pseudoAcct+`:db:`+hexName+`$`)
	mustMatch(t, "logs", fmt.Sprint(resources[2]), `^arn:aws:logs:us-east-1:`+pseudoAcct+`:log-group:`+hexName+`:\*$`)
	mustMatch(t, "qualifier after a name", fmt.Sprint(resources[3]), `^arn:aws:lambda:us-east-1:`+pseudoAcct+`:function:`+hexName+`:1$`)
	mustMatch(t, "stable_fact_key", env.StableFactKey, `^aws:lambda:`+pseudoAcct+`:us-east-1:function:`+hexName+`$`)
	mustMatch(t, "source_uri", env.SourceRef.SourceURI, `^aws://lambda/`+pseudoAcct+`/function/`+hexName+`$`)
	mustMatch(t, "source_record_id", env.SourceRef.SourceRecordID, `^`+hexName+`$`)
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// TestAWSVocabularyWordsAreNeverLearned (round 4 F9): a tag value or name
// equal to an AWS service or ARN type token (rds, db, iam) is AWS
// vocabulary, not customer data. It is never learned, so it never rewrites
// the service or type segment of a scope id, a partition key, a stable
// key, a source uri, an ARN or a Keep service_kind.
func TestAWSVocabularyWordsAreNeverLearned(t *testing.T) {
	key := mustKey(t, keyA)
	rds := generation("aws:"+acct+":us-east-1:rds", nil, map[string]any{
		"account_id":   acct,
		"service_kind": "rds",
		"arn":          "arn:aws:rds:us-east-1:" + acct + ":db:acme-db",
		"name":         "acme-db",
		"tags":         map[string]any{"Tier": "db", "Service": "rds"},
	})
	rds = withRefs(rds, "aws:rds:"+acct+":db:acme-db", "aws://rds/"+acct+"/db/acme-db", "acme-db")
	iam := generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
		"account_id":   acct,
		"service_kind": "iam",
		"arn":          "arn:aws:iam::" + acct + ":role/iam",
		"name":         "iam",
	})
	iam = withRefs(iam, "aws:iam:"+acct+":role:iam", "aws://iam/"+acct+"/role/iam", "iam")
	gens, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{rds, iam}}, key, recordpolicy.Policy())

	env := envs[0][0]
	mustMatch(t, "rds scope_id", gens[0].Scope.ScopeID, `^aws:`+pseudoAcct+`:us-east-1:rds$`)
	mustMatch(t, "rds partition_key", gens[0].Scope.PartitionKey, `^aws:`+pseudoAcct+`:us-east-1:rds$`)
	mustMatch(t, "rds stable_fact_key", env.StableFactKey, `^aws:rds:`+pseudoAcct+`:db:`+hexName+`$`)
	mustMatch(t, "rds source_uri", env.SourceRef.SourceURI, `^aws://rds/`+pseudoAcct+`/db/`+hexName+`$`)
	mustMatch(t, "rds arn", fmt.Sprint(env.Payload["arn"]), `^arn:aws:rds:us-east-1:`+pseudoAcct+`:db:`+hexName+`$`)
	if got := fmt.Sprint(env.Payload["service_kind"]); got != "rds" {
		t.Errorf("service_kind rewritten to shape %q", shapeOf(got))
	}
	tags, _ := env.Payload["tags"].(map[string]any)
	for tagKey, want := range map[string]string{"Tier": "db", "Service": "rds"} {
		if got := fmt.Sprint(tags[tagKey]); got != want {
			t.Errorf("vocabulary tag value %s rewritten to shape %q", tagKey, shapeOf(got))
		}
	}

	env = envs[1][0]
	mustMatch(t, "iam scope_id", gens[1].Scope.ScopeID, `^aws:`+pseudoAcct+`:us-east-1:iam$`)
	mustMatch(t, "iam stable_fact_key", env.StableFactKey, `^aws:iam:`+pseudoAcct+`:role:iam$`)
	mustMatch(t, "iam source_uri", env.SourceRef.SourceURI, `^aws://iam/`+pseudoAcct+`/role/iam$`)
	mustMatch(t, "iam arn", fmt.Sprint(env.Payload["arn"]), `^arn:aws:iam::`+pseudoAcct+`:role/iam$`)
	if got := fmt.Sprint(env.Payload["name"]); got != "iam" {
		t.Errorf("vocabulary name rewritten to shape %q", shapeOf(got))
	}
}

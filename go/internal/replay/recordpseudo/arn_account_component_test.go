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

// TestAccountsInsideARNPathsArePseudonymized (round 4 F8): a 12-digit
// account that is a resource component -- a foreign account in an S3 log
// key path, the member account of an organizations ARN -- is an account,
// not a numeric qualifier, and takes the reserved 0000 form.
func TestAccountsInsideARNPathsArePseudonymized(t *testing.T) {
	key := mustKey(t, keyA)
	const foreign, member = "310987654322", "410987654323"
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
			"account_id": acct,
			"resources": []any{
				"arn:aws:s3:::acme-logs/AWSLogs/" + foreign + "/*",
				"arn:aws:organizations::" + acct + ":account/o-abc123def/" + member,
			},
		}),
	}}, key, recordpolicy.Policy())
	resources, _ := envs[0][0].Payload["resources"].([]any)
	if len(resources) != 2 {
		t.Fatalf("resources = %v", resources)
	}
	mustMatch(t, "s3 log path", fmt.Sprint(resources[0]), `^arn:aws:s3:::`+hexName+`/`+hexName+`/`+pseudoAcct+`/\*$`)
	mustMatch(t, "organizations", fmt.Sprint(resources[1]), `^arn:aws:organizations::`+pseudoAcct+`:account/`+hexName+`/`+pseudoAcct+`$`)
	rendered := fmt.Sprint(envs[0][0].Payload)
	for _, raw := range []string{foreign, member, acct} {
		if strings.Contains(rendered, raw) {
			t.Errorf("a raw 12-digit account survived in the payload")
		}
	}
}

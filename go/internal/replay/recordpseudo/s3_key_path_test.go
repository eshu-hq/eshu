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

// TestS3KeyPathComponentsArePseudonymized (P0): a prefix-scoped S3 policy
// resource keeps the customer's key path raw in the ARN field, the
// resources list and the composite stable key unless every path component
// is learned. Only "*" and the "/" structure survive.
func TestS3KeyPathComponentsArePseudonymized(t *testing.T) {
	key := mustKey(t, keyA)
	arn := "arn:aws:s3:::acme-uploads/home/jdoe/*"
	src := &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
			"arn":       arn,
			"resources": []any{arn, "arn:aws:s3:::acme-uploads"},
		}),
	}}
	src.gens[0] = withStableKey(src.gens[0], "aws:iam:"+acct+":policy:"+arn)
	_, envs, _ := wrapGens(t, src, key, recordpolicy.Policy())
	payload := envs[0][0].Payload
	want := `^arn:aws:s3:::` + hexName + `/` + hexName + `/` + hexName + `/\*$`
	got, _ := payload["arn"].(string)
	mustMatch(t, "arn", got, want)
	resources, _ := payload["resources"].([]any)
	if len(resources) != 2 {
		t.Fatalf("resources = %v", resources)
	}
	mustMatch(t, "resources[0]", fmt.Sprint(resources[0]), want)
	mustMatch(t, "resources[1]", fmt.Sprint(resources[1]), `^arn:aws:s3:::`+hexName+`$`)
	// The bucket pseudonym is the same with and without a key path.
	if !strings.HasPrefix(got, strings.TrimSuffix(fmt.Sprint(resources[1]), "")+"/") {
		t.Errorf("bucket pseudonym differs between the bare and the key-path ARN")
	}
	for _, raw := range []string{"home", "jdoe", "acme"} {
		if strings.Contains(envs[0][0].StableFactKey, raw) || strings.Contains(fmt.Sprint(payload), raw) {
			t.Errorf("key-path component of length %d survived raw", len(raw))
		}
	}
}

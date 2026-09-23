// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"bytes"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// Review finding F2 on 40a4ac36e (verdict-p3.md). Every literal is synthetic.

// TestNameEqualToTagValueIsDeterministic (F2): a resource whose Name tag
// equals its name must come out byte-identical on every run and in the name
// format, whichever field the walker happened to visit first.
func TestNameEqualToTagValueIsDeterministic(t *testing.T) {
	key := mustKey(t, keyA)
	build := func() collector.CollectedGeneration {
		return generation("aws:"+acct+":us-east-1:ec2", map[string]string{"account_id": acct, "region": "us-east-1", "service_kind": "ec2"}, map[string]any{
			"name":        "acme-orders-db",
			"resource_id": "i-0abc123def4567890",
			"tags":        map[string]any{"Name": "acme-orders-db", "acme-demo:owner": "team-orders", "zz": "acme-orders-db"},
		})
	}
	var first []byte
	for i := 0; i < 40; i++ {
		out := recordBytes(t, filepath.Join(t.TempDir(), "run.json"), &sliceSource{gens: []collector.CollectedGeneration{build()}}, key)
		if first == nil {
			first = out
			continue
		}
		if !bytes.Equal(first, out) {
			t.Fatalf("run %d differs from run 0: output depends on map iteration", i)
		}
	}
	payloads, _ := wrapAll(t, &sliceSource{gens: []collector.CollectedGeneration{build()}}, key, recordpolicy.Policy())
	name, _ := payloads[0]["name"].(string)
	if !regexp.MustCompile(`^n[0-9a-f]{11}$`).MatchString(name) {
		t.Errorf("name has shape %q, want the name format regardless of the tag", shapeOf(name))
	}
	tags, _ := payloads[0]["tags"].(map[string]any)
	if tags["Name"] != name {
		t.Errorf("tags.Name and name diverged: the same raw token got two pseudonyms")
	}
}

// TestARNValuedTagKeepsGrammar (F2b): a tag value that is an ARN (the
// cloudformation:stack-id tag) must not be learned as one whole tag token
// that then swallows every ARN field carrying the same value.
func TestARNValuedTagKeepsGrammar(t *testing.T) {
	key := mustKey(t, keyA)
	roleARN := "arn:aws:iam::" + acct + ":role/acme-deployer"
	payloads, _ := wrapAll(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:lambda", nil, map[string]any{
			"tags":     map[string]any{"aws:cloudformation:stack-id": roleARN, "Owner": "oncall@acme-demo.com"},
			"role_arn": roleARN,
		}),
	}}, key, recordpolicy.Policy())
	got, _ := payloads[0]["role_arn"].(string)
	if !regexp.MustCompile(`^arn:aws:iam::0000[0-9]{8}:role/n[0-9a-f]{11}$`).MatchString(got) {
		t.Errorf("role_arn shape %q lost its ARN grammar", shapeOf(got))
	}
	tags, _ := payloads[0]["tags"].(map[string]any)
	if tags["aws:cloudformation:stack-id"] != got {
		t.Errorf("the ARN-valued tag and the role_arn field diverged")
	}
	if owner, _ := tags["Owner"].(string); !regexp.MustCompile(`^[0-9a-f]{11}@example\.com$`).MatchString(owner) {
		t.Errorf("email-valued tag has shape %q, want the email format", shapeOf(owner))
	}
}

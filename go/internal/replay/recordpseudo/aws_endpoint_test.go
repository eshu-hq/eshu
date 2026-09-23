// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// awsEndpointHosts are synthetic customer endpoints under AWS suffixes, one
// per shape a recording carries: ELB (both label orders), RDS, API Gateway,
// ECR under a foreign account.
var awsEndpointHosts = []string{
	"payments-lb-1234.us-east-1.elb.amazonaws.com",
	"internal-orders-lb.elb.us-east-1.amazonaws.com",
	"orders-db.c9akciq32.us-east-1.rds.amazonaws.com",
	"a1b2c3d4e5.execute-api.us-east-1.amazonaws.com",
	acct + ".dkr.ecr.us-east-1.amazonaws.com",
}

// TestVerifyAdmitsProducedAWSEndpoints: a customer endpoint under an AWS
// suffix whose customer labels are h-pseudonyms this run produced passes
// Verify; the same bytes with no produced set, and the raw host, are
// refused.
func TestVerifyAdmitsProducedAWSEndpoints(t *testing.T) {
	key := mustKey(t, keyA)
	payloads := make([]map[string]any, 0, len(awsEndpointHosts))
	for _, host := range awsEndpointHosts {
		payloads = append(payloads, map[string]any{"dns_name": host})
	}
	_, envs, report := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:elbv2", nil, payloads...),
	}}, key, recordpolicy.Policy())
	var doc strings.Builder
	doc.WriteString("{")
	for i, env := range envs[0] {
		got := fmt.Sprint(env.Payload["dns_name"])
		if got == awsEndpointHosts[i] {
			t.Fatalf("endpoint %d was not pseudonymized", i)
		}
		fmt.Fprintf(&doc, `"h%d":%q,`, i, got)
	}
	doc.WriteString(`"end":true}`)
	if err := recordpseudo.Verify([]byte(doc.String()), report.Produced); err != nil {
		t.Fatalf("produced AWS endpoints refused: %v", err)
	}
	if err := recordpseudo.Verify([]byte(doc.String()), recordpseudo.Set{}); err == nil {
		t.Fatal("h-label AWS endpoints passed with an empty produced set: the form is not membership-checked")
	}
	for _, raw := range awsEndpointHosts[:4] {
		err := recordpseudo.Verify([]byte(`{"a":"`+raw+`"}`), report.Produced)
		if err == nil || !strings.Contains(err.Error(), "alternative hostname") {
			t.Errorf("raw endpoint of shape %q passed Verify: %v", shapeOf(raw), err)
		}
	}
	// An h-label the run did not produce, on an otherwise valid shape.
	if err := recordpseudo.Verify([]byte(`{"a":"h0123456789.us-east-1.elb.amazonaws.com"}`), report.Produced); err == nil {
		t.Error("an unproduced h-label AWS endpoint passed Verify")
	}
}

// TestRegionShapedCustomerLabelIsPseudonymized: a customer label that only
// looks like a region (db-main-1: two letters, a word, a digit) is not an
// AWS region and must not stop the label walk, or every label right of it
// is written raw.
func TestRegionShapedCustomerLabelIsPseudonymized(t *testing.T) {
	key := mustKey(t, keyA)
	got := pseudonymOf(t, key, "dns_name", "db-main-1.c9akciq32.us-east-1.rds.amazonaws.com")
	mustMatch(t, "region-shaped label", got, `^h[0-9a-f]{10}\.h[0-9a-f]{10}\.us-east-1\.rds\.amazonaws\.com$`)
}

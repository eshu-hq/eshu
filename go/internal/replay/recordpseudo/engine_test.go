// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/recorder"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Every literal in this file is synthetic; the account is the gate's own
// planted sample and the names are invented.
const (
	keyA      = "6965-p3-key-a-0123456789abcdef0123456789abcdef0123456789abcdef"
	keyB      = "6965-p3-key-b-fedcba9876543210fedcba9876543210fedcba9876543210"
	acct      = "210987654321"
	corpusDir = "../../../../testdata/cassettes"
)

func mustKey(t *testing.T, material string) recordpseudo.Key {
	t.Helper()
	key, err := recordpseudo.NewKey([]byte(material))
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return key
}

type sliceSource struct {
	gens  []collector.CollectedGeneration
	index int
}

func (s *sliceSource) Next(context.Context) (collector.CollectedGeneration, bool, error) {
	if s.index >= len(s.gens) {
		return collector.CollectedGeneration{}, false, nil
	}
	gen := s.gens[s.index]
	s.index++
	return gen, true, nil
}

func generation(scopeID string, metadata map[string]string, payloads ...map[string]any) collector.CollectedGeneration {
	observedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	s := scope.IngestionScope{
		ScopeID: scopeID, SourceSystem: "aws", ScopeKind: scope.KindRegion,
		CollectorKind: scope.CollectorAWS, PartitionKey: scopeID, Metadata: metadata,
	}
	g := scope.ScopeGeneration{
		GenerationID: "gen-1", ScopeID: scopeID, ObservedAt: observedAt, IngestedAt: observedAt,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
	}
	envs := make([]facts.Envelope, 0, len(payloads))
	for i, payload := range payloads {
		envs = append(envs, facts.Envelope{
			FactKind: facts.AWSResourceFactKind, StableFactKey: fmt.Sprintf("key-%d", i), SchemaVersion: "1",
			CollectorKind: "aws", FencingToken: 1, SourceConfidence: "reported", Payload: payload,
			SourceRef: facts.Ref{SourceURI: "aws://" + scopeID, SourceRecordID: fmt.Sprintf("rec-%d", i)},
		})
	}
	return collector.FactsFromSlice(s, g, envs)
}

// wrapAll drains a wrapped source and returns the rewritten payloads in
// order plus the report.
func wrapAll(t *testing.T, src collector.Source, key recordpseudo.Key, policy recordpseudo.Policy) ([]map[string]any, *recordpseudo.Report) {
	t.Helper()
	wrapped, report, err := recordpseudo.Wrap(src, key, policy)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	var out []map[string]any
	for {
		gen, ok, err := wrapped.Next(context.Background())
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if !ok {
			return out, report
		}
		for env := range gen.Facts {
			out = append(out, env.Payload)
		}
	}
}

// pseudonymOf pseudonymizes one value under key in the aws policy field
// named by field and returns the rewritten string.
func pseudonymOf(t *testing.T, key recordpseudo.Key, field, value string) string {
	t.Helper()
	payloads, _ := wrapAll(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{field: value}),
	}}, key, recordpolicy.Policy())
	got, _ := payloads[0][field].(string)
	return got
}

func recordBytes(t *testing.T, path string, src collector.Source, key recordpseudo.Key) []byte {
	t.Helper()
	err := recorder.Run(context.Background(), src, recorder.Options{
		Path: path, CollectorLabel: "aws",
		Pseudonymize: &recordpseudo.Config{Key: key, Policy: recordpolicy.Policy()},
	})
	if err != nil {
		t.Fatalf("recorder.Run: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func committedSource(t *testing.T, collectorName string) *cassette.Source {
	t.Helper()
	src, err := cassette.NewSource(filepath.Join(corpusDir, collectorName, "supply-chain-demo.json"))
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	return src
}

// TestWrapIsDeterministicPerKey: one key twice gives byte-identical
// cassettes; a second key gives different bytes; both pass Verify.
func TestWrapIsDeterministicPerKey(t *testing.T) {
	dir := t.TempDir()
	first := recordBytes(t, filepath.Join(dir, "a1.json"), committedSource(t, "awscloud"), mustKey(t, keyA))
	second := recordBytes(t, filepath.Join(dir, "a2.json"), committedSource(t, "awscloud"), mustKey(t, keyA))
	other := recordBytes(t, filepath.Join(dir, "b.json"), committedSource(t, "awscloud"), mustKey(t, keyB))
	if !bytes.Equal(first, second) {
		t.Fatal("same key, same input: bytes differ")
	}
	if bytes.Equal(first, other) {
		t.Fatal("different keys produced identical bytes")
	}
	if bytes.Contains(other, []byte("123456789012")) {
		t.Error("the committed documentation account survived under key B")
	}
}

// TestEqualInputsAgreeAcrossSources: an aws-shaped source classifying the
// account as account_id and an oci-shaped source seeing the same account as
// an ECR host label agree on the registry host and the repository pseudonym.
func TestEqualInputsAgreeAcrossSources(t *testing.T) {
	key := mustKey(t, keyA)
	awsOut, _ := wrapAll(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:ecr", map[string]string{"account_id": acct}, map[string]any{
			"account_id": acct, "repository_name": "payments-api",
			"uri": acct + ".dkr.ecr.us-east-1.amazonaws.com/payments-api",
		}),
	}}, key, recordpolicy.Policy())
	ociPolicy := recordpseudo.Policy{Fields: map[string]recordpseudo.Class{
		"registry": recordpseudo.ClassHost, "repository": recordpseudo.ClassIdent, "descriptor_id": recordpseudo.ClassIdent,
	}}
	ociOut, _ := wrapAll(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("oci:registry-1", nil, map[string]any{
			"registry": acct + ".dkr.ecr.us-east-1.amazonaws.com", "repository": "payments-api",
			"descriptor_id": "oci-descriptor://" + acct + ".dkr.ecr.us-east-1.amazonaws.com/payments-api@sha256:aaaa",
		}),
	}}, key, ociPolicy)
	awsURI, _ := awsOut[0]["uri"].(string)
	ociRegistry, _ := ociOut[0]["registry"].(string)
	ociRepo, _ := ociOut[0]["repository"].(string)
	awsRepo, _ := awsOut[0]["repository_name"].(string)
	if !strings.HasPrefix(awsURI, ociRegistry+"/") {
		t.Errorf("aws uri does not start with the oci registry host: shapes differ")
	}
	if !strings.HasSuffix(awsURI, "/"+ociRepo) || awsRepo != ociRepo {
		t.Errorf("repository pseudonyms differ across sources")
	}
	descriptor, _ := ociOut[0]["descriptor_id"].(string)
	if descriptor != "oci-descriptor://"+ociRegistry+"/"+ociRepo+"@sha256:aaaa" {
		t.Errorf("descriptor id did not follow the same substitution")
	}
	if !regexp.MustCompile(`^0000[0-9]{8}\.dkr\.ecr\.us-east-1\.amazonaws\.com$`).MatchString(ociRegistry) {
		t.Errorf("ECR host does not carry the reserved account form")
	}
}

func TestARNGrammar(t *testing.T) {
	key := mustKey(t, keyA)
	hexName := `n[0-9a-f]{11}`
	pseudoAcct := `0000[0-9]{8}`
	cases := []struct{ arn, want string }{
		{"arn:aws:s3:::payments-bucket", `^arn:aws:s3:::` + hexName + `$`},
		{"arn:aws:s3:::payments-bucket/some/object", `^arn:aws:s3:::` + hexName + `/` + hexName + `/` + hexName + `$`},
		{"arn:aws:s3:::payments-bucket/home/jdoe/*", `^arn:aws:s3:::` + hexName + `/` + hexName + `/` + hexName + `/\*$`},
		{"arn:aws:lambda:us-east-1:" + acct + ":function:img-resizer", `^arn:aws:lambda:us-east-1:` + pseudoAcct + `:function:` + hexName + `$`},
		{"arn:aws:lambda:us-east-1:" + acct + ":function:img-resizer:$LATEST", `^arn:aws:lambda:us-east-1:` + pseudoAcct + `:function:` + hexName + `:\$LATEST$`},
		{"arn:aws:ecs:us-east-1:" + acct + ":task/demo-cluster/0123456789abcdef0123456789abcdef", `^arn:aws:ecs:us-east-1:` + pseudoAcct + `:task/` + hexName + `/[0-9a-f]{32}$`},
		{"arn:aws:ecs:us-east-1:" + acct + ":task-definition/payments:7", `^arn:aws:ecs:us-east-1:` + pseudoAcct + `:task-definition/` + hexName + `:7$`},
		{"arn:aws:iam::" + acct + ":role/payments-deployer", `^arn:aws:iam::` + pseudoAcct + `:role/` + hexName + `$`},
		{"arn:aws:iam::" + acct + ":role/service-role/payments-deployer", `^arn:aws:iam::` + pseudoAcct + `:role/` + hexName + `/` + hexName + `$`},
		{"arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess", `^arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess$`},
		{"arn:aws:sns:us-east-1:" + acct + ":order-events", `^arn:aws:sns:us-east-1:` + pseudoAcct + `:` + hexName + `$`},
		{"arn:aws:elasticloadbalancing:us-east-1:" + acct + ":loadbalancer/app/payments-lb/50dc6c495c0c9188", `^arn:aws:elasticloadbalancing:us-east-1:` + pseudoAcct + `:loadbalancer/app/` + hexName + `/` + hexName + `$`},
		{"arn:aws:ec2:us-east-1:" + acct + ":instance/i-0abc123def4567890", `^arn:aws:ec2:us-east-1:` + pseudoAcct + `:instance/i-[0-9a-f]{17}$`},
	}
	for _, tc := range cases {
		got := pseudonymOf(t, key, "arn", tc.arn)
		if !regexp.MustCompile(tc.want).MatchString(got) {
			t.Errorf("ARN of shape %q -> shape %q does not match %s", shapeOf(tc.arn), shapeOf(got), tc.want)
		}
	}
}

// shapeOf masks letters and digits so a failure message describes a shape,
// never a value.
func shapeOf(s string) string {
	return regexp.MustCompile(`[A-Za-z0-9]`).ReplaceAllString(s, "x")
}

func TestAWSIssuedIDKeepsPrefixAndLength(t *testing.T) {
	key := mustKey(t, keyA)
	for field, raw := range map[string]string{
		"instance_id": "i-0abc123def4567890", "ami_id": "ami-0fedcba9876543210", "subnet_id": "subnet-0a1b2c3d",
		"network_interface_id": "eni-0123456789abcdef0", "volume_id": "vol-0a1b2c3d4e5f60718", "group_id": "sg-0abcdef12",
	} {
		got := pseudonymOf(t, key, field, raw)
		prefix := raw[:strings.Index(raw, "-")+1]
		if !strings.HasPrefix(got, prefix) || len(got) != len(raw) || got == raw {
			t.Errorf("%s: shape %q -> %q; want prefix kept, equal length, different value", field, shapeOf(raw), shapeOf(got))
		}
		if !regexp.MustCompile(`^[a-z]+-[0-9a-f]+$`).MatchString(got) {
			t.Errorf("%s: pseudonym is not prefix+hex", field)
		}
	}
	task := pseudonymOf(t, key, "name", "0123456789abcdef0123456789abcdef")
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(task) || task == "0123456789abcdef0123456789abcdef" {
		t.Errorf("32-hex id must stay 32 hex and change")
	}
}

func TestIPv4IntoRFC5737(t *testing.T) {
	key := mustKey(t, keyA)
	rfc5737 := regexp.MustCompile(`^(?:192\.0\.2|198\.51\.100|203\.0\.113)\.(?:[1-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-4])$`)
	for _, raw := range []string{"10.20.30.40", "172.16.9.4", "192.168.1.1", "203.0.114.5"} {
		got := pseudonymOf(t, key, "private_ipv4_address", raw)
		if !rfc5737.MatchString(got) {
			t.Errorf("address of shape %q -> shape %q outside RFC 5737 hosts", shapeOf(raw), shapeOf(got))
		}
	}
	for _, kept := range []string{"127.0.0.1", "0.0.0.0", "192.0.2.10"} {
		if got := pseudonymOf(t, key, "public_ip_address", kept); got != kept {
			t.Errorf("safe address was rewritten")
		}
	}
	// Two distinct raw addresses never share a pseudonym within one run.
	payloads, report := wrapAll(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation(
			"aws:"+acct+":us-east-1:ec2", nil,
			map[string]any{"private_ipv4_address": "10.0.0.1"},
			map[string]any{"private_ipv4_address": "10.0.0.2"},
			map[string]any{"private_ipv4_address": "10.0.0.3"},
		),
	}}, key, recordpolicy.Policy())
	seen := map[string]int{}
	for _, p := range payloads {
		seen[p["private_ipv4_address"].(string)]++
	}
	if len(seen) != 3 {
		t.Errorf("3 raw addresses -> %d pseudonyms; collisions=%d", len(seen), report.IPv4Collisions)
	}
}

func TestCIDRKeepsPrefixAndInternet(t *testing.T) {
	key := mustKey(t, keyA)
	got := pseudonymOf(t, key, "source_value", "10.0.0.0/8")
	if !regexp.MustCompile(`^(?:192\.0\.2|198\.51\.100|203\.0\.113)\.[0-9]{1,3}/8$`).MatchString(got) {
		t.Errorf("CIDR shape %q -> %q; want RFC 5737 network with /8 kept", shapeOf("10.0.0.0/8"), shapeOf(got))
	}
	for _, kept := range []string{"0.0.0.0/0", "::/0", "127.0.0.0/8"} {
		if pseudonymOf(t, key, "source_value", kept) != kept {
			t.Errorf("internet/loopback CIDR was rewritten")
		}
	}
	if pseudonymOf(t, key, "source_value", "sg-0abcdef12") == "sg-0abcdef12" {
		t.Errorf("security-group id in source_value was not pseudonymized")
	}
}

func TestIPv6IntoDocumentationBlock(t *testing.T) {
	key := mustKey(t, keyA)
	policy := recordpseudo.Policy{Fields: map[string]recordpseudo.Class{"address": recordpseudo.ClassIPv6, "cidr": recordpseudo.ClassCIDR}}
	payloads, _ := wrapAll(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("scope", nil, map[string]any{"address": "fd00:1234::1", "cidr": "fd00:1234::/64"}),
	}}, key, policy)
	address, _ := payloads[0]["address"].(string)
	if !strings.HasPrefix(address, "2001:db8:") || strings.Count(address, ":") != 7 {
		t.Errorf("IPv6 pseudonym shape %q is not a full 2001:db8:: form", shapeOf(address))
	}
	cidr, _ := payloads[0]["cidr"].(string)
	if !strings.HasPrefix(cidr, "2001:db8:") || !strings.HasSuffix(cidr, "/64") {
		t.Errorf("IPv6 CIDR shape %q lost its prefix length or block", shapeOf(cidr))
	}
}

func TestHostnameLabelsAndSuffixes(t *testing.T) {
	key := mustKey(t, keyA)
	label := `h[0-9a-f]{10}`
	cases := []struct{ raw, want string }{
		{"api.prod.org-demo.com", `^` + label + `\.` + label + `\.` + label + `\.example$`},
		{"*.apps.org-demo.com", `^\*\.` + label + `\.` + label + `\.example$`},
		{"org-demo.com.", `^` + label + `\.example\.$`},
		{"payments.team-a.svc.cluster.local", `^` + label + `\.` + label + `\.` + label + `\.` + label + `\.example$`},
		{"payments-lb-1234.us-east-1.elb.amazonaws.com", `^` + label + `\.us-east-1\.elb\.amazonaws\.com$`},
		{"orders-db.c9akciq32.us-east-1.rds.amazonaws.com", `^` + label + `\.` + label + `\.us-east-1\.rds\.amazonaws\.com$`},
		{acct + ".dkr.ecr.eu-west-1.amazonaws.com", `^0000[0-9]{8}\.dkr\.ecr\.eu-west-1\.amazonaws\.com$`},
		{"registry.example.com", `^registry\.example\.com$`},
		{"localhost", `^localhost$`},
	}
	for _, tc := range cases {
		got := pseudonymOf(t, key, "dns_name", tc.raw)
		if !regexp.MustCompile(tc.want).MatchString(got) {
			t.Errorf("host shape %q -> shape %q does not match %s", shapeOf(tc.raw), shapeOf(got), tc.want)
		}
	}
	// Suffix relation: the zone's pseudonym is a suffix of the record's.
	zone := pseudonymOf(t, key, "hosted_zone_name", "org-demo.com")
	record := pseudonymOf(t, key, "record_name", "api.org-demo.com")
	if !strings.HasSuffix(record, "."+zone) {
		t.Errorf("record pseudonym does not end in the zone pseudonym: suffix relation lost")
	}
}

func TestEmailTagAndOpaqueShapes(t *testing.T) {
	key := mustKey(t, keyA)
	policy := recordpseudo.Policy{Fields: map[string]recordpseudo.Class{
		"contact": recordpseudo.ClassEmail, "tags": recordpseudo.ClassTagValue, "notes": recordpseudo.ClassOpaque, "name": recordpseudo.ClassIdent,
	}}
	payloads, report := wrapAll(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("scope", nil, map[string]any{
			"contact": "oncall@org-demo.com",
			"tags":    map[string]any{"Name": "payments", "org-demo:cost-center": "cc-4711"},
			"notes":   "owned by the payments team",
			"name":    "payments",
			"unknown": "anything",
		}),
	}}, key, policy)
	p := payloads[0]
	if got, _ := p["contact"].(string); !regexp.MustCompile(`^[0-9a-f]{11}@example\.com$`).MatchString(got) {
		t.Errorf("email shape %q", shapeOf(got))
	}
	tags, _ := p["tags"].(map[string]any)
	if _, ok := tags["Name"]; !ok {
		t.Errorf("well-known tag key Name was not kept")
	}
	if _, ok := tags["org-demo:cost-center"]; ok {
		t.Errorf("customer tag key survived")
	}
	for k, v := range tags {
		if s, _ := v.(string); !regexp.MustCompile(`^(?:t|n)[0-9a-f]{11}$`).MatchString(s) {
			t.Errorf("tag value under key shape %q has shape %q", shapeOf(k), shapeOf(s))
		}
	}
	if got, _ := p["notes"].(string); !regexp.MustCompile(`^o[0-9a-f]{11}$`).MatchString(got) {
		t.Errorf("opaque shape %q", shapeOf(got))
	}
	if got, _ := p["unknown"].(string); !regexp.MustCompile(`^o[0-9a-f]{11}$`).MatchString(got) {
		t.Errorf("unclassified key was not made opaque: shape %q", shapeOf(got))
	}
	if strings.Join(report.UnclassifiedPaths, ",") != "payload.unknown" {
		t.Errorf("unclassified paths = %v, want [payload.unknown]", report.UnclassifiedPaths)
	}
	if strings.Join(report.OpaquePaths, ",") != "payload.notes,payload.unknown" {
		t.Errorf("opaque paths = %v", report.OpaquePaths)
	}
	// The tag VALUE "payments" and the name "payments" share one pseudonym
	// only if learned under the same class; the report must not carry either.
	rendered := fmt.Sprint(report.LogAttrs())
	for _, raw := range []string{"payments", "org-demo", "oncall", "cc-4711", "owned by"} {
		if strings.Contains(rendered, raw) {
			t.Errorf("report log attrs carry a raw value")
		}
	}
}

func TestPolicyRejectsUnknownAndEmpty(t *testing.T) {
	if err := (recordpseudo.Policy{}).Validate(); err == nil {
		t.Error("empty policy validated")
	}
	bad := recordpseudo.Policy{Fields: map[string]recordpseudo.Class{"x": recordpseudo.ClassUnknown}}
	if err := bad.Validate(); err == nil {
		t.Error("policy with an explicit unknown class validated")
	}
	if _, _, err := recordpseudo.Wrap(&sliceSource{}, recordpseudo.Key{}, recordpolicy.Policy()); err == nil {
		t.Error("Wrap accepted the zero key")
	}
	if _, err := recordpseudo.NewKey([]byte(strings.Repeat("k", recordpseudo.MinKeyBytes-1))); err == nil {
		t.Error("NewKey accepted short material")
	}
	if fp := mustKey(t, keyA).Fingerprint(); !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(fp) || fp == mustKey(t, keyB).Fingerprint() {
		t.Errorf("fingerprint shape %q or not key-specific", fp)
	}
}

// TestTypedPayloadValuesAreNotFailOpen: live collectors build payloads from
// typed values ([]string anchors, map[string]string tags, *string fields,
// numbers). Every one of them must be classified, never passed through
// because the walker did not recognise the Go type.
func TestTypedPayloadValuesAreNotFailOpen(t *testing.T) {
	key := mustKey(t, keyA)
	arn := "arn:aws:iam::" + acct + ":role/orders-deployer"
	name := "orders-deployer"
	payloads, report := wrapAll(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
			"account_id":             acct,
			"arn":                    &arn,
			"name":                   &name,
			"correlation_anchors":    []string{arn},
			"tags":                   map[string]string{"Name": name, "org-demo:owner": "team-orders"},
			"cpu":                    int64(256),
			"weight":                 1.5,
			"evaluate_target_health": true,
		}),
	}}, key, recordpolicy.Policy())
	rendered := fmt.Sprint(payloads[0])
	for _, raw := range []string{acct, "orders-deployer", "org-demo", "team-orders"} {
		if strings.Contains(rendered, raw) {
			t.Errorf("typed value of length %d passed through unclassified", len(raw))
		}
	}
	if fmt.Sprint(payloads[0]["cpu"]) != "256" || fmt.Sprint(payloads[0]["weight"]) != "1.5" || payloads[0]["evaluate_target_health"] != true {
		t.Errorf("numbers or booleans were altered: %v %v %v", payloads[0]["cpu"], payloads[0]["weight"], payloads[0]["evaluate_target_health"])
	}
	if len(report.UnclassifiedPaths) != 0 {
		t.Errorf("unclassified paths = %v, want none", report.UnclassifiedPaths)
	}
}

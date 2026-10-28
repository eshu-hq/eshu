// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudjoin

import (
	"testing"

	awsv1 "github.com/eshu-hq/eshu-sdk/factschema/aws/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

// cov8AWSResourceEnvelope builds one aws_resource envelope with the given
// scope and payload fields.
func cov8AWSResourceEnvelope(scope string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:   "fact-" + scope,
		ScopeID:  scope,
		FactKind: cloud.AWSResourceFactKind,
		Payload:  payload,
	}
}

// cov8VPCPayload returns a minimal valid VPC resource payload.
func cov8VPCPayload(id string) map[string]any {
	return map[string]any{
		"account_id":          "111122223333",
		"region":              "us-east-1",
		"resource_type":       "aws_ec2_vpc",
		"resource_id":         id,
		"arn":                 "arn:aws:ec2:us-east-1:111122223333:vpc/" + id,
		"name":                "main",
		"correlation_anchors": []any{id},
	}
}

// TestCov8ScopeInBounds pins the trust-boundary guard: empty scopes stay
// permissive, foreign-scope uids are refused, and same-scope uids pass.
func TestCov8ScopeInBounds(t *testing.T) {
	t.Parallel()

	index, quarantined, err := BuildCloudResourceJoinIndex([]facts.Envelope{
		cov8AWSResourceEnvelope("scope-a", cov8VPCPayload("vpc-1")),
	})
	if err != nil || len(quarantined) != 0 {
		t.Fatalf("Build() = %v, %v; want nil, no quarantine", quarantined, err)
	}
	var uid string
	for _, id := range index.ByResourceID {
		uid = id
	}
	if uid == "" {
		t.Fatal("index holds no resource")
	}
	if !index.ScopeInBounds(uid, "scope-a") {
		t.Error("same-scope uid out of bounds, want in bounds")
	}
	if index.ScopeInBounds(uid, "scope-b") {
		t.Error("foreign-scope uid in bounds, want refused")
	}
	if !index.ScopeInBounds(uid, "") {
		t.Error("empty intent scope must keep pre-#6162 permissive behavior")
	}
	if !index.ScopeInBounds("", "scope-b") {
		t.Error("empty uid must stay permissive")
	}
	if index.ScopeInBounds("never-indexed", "scope-b") {
		t.Error("unknown uid in a foreign scope must fail closed, want refused")
	}
	if !index.ScopeInBounds(uid, "scope-a") {
		t.Error("same-scope uid must stay in bounds on repeat reads")
	}

	// A mixed-scope build records the disagreeing scope per uid.
	mixed, _, err := BuildCloudResourceJoinIndex([]facts.Envelope{
		cov8AWSResourceEnvelope("scope-a", cov8VPCPayload("vpc-1")),
		cov8AWSResourceEnvelope("scope-b", cov8VPCPayload("vpc-2")),
	})
	if err != nil {
		t.Fatalf("Build(mixed) = %v, want nil", err)
	}
	uidB, ok := mixed.ByResourceID["vpc-2"]
	if !ok {
		t.Fatal("mixed build misses the second-scope resource")
	}
	if mixed.ScopeInBounds(uidB, "scope-a") {
		t.Error("second-scope uid in first scope bounds, want refused")
	}
	if !mixed.ScopeInBounds(uidB, "scope-b") {
		t.Error("second-scope uid out of its own bounds, want in bounds")
	}
}

// TestCov8BuildCloudResourceJoinIndexSkipsAndQuarantines pins per-fact
// isolation: non-resource kinds and incomplete identities skip quietly,
// one malformed fact quarantines while valid facts still index.
func TestCov8BuildCloudResourceJoinIndexSkipsAndQuarantines(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		cov8AWSResourceEnvelope("scope-a", cov8VPCPayload("vpc-1")),
		{FactKind: cloud.AWSRelationshipFactKind, ScopeID: "scope-a", Payload: map[string]any{}},
		{FactKind: "unrelated_kind", ScopeID: "scope-a", Payload: map[string]any{}},
		// Present-but-empty resource_type: a valid decode with an
		// incomplete identity, skipped without quarantine (distinct from
		// an absent required key, which quarantines).
		cov8AWSResourceEnvelope("scope-a", map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "", "resource_id": "vpc-x",
		}),
		// No account_id: a missing required identity field, quarantined.
		cov8AWSResourceEnvelope("scope-a", map[string]any{
			"region": "us-east-1", "resource_type": "aws_ec2_vpc", "resource_id": "vpc-bad",
		}),
	}
	index, quarantined, err := BuildCloudResourceJoinIndex(envelopes)
	if err != nil {
		t.Fatalf("Build() = %v, want nil", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined = %d, want exactly the malformed fact", len(quarantined))
	}
	if len(index.ByResourceID) != 1 {
		t.Errorf("indexed resources = %d, want only the valid VPC", len(index.ByResourceID))
	}
}

// TestCov8JoinIndexIdentityMaps pins the lookup surfaces: ARN, bare
// resource id, anchor fallback with first-writer-wins, and UID reversal.
func TestCov8JoinIndexIdentityMaps(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:111122223333:vpc/vpc-1"
	first := cov8VPCPayload("vpc-1")
	second := cov8VPCPayload("vpc-2")
	second["arn"] = "arn:aws:ec2:us-east-1:111122223333:vpc/vpc-2"
	second["correlation_anchors"] = []any{"vpc-1"} // collides with first's anchor
	index, _, err := BuildCloudResourceJoinIndex([]facts.Envelope{
		cov8AWSResourceEnvelope("scope-a", first),
		cov8AWSResourceEnvelope("scope-a", second),
	})
	if err != nil {
		t.Fatalf("Build() = %v, want nil", err)
	}
	uid, ok := index.ByARN[arn]
	if !ok || uid == "" {
		t.Fatalf("ByARN misses %q", arn)
	}
	if back, ok := index.ARNForUID(uid); !ok || back != arn {
		t.Errorf("ARNForUID() = %q, %v; want %q, true", back, ok, arn)
	}
	if _, ok := index.ARNForUID("never-indexed"); ok {
		t.Error("ARNForUID(unknown) = true, want false")
	}
	// Anchor collision: the first writer wins.
	if got := index.ByAnchor["vpc-1"]; got != uid {
		t.Errorf("ByAnchor[vpc-1] = %q, want first writer %q", got, uid)
	}

	// An ARN-less resource indexes by bare id only.
	arnLess, _, err := BuildCloudResourceJoinIndex([]facts.Envelope{
		cov8AWSResourceEnvelope("scope-a", map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": "aws_ec2_vpc", "resource_id": "vpc-bare",
		}),
	})
	if err != nil {
		t.Fatalf("Build(arn-less) = %v, want nil", err)
	}
	bareUID, ok := arnLess.ByResourceID["vpc-bare"]
	if !ok {
		t.Fatal("bare-id resource not indexed by resource id")
	}
	if _, ok := arnLess.ARNForUID(bareUID); ok {
		t.Error("ARNForUID(bare-id uid) = true, want false (no ARN scanned)")
	}
}

// TestCov8CloudResourceUID pins the stable node identity: deterministic
// per inputs and distinct across identities.
func TestCov8CloudResourceUID(t *testing.T) {
	t.Parallel()

	a := CloudResourceUID("111122223333", "us-east-1", "aws_ec2_vpc", "vpc-1")
	if a == "" {
		t.Fatal("CloudResourceUID() is empty")
	}
	if b := CloudResourceUID("111122223333", "us-east-1", "aws_ec2_vpc", "vpc-1"); b != a {
		t.Error("CloudResourceUID() is not deterministic")
	}
	for name, other := range map[string]string{
		"account": CloudResourceUID("999988887777", "us-east-1", "aws_ec2_vpc", "vpc-1"),
		"region":  CloudResourceUID("111122223333", "eu-west-1", "aws_ec2_vpc", "vpc-1"),
		"type":    CloudResourceUID("111122223333", "us-east-1", "aws_s3_bucket", "vpc-1"),
		"id":      CloudResourceUID("111122223333", "us-east-1", "aws_ec2_vpc", "vpc-2"),
	} {
		if other == a {
			t.Errorf("CloudResourceUID() collides on %s input", name)
		}
	}
}

// TestCov8ResolveSource pins source resolution order: ARN first, then a
// resource id that may itself be an ARN, then the bare id.
func TestCov8ResolveSource(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:111122223333:vpc/vpc-1"
	index, _, err := BuildCloudResourceJoinIndex([]facts.Envelope{
		cov8AWSResourceEnvelope("scope-a", cov8VPCPayload("vpc-1")),
	})
	if err != nil {
		t.Fatalf("Build() = %v, want nil", err)
	}
	uid := index.ByResourceID["vpc-1"]
	if got, ok := ResolveSource(index, arn, ""); got != uid || !ok {
		t.Errorf("ResolveSource(arn) = %q, %v; want %q, true", got, ok, uid)
	}
	if got, ok := ResolveSource(index, "", arn); got != uid || !ok {
		t.Errorf("ResolveSource(arn-as-id) = %q, %v; want %q, true", got, ok, uid)
	}
	if got, ok := ResolveSource(index, "", "vpc-1"); got != uid || !ok {
		t.Errorf("ResolveSource(bare id) = %q, %v; want %q, true", got, ok, uid)
	}
	if got, ok := ResolveSource(index, "arn:aws:ec2:us-east-1:111122223333:vpc/ghost", "ghost"); got != "" || ok {
		t.Errorf("ResolveSource(unknown) = %q, %v; want empty, false", got, ok)
	}
	if got, ok := ResolveSource(index, "", ""); got != "" || ok {
		t.Errorf("ResolveSource(empty) = %q, %v; want empty, false", got, ok)
	}
}

// TestCov8SplitAWSFactEnvelopes pins the one-pass partition: resources
// and relationships split, every other kind drops.
func TestCov8SplitAWSFactEnvelopes(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		cov8AWSResourceEnvelope("s", cov8VPCPayload("vpc-1")),
		{FactKind: cloud.AWSRelationshipFactKind, ScopeID: "s"},
		{FactKind: "unrelated_kind", ScopeID: "s"},
	}
	resources, relationships := SplitAWSFactEnvelopes(envelopes)
	if len(resources) != 1 || len(relationships) != 1 {
		t.Errorf("split = %d+%d, want 1+1", len(resources), len(relationships))
	}
	resources, relationships = SplitAWSFactEnvelopes(nil)
	if len(resources) != 0 || len(relationships) != 0 {
		t.Errorf("split(nil) = %d+%d, want 0+0", len(resources), len(relationships))
	}
}

// cov8S3Payload returns a valid S3 bucket resource payload.
func cov8S3Payload(mut func(map[string]any)) map[string]any {
	payload := map[string]any{
		"account_id":    "111122223333",
		"region":        "us-east-1",
		"resource_type": awsv1.ResourceTypeS3Bucket,
		"resource_id":   "arn:aws:s3:::logs-bucket",
		"arn":           "arn:aws:s3:::logs-bucket",
		"name":          "logs-bucket",
	}
	if mut != nil {
		mut(payload)
	}
	return payload
}

// TestCov8S3BucketJoinIndex pins bucket-name resolution: the Name field
// wins, then the ARN tail, then the s3:// anchor; non-buckets and
// nameless buckets never index.
func TestCov8S3BucketJoinIndex(t *testing.T) {
	t.Parallel()

	index, quarantined, err := BuildS3BucketJoinIndex([]facts.Envelope{
		cov8AWSResourceEnvelope("s", cov8S3Payload(nil)),
		// Non-S3 resources never join a bucket endpoint.
		cov8AWSResourceEnvelope("s", cov8VPCPayload("vpc-1")),
		{FactKind: cloud.AWSRelationshipFactKind, ScopeID: "s"},
		// Malformed S3 fact quarantines without dropping the index.
		cov8AWSResourceEnvelope("s", map[string]any{
			"resource_type": awsv1.ResourceTypeS3Bucket,
		}),
		// Nameless, ARN-less bucket: no derivable name, skipped.
		cov8AWSResourceEnvelope("s", map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": awsv1.ResourceTypeS3Bucket, "resource_id": "orphan",
		}),
	})
	if err != nil {
		t.Fatalf("Build() = %v, want nil", err)
	}
	if len(quarantined) != 1 {
		t.Errorf("quarantined = %d, want the malformed S3 fact", len(quarantined))
	}
	uid, ok := index.Resolve("logs-bucket")
	if !ok || uid == "" {
		t.Fatal("Resolve(logs-bucket) misses")
	}
	if got, ok := index.Resolve("  logs-bucket  "); !ok || got != uid {
		t.Errorf("Resolve(padded) = %q, %v; want %q, true", got, ok, uid)
	}
	if _, ok := index.Resolve("ghost-bucket"); ok {
		t.Error("Resolve(unknown) = true, want false")
	}

	// ARN-tail fallback when Name is absent.
	arnIndex, _, err := BuildS3BucketJoinIndex([]facts.Envelope{
		cov8AWSResourceEnvelope("s", cov8S3Payload(func(p map[string]any) {
			delete(p, "name")
		})),
	})
	if err != nil {
		t.Fatalf("Build(arn-name) = %v, want nil", err)
	}
	if _, ok := arnIndex.Resolve("logs-bucket"); !ok {
		t.Error("ARN-tail bucket name misses")
	}

	// s3:// anchor fallback when Name and ARN are absent.
	anchorIndex, _, err := BuildS3BucketJoinIndex([]facts.Envelope{
		cov8AWSResourceEnvelope("s", map[string]any{
			"account_id": "111122223333", "region": "us-east-1",
			"resource_type": awsv1.ResourceTypeS3Bucket, "resource_id": "anchor-bucket",
			"correlation_anchors": []any{"s3://anchor-bucket"},
		}),
	})
	if err != nil {
		t.Fatalf("Build(anchor-name) = %v, want nil", err)
	}
	if _, ok := anchorIndex.Resolve("anchor-bucket"); !ok {
		t.Error("anchor-derived bucket name misses")
	}

	// Duplicate facts for one bucket keep the first uid.
	dupIndex, _, err := BuildS3BucketJoinIndex([]facts.Envelope{
		cov8AWSResourceEnvelope("s", cov8S3Payload(nil)),
		cov8AWSResourceEnvelope("s", cov8S3Payload(nil)),
	})
	if err != nil {
		t.Fatalf("Build(dup) = %v, want nil", err)
	}
	if got, ok := dupIndex.Resolve("logs-bucket"); !ok || got != uid {
		t.Errorf("duplicate Resolve() = %q, %v; want first uid %q", got, ok, uid)
	}
}

// TestCov8S3BucketNameFromARN pins ARN-tail parsing across partitions
// and the non-ARN rejections.
func TestCov8S3BucketNameFromARN(t *testing.T) {
	t.Parallel()

	cases := []struct {
		arn  string
		want string
	}{
		{"arn:aws:s3:::logs-bucket", "logs-bucket"},
		{"arn:aws-cn:s3:::logs-bucket", "logs-bucket"},
		{"  arn:aws:s3:::spaced  ", "spaced"},
		{"arn:aws:ec2:us-east-1:111122223333:vpc/vpc-1", ""},
		{"not-an-arn", ""},
		{"", ""},
		{"s3://logs-bucket", ""},
	}
	for _, tc := range cases {
		if got := S3BucketNameFromARN(tc.arn); got != tc.want {
			t.Errorf("S3BucketNameFromARN(%q) = %q, want %q", tc.arn, got, tc.want)
		}
	}
}

// TestCov8S3PostureBucketName pins posture source naming: BucketName
// first, then the BucketArn tail.
func TestCov8S3PostureBucketName(t *testing.T) {
	t.Parallel()

	str := func(s string) *string { return &s }
	if got := S3PostureBucketName(awsv1.S3BucketPosture{
		BucketName: str("explicit"),
		BucketARN:  str("arn:aws:s3:::other"),
	}); got != "explicit" {
		t.Errorf("posture with name = %q, want explicit", got)
	}
	if got := S3PostureBucketName(awsv1.S3BucketPosture{
		BucketARN: str("arn:aws:s3:::from-arn"),
	}); got != "from-arn" {
		t.Errorf("posture with arn = %q, want from-arn", got)
	}
	if got := S3PostureBucketName(awsv1.S3BucketPosture{}); got != "" {
		t.Errorf("empty posture = %q, want empty", got)
	}
}

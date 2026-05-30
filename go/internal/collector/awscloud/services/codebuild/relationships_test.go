package codebuild

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestS3BucketARNFromLocationDerivesPartition pins the GovCloud/China graph-join
// contract: a CodeBuild S3 source/artifact location synthesizes the target
// bucket ARN with the boundary partition (not a hardcoded `aws`), and an already
// partition-qualified ARN location keeps its own partition. The S3 bucket
// scanner publishes its node identity as arn:<partition>:s3:::<bucket>, so a
// hardcoded `aws` dangles the project->S3 edge in aws-us-gov / aws-cn.
func TestS3BucketARNFromLocationDerivesPartition(t *testing.T) {
	cases := []struct {
		name      string
		partition string
		location  string
		want      string
	}{
		{name: "bare commercial", partition: "aws", location: "my-bucket/path/to", want: "arn:aws:s3:::my-bucket"},
		{name: "bare govcloud", partition: "aws-us-gov", location: "my-bucket/path", want: "arn:aws-us-gov:s3:::my-bucket"},
		{name: "bare china", partition: "aws-cn", location: "my-bucket", want: "arn:aws-cn:s3:::my-bucket"},
		// An explicit ARN location keeps its own partition and trims to the bucket.
		{name: "object arn govcloud preserved", partition: "aws", location: "arn:aws-us-gov:s3:::b/key.zip", want: "arn:aws-us-gov:s3:::b"},
		{name: "object arn china preserved", partition: "aws", location: "arn:aws-cn:s3:::b/o/k", want: "arn:aws-cn:s3:::b"},
		{name: "bucket arn commercial preserved", partition: "aws-us-gov", location: "arn:aws:s3:::b", want: "arn:aws:s3:::b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s3BucketARNFromLocation(tc.partition, tc.location); got != tc.want {
				t.Fatalf("s3BucketARNFromLocation(%q, %q) = %q, want %q", tc.partition, tc.location, got, tc.want)
			}
		})
	}
}

// TestSourceRelationshipS3DerivesPartition proves the boundary partition is
// wired through to the synthesized target ARN end to end.
func TestSourceRelationshipS3DerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:s3:::src-bucket"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:s3:::src-bucket"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:s3:::src-bucket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region}
			source := ProjectSource{Type: "S3", Location: "src-bucket/path/spec"}
			obs, ok := sourceRelationship(boundary, source, "arn:project", "project-1")
			if !ok {
				t.Fatalf("expected an S3 source relationship")
			}
			if obs.TargetResourceID != tc.want {
				t.Fatalf("target_resource_id = %q, want %q", obs.TargetResourceID, tc.want)
			}
			if obs.TargetARN != tc.want {
				t.Fatalf("target_arn = %q, want %q", obs.TargetARN, tc.want)
			}
		})
	}
}

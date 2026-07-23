// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ecs

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

const testTaskDigest = "sha256:00000000000000000000000000000000000000000000000000000000000000aa"

// TestScannerEmitsImageReferenceForRunningECRContainer proves the #5451
// emitter: a RUNNING task container with a non-blank ImageDigest whose image
// is hosted in ECR produces a first-class aws_image_reference fact carrying
// the account/region/repository/registry_id/digest/tag the digest-keyed
// container_image_identity resolver needs.
func TestScannerEmitsImageReferenceForRunningECRContainer(t *testing.T) {
	key, err := redact.NewKey([]byte("ecs-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	taskDefinitionARN := "arn:aws:ecs:us-east-1:123456789012:task-definition/api:7"
	client := fakeClient{
		clusters: []Cluster{{
			ARN:  "arn:aws:ecs:us-east-1:123456789012:cluster/prod",
			Name: "prod",
		}},
		tasks: map[string][]Task{
			"arn:aws:ecs:us-east-1:123456789012:cluster/prod": {
				{
					ARN:               "arn:aws:ecs:us-east-1:123456789012:task/prod/task-1",
					ClusterARN:        "arn:aws:ecs:us-east-1:123456789012:cluster/prod",
					TaskDefinitionARN: taskDefinitionARN,
					LastStatus:        "RUNNING",
					DesiredStatus:     "RUNNING",
					LaunchType:        "FARGATE",
					Containers: []TaskContainer{{
						Name:        "api",
						Image:       "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
						ImageDigest: testTaskDigest,
						RuntimeID:   "task-1-runtime-api",
					}},
				},
			},
		},
	}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSImageReferenceFactKind] != 1 {
		t.Fatalf("aws_image_reference count = %d, want 1: %#v", counts[facts.AWSImageReferenceFactKind], envelopes)
	}
	envelope := imageReferenceEnvelope(t, envelopes)
	assertPayloadString(t, envelope, "account_id", "123456789012")
	assertPayloadString(t, envelope, "region", "us-east-1")
	assertPayloadString(t, envelope, "repository_name", "supply-chain-demo")
	assertPayloadString(t, envelope, "registry_id", "123456789012")
	assertPayloadString(t, envelope, "image_digest", testTaskDigest)
	assertPayloadString(t, envelope, "manifest_digest", testTaskDigest)
	assertPayloadString(t, envelope, "tag", "latest")
}

// TestScannerSkipsImageReferenceForBlankDigest proves a running container
// whose digest ECS has not yet resolved produces no aws_image_reference —
// there is no digest to key a reference on.
func TestScannerSkipsImageReferenceForBlankDigest(t *testing.T) {
	key, err := redact.NewKey([]byte("ecs-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	client := fakeClient{
		clusters: []Cluster{{ARN: "arn:aws:ecs:us-east-1:123456789012:cluster/prod", Name: "prod"}},
		tasks: map[string][]Task{
			"arn:aws:ecs:us-east-1:123456789012:cluster/prod": {
				{
					ARN:        "arn:aws:ecs:us-east-1:123456789012:task/prod/task-1",
					ClusterARN: "arn:aws:ecs:us-east-1:123456789012:cluster/prod",
					LastStatus: "RUNNING",
					Containers: []TaskContainer{{
						Name:  "api",
						Image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
						// ImageDigest intentionally blank.
					}},
				},
			},
		},
	}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if got := factKindCounts(envelopes)[facts.AWSImageReferenceFactKind]; got != 0 {
		t.Fatalf("aws_image_reference count = %d, want 0 for blank digest", got)
	}
}

// TestScannerSkipsImageReferenceForNonECRImage proves a running container
// pulling from a non-ECR registry (docker.io here) is skipped: the
// aws_image_reference account/region/repository shape does not fit a
// non-AWS-registry image, so the scanner does not force it (see the ECS
// README "Gotchas / invariants").
func TestScannerSkipsImageReferenceForNonECRImage(t *testing.T) {
	key, err := redact.NewKey([]byte("ecs-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	client := fakeClient{
		clusters: []Cluster{{ARN: "arn:aws:ecs:us-east-1:123456789012:cluster/prod", Name: "prod"}},
		tasks: map[string][]Task{
			"arn:aws:ecs:us-east-1:123456789012:cluster/prod": {
				{
					ARN:        "arn:aws:ecs:us-east-1:123456789012:task/prod/task-1",
					ClusterARN: "arn:aws:ecs:us-east-1:123456789012:cluster/prod",
					LastStatus: "RUNNING",
					Containers: []TaskContainer{{
						Name:        "cache",
						Image:       "docker.io/library/redis:7",
						ImageDigest: testTaskDigest,
					}},
				},
			},
		},
	}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if got := factKindCounts(envelopes)[facts.AWSImageReferenceFactKind]; got != 0 {
		t.Fatalf("aws_image_reference count = %d, want 0 for non-ECR image", got)
	}
}

// TestScannerSkipsImageReferenceForNonRunningTask proves a STOPPED task's
// container digest is not promoted: only the RUNNING task's digest is the
// deployed-code signal aws_image_reference exists to carry.
func TestScannerSkipsImageReferenceForNonRunningTask(t *testing.T) {
	key, err := redact.NewKey([]byte("ecs-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	client := fakeClient{
		clusters: []Cluster{{ARN: "arn:aws:ecs:us-east-1:123456789012:cluster/prod", Name: "prod"}},
		tasks: map[string][]Task{
			"arn:aws:ecs:us-east-1:123456789012:cluster/prod": {
				{
					ARN:        "arn:aws:ecs:us-east-1:123456789012:task/prod/task-2",
					ClusterARN: "arn:aws:ecs:us-east-1:123456789012:cluster/prod",
					LastStatus: "STOPPED",
					Containers: []TaskContainer{{
						Name:        "api",
						Image:       "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
						ImageDigest: testTaskDigest,
					}},
				},
			},
		},
	}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if got := factKindCounts(envelopes)[facts.AWSImageReferenceFactKind]; got != 0 {
		t.Fatalf("aws_image_reference count = %d, want 0 for a non-RUNNING task", got)
	}
}

// TestParseECRImage covers the host-shape parser directly: ECR host forms
// with tag, digest, both, or neither, plus non-ECR hosts that must not match.
func TestParseECRImage(t *testing.T) {
	tests := []struct {
		name               string
		image              string
		wantRegistryID     string
		wantRepositoryName string
		wantTag            string
		wantOK             bool
	}{
		{
			name:               "tag only",
			image:              "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest",
			wantRegistryID:     "123456789012",
			wantRepositoryName: "supply-chain-demo",
			wantTag:            "latest",
			wantOK:             true,
		},
		{
			name:               "digest only",
			image:              "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api@sha256:00aa",
			wantRegistryID:     "123456789012",
			wantRepositoryName: "team/api",
			wantTag:            "",
			wantOK:             true,
		},
		{
			name:               "tag and digest",
			image:              "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:prod@sha256:00aa",
			wantRegistryID:     "123456789012",
			wantRepositoryName: "team/api",
			wantTag:            "prod",
			wantOK:             true,
		},
		{
			name:   "non-ecr host",
			image:  "docker.io/library/redis:7",
			wantOK: false,
		},
		{
			// China-partition ECR hosts are intentionally skipped, not
			// matched: addAWSImageReference (container_image_identity_typed_
			// evidence.go) hardcodes ".amazonaws.com" when reconstructing the
			// registry hostname, so a ".cn" aws_image_reference could never
			// resolve against its OCI registry observation. See the
			// ecrImageHostPattern doc comment and the ECS README "Gotchas /
			// invariants" for the tracked follow-up.
			name:   "china partition host is skipped, not matched",
			image:  "123456789012.dkr.ecr.cn-north-1.amazonaws.com.cn/team/api:prod",
			wantOK: false,
		},
		{
			name:   "no repository path",
			image:  "123456789012.dkr.ecr.us-east-1.amazonaws.com",
			wantOK: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registryID, repositoryName, tag, ok := parseECRImage(test.image)
			if ok != test.wantOK {
				t.Fatalf("parseECRImage(%q) ok = %v, want %v", test.image, ok, test.wantOK)
			}
			if !test.wantOK {
				return
			}
			if test.wantRegistryID != "" && registryID != test.wantRegistryID {
				t.Fatalf("registryID = %q, want %q", registryID, test.wantRegistryID)
			}
			if test.wantRepositoryName != "" && repositoryName != test.wantRepositoryName {
				t.Fatalf("repositoryName = %q, want %q", repositoryName, test.wantRepositoryName)
			}
			if tag != test.wantTag {
				t.Fatalf("tag = %q, want %q", tag, test.wantTag)
			}
		})
	}
}

func imageReferenceEnvelope(t *testing.T, envelopes []facts.Envelope) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSImageReferenceFactKind {
			return envelope
		}
	}
	t.Fatalf("missing aws_image_reference envelope in %#v", envelopes)
	return facts.Envelope{}
}

func assertPayloadString(t *testing.T, envelope facts.Envelope, key string, want string) {
	t.Helper()
	got, _ := envelope.Payload[key].(string)
	if got != want {
		t.Fatalf("payload[%q] = %q, want %q", key, got, want)
	}
}

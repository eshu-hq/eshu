// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ecs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ecrImageHostPattern matches an ECR registry host of the shape
// "<registry-id>.dkr.ecr.<region>.amazonaws.com" — the standard AWS
// partition ONLY. The China partition host
// "<registry-id>.dkr.ecr.<region>.amazonaws.com.cn" deliberately does NOT
// match (see the doc comment on runningContainerImageReferences for why:
// the reducer's registry reconstruction hardcodes ".amazonaws.com" and would
// never resolve a China-partition reference). The registry id (a 12-digit
// AWS account id) is captured so the emitted aws_image_reference can carry it
// even when it differs from the ECS task's own account (a cross-account ECR
// pull within the standard partition).
var ecrImageHostPattern = regexp.MustCompile(`^[0-9]{12}\.dkr\.ecr\.[a-z0-9-]+\.amazonaws\.com$`)

// runningContainerImageReferences emits one aws_image_reference fact per
// running task container whose image is hosted in an ECR registry and whose
// observed ImageDigest is non-blank (#5451).
//
// TaskContainer.ImageDigest is the digest DescribeTasks reports for the
// container actually running right now — the strongest available deployed-code
// signal ECS offers. Before this change that digest was captured only inside
// the task's aws_resource "containers[]" attribute, which the digest-keyed
// container_image_identity resolver never reads (it reads only
// "*_image_reference" fact kinds). Promoting it to a first-class
// aws_image_reference fact lets the existing resolver join a running task
// straight to the repository and commit that built its image.
//
// Only ECR-hosted images in the standard AWS partition are emitted: the image
// host must match ecrImageHostPattern. aws_image_reference models an
// AWS-registry account/region/repository shape, so a non-ECR running image
// (docker.io, ghcr.io, a private registry, ...) does not fit it and is
// intentionally skipped rather than forced into a shape that would
// misrepresent it. See the ECS README "Gotchas / invariants" for this bounded
// gap. A task container with a blank ImageDigest (the digest was not yet
// resolved when ECS reported the task) is also skipped; there is no digest to
// key a reference on.
//
// A China-partition ECR host (".amazonaws.com.cn") is ALSO skipped, even
// though it is a real ECR registry: the reducer's addAWSImageReference
// (container_image_identity_typed_evidence.go) reconstructs the registry
// hostname as "<registry_id>.dkr.ecr.<region>.amazonaws.com" unconditionally
// — it has no partition field, so it can never match a ".cn" OCI registry
// observation. Emitting a China-partition aws_image_reference fact would
// therefore silently never resolve. See the ECS README "Gotchas /
// invariants" for the tracked follow-up (threading the registry partition
// through the fact contract and the reducer).
func runningContainerImageReferences(boundary awscloud.Boundary, task Task) ([]facts.Envelope, error) {
	if strings.TrimSpace(task.LastStatus) != "RUNNING" {
		return nil, nil
	}
	var envelopes []facts.Envelope
	for _, container := range task.Containers {
		digest := strings.TrimSpace(container.ImageDigest)
		if digest == "" {
			continue
		}
		registryID, repositoryName, tag, ok := parseECRImage(container.Image)
		if !ok {
			continue
		}
		envelope, err := awscloud.NewImageReferenceEnvelope(awscloud.ImageReferenceObservation{
			Boundary:       boundary,
			RepositoryName: repositoryName,
			RegistryID:     registryID,
			ImageDigest:    digest,
			Tag:            tag,
		})
		if err != nil {
			return nil, fmt.Errorf("build ECS running-container image reference: %w", err)
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

// parseECRImage splits a running container's image reference into the ECR
// registry id and repository name when the image host matches the ECR
// registry shape (ecrImageHostPattern). ok is false for any image whose host
// does not match — the common case being a non-ECR registry image, which
// runningContainerImageReferences intentionally skips.
//
// The image may carry a tag (repo:tag), a digest (repo@sha256:...), both, or
// neither; a trailing digest suffix is discarded here because the caller
// already has the authoritative running digest from TaskContainer.ImageDigest.
// The returned tag is empty when the image carries none.
func parseECRImage(image string) (registryID, repositoryName, tag string, ok bool) {
	host, rest, found := strings.Cut(strings.TrimSpace(image), "/")
	if !found {
		return "", "", "", false
	}
	if !ecrImageHostPattern.MatchString(host) {
		return "", "", "", false
	}
	// ecrImageHostPattern already anchors the host to exactly 12 leading
	// digits followed by ".dkr.ecr...."; the match guarantees this slice is
	// the registry id, so no separate Index lookup (and its -1 case) is
	// needed here.
	const registryIDLength = 12
	registryID = host[:registryIDLength]
	if at := strings.Index(rest, "@"); at >= 0 {
		rest = rest[:at]
	}
	repositoryName = rest
	if colon := strings.LastIndex(repositoryName, ":"); colon >= 0 {
		tag = repositoryName[colon+1:]
		repositoryName = repositoryName[:colon]
	}
	if repositoryName == "" {
		return "", "", "", false
	}
	return registryID, repositoryName, tag, true
}

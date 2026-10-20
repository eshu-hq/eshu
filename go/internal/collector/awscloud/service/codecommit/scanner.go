// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codecommit

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS CodeCommit repository metadata facts for one claimed
// account and region. It is metadata-only: it never reads commits, refs,
// blobs, file contents, pull-request bodies, or comment text, and never
// mutates any CodeCommit resource. The repository resource is a code-to-cloud
// correlation anchor: it publishes the repository name and clone URLs as
// correlation anchors so a CodeBuild project, CodePipeline source action, or
// Amplify app whose Git source points at the repository joins it.
type Scanner struct {
	// Client is the metadata-only CodeCommit read surface.
	Client Client
}

// Scan observes CodeCommit repositories, their encryption key, and their
// trigger destinations through the configured client. It returns one
// aws_resource fact per repository plus aws_relationship facts for the
// repository-to-KMS-key and repository-to-SNS-topic edges CodeCommit reports.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("codecommit scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCodeCommit:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceCodeCommit
	default:
		return nil, fmt.Errorf("codecommit scanner received service_kind %q", boundary.ServiceKind)
	}

	repositories, err := s.Client.ListRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodeCommit repositories: %w", err)
	}
	var envelopes []facts.Envelope
	for _, repository := range repositories {
		next, err := repositoryEnvelopes(boundary, repository)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

// repositoryEnvelopes builds the repository resource fact plus its KMS-key and
// SNS-topic relationship facts.
func repositoryEnvelopes(boundary awscloud.Boundary, repository Repository) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(repositoryObservation(boundary, repository))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	if relationship := kmsKeyRelationship(boundary, repository); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, relationship := range triggerRelationships(boundary, repository) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

// repositoryObservation maps a CodeCommit repository into its aws_resource
// observation. The emitted clone-URL evidence is host-only so credentials,
// paths, or userinfo a clone URL string could carry never persist as
// attributes, while the full clone URLs are published as correlation anchors so
// CI Git-source edges keyed by clone URL join this repository.
func repositoryObservation(boundary awscloud.Boundary, repository Repository) awscloud.ResourceObservation {
	repositoryARN := strings.TrimSpace(repository.ARN)
	repositoryName := strings.TrimSpace(repository.Name)
	cloneURLHTTP := strings.TrimSpace(repository.CloneURLHTTP)
	cloneURLSSH := strings.TrimSpace(repository.CloneURLSSH)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          repositoryARN,
		ResourceID:   firstNonEmpty(repositoryARN, repositoryName),
		ResourceType: awscloud.ResourceTypeCodeCommitRepository,
		Name:         repositoryName,
		Tags:         cloneStringMap(repository.Tags),
		Attributes: map[string]any{
			"repository_id":       strings.TrimSpace(repository.ID),
			"account_id":          strings.TrimSpace(repository.AccountID),
			"default_branch":      strings.TrimSpace(repository.DefaultBranch),
			"clone_url_http_host": cloneURLHost(cloneURLHTTP),
			"clone_url_ssh_host":  cloneURLHost(cloneURLSSH),
			"kms_key_id":          strings.TrimSpace(repository.KMSKeyID),
			"trigger_count":       len(repository.Triggers),
			"created_at":          timeOrNil(repository.CreatedAt),
			"last_modified_at":    timeOrNil(repository.LastModifiedAt),
		},
		CorrelationAnchors: repositoryAnchors(repositoryARN, repositoryName, cloneURLHTTP, cloneURLSSH),
		SourceRecordID:     firstNonEmpty(repositoryARN, repositoryName),
	}
}

// repositoryAnchors builds the correlation-anchor set that makes a CodeCommit
// repository a code-to-cloud join target. The repository name and full clone
// URLs are the anchors a CodeBuild project (git source location), CodePipeline
// source action, or Amplify app reports, so publishing them here lets those
// edges resolve to this repository node.
func repositoryAnchors(repositoryARN, repositoryName, cloneURLHTTP, cloneURLSSH string) []string {
	anchors := make([]string, 0, 4)
	for _, value := range []string{repositoryARN, repositoryName, cloneURLHTTP, cloneURLSSH} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			anchors = append(anchors, trimmed)
		}
	}
	return anchors
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "time"

// ImageReference is the schema-version-1 typed payload for
// "aws_image_reference".
type ImageReference struct {
	AccountID           string     `json:"account_id"`
	Region              string     `json:"region"`
	ServiceKind         *string    `json:"service_kind,omitempty"`
	CollectorInstanceID *string    `json:"collector_instance_id,omitempty"`
	RepositoryARN       *string    `json:"repository_arn,omitempty"`
	RepositoryName      string     `json:"repository_name"`
	RegistryID          *string    `json:"registry_id,omitempty"`
	ImageDigest         string     `json:"image_digest"`
	ManifestDigest      string     `json:"manifest_digest"`
	Tag                 *string    `json:"tag,omitempty"`
	PushedAt            *time.Time `json:"pushed_at,omitempty"`
	ImageSizeInBytes    *int64     `json:"image_size_in_bytes,omitempty"`
	ManifestMediaType   *string    `json:"manifest_media_type,omitempty"`
	ArtifactMediaType   *string    `json:"artifact_media_type,omitempty"`
	CorrelationAnchors  []string   `json:"correlation_anchors,omitempty"`
}

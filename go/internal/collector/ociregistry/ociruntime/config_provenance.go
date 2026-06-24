// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"context"
	"encoding/json"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const maxOCIImageConfigBlobBytes int64 = 1 << 20

func (s *Source) configProvenanceLabels(
	ctx context.Context,
	client RegistryClient,
	target TargetConfig,
	collectorInstanceID string,
	generationID string,
	observedAt time.Time,
	config ociregistry.Descriptor,
) (map[string]string, []facts.Envelope, error) {
	if config.Digest == "" {
		return nil, nil, nil
	}
	if config.SizeBytes > maxOCIImageConfigBlobBytes {
		warning, err := s.warningEnvelope(
			target,
			collectorInstanceID,
			generationID,
			observedAt,
			ociregistry.WarningConfigBlobOversized,
			"image config blob exceeds bounded provenance label read limit",
			config.Digest,
		)
		return nil, []facts.Envelope{warning}, err
	}

	var blobLabels map[string]string
	var blobSize int64
	err := s.recordAPICall(ctx, target, "get_blob", func(context.Context) error {
		blob, err := client.GetBlob(ctx, target.Repository, config.Digest)
		if err != nil {
			return err
		}
		blobSize = int64(len(blob.Body))
		if blobSize > maxOCIImageConfigBlobBytes {
			return nil
		}
		blobLabels = imageConfigLabels(blob.Body)
		return nil
	})
	if err != nil {
		warning, warningErr := s.warningEnvelope(
			target,
			collectorInstanceID,
			generationID,
			observedAt,
			ociregistry.WarningConfigBlobUnavailable,
			"image config blob unavailable for provenance label read",
			config.Digest,
		)
		return nil, []facts.Envelope{warning}, warningErr
	}
	if blobSize > maxOCIImageConfigBlobBytes {
		warning, warningErr := s.warningEnvelope(
			target,
			collectorInstanceID,
			generationID,
			observedAt,
			ociregistry.WarningConfigBlobOversized,
			"image config blob exceeds bounded provenance label read limit",
			config.Digest,
		)
		return nil, []facts.Envelope{warning}, warningErr
	}
	return blobLabels, nil, nil
}

func imageConfigLabels(body []byte) map[string]string {
	var decoded struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil
	}
	if len(decoded.Config.Labels) == 0 {
		return nil
	}
	return decoded.Config.Labels
}

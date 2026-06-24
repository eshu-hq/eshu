// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func (g *Generation) imageReferenceEnvelopes(obs ResourceObservation) ([]facts.Envelope, error) {
	if g.key.IsZero() || imageReferenceObservationCount(obs.ImageReferences) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, imageReferenceObservationCount(obs.ImageReferences))
	for _, image := range obs.ImageReferences {
		image = imageReferenceWithResourceDefaults(image, obs)
		if !hasUsableImageReferenceObservation(image) {
			continue
		}
		image.Boundary = g.boundary
		image.SourceRecordID = imageSourceRecordID(
			obs.SourceRecordID,
			image.ImageReference,
			image.ImageDigest,
			image.ContainerName,
		)
		image.SourceURI = obs.SourceURI
		env, err := NewImageReferenceEnvelope(image, g.key)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, env)
	}
	sort.Slice(envelopes, func(i, j int) bool {
		return envelopes[i].StableFactKey < envelopes[j].StableFactKey
	})
	return envelopes, nil
}

func imageReferenceObservationCount(images []ImageReferenceObservation) int {
	count := 0
	for _, image := range images {
		if hasUsableImageReferenceObservation(image) {
			count++
		}
	}
	return count
}

func hasUsableImageReferenceObservation(image ImageReferenceObservation) bool {
	return strings.TrimSpace(image.ImageReference) != "" || strings.TrimSpace(image.ImageDigest) != ""
}

func imageReferenceWithResourceDefaults(
	image ImageReferenceObservation,
	obs ResourceObservation,
) ImageReferenceObservation {
	if strings.TrimSpace(image.OwningFullResourceName) == "" {
		image.OwningFullResourceName = obs.Name
	}
	if image.UpdateTime.IsZero() {
		image.UpdateTime = obs.UpdateTime
	}
	return image
}

func imageSourceRecordID(sourceRecordID, imageReference, imageDigest, containerName string) string {
	sourceRecordID = strings.TrimSpace(sourceRecordID)
	imageReference = strings.TrimSpace(imageReference)
	imageDigest = strings.TrimSpace(imageDigest)
	containerName = strings.TrimSpace(containerName)
	if imageReference == "" && imageDigest == "" {
		return sourceRecordID
	}
	imageID := facts.StableID("GCPImageReferenceSourceRecord", map[string]any{
		"container_name":  containerName,
		"image_digest":    imageDigest,
		"image_reference": imageReference,
	})
	if sourceRecordID == "" {
		return "image|" + imageID
	}
	return sourceRecordID + "|image|" + imageID
}

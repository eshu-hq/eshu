// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociregistry

import (
	"strings"

	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

func typedDescriptors(descriptors []Descriptor) []ociregistryv1.Descriptor {
	if len(descriptors) == 0 {
		return nil
	}
	mapped := make([]ociregistryv1.Descriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		mapped = append(mapped, typedDescriptor(descriptor))
	}
	return mapped
}

func typedDescriptorPtr(descriptor Descriptor) *ociregistryv1.Descriptor {
	typed := typedDescriptor(descriptor)
	return &typed
}

func typedDescriptor(descriptor Descriptor) ociregistryv1.Descriptor {
	digest, _ := normalizeDigest(descriptor.Digest)
	return ociregistryv1.Descriptor{
		Digest:       stringPtr(digest),
		MediaType:    stringPtr(strings.TrimSpace(descriptor.MediaType)),
		SizeBytes:    int64Ptr(descriptor.SizeBytes),
		ArtifactType: stringPtr(strings.TrimSpace(descriptor.ArtifactType)),
		Annotations:  redactedAnnotations(descriptor.Annotations),
		Platform:     platformMap(descriptor.Platform),
	}
}

func mergeContractPayload(payload map[string]any, encode func() (map[string]any, error)) error {
	encoded, err := encode()
	if err != nil {
		return err
	}
	for key, value := range encoded {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	return nil
}

func stringPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

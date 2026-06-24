// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func extractOCIConfigProvenanceRefs(envelopes []facts.Envelope) []containerImageRefEvidence {
	repositories := extractPackageSourceRepositories(envelopes)
	if len(repositories) == 0 {
		return nil
	}
	refs := make([]containerImageRefEvidence, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.OCIImageManifestFactKind {
			continue
		}
		labels := configLabelMap(envelope.Payload["config_labels"])
		sourceURL, ok := singleOCIConfigSourceLabel(labels)
		if !ok {
			continue
		}
		match, ok := matchOCIConfigSourceRepository(sourceURL, repositories)
		if !ok {
			continue
		}
		digest := payloadStr(envelope.Payload, "digest")
		repositoryID := ociRepositoryID(envelope.Payload)
		imageRef := imageRefFromOCIRepositoryID(repositoryID, digest)
		parsed, parsedOK := parseContainerImageRef(imageRef)
		if !parsedOK {
			continue
		}
		refs = append(refs, containerImageRefEvidence{
			imageRef:            imageRef,
			parsed:              parsed,
			resolvedDigest:      digest,
			sourceRepositoryIDs: []string{match.RepositoryID},
			sourceRevision:      normalizeOCIConfigRevision(labels),
			sourceLabelEvidence: true,
			factIDs:             []string{envelope.FactID},
		})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].imageRef < refs[j].imageRef
	})
	return refs
}

func configLabelMap(raw any) map[string]string {
	switch typed := raw.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, value := range typed {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				out[trimmed] = strings.TrimSpace(strings.Trim(fmt.Sprint(value), `"`))
			}
		}
		return out
	default:
		return nil
	}
}

func singleOCIConfigSourceLabel(labels map[string]string) (string, bool) {
	var sourceValues []string
	for _, key := range []string{
		"org.opencontainers.image.source",
		"org.label-schema.vcs-url",
	} {
		if value := strings.TrimSpace(labels[key]); value != "" && value != "[redacted]" {
			sourceValues = append(sourceValues, value)
		}
	}
	if len(sourceValues) == 0 {
		return "", false
	}
	keys := make(map[string]string, len(sourceValues))
	for _, value := range sourceValues {
		key := canonicalPackageSourceURLKey(value)
		if key == "" {
			return "", false
		}
		keys[key] = value
	}
	if len(keys) != 1 {
		return "", false
	}
	for _, value := range keys {
		return value, true
	}
	return "", false
}

func matchOCIConfigSourceRepository(
	sourceURL string,
	repositories []packageSourceRepository,
) (packageSourceRepository, bool) {
	hint := packageSourceHint{
		PackageID: "container-image",
		HintKind:  "repository",
		SourceURL: sourceURL,
	}
	active, _ := matchPackageSourceRepositories(hint, repositories)
	if len(active) != 1 {
		return packageSourceRepository{}, false
	}
	return active[0], true
}

func normalizeOCIConfigRevision(labels map[string]string) string {
	for _, key := range []string{
		"org.opencontainers.image.revision",
		"org.label-schema.vcs-ref",
	} {
		if revision := strings.TrimSpace(labels[key]); revision != "" && revision != "[redacted]" {
			return strings.ToLower(revision)
		}
	}
	return ""
}

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
			// An OCI config source label is build evidence: the image itself
			// declares the repository it was built from, which is what lets
			// DERIVED_FROM treat it as that repository's child (#5460).
			buildProvenanceRepositoryIDs: []string{match.RepositoryID},
			sourceRevision:               normalizeOCIConfigRevision(labels),
			sourceLabelEvidence:          true,
			factIDs:                      []string{envelope.FactID},
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

// matchOCIConfigSourceRepository resolves an OCI config source label to the one
// repository it names, or reports false when the label is ambiguous.
//
// Ambiguity means two DIFFERENT repositories claim the same remote URL, not the
// same repository observed more than once. matchPackageSourceRepositories returns
// one entry per matching repository FACT, and a repository legitimately carries
// several active facts (more than one scope or collector observing the same
// repo), so counting raw matches rejected a perfectly unambiguous label as soon
// as a second fact for that repository existed. That silently disabled this
// build-evidence tier -- and with it base-image lineage's child attribution
// (#5460) -- in any environment with duplicate repository facts, which is every
// real corpus. Dedupe by repository identity before applying the exactly-one
// rule.
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
	distinct := make(map[string]packageSourceRepository, len(active))
	for _, repository := range active {
		distinct[repository.RepositoryID] = repository
	}
	if len(distinct) != 1 {
		return packageSourceRepository{}, false
	}
	for _, repository := range distinct {
		return repository, true
	}
	return packageSourceRepository{}, false
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

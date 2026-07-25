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

// matchOCIConfigSourceRepositoryByDistinctRepository resolves an OCI config
// source label to the one REPOSITORY it names, deduping multiple facts for the
// same repository first.
//
// matchOCIConfigSourceRepository above counts raw repository FACT matches, and a
// repository legitimately carries several active `repository` facts (more than
// one scope or collector observing it), so a second fact makes an unambiguous
// label look ambiguous. That keeps the identity tier this feeds
// (oci_config_source_label_with_digest) effectively dead in a real corpus -- a
// pre-existing defect tracked in #5801, deliberately NOT repaired here: widening
// it changes SourceRepositoryIDs for every image with a label, which perturbs
// downstream consumers such as supply-chain-impact repository anchoring.
//
// Base-image lineage needs only the build-provenance answer, so it resolves the
// label through this deduping variant and contributes nothing to
// SourceRepositoryIDs. Genuine ambiguity -- two DISTINCT repository ids claiming
// the same remote -- still resolves to neither.
func matchOCIConfigSourceRepositoryByDistinctRepository(
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

// extractOCIConfigBuildProvenanceRefs yields build-provenance-only evidence: for
// each OCI manifest whose config source label resolves to exactly one
// repository, the fact that repository BUILT this digest. It deliberately sets
// no sourceRepositoryIDs, sourceRevision, or sourceLabelEvidence, so activating
// this read cannot shift any existing identity tier or downstream repository
// anchor -- it only feeds the DERIVED_FROM child gate (#5460).
func extractOCIConfigBuildProvenanceRefs(envelopes []facts.Envelope) []containerImageRefEvidence {
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
		match, ok := matchOCIConfigSourceRepositoryByDistinctRepository(sourceURL, repositories)
		if !ok {
			continue
		}
		digest := payloadStr(envelope.Payload, "digest")
		imageRef := imageRefFromOCIRepositoryID(ociRepositoryID(envelope.Payload), digest)
		parsed, parsedOK := parseContainerImageRef(imageRef)
		if !parsedOK {
			continue
		}
		refs = append(refs, containerImageRefEvidence{
			imageRef:                     imageRef,
			parsed:                       parsed,
			buildProvenanceRepositoryIDs: []string{match.RepositoryID},
		})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].imageRef < refs[j].imageRef
	})
	return refs
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
